package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/pkg/app"
	scanpkg "github.com/chainreactors/aiscan/pkg/tools/scan"
)

type LLMConfigStore interface {
	GetLLMConfig(ctx context.Context) (LLMConfig, error)
	SaveLLMConfig(ctx context.Context, cfg LLMConfig) (LLMConfig, error)
}

type ServiceConfig struct {
	Store         Store
	App           *app.App
	ConfigStore   LLMConfigStore
	AppFactory    func(ctx context.Context) (*app.App, error)
	MaxConcurrent int
	ScanTimeout   time.Duration
}

type Service struct {
	store   Store
	appMu   sync.RWMutex
	app     *app.App
	config  LLMConfigStore
	reload  func(ctx context.Context) (*app.App, error)
	hub     *Hub
	sem     chan struct{}
	timeout time.Duration

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewService(cfg ServiceConfig) *Service {
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	timeout := cfg.ScanTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &Service{
		store:   cfg.Store,
		app:     cfg.App,
		config:  cfg.ConfigStore,
		reload:  cfg.AppFactory,
		hub:     NewHub(),
		sem:     make(chan struct{}, maxConcurrent),
		timeout: timeout,
		cancels: make(map[string]context.CancelFunc),
	}
}

func (s *Service) Hub() *Hub { return s.hub }

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.appMu.Lock()
	app := s.app
	s.app = nil
	s.appMu.Unlock()
	if app != nil {
		app.Close()
	}
}

func (s *Service) Status() ServiceStatus {
	app := s.appSnapshot()
	status := ServiceStatus{
		LLMAvailable: app != nil && app.Provider != nil,
	}
	if app != nil {
		status.LLMProvider = app.ProviderConfig.Provider
		status.LLMModel = app.ProviderConfig.Model
		status.LLMAPIKeyConfigured = strings.TrimSpace(app.ProviderConfig.APIKey) != ""
	}
	if s.config != nil {
		if cfg, err := s.config.GetLLMConfig(context.Background()); err == nil {
			status.ConfigPath = cfg.ConfigPath
			status.ConfigLoaded = cfg.ConfigLoaded
			if status.LLMProvider == "" {
				status.LLMProvider = cfg.Provider
			}
			if status.LLMModel == "" {
				status.LLMModel = cfg.Model
			}
			status.LLMAPIKeyConfigured = status.LLMAPIKeyConfigured || cfg.APIKeyConfigured
		}
	}
	return status
}

func (s *Service) GetLLMConfig(ctx context.Context) (LLMConfig, error) {
	if s.config == nil {
		return LLMConfig{}, fmt.Errorf("LLM config store is not configured")
	}
	cfg, err := s.config.GetLLMConfig(ctx)
	if err != nil {
		return LLMConfig{}, err
	}
	cfg.APIKey = ""
	return cfg, nil
}

func (s *Service) SaveLLMConfig(ctx context.Context, cfg LLMConfig) (LLMConfig, error) {
	if s.config == nil {
		return LLMConfig{}, fmt.Errorf("LLM config store is not configured")
	}
	saved, err := s.config.SaveLLMConfig(ctx, cfg)
	if err != nil {
		return LLMConfig{}, err
	}
	if s.reload != nil {
		app, err := s.reload(ctx)
		if err != nil {
			return saved, fmt.Errorf("reload aiscan runtime: %w", err)
		}
		s.swapApp(app)
	}
	saved.APIKey = ""
	return saved, nil
}

func (s *Service) SubmitScan(ctx context.Context, target, mode string, verify, sniper, deep bool) (*ScanJob, error) {
	target, err := ValidateTarget(target)
	if err != nil {
		return nil, err
	}
	mode, err = ValidateMode(mode)
	if err != nil {
		return nil, err
	}
	if (verify || sniper || deep) && !s.aiAvailable() {
		return nil, fmt.Errorf("selected analysis options require an LLM provider")
	}

	now := time.Now()
	job := &ScanJob{
		ID:        generateID(),
		Target:    target,
		Mode:      mode,
		Verify:    verify,
		Sniper:    sniper,
		AI:        verify || sniper,
		Deep:      deep,
		Status:    StatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("store create: %w", err)
	}

	go s.runScan(job.ID)

	return job, nil
}

func (s *Service) GetScan(ctx context.Context, id string) (*ScanJob, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) ListScans(ctx context.Context) ([]*ScanJob, error) {
	return s.store.List(ctx, 100)
}

func (s *Service) CancelScan(id string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[id]
	s.mu.Unlock()
	if ok {
		cancel()
	}
	ctx := context.Background()
	job, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Status == StatusRunning || job.Status == StatusQueued {
		job.Status = StatusCancelled
		job.UpdatedAt = time.Now()
		return s.store.Update(ctx, job)
	}
	return nil
}

func (s *Service) GetReport(ctx context.Context, id string) (string, error) {
	job, err := s.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return job.Report, nil
}

func (s *Service) runScan(jobID string) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	s.mu.Lock()
	s.cancels[jobID] = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.cancels, jobID)
		s.mu.Unlock()
	}()

	job, err := s.store.Get(ctx, jobID)
	if err != nil {
		return
	}
	if job.Status == StatusCancelled {
		return
	}

	job.Status = StatusRunning
	job.UpdatedAt = time.Now()
	s.store.Update(ctx, job)

	s.hub.Broadcast(jobID, ScanEvent{
		Type:   "status",
		ScanID: jobID,
		Status: string(StatusRunning),
	})

	streamWriter := &sseStreamWriter{
		hub:     s.hub,
		scanID:  jobID,
		store:   s.store,
		job:     job,
		ctx:     ctx,
		partial: newPartialStructuredBuilder(job.Target, job.CreatedAt),
	}

	// Run scan with streaming real-time progress.
	// --report is NOT passed because it disables streaming output.
	args := scanArgsForJob(job)
	output, result, err := s.executeScan(ctx, args, streamWriter)
	if err != nil {
		job.Status = StatusFailed
		job.Error = err.Error()
		job.UpdatedAt = time.Now()
		s.store.Update(ctx, job)
		s.hub.Broadcast(jobID, ScanEvent{
			Type:   "error",
			ScanID: jobID,
			Error:  err.Error(),
		})
		return
	}

	// Build a markdown report from the streamed lines and terminal output.
	report := buildMarkdownReport(job.Target, job.Mode, streamWriter.lines, output)

	job.Status = StatusCompleted
	job.Report = report
	job.Result = result
	job.UpdatedAt = time.Now()
	s.store.Update(ctx, job)

	s.hub.Broadcast(jobID, ScanEvent{
		Type:   "complete",
		ScanID: jobID,
		Status: string(StatusCompleted),
		Result: result,
	})
}

func (s *Service) aiAvailable() bool {
	app := s.appSnapshot()
	return app != nil && app.Provider != nil
}

func (s *Service) appSnapshot() *app.App {
	if s == nil {
		return nil
	}
	s.appMu.RLock()
	defer s.appMu.RUnlock()
	return s.app
}

func (s *Service) swapApp(next *app.App) {
	if s == nil || next == nil {
		return
	}
	s.appMu.Lock()
	prev := s.app
	s.app = next
	s.appMu.Unlock()
	if prev != nil && prev != next {
		prev.Close()
	}
}

func scanArgsForJob(job *ScanJob) []string {
	args := []string{"-i", job.Target, "--mode", job.Mode}
	if job.Verify {
		args = append(args, "--verify=high")
	}
	if job.Sniper {
		args = append(args, "--sniper")
	}
	if job.Deep {
		args = append(args, "--deep")
	}
	return args
}

type structuredScanCommand interface {
	ExecuteStructured(ctx context.Context, args []string, stream io.Writer) (string, *scanpkg.StructuredResult, error)
}

func (s *Service) executeScan(ctx context.Context, args []string, stream io.Writer) (string, *scanpkg.StructuredResult, error) {
	app := s.appSnapshot()
	if app == nil || app.Commands == nil {
		return "", nil, fmt.Errorf("aiscan runtime is not ready")
	}
	cmd, ok := app.Commands.Get("scan")
	if !ok {
		return "", nil, fmt.Errorf("scan command is not registered")
	}
	structured, ok := cmd.(structuredScanCommand)
	if !ok {
		return "", nil, fmt.Errorf("scan command does not support structured results")
	}
	return structured.ExecuteStructured(ctx, args, stream)
}

type sseStreamWriter struct {
	hub     *Hub
	scanID  string
	store   Store
	job     *ScanJob
	ctx     context.Context
	partial *partialStructuredBuilder
	buf     []byte
	lines   []string
}

func (w *sseStreamWriter) Write(p []byte) (int, error) {
	if w.ctx != nil {
		select {
		case <-w.ctx.Done():
			return 0, w.ctx.Err()
		default:
		}
	}
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]

		line = stripANSI(line)
		if line == "" {
			continue
		}

		w.lines = append(w.lines, line)
		if w.partial != nil {
			w.partial.ObserveLine(line)
		}
		fmt.Fprintf(os.Stderr, "[scan:%s] %s\n", w.scanID, line)

		current, err := w.store.Get(context.Background(), w.scanID)
		if err != nil {
			return 0, err
		}
		if current.Status == StatusCancelled {
			return 0, context.Canceled
		}
		current.Progress = line
		current.UpdatedAt = time.Now()
		if w.partial != nil {
			current.Result = w.partial.Result(current.UpdatedAt)
		}
		if err := w.store.Update(context.Background(), current); err != nil {
			return 0, err
		}
		w.job = current

		w.hub.Broadcast(w.scanID, ScanEvent{
			Type:   "progress",
			ScanID: w.scanID,
			Data:   line,
			Result: current.Result,
		})
	}
	return len(p), nil
}

func buildMarkdownReport(target, mode string, lines []string, terminalOutput string) string {
	var sb strings.Builder
	sb.WriteString("# Penetration Test Report\n\n")
	sb.WriteString(fmt.Sprintf("**Target:** `%s`  \n", target))
	sb.WriteString(fmt.Sprintf("**Mode:** %s  \n", mode))
	sb.WriteString(fmt.Sprintf("**Date:** %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString("---\n\n")

	var services, web, fingerprints, risks, vulns, ai, summary []string

	for _, line := range lines {
		l := strings.ToLower(line)
		switch {
		case strings.Contains(l, "[service"):
			services = append(services, line)
		case strings.Contains(l, "[web"):
			web = append(web, line)
		case strings.Contains(l, "[fingerprint"):
			fingerprints = append(fingerprints, line)
		case strings.Contains(l, "[risk") || strings.Contains(l, "weakpass"):
			risks = append(risks, line)
		case strings.Contains(l, "[vuln"):
			vulns = append(vulns, line)
		case strings.Contains(l, "[ai") || strings.Contains(l, "[sniper") || strings.Contains(l, "[deep") || strings.Contains(l, "verified"):
			ai = append(ai, line)
		case strings.Contains(l, "[summary") || strings.Contains(l, "completed"):
			summary = append(summary, line)
		}
	}

	writeSection := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		sb.WriteString(fmt.Sprintf("## %s\n\n", title))
		for _, item := range items {
			sb.WriteString(fmt.Sprintf("- `%s`\n", item))
		}
		sb.WriteString("\n")
	}

	writeSection("Open Services", services)
	writeSection("Web Endpoints", web)
	writeSection("Fingerprints", fingerprints)
	writeSection("Risks (Weak Credentials)", risks)
	writeSection("Vulnerabilities", vulns)
	writeSection("Analysis", ai)

	if len(summary) > 0 {
		sb.WriteString("## Summary\n\n")
		for _, line := range summary {
			sb.WriteString(line + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("---\n\n")
	sb.WriteString("## Raw Output\n\n")
	sb.WriteString("```\n")
	sb.WriteString(stripANSI(terminalOutput))
	sb.WriteString("\n```\n")

	return sb.String()
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func stripANSI(s string) string {
	var out []byte
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !((s[j] >= 'A' && s[j] <= 'Z') || (s[j] >= 'a' && s[j] <= 'z')) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}

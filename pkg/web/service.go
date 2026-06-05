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
	"github.com/chainreactors/aiscan/pkg/output"
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
		hub:    s.hub,
		scanID: jobID,
		store:  s.store,
		job:    job,
		ctx:    ctx,
	}

	// Run scan with streaming real-time progress.
	// --report is NOT passed because it disables streaming output.
	args := scanArgsForJob(job)
	_, result, err := s.executeScan(ctx, args, streamWriter)
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

	report := buildMarkdownReport(job.Target, job.Mode, result)

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
	ExecuteStructured(ctx context.Context, args []string, stream io.Writer) (string, *output.Result, error)
}

func (s *Service) executeScan(ctx context.Context, args []string, stream io.Writer) (string, *output.Result, error) {
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
	hub    *Hub
	scanID string
	store  Store
	job    *ScanJob
	ctx    context.Context
	buf    []byte
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
		if err := w.store.Update(context.Background(), current); err != nil {
			return 0, err
		}
		w.job = current

		w.hub.Broadcast(w.scanID, ScanEvent{
			Type:   "progress",
			ScanID: w.scanID,
			Data:   line,
		})
	}
	return len(p), nil
}

func buildMarkdownReport(target, mode string, result *output.Result) string {
	var sb strings.Builder
	sb.WriteString("# Penetration Test Report\n\n")
	sb.WriteString(fmt.Sprintf("**Target:** `%s`  \n", target))
	sb.WriteString(fmt.Sprintf("**Mode:** %s  \n", mode))
	sb.WriteString(fmt.Sprintf("**Date:** %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString("---\n\n")

	if result == nil {
		sb.WriteString("No structured result was returned.\n")
		return sb.String()
	}

	sb.WriteString("## Summary\n\n")
	sb.WriteString("| Metric | Value |\n|---|---:|\n")
	sb.WriteString(fmt.Sprintf("| Targets | %d |\n", result.Summary.Targets))
	sb.WriteString(fmt.Sprintf("| Services | %d |\n", result.Summary.Services))
	sb.WriteString(fmt.Sprintf("| Web | %d |\n", result.Summary.Webs))
	sb.WriteString(fmt.Sprintf("| Probes | %d |\n", result.Summary.Probes))
	sb.WriteString(fmt.Sprintf("| Fingerprints | %d |\n", resultFingerprintCount(result)))
	sb.WriteString(fmt.Sprintf("| Findings | %d |\n", result.Summary.Risks+result.Summary.Vulns))
	sb.WriteString(fmt.Sprintf("| AI | %d |\n", len(result.AI)))
	sb.WriteString(fmt.Sprintf("| Errors | %d |\n", result.Summary.Errors))
	if result.Summary.Duration != "" {
		sb.WriteString(fmt.Sprintf("| Duration | %s |\n", result.Summary.Duration))
	}
	sb.WriteString("\n")

	if len(result.Assets) == 0 {
		return sb.String()
	}

	sb.WriteString("## Assets\n\n")
	for _, asset := range result.Assets {
		title := output.FirstNonEmpty(asset.Title, asset.Target, asset.Key, "Asset")
		sb.WriteString(fmt.Sprintf("### %s\n\n", title))
		if asset.Target != "" && asset.Target != title {
			sb.WriteString(fmt.Sprintf("- **Target:** %s\n", markdownCode(asset.Target)))
		}
		if asset.Status != "" {
			sb.WriteString(fmt.Sprintf("- **State:** %s\n", markdownCode(asset.Status)))
		}
		writeMarkdownList(&sb, "Services", assetServiceFacts(asset.Items))
		writeMarkdownList(&sb, "HTTP", assetHTTPStatuses(asset.Items))
		writeMarkdownList(&sb, "Fingers", assetFingers(asset.Items))
		writeMarkdownList(&sb, "Sources", assetSources(asset.Items))
		if paths := assetPathCount(asset.Items); paths > 0 {
			sb.WriteString(fmt.Sprintf("- **Paths:** %d\n", paths))
		}
		writeAssetFindingsMarkdown(&sb, asset.Items)
		sb.WriteString("\n")
	}

	return sb.String()
}

func writeMarkdownList(sb *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		return
	}
	coded := make([]string, 0, len(values))
	for _, value := range values {
		coded = append(coded, markdownCode(value))
	}
	sb.WriteString(fmt.Sprintf("- **%s:** %s\n", label, strings.Join(coded, ", ")))
}

func writeAssetFindingsMarkdown(sb *strings.Builder, items []output.AssetItem) {
	var lines []string
	for _, item := range items {
		switch item.Kind {
		case output.AssetItemFinding, output.AssetItemNote, output.AssetItemResponse, output.AssetItemError:
			text := output.FirstNonEmpty(item.Summary, item.Title, item.Detail, item.Raw)
			if text == "" {
				continue
			}
			prefix := output.FirstNonEmpty(item.Source, item.Kind)
			if item.Status != "" {
				prefix += ":" + item.Status
			}
			lines = append(lines, fmt.Sprintf("  - **%s** %s", prefix, text))
		}
	}
	if len(lines) == 0 {
		return
	}
	sb.WriteString("- **Findings:**\n")
	for _, line := range lines {
		sb.WriteString(line + "\n")
	}
}

func assetServiceFacts(items []output.AssetItem) []string {
	var values []string
	for _, item := range items {
		if item.Kind != output.AssetItemService {
			continue
		}
		values = append(values, strings.Join(output.CompactStrings(
			output.AssetDataString(item.Data, "protocol"),
			output.AssetDataString(item.Data, "service"),
			output.AssetDataString(item.Data, "port"),
		), " "))
	}
	return output.CompactStrings(values...)
}

func assetHTTPStatuses(items []output.AssetItem) []string {
	var values []string
	for _, item := range items {
		if item.Kind == output.AssetItemPath && item.Status != "" {
			values = append(values, item.Status)
		}
	}
	return output.CompactStrings(values...)
}

func assetFingers(items []output.AssetItem) []string {
	var values []string
	for _, item := range items {
		switch item.Kind {
		case output.AssetItemFingerprint:
			values = append(values, output.FirstNonEmpty(item.Title, output.AssetDataString(item.Data, "name")))
		case output.AssetItemPath:
			values = append(values, output.AssetDataStrings(item.Data, "fingers")...)
		}
	}
	return output.CompactStrings(values...)
}

func assetSources(items []output.AssetItem) []string {
	var values []string
	for _, item := range items {
		values = append(values, item.Source)
	}
	return output.CompactStrings(values...)
}

func assetPathCount(items []output.AssetItem) int {
	count := 0
	for _, item := range items {
		if item.Kind == output.AssetItemPath {
			count++
		}
	}
	return count
}

func resultFingerprintCount(result *output.Result) int {
	if result == nil {
		return 0
	}
	seen := make(map[string]struct{})
	for _, asset := range result.Assets {
		for _, finger := range assetFingers(asset.Items) {
			seen[strings.ToLower(finger)] = struct{}{}
		}
	}
	return len(seen)
}

func markdownCode(value string) string {
	value = strings.ReplaceAll(value, "`", "'")
	return "`" + value + "`"
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

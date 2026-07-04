package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/core/output"
	"github.com/chainreactors/aiscan/core/runner"
	"github.com/chainreactors/aiscan/pkg/webproto"
)

type ConfigStore interface {
	GetDistributeConfig(ctx context.Context) (path string, loaded bool, cfg webproto.DistributeConfig, err error)
	SaveDistributeConfig(ctx context.Context, cfg webproto.DistributeConfig) error
}

type ServiceConfig struct {
	Store          Store
	App            *runner.App
	ConfigStore    ConfigStore
	AppFactory     func(ctx context.Context) (*runner.App, error)
	RuntimeContext context.Context
	AgentPool      *AgentPool
	MaxConcurrent  int
	ScanTimeout    time.Duration
}

type Service struct {
	store    Store
	appMu    sync.RWMutex
	app      *runner.App
	appRefs  *sync.WaitGroup
	appCtx   context.Context
	config   ConfigStore
	reload   func(ctx context.Context) (*runner.App, error)
	reloadMu sync.Mutex
	agents   *AgentPool
	hub      *Hub
	sem      chan struct{}
	timeout  time.Duration

	mu           sync.Mutex
	cancels      map[string]context.CancelFunc
	taskSessions map[string]string // taskID → sessionID
	taskAgents   map[string]string // taskID → agentID
	taskCanceled map[string]bool
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
	appCtx := cfg.RuntimeContext
	if appCtx == nil {
		appCtx = context.Background()
	}
	svc := &Service{
		store:        cfg.Store,
		app:          cfg.App,
		appRefs:      new(sync.WaitGroup),
		appCtx:       appCtx,
		config:       cfg.ConfigStore,
		reload:       cfg.AppFactory,
		agents:       cfg.AgentPool,
		hub:          NewHub(),
		sem:          make(chan struct{}, maxConcurrent),
		timeout:      timeout,
		cancels:      make(map[string]context.CancelFunc),
		taskSessions: make(map[string]string),
		taskAgents:   make(map[string]string),
		taskCanceled: make(map[string]bool),
	}
	if cfg.AgentPool != nil {
		cfg.AgentPool.SetSessionLookup(svc)
		cfg.AgentPool.SetConfigProvider(svc.currentDistributeConfig)
	}
	return svc
}

// currentDistributeConfig returns the latest persisted config, used to push
// config to agents the moment they (re)connect.
func (s *Service) currentDistributeConfig() (webproto.DistributeConfig, bool) {
	if s.config == nil {
		return webproto.DistributeConfig{}, false
	}
	_, loaded, dc, err := s.config.GetDistributeConfig(s.appCtx)
	if err != nil || !loaded {
		return webproto.DistributeConfig{}, false
	}
	return dc, true
}

func (s *Service) Hub() *Hub { return s.hub }

func (s *Service) SetAgentPool(pool *AgentPool) {
	s.agents = pool
	pool.SetSessionLookup(s)
	pool.SetConfigProvider(s.currentDistributeConfig)
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	for _, cancel := range s.cancels {
		cancel()
	}
	s.mu.Unlock()
	s.appMu.Lock()
	app := s.app
	refs := s.appRefs
	s.app = nil
	s.appRefs = new(sync.WaitGroup)
	s.appMu.Unlock()
	if app != nil {
		if refs != nil {
			refs.Wait()
		}
		app.Close()
	}
}

func (s *Service) Status() ServiceStatus {
	app, release := s.borrowApp()
	defer release()
	status := ServiceStatus{
		LLMAvailable: app != nil && app.Provider != nil,
	}
	if app != nil {
		status.LLMProvider = app.ProviderConfig.Provider
		status.LLMModel = app.ProviderConfig.Model
		status.LLMAPIKeyConfigured = strings.TrimSpace(app.ProviderConfig.APIKey) != ""
	}
	if s.config != nil {
		if path, loaded, dc, err := s.config.GetDistributeConfig(context.Background()); err == nil {
			status.ConfigPath = path
			status.ConfigLoaded = loaded
			if status.LLMProvider == "" {
				status.LLMProvider = dc.LLM.Provider
			}
			if status.LLMModel == "" {
				status.LLMModel = dc.LLM.Model
			}
			status.LLMAPIKeyConfigured = status.LLMAPIKeyConfigured || dc.LLM.APIKey != ""
		}
	}
	return status
}

func (s *Service) GetConfigStatus(ctx context.Context) (ConfigStatus, error) {
	if s.config == nil {
		return ConfigStatus{}, fmt.Errorf("config store is not configured")
	}
	path, loaded, dc, err := s.config.GetDistributeConfig(ctx)
	if err != nil {
		return ConfigStatus{}, err
	}
	return ConfigStatusFromDistribute(&dc, path, loaded), nil
}

func (s *Service) SaveConfig(ctx context.Context, cfg webproto.DistributeConfig) (ConfigStatus, error) {
	if s.config == nil {
		return ConfigStatus{}, fmt.Errorf("config store is not configured")
	}
	// Serialize saves so concurrent reloads don't race on the rebuild or on the
	// process-global runtime defaults touched while loading config.
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	if err := s.config.SaveDistributeConfig(ctx, cfg); err != nil {
		return ConfigStatus{}, err
	}
	fullCfg := cfg
	if _, _, stored, err := s.config.GetDistributeConfig(ctx); err == nil {
		fullCfg = stored
	}
	if s.reload != nil {
		app, err := s.reload(s.appCtx)
		if err != nil {
			cs, _ := s.GetConfigStatus(ctx)
			return cs, fmt.Errorf("reload aiscan runtime: %w", err)
		}
		s.swapApp(app)
	}
	if s.agents != nil {
		s.agents.BroadcastConfig(fullCfg)
	}
	return s.GetConfigStatus(ctx)
}

func (s *Service) GetDistributeConfig(ctx context.Context) (webproto.DistributeConfig, error) {
	if s.config == nil {
		return webproto.DistributeConfig{}, fmt.Errorf("config store is not configured")
	}
	_, _, dc, err := s.config.GetDistributeConfig(ctx)
	return dc, err
}

func (s *Service) SubmitScan(ctx context.Context, target, mode string, verify, sniper, deep bool, project string) (*ScanJob, error) {
	// target may carry one or many targets (comma/newline/space separated); the
	// job aggregates all valid ones into a single scan run. Invalid tokens are
	// skipped (not fatal) unless none are valid, so one typo in a pasted batch
	// no longer sinks the whole scan.
	targets, skipped, err := ParseTargets(target)
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

	project, err = s.resolveProjectID(ctx, project)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	job := &ScanJob{
		ID:        generateID(),
		Target:    strings.Join(targets, ", "),
		Mode:      mode,
		Verify:    verify,
		Sniper:    sniper,
		AI:        verify || sniper,
		Deep:      deep,
		Project:   project,
		Status:    StatusQueued,
		Skipped:   skipped,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.store.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("store create: %w", err)
	}

	go s.runScan(job.ID) //nolint:gosec // G118: background scan outlives the request

	return job, nil
}

func (s *Service) GetScan(ctx context.Context, id string) (*ScanJob, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) ListScans(ctx context.Context) ([]*ScanJob, error) {
	return s.store.List(ctx, 100)
}

// DeleteScan removes a scan from history. A running or queued scan is canceled
// first so its goroutine unwinds, then the record is dropped from the store.
// Deleting an already-finished scan simply removes the row.
func (s *Service) DeleteScan(id string) error {
	s.mu.Lock()
	cancel, ok := s.cancels[id]
	if ok {
		delete(s.cancels, id)
	}
	s.mu.Unlock()
	if ok {
		cancel()
	}
	return s.store.Delete(context.Background(), id)
}

func (s *Service) GetReport(ctx context.Context, id, lang string) (string, error) {
	job, err := s.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	// Re-render from the structured result so the requested language is honored.
	// Fall back to the stored report for legacy jobs without a structured result.
	if job.Result != nil {
		return buildMarkdownReport(job.Target, job.Mode, job.Result, lang), nil
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
	if job.Status == StatusCanceled {
		return
	}

	job.Status = StatusRunning
	job.UpdatedAt = time.Now()
	_ = s.store.Update(ctx, job)

	s.hub.Broadcast(jobID, HubEvent{
		Type: "status",
		Data: mustJSON(map[string]string{"scan_id": jobID, "status": string(StatusRunning)}),
	})

	// Try agent dispatch first, fall back to local execution.
	if s.agents != nil && s.agents.Count() > 0 {
		s.runScanViaAgent(ctx, job)
		return
	}
	s.runScanLocally(ctx, job)
}

func (s *Service) runScanViaAgent(ctx context.Context, job *ScanJob) {
	agent := s.agents.Pick()
	if agent == nil {
		s.failJob(job, "no agents available")
		return
	}

	cmd := "scan " + strings.Join(scanArgsForJob(job), " ")
	resultCh, err := s.agents.DispatchCommand(agent.id, job.ID, cmd)
	if err != nil {
		s.failJob(job, err.Error())
		return
	}

	// Wait for agent to complete. Output is forwarded to SSE hub by
	// AgentPool.HandleOutput as the agent POSTs progress lines. Honor ctx so a
	// hung-but-connected agent can't pin this goroutine (and its bounded scan
	// semaphore slot) forever; on cancel, tell the remote to stop scanning so
	// the billable node doesn't keep running.
	var res taskResult
	select {
	case r, ok := <-resultCh:
		if !ok {
			s.failJob(job, "agent disconnected")
			return
		}
		res = r
	case <-ctx.Done():
		s.agents.CancelTask(agent.id, job.ID)
		s.failJob(job, ctx.Err().Error())
		return
	}
	if res.Err != "" {
		s.failJob(job, res.Err)
		return
	}
	if progress := lastOutputLine(res.Output); progress != "" {
		job.Progress = progress
	}

	var result *output.Result
	if len(res.Result) > 0 {
		result = &output.Result{}
		_ = json.Unmarshal(res.Result, result)
	}

	report := buildMarkdownReport(job.Target, job.Mode, result, "en")
	job.Status = StatusCompleted
	job.Report = report
	job.Result = result
	job.UpdatedAt = time.Now()
	_ = s.store.Update(ctx, job)
	s.ingestScanAssets(ctx, job, result)

	s.persistResultRecords(job.ID, agent.id, result)

	s.hub.Broadcast(job.ID, HubEvent{
		Type:     "complete",
		Data:     mustJSON(map[string]any{"scan_id": job.ID, "status": "completed", "result": result}),
		Reliable: true,
	})
	s.broadcastScanComplete(job.ID, result)
}

func (s *Service) runScanLocally(ctx context.Context, job *ScanJob) {
	streamWriter := &sseStreamWriter{
		hub:    s.hub,
		scanID: job.ID,
		store:  s.store,
		job:    job,
		ctx:    ctx,
	}

	args := scanArgsForJob(job)
	_, result, err := s.executeScan(ctx, args, streamWriter)
	if err != nil {
		s.failJob(job, err.Error())
		return
	}
	if streamWriter.job != nil {
		job = streamWriter.job
	}

	report := buildMarkdownReport(job.Target, job.Mode, result, "en")
	job.Status = StatusCompleted
	job.Report = report
	job.Result = result
	job.UpdatedAt = time.Now()
	_ = s.store.Update(ctx, job)
	s.ingestScanAssets(ctx, job, result)

	s.persistResultRecords(job.ID, "", result)

	s.hub.Broadcast(job.ID, HubEvent{
		Type:     "complete",
		Data:     mustJSON(map[string]any{"scan_id": job.ID, "status": "completed", "result": result}),
		Reliable: true,
	})
	s.broadcastScanComplete(job.ID, result)
}

func (s *Service) persistResultRecords(scanID, agentID string, result *output.Result) {
	recs := resultToRecords(scanID, agentID, result)
	if len(recs) > 0 {
		_ = s.store.InsertRecords(context.Background(), recs)
	}
}

func (s *Service) failJob(job *ScanJob, errMsg string) {
	job.Status = StatusFailed
	job.Error = errMsg
	job.UpdatedAt = time.Now()
	_ = s.store.Update(context.Background(), job)
	s.hub.Broadcast(job.ID, HubEvent{
		Type:     "error",
		Data:     mustJSON(map[string]string{"scan_id": job.ID, "error": errMsg}),
		Reliable: true,
	})
}

func (s *Service) aiAvailable() bool {
	app, release := s.borrowApp()
	defer release()
	return app != nil && app.Provider != nil
}

// borrowApp returns the live App together with a release func the caller must
// invoke when done. Outstanding borrows are drained before a swapped-out App is
// closed, so reloading config never closes engines out from under a running scan.
func (s *Service) borrowApp() (*runner.App, func()) {
	if s == nil {
		return nil, func() {}
	}
	s.appMu.RLock()
	app := s.app
	refs := s.appRefs
	if app != nil && refs != nil {
		refs.Add(1)
	}
	s.appMu.RUnlock()
	if app == nil {
		return nil, func() {}
	}
	return app, func() { refs.Done() }
}

func (s *Service) swapApp(next *runner.App) {
	if s == nil || next == nil {
		return
	}
	s.appMu.Lock()
	prev := s.app
	prevRefs := s.appRefs
	s.app = next
	s.appRefs = new(sync.WaitGroup)
	s.appMu.Unlock()
	if prev != nil && prev != next {
		// Close the old App only after in-flight borrowers have returned.
		go func() {
			if prevRefs != nil {
				prevRefs.Wait()
			}
			prev.Close()
		}()
	}
}

func scanArgsForJob(job *ScanJob) []string {
	var args []string
	for _, t := range splitTargets(job.Target) {
		args = append(args, "-i", t)
	}
	args = append(args, "--mode", job.Mode)
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
	app, release := s.borrowApp()
	defer release()
	if app == nil || app.Commands == nil {
		return "", nil, fmt.Errorf("aiscan runtime is not ready")
	}
	// Engines (and the scan command) finish initializing asynchronously after a
	// reload; wait here so a scan issued right after saving config doesn't race them.
	if err := app.WaitEngines(ctx); err != nil {
		return "", nil, fmt.Errorf("scan engines not ready: %w", err)
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
	mu     sync.Mutex
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
	w.mu.Lock()
	defer w.mu.Unlock()
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
		if current.Status == StatusCanceled {
			return 0, context.Canceled
		}
		current.Progress = line
		current.UpdatedAt = time.Now()
		if err := w.store.Update(context.Background(), current); err != nil {
			return 0, err
		}
		w.job = current

		w.hub.Broadcast(w.scanID, HubEvent{
			Type: "progress",
			Data: mustJSON(map[string]string{"scan_id": w.scanID, "data": line}),
		})
	}
	return len(p), nil
}

// ScanSnapshot implements scan.ScanSnapshotSink. For a local (in-process) scan
// it pushes an incremental structured result to the scan's SSE topic under the
// "stats" event, lighting up the live counters, findings, and asset tree while
// the scan runs. The authoritative final result still arrives with "complete".
func (w *sseStreamWriter) ScanSnapshot(result *output.Result) {
	if result == nil || w.hub == nil {
		return
	}
	if w.ctx != nil {
		select {
		case <-w.ctx.Done():
			return
		default:
		}
	}
	w.hub.Broadcast(w.scanID, HubEvent{
		Type: "stats",
		Data: mustJSON(map[string]any{"scan_id": w.scanID, "result": result}),
	})
}

// reportLabels holds the localized structural labels for a markdown report.
// Only the labels are translated; scan data (targets, fingerprint names,
// evidence) is always emitted verbatim.
type reportLabels struct {
	title, target, mode, date                    string
	summary, metric, value                       string
	targets, services, web, probes, fingerprints string
	loots, errors, duration                      string
	assets, state, http, fingers, sources, paths string
	analysis, source, assetFallback              string
	noStructuredResult                           string
}

// normalizeReportLang maps an arbitrary lang hint to a supported locale.
func normalizeReportLang(lang string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(lang)), "zh") {
		return "zh"
	}
	return "en"
}

func reportLabelsFor(lang string) reportLabels {
	if normalizeReportLang(lang) == "zh" {
		return reportLabels{
			title: "渗透测试报告", target: "目标", mode: "模式", date: "日期",
			summary: "摘要", metric: "指标", value: "数值",
			targets: "目标数", services: "服务", web: "Web", probes: "探测", fingerprints: "指纹",
			loots: "战利品", errors: "错误", duration: "耗时",
			assets: "资产", state: "状态", http: "HTTP", fingers: "指纹", sources: "来源", paths: "路径",
			analysis: "分析", source: "来源", assetFallback: "资产",
			noStructuredResult: "未返回结构化结果。",
		}
	}
	return reportLabels{
		title: "Penetration Test Report", target: "Target", mode: "Mode", date: "Date",
		summary: "Summary", metric: "Metric", value: "Value",
		targets: "Targets", services: "Services", web: "Web", probes: "Probes", fingerprints: "Fingerprints",
		loots: "Loots", errors: "Errors", duration: "Duration",
		assets: "Assets", state: "State", http: "HTTP", fingers: "Fingers", sources: "Sources", paths: "Paths",
		analysis: "Analysis", source: "Source", assetFallback: "Asset",
		noStructuredResult: "No structured result was returned.",
	}
}

var reportStateValuesZh = map[string]string{
	"low": "低", "medium": "中", "high": "高", "critical": "严重", "info": "信息", "unknown": "未知",
}

// translateStateValue localizes an asset state token (low/high/...). Unknown
// values (real data) fall through unchanged.
func translateStateValue(lang, value string) string {
	if value == "" || normalizeReportLang(lang) != "zh" {
		return value
	}
	if v, ok := reportStateValuesZh[strings.ToLower(value)]; ok {
		return v
	}
	return value
}

func buildMarkdownReport(target, mode string, result *output.Result, lang string) string {
	lbl := reportLabelsFor(lang)
	var sb strings.Builder
	sb.WriteString("# " + lbl.title + "\n\n")
	sb.WriteString(fmt.Sprintf("**%s:** `%s`  \n", lbl.target, target))
	sb.WriteString(fmt.Sprintf("**%s:** %s  \n", lbl.mode, mode))
	sb.WriteString(fmt.Sprintf("**%s:** %s\n\n", lbl.date, time.Now().Format("2006-01-02 15:04:05")))
	sb.WriteString("---\n\n")

	if result == nil {
		sb.WriteString(lbl.noStructuredResult + "\n")
		return sb.String()
	}

	sb.WriteString("## " + lbl.summary + "\n\n")
	sb.WriteString(fmt.Sprintf("| %s | %s |\n|---|---:|\n", lbl.metric, lbl.value))
	sb.WriteString(fmt.Sprintf("| %s | %d |\n", lbl.targets, result.Summary.Targets))
	sb.WriteString(fmt.Sprintf("| %s | %d |\n", lbl.services, result.Summary.Services))
	sb.WriteString(fmt.Sprintf("| %s | %d |\n", lbl.web, result.Summary.Webs))
	sb.WriteString(fmt.Sprintf("| %s | %d |\n", lbl.probes, result.Summary.Probes))
	sb.WriteString(fmt.Sprintf("| %s | %d |\n", lbl.fingerprints, resultFingerprintCount(result)))
	sb.WriteString(fmt.Sprintf("| %s | %d |\n", lbl.loots, result.Summary.Loots))
	sb.WriteString(fmt.Sprintf("| %s | %d |\n", lbl.errors, result.Summary.Errors))
	if result.Summary.Duration != "" {
		sb.WriteString(fmt.Sprintf("| %s | %s |\n", lbl.duration, result.Summary.Duration))
	}
	sb.WriteString("\n")

	if len(result.Assets) == 0 {
		return sb.String()
	}

	sb.WriteString("## " + lbl.assets + "\n\n")
	for _, asset := range result.Assets {
		title := output.FirstNonEmpty(asset.Title, asset.Target, asset.Key, lbl.assetFallback)
		sb.WriteString(fmt.Sprintf("### %s\n\n", title))
		if asset.Target != "" && asset.Target != title {
			sb.WriteString(fmt.Sprintf("- **%s:** %s\n", lbl.target, markdownCode(asset.Target)))
		}
		if asset.Status != "" {
			sb.WriteString(fmt.Sprintf("- **%s:** %s\n", lbl.state, markdownCode(translateStateValue(lang, asset.Status))))
		}
		writeMarkdownList(&sb, lbl.services, assetServiceFacts(asset.Items))
		writeMarkdownList(&sb, lbl.http, assetHTTPStatuses(asset.Items))
		writeMarkdownList(&sb, lbl.fingers, assetFingers(asset.Items))
		writeMarkdownList(&sb, lbl.sources, assetSources(asset.Items))
		if paths := assetPathCount(asset.Items); paths > 0 {
			sb.WriteString(fmt.Sprintf("- **%s:** %d\n", lbl.paths, paths))
		}
		writeAssetLootMarkdown(&sb, asset.Items, lbl)
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

func writeAssetLootMarkdown(sb *strings.Builder, items []output.AssetItem, lbl reportLabels) {
	wrote := false
	for _, item := range items {
		switch item.Kind {
		case output.AssetItemLoot, output.AssetItemNote, output.AssetItemResponse, output.AssetItemError:
			summary := output.FirstNonEmpty(item.Summary, item.Title)
			detail := output.AssetItemDetail(item)
			if summary == "" && detail == "" {
				continue
			}
			prefix := output.FirstNonEmpty(item.Source, item.Kind)
			if item.Status != "" {
				prefix += ":" + item.Status
			}
			if !wrote {
				sb.WriteString("\n#### " + lbl.analysis + "\n\n")
				wrote = true
			}
			if summary == "" {
				summary = firstMarkdownLine(detail)
			}
			sb.WriteString(fmt.Sprintf("##### %s\n\n", markdownHeading(summary)))
			sb.WriteString(fmt.Sprintf("**%s:** %s\n\n", lbl.source, markdownCode(prefix)))
			if detail != "" && !sameMarkdownText(summary, detail) {
				writeMarkdownBlock(sb, detail)
			} else if detail == "" && summary != "" {
				sb.WriteString(summary)
				sb.WriteString("\n\n")
			}
		}
	}
}

func firstMarkdownLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		return strings.TrimSpace(value[:idx])
	}
	return value
}

func sameMarkdownText(left, right string) bool {
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}

func writeMarkdownBlock(sb *strings.Builder, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	sb.WriteString(value)
	sb.WriteString("\n\n")
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

func markdownHeading(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\n", " ")
	if value == "" {
		return "Analysis"
	}
	return strings.TrimLeft(value, "# ")
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func stripANSI(s string) string {
	return output.StripANSI(s)
}

func lastOutputLine(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(stripANSI(lines[i]))
		if line != "" {
			return line
		}
	}
	return ""
}

// --- Chat session service methods ---

func sessionTopic(id string) string {
	return "session:" + id
}

func (s *Service) TaskSession(taskID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sid, ok := s.taskSessions[taskID]
	return sid, ok
}

func (s *Service) registerSessionTask(taskID, sessionID, agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskSessions[taskID] = sessionID
	if agentID != "" {
		s.taskAgents[taskID] = agentID
	}
	delete(s.taskCanceled, taskID)
}

func (s *Service) finishSessionTask(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	canceled := s.taskCanceled[taskID]
	delete(s.taskSessions, taskID)
	delete(s.taskAgents, taskID)
	delete(s.taskCanceled, taskID)
	return canceled
}

func (s *Service) CancelSession(ctx context.Context, sessionID string) error {
	if _, err := s.store.GetSession(ctx, sessionID); err != nil {
		return err
	}

	type activeTask struct {
		taskID  string
		agentID string
	}
	var tasks []activeTask
	s.mu.Lock()
	for taskID, sid := range s.taskSessions {
		if sid != sessionID {
			continue
		}
		tasks = append(tasks, activeTask{taskID: taskID, agentID: s.taskAgents[taskID]})
		s.taskCanceled[taskID] = true
	}
	s.mu.Unlock()

	if len(tasks) == 0 {
		s.broadcastSystemMessage(sessionID, "No running task.")
		return nil
	}
	if s.agents != nil {
		for _, task := range tasks {
			if task.agentID != "" {
				s.agents.CancelTask(task.agentID, task.taskID)
			}
		}
	}
	s.broadcastSystemMessage(sessionID, "Paused.")
	return nil
}

func (s *Service) HandleFileUpload(ctx context.Context, sessionID, filename string, data []byte) (*webproto.FileUploadResult, error) {
	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	if s.agents == nil {
		return nil, fmt.Errorf("no agent pool available")
	}
	agentID := session.AgentID
	if agentID == "" {
		return nil, fmt.Errorf("session has no assigned agent")
	}

	payload := webproto.FileUploadPayload{
		Filename:  filename,
		FileSize:  int64(len(data)),
		MimeType:  http.DetectContentType(data),
		SessionID: sessionID,
	}
	payloadJSON, _ := json.Marshal(payload)

	taskID := generateID()
	msg := WSMessage{
		Type:    "upload",
		TaskID:  taskID,
		DataB64: base64.StdEncoding.EncodeToString(data),
		Payload: payloadJSON,
	}

	resultCh, err := s.agents.dispatchMessage(agentID, taskID, msg)
	if err != nil {
		return nil, fmt.Errorf("agent dispatch failed: %w", err)
	}

	select {
	case res, ok := <-resultCh:
		if !ok {
			return nil, fmt.Errorf("agent disconnected during upload")
		}
		var result webproto.FileUploadResult
		if len(res.Result) > 0 {
			if err := json.Unmarshal(res.Result, &result); err != nil {
				return &webproto.FileUploadResult{
					Filename: filename,
					Path:     res.Output,
					Size:     int64(len(data)),
				}, nil
			}
		} else {
			result.Filename = filename
			result.Path = res.Output
			result.Size = int64(len(data))
		}
		if result.Error != "" {
			return nil, fmt.Errorf("agent upload error: %s", result.Error)
		}
		s.broadcastSystemMessage(sessionID, fmt.Sprintf("File uploaded: %s → %s", filename, result.Path))
		return &result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) CreateSession(ctx context.Context, agentID, title string) (*ChatSession, error) {
	var agentName string
	if s.agents != nil {
		if info := s.agents.get(agentID); info != nil {
			agentName = info.name
		}
	}
	now := time.Now()
	session := &ChatSession{
		ID:        generateID(),
		AgentID:   agentID,
		AgentName: agentName,
		Title:     title,
		Status:    SessionActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.CreateSession(ctx, session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

func (s *Service) GetSession(ctx context.Context, id string) (*ChatSession, error) {
	return s.store.GetSession(ctx, id)
}

func (s *Service) ListSessions(ctx context.Context) ([]*ChatSession, error) {
	return s.store.ListSessions(ctx, 100)
}

func (s *Service) DeleteSession(ctx context.Context, id string) error {
	return s.store.DeleteSession(ctx, id)
}

func (s *Service) GetMessages(ctx context.Context, sessionID string) ([]*ChatMessage, error) {
	return s.store.ListMessages(ctx, sessionID, 500)
}

func (s *Service) BroadcastChatEvent(sessionID string, event ChatEvent) {
	event.SessionID = sessionID
	if !event.Transient {
		s.persistRuntimeChatEvent(sessionID, event)
	}
	s.hub.Broadcast(sessionTopic(sessionID), HubEvent{
		Type: event.Type,
		Data: mustJSON(event),
	})
}

func (s *Service) persistRuntimeChatEvent(sessionID string, event ChatEvent) {
	if s == nil || s.store == nil || sessionID == "" {
		return
	}

	now := time.Now()
	msg := &ChatMessage{
		ID:        generateID(),
		SessionID: sessionID,
		AgentID:   event.AgentID,
		AgentName: event.AgentName,
		CreatedAt: now,
	}
	metadata := map[string]any{
		"event_type": event.Type,
	}
	if event.Turn > 0 {
		metadata["turn"] = event.Turn
	}

	switch event.Type {
	case ChatEventThinking:
		msg.Role = "system"
		msg.Content = strings.TrimSpace(event.Content)
		if msg.Content == "" {
			msg.Content = "thinking"
		}

	case ChatEventAgentJoined:
		msg.Role = "system"
		msg.Content = strings.TrimSpace(event.AgentName + " joined")

	case ChatEventToolCall:
		msg.Role = "tool_call"
		msg.Content = event.ToolArgs
		metadata["tool_call_id"] = event.ToolCallID
		metadata["tool_name"] = event.ToolName
		metadata["tool_args"] = event.ToolArgs

	case ChatEventToolResult:
		msg.Role = "tool_result"
		msg.Content = event.Content
		metadata["tool_call_id"] = event.ToolCallID

	default:
		return
	}

	if data, err := json.Marshal(metadata); err == nil {
		msg.Metadata = data
	}
	_ = s.store.AddMessage(context.Background(), msg)
}

func (s *Service) HandleUserMessage(ctx context.Context, sessionID, content string, opts ChatOptions) (*ChatMessage, error) {
	now := time.Now()
	msg := &ChatMessage{
		ID:        generateID(),
		SessionID: sessionID,
		Role:      "user",
		Content:   content,
		CreatedAt: now,
	}
	if err := s.store.AddMessage(ctx, msg); err != nil {
		return nil, fmt.Errorf("store message: %w", err)
	}

	// Update session timestamp and auto-title from first message.
	session, err := s.store.GetSession(ctx, sessionID)
	if err == nil {
		session.UpdatedAt = now
		if session.Title == "" {
			title := content
			if len(title) > 60 {
				title = title[:60] + "..."
			}
			session.Title = title
		}
		_ = s.store.UpdateSession(ctx, session)
	}

	go s.dispatchUserMessage(sessionID, msg, opts)

	return msg, nil
}

func (s *Service) dispatchUserMessage(sessionID string, msg *ChatMessage, opts ChatOptions) {
	content := strings.TrimSpace(msg.Content)

	// Slash commands act on the hub itself (scan pipeline, agent roster, raw
	// shell) rather than being forwarded to the LLM. Anything that is not a
	// recognized verb — including plain messages that merely start with "/" —
	// falls through and is dispatched to the agent as normal chat.
	if cmd, args, ok := parseSlashCommand(content); ok {
		switch cmd {
		case "scan":
			s.handleScanCommand(sessionID, args)
			return
		case "agents":
			s.handleAgentsCommand(sessionID)
			return
		case "shell", "sh":
			s.handleShellCommand(sessionID, args)
			return
		case "help":
			s.handleHelpCommand(sessionID)
			return
		}
	}

	s.handleChatMessage(sessionID, content, opts)
}

// parseSlashCommand splits a leading "/verb args..." into its lowercased verb
// and the trimmed remainder. ok is false when content does not begin with a
// non-empty "/verb".
func parseSlashCommand(content string) (cmd, args string, ok bool) {
	if !strings.HasPrefix(content, "/") {
		return "", "", false
	}
	rest := strings.TrimSpace(content[1:])
	if rest == "" {
		return "", "", false
	}
	if i := strings.IndexAny(rest, " \t\r\n"); i >= 0 {
		return strings.ToLower(rest[:i]), strings.TrimSpace(rest[i:]), true
	}
	return strings.ToLower(rest), "", true
}

func (s *Service) handleScanCommand(sessionID, args string) {
	ctx := context.Background()
	parts := strings.Fields(args)
	if len(parts) == 0 {
		s.BroadcastChatEvent(sessionID, ChatEvent{
			Type:  ChatEventError,
			Error: "usage: /scan <target> [--mode full] [--verify] [--sniper] [--deep]",
		})
		return
	}

	target := parts[0]
	mode := "quick"
	var verify, sniper, deep bool
	for _, p := range parts[1:] {
		switch p {
		case "--mode":
			// next arg handled below
		case "full":
			mode = "full"
		case "--verify":
			verify = true
		case "--sniper":
			sniper = true
		case "--deep":
			deep = true
		}
	}
	for i, p := range parts {
		if p == "--mode" && i+1 < len(parts) {
			mode = parts[i+1]
		}
	}

	job, err := s.SubmitScan(ctx, target, mode, verify, sniper, deep, DefaultProjectID)
	if err != nil {
		s.BroadcastChatEvent(sessionID, ChatEvent{
			Type:  ChatEventError,
			Error: fmt.Sprintf("scan failed: %s", err),
		})
		return
	}

	_ = s.store.LinkScanToSession(ctx, sessionID, job.ID)

	s.registerSessionTask(job.ID, sessionID, "")

	s.BroadcastChatEvent(sessionID, ChatEvent{
		Type:   ChatEventScanStarted,
		ScanID: job.ID,
		Data:   fmt.Sprintf("Scan started: %s (%s)", target, mode),
	})
}

func (s *Service) handleHelpCommand(sessionID string) {
	s.broadcastSystemMessage(sessionID, "**Commands**\n"+
		"- `/scan <target> [--mode full] [--verify] [--sniper] [--deep]` — run a scan in this session\n"+
		"- `/agents` — list connected agents\n"+
		"- `/shell <command>` — run a shell command on the session's agent\n"+
		"- `/help` — show this message\n\n"+
		"Anything else is sent to the agent as a chat message.")
}

func (s *Service) handleAgentsCommand(sessionID string) {
	if s.agents == nil || s.agents.Count() == 0 {
		s.broadcastSystemMessage(sessionID, "No agents connected.")
		return
	}
	agents := s.agents.List()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d agent(s) connected:\n", len(agents)))
	for _, a := range agents {
		status := "idle"
		if a.Busy {
			status = "busy"
		}
		sb.WriteString(fmt.Sprintf("- **%s** (%s) — %s", a.Name, a.ID[:8], status))
		if a.Identity.Model != "" {
			sb.WriteString(fmt.Sprintf(" — %s/%s", a.Identity.Provider, a.Identity.Model))
		}
		sb.WriteString("\n")
	}
	s.broadcastSystemMessage(sessionID, sb.String())
}

func (s *Service) sessionAgent(sessionID string) *remoteAgent {
	session, err := s.store.GetSession(context.Background(), sessionID)
	if err != nil || session.AgentID == "" {
		return nil
	}
	if s.agents == nil {
		return nil
	}
	return s.agents.get(session.AgentID)
}

func (s *Service) handleShellCommand(sessionID, command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}

	agent := s.sessionAgent(sessionID)
	if agent == nil {
		s.BroadcastChatEvent(sessionID, ChatEvent{
			Type:  ChatEventError,
			Error: "agent is not connected",
		})
		return
	}

	taskID := generateID()
	s.registerSessionTask(taskID, sessionID, agent.id)

	s.BroadcastChatEvent(sessionID, ChatEvent{
		Type:      ChatEventAgentJoined,
		AgentID:   agent.id,
		AgentName: agent.name,
	})

	resultCh, err := s.agents.DispatchCommand(agent.id, taskID, command)
	if err != nil {
		s.finishSessionTask(taskID)
		s.BroadcastChatEvent(sessionID, ChatEvent{
			Type:  ChatEventError,
			Error: err.Error(),
		})
		return
	}

	go func() {
		res, ok := <-resultCh
		canceled := s.finishSessionTask(taskID)
		if !ok {
			s.BroadcastChatEvent(sessionID, ChatEvent{
				Type:  ChatEventError,
				Error: "agent disconnected",
			})
			return
		}
		if canceled {
			return
		}
		content := res.Output
		if res.Err != "" {
			content = "Error: " + res.Err
		}
		s.persistAssistantMessage(sessionID, agent.id, agent.name, content, res.Turn)
	}()
}

func (s *Service) handleChatMessage(sessionID, content string, opts ChatOptions) {
	agent := s.sessionAgent(sessionID)
	if agent == nil {
		s.broadcastSystemMessage(sessionID, "Agent is not connected. Reconnect the agent to continue chatting.")
		return
	}

	taskID := generateID()
	s.registerSessionTask(taskID, sessionID, agent.id)

	s.BroadcastChatEvent(sessionID, ChatEvent{
		Type:      ChatEventAgentJoined,
		AgentID:   agent.id,
		AgentName: agent.name,
	})

	resultCh, err := s.agents.DispatchChatSession(agent.id, taskID, sessionID, content, opts)
	if err != nil {
		s.finishSessionTask(taskID)
		s.BroadcastChatEvent(sessionID, ChatEvent{
			Type:  ChatEventError,
			Error: err.Error(),
		})
		return
	}

	go func() {
		res, ok := <-resultCh
		canceled := s.finishSessionTask(taskID)
		if !ok {
			return
		}
		if canceled {
			return
		}
		reply := res.Output
		if res.Err != "" {
			reply = "Error: " + res.Err
		}
		s.persistAssistantMessage(sessionID, agent.id, agent.name, reply, res.Turn)
	}()
}

func (s *Service) broadcastSystemMessage(sessionID, content string) {
	now := time.Now()
	msg := &ChatMessage{
		ID:        generateID(),
		SessionID: sessionID,
		Role:      "system",
		Content:   content,
		CreatedAt: now,
	}
	_ = s.store.AddMessage(context.Background(), msg)
	s.BroadcastChatEvent(sessionID, ChatEvent{
		Type:      ChatEventMessage,
		MessageID: msg.ID,
		Role:      "system",
		Content:   content,
	})
}

// --- Asset pool ---

func (s *Service) ListAssets(ctx context.Context, projectID string) ([]*PoolAsset, error) {
	return s.store.ListAssets(ctx, projectID, 500)
}

// AddAssets validates and upserts a batch of targets into the pool. Invalid
// tokens (bash fragments, file paths, prose) are silently skipped so the
// agent-recon extractor and manual entry can both feed it raw material.
func (s *Service) AddAssets(ctx context.Context, projectID string, targets []string, source, label string) ([]*PoolAsset, error) {
	if source == "" {
		source = AssetSourceManual
	}
	var err error
	projectID, err = s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var added []*PoolAsset
	for _, raw := range targets {
		t, err := validateOneTarget(raw)
		if err != nil || seen[t] {
			continue
		}
		seen[t] = true
		a := &PoolAsset{Target: t, Source: source, Label: label, ProjectID: projectID}
		if err := s.store.UpsertAsset(ctx, a); err != nil {
			return nil, err
		}
		added = append(added, a)
	}
	return added, nil
}

func (s *Service) DeleteAsset(ctx context.Context, projectID, id string) error {
	projectID, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return err
	}
	return s.store.DeleteAsset(ctx, projectID, id)
}

// --- Projects (asset-pool scope) ---

func (s *Service) ListProjects(ctx context.Context) ([]*Project, error) {
	return s.store.ListProjects(ctx)
}

// CreateProject registers (or renames) a project. ID defaults to a slug of the
// name; a blank slug (e.g. an all-punctuation name) falls back to a generated
// id. The returned Project carries the resolved id the client should switch to.
func (s *Service) CreateProject(ctx context.Context, req ProjectRequest) (*Project, error) {
	name := strings.TrimSpace(req.Name)
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = slugify(name)
	}
	if id == "" {
		id = generateID()
	}
	if name == "" {
		name = id
	}
	p := &Project{ID: id, Name: name, CreatedAt: time.Now()}
	if err := s.store.CreateProject(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// DeleteProject removes a project and its entire asset pool. The default project
// is permanent — it holds the pre-project rows and is the fallback scope for
// every unscoped operation — so deleting it is refused.
func (s *Service) DeleteProject(ctx context.Context, id string) error {
	id = normalizeProjectID(id)
	if id == DefaultProjectID {
		return fmt.Errorf("the default project cannot be deleted")
	}
	ok, err := s.store.ProjectExists(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("project %q not found", id)
	}
	return s.store.DeleteProject(ctx, id)
}

func normalizeProjectID(projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return DefaultProjectID
	}
	return projectID
}

func (s *Service) resolveProjectID(ctx context.Context, projectID string) (string, error) {
	projectID = normalizeProjectID(projectID)
	if s == nil || s.store == nil {
		return projectID, nil
	}
	ok, err := s.store.ProjectExists(ctx, projectID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("project %q not found", projectID)
	}
	return projectID, nil
}

// slugify converts a display name into a compact, url/space-safe id: lowercase
// ASCII alphanumerics and CJK ideographs are kept, every other run collapses to
// a single hyphen. Returns "" when nothing usable remains.
func slugify(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r >= 0x4e00 && r <= 0x9fff:
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// ingestScanAssets folds a completed scan's structured assets into the shared
// pool so anything the scanner confirmed becomes a first-class, re-testable
// target. Best-effort: pool errors never fail a scan.
func (s *Service) ingestScanAssets(ctx context.Context, job *ScanJob, result *output.Result) {
	if s == nil || s.store == nil || job == nil || result == nil {
		return
	}
	project := job.Project
	if project == "" {
		project = DefaultProjectID
	}
	for _, asset := range result.Assets {
		target, err := validateOneTarget(asset.Target)
		if err != nil {
			continue
		}
		services := 0
		for _, item := range asset.Items {
			if item.Kind == output.AssetItemService {
				services++
			}
		}
		_ = s.store.UpsertAsset(ctx, &PoolAsset{
			ProjectID:  project,
			Target:     target,
			Source:     AssetSourceScan,
			Status:     asset.Status,
			Label:      asset.Title,
			Services:   services,
			LastScanID: job.ID,
		})
	}
}

func (s *Service) broadcastScanComplete(scanID string, result *output.Result) {
	s.mu.Lock()
	sid, ok := s.taskSessions[scanID]
	s.mu.Unlock()
	if !ok {
		return
	}
	if s.finishSessionTask(scanID) {
		return
	}
	s.BroadcastChatEvent(sid, ChatEvent{
		Type:   ChatEventScanComplete,
		ScanID: scanID,
		Result: result,
	})
}

func (s *Service) persistAssistantMessage(sessionID, agentID, agentName, content string, turn int) {
	content = strings.TrimRight(content, " \t\r\n")
	now := time.Now()
	msg := &ChatMessage{
		ID:        generateID(),
		SessionID: sessionID,
		Role:      "assistant",
		AgentID:   agentID,
		AgentName: agentName,
		Content:   content,
		CreatedAt: now,
	}
	if turn > 0 {
		if data, err := json.Marshal(map[string]any{"turn": turn}); err == nil {
			msg.Metadata = data
		}
	}
	_ = s.store.AddMessage(context.Background(), msg)
	s.BroadcastChatEvent(sessionID, ChatEvent{
		Type:      ChatEventMessage,
		MessageID: msg.ID,
		Role:      "assistant",
		AgentID:   agentID,
		AgentName: agentName,
		Turn:      turn,
		Content:   content,
	})
}

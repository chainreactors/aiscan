package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/resources"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/scan"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	"github.com/chainreactors/aiscan/skills"
	ioaclient "github.com/chainreactors/ioa/client"

	// Register scanner command factories with the unified command registry.
	_ "github.com/chainreactors/aiscan/pkg/tools"
)

type Config struct {
	Provider ProviderConfig
	Scanner  ScannerConfig
	Tools    ToolConfig
	IOA      *IOAConfig
	Logger   telemetry.Logger
}

type ProviderConfig struct {
	Enabled  bool
	Config   provider.ProviderConfig
	Optional bool
}

type ScannerConfig struct {
	CyberhubURL       string
	CyberhubKey       string
	CyberhubMode      string
	AIEnabled         bool
	EnableAllAISkills bool
	AITimeout         int
	VerifyMode        string
	Proxy             string
	FofaEmail         string
	FofaKey           string
	HunterToken       string
	HunterAPIKey      string
	ReconProxy        string
	ReconLimit        int
}

type ToolConfig struct {
	Enabled     bool
	BashTimeout int
	TavilyKeys  string // comma-separated Tavily API keys (build-time fallback)
}

type IOAConfig struct {
	URL           string
	NodeID        string
	NodeName      string
	RegisterTools bool
	AutoRegister  bool
	NodeMeta      map[string]any
}

type App struct {
	Provider         provider.Provider
	ProviderConfig   provider.ProviderConfig
	Commands         *command.CommandRegistry
	Engines          *engine.Set
	Skills           *skills.Store
	SkillDiagnostics []skills.Diagnostic
	IOAClient        ioaclient.API
	IOAStreamClient  ioaclient.StreamAPI
}

func New(ctx context.Context, cfg Config) (*App, error) {
	app := &App{}
	logger := cfg.Logger
	if logger == nil {
		logger = telemetry.NopLogger()
	}

	store, diagnostics := skills.LoadEmbeddedStore()
	app.Skills = store
	app.SkillDiagnostics = diagnostics

	if cfg.Provider.Enabled {
		llmProvider, resolved, err := initProvider(cfg.Provider.Config, logger)
		if err != nil {
			if !cfg.Provider.Optional {
				return nil, err
			}
			logger.Debugf("provider not configured: %s", err)
		} else {
			app.Provider = llmProvider
			app.ProviderConfig = *resolved
		}
	}

	app.Engines = initEngines(ctx, cfg.Scanner, logger)

	app.Commands = initCommandRegistry(app.Engines, cfg.Scanner, cfg.Tools, app.Provider, app.ProviderConfig.Model, app.Skills, logger)

	if cfg.IOA != nil {
		if err := app.InitIOA(ctx, *cfg.IOA); err != nil {
			app.Close()
			return nil, err
		}
	}

	return app, nil
}

func (a *App) Close() {
	if a == nil {
		return
	}
	if a.Commands != nil {
		for _, t := range a.Commands.Tools() {
			if closer, ok := t.(interface{ Close() }); ok {
				closer.Close()
			}
		}
		for _, cmd := range a.Commands.All() {
			if closer, ok := cmd.(interface{ Close() }); ok {
				closer.Close()
			}
		}
	}
	if a.Engines != nil {
		a.Engines.Close()
	}
}

func initProvider(cfg provider.ProviderConfig, logger telemetry.Logger) (provider.Provider, *provider.ProviderConfig, error) {
	resolved, err := provider.Resolve(&cfg)
	if err != nil {
		return nil, nil, err
	}
	logger.Infof("provider init provider=%s model=%s", resolved.Provider, resolved.Model)
	llmProvider, err := provider.NewProviderFromResolved(resolved)
	if err != nil {
		return nil, nil, err
	}
	return llmProvider, resolved, nil
}

func initEngines(ctx context.Context, cfg ScannerConfig, logger telemetry.Logger) *engine.Set {
	engineSet, err := engine.InitWithOptions(ctx, resources.Options{
		CyberhubURL: cfg.CyberhubURL,
		APIKey:      cfg.CyberhubKey,
		Mode:        cfg.CyberhubMode,
		Proxy:       cfg.Proxy,
	}, logger)
	if err != nil {
		logger.Warnf("scanner engines init error=%q action=continue_without_scanners", err)
		return nil
	}
	recon := engine.ReconOptions{
		FofaEmail:    cfg.FofaEmail,
		FofaKey:      cfg.FofaKey,
		HunterToken:  cfg.HunterToken,
		HunterAPIKey: cfg.HunterAPIKey,
		IngressProxy: cfg.ReconProxy,
		Limit:        cfg.ReconLimit,
	}
	engineSet.SetupUncover(recon, logger)
	return engineSet
}

func initCommandRegistry(engineSet *engine.Set, scanCfg ScannerConfig, toolCfg ToolConfig, llmProvider provider.Provider, model string, skillStore *skills.Store, logger telemetry.Logger) *command.CommandRegistry {
	cmdReg := command.NewRegistry()

	workDir, _ := os.Getwd()

	var scanOpts []any
	if scanCfg.AIEnabled && llmProvider != nil {
		p := llmProvider
		scanOpts = append(scanOpts, scan.WithReportFunc(func(ctx context.Context, prompt, systemPrompt, model string, maxTokens int) (string, error) {
			cfg := agent.Config{
				Provider:       p,
				Tools:          cmdReg,
				Model:          model,
				MaxTokens:      maxTokens,
				SystemPrompt:   buildScanAISystemPrompt(cmdReg, skillStore, systemPrompt),
				BeforeToolCall: scanVerifyBeforeToolCall,
				Logger:         logger,
			}
			result, err := cfg.Run(ctx, prompt)
			if err != nil {
				return "", err
			}
			return result.Output, nil
		}))
		scanOpts = append(scanOpts, scan.WithAISkillConfig(scan.AISkillConfig{
			Model:      model,
			Timeout:    scanCfg.AITimeout,
			Workers:    3,
			Enable:     scanCfg.EnableAllAISkills,
			VerifyMode: scanCfg.VerifyMode,
		}))
		scanOpts = append(scanOpts, scan.WithDeepBrowserFunc(func(ctx context.Context, targetURL string) (string, error) {
			return collectDeepBrowserArtifacts(ctx, cmdReg, targetURL, logger)
		}))
		scanOpts = append(scanOpts, scan.WithAgentFunc(func(ctx context.Context, prompt, systemPrompt, model string, maxTokens int) (*scan.AgentRunResult, error) {
			cp := command.NewCheckpointTool()

			verifyReg := command.NewRegistry()
			for _, t := range cmdReg.Tools() {
				verifyReg.RegisterTool(t)
			}
			verifyReg.RegisterTool(cp)

			cfg := agent.Config{
				Provider:            p,
				Tools:               verifyReg,
				Model:               model,
				MaxTokens:           maxTokens,
				SystemPrompt:        buildScanVerifySystemPrompt(systemPrompt),
				BeforeToolCall:      scanVerifyBeforeToolCall,
				Logger:              logger,
				ShouldStopAfterTurn: makeMaxTurnStop(10),
			}
			result, err := cfg.Run(ctx, prompt)
			if err != nil {
				return nil, err
			}
			raw := ""
			if result != nil {
				raw = result.Output
			}

			if r := cp.Result(); r != nil {
				return &scan.AgentRunResult{Raw: raw, Checkpoint: r}, nil
			}
			return &scan.AgentRunResult{Raw: raw}, nil
		}))
	}
	scanOpts = append(scanOpts, scan.WithLogger(logger))

	deps := &command.Deps{
		WorkDir:        workDir,
		BashTimeout:    toolCfg.BashTimeout,
		SkillStore:     skillStore,
		EngineSet:      engineSet,
		ScannerProxy: scanCfg.Proxy,
		ScanOpts:     scanOpts,
		Logger:       logger,
		Model:        model,
		TavilyKeys:   toolCfg.TavilyKeys,
	}
	if engineSet != nil {
		deps.Resources = engineSet.Resources
	}

	command.BuildAll(deps, cmdReg)

	logger.Infof("commands=%s", fmt.Sprintf("%v", cmdReg.Names()))
	return cmdReg
}

func buildScanAISystemPrompt(_ *command.CommandRegistry, _ *skills.Store, skillPrompt string) string {
	if strings.TrimSpace(skillPrompt) != "" {
		return skillPrompt
	}
	return "You are aiscan's scan AI skill agent. Analyze the provided scan finding using your knowledge. Do not call any tools. Return only the requested JSON output."
}

func buildScanVerifySystemPrompt(skillPrompt string) string {
	if strings.TrimSpace(skillPrompt) != "" {
		return skillPrompt
	}
	return "You are aiscan's active verification agent. Probe the target to confirm or deny the finding. " +
		"When you have reached your conclusion, call the checkpoint tool with target, status, title, and content. " +
		"Use labels for severity tags only (e.g. high, critical). " +
		"Do not output raw JSON directly."
}

func collectDeepBrowserArtifacts(ctx context.Context, reg *command.CommandRegistry, targetURL string, logger telemetry.Logger) (string, error) {
	if reg == nil || !reg.Has("playwright") {
		return "", fmt.Errorf("playwright command unavailable; rebuild web with browser tag")
	}
	targetURL = strings.TrimSpace(targetURL)
	if targetURL == "" {
		return "", fmt.Errorf("target URL is empty")
	}

	session := fmt.Sprintf("deep%d", time.Now().UnixNano())
	closed := false
	defer func() {
		if closed {
			return
		}
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = reg.Execute(closeCtx, "playwright close "+session)
	}()

	script := `(()=>JSON.stringify({url:location.href,title:document.title,forms:[...document.forms].map((f,i)=>({i,action:f.action,method:f.method,inputs:[...f.elements].map(e=>({tag:e.tagName,type:e.type,name:e.name,id:e.id,placeholder:e.placeholder}))})),buttons:[...document.querySelectorAll("button,input[type=button],input[type=submit],a")].slice(0,80).map(e=>({tag:e.tagName,text:(e.innerText||e.value||e.getAttribute("aria-label")||"").trim(),href:e.href||"",type:e.type||"",id:e.id||"",name:e.name||""})),scripts:[...document.scripts].map(s=>s.src).filter(Boolean).slice(0,50),localStorage:Object.keys(localStorage),sessionStorage:Object.keys(sessionStorage)}))()`
	steps := []struct {
		name    string
		command string
	}{
		{"open", fmt.Sprintf("playwright open %s --session %s --ttl 0 --op-timeout 8 --record", quoteCommandArg(targetURL), session)},
		{"network-start", "playwright network " + session + " --start"},
		{"reload", "playwright reload " + session},
		{"wait-idle", "playwright wait-for " + session + " --idle"},
		{"url", "playwright url " + session},
		{"discover", "playwright discover " + session},
		{"text-content", "playwright text-content " + session},
		{"storage-links-scripts", fmt.Sprintf("playwright evaluate %s %s", session, quoteCommandArg(script))},
		{"network-dump", "playwright network " + session + " --dump"},
	}

	const stepTimeout = 12 * time.Second
	var sb strings.Builder
	sb.WriteString("Target: ")
	sb.WriteString(targetURL)
	sb.WriteString("\nSession: ")
	sb.WriteString(session)
	sb.WriteString("\n")
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			appendDeepBrowserStep(&sb, step.name, step.command, "", err)
			break
		}
		out, err := executeRegistryCommand(ctx, reg, step.command, stepTimeout)
		appendDeepBrowserStep(&sb, step.name, step.command, out, err)
		if err != nil && logger != nil {
			logger.Debugf("deep browser step=%s error=%q", step.name, err)
		}
		if err != nil {
			break
		}
	}

	closeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	out, err := executeRegistryCommand(closeCtx, reg, "playwright close "+session, 8*time.Second)
	cancel()
	closed = true
	appendDeepBrowserStep(&sb, "close", "playwright close "+session, out, err)

	return truncateDeepBrowserArtifact(sb.String(), 42000), nil
}

func executeRegistryCommand(ctx context.Context, reg *command.CommandRegistry, commandLine string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		return reg.Execute(ctx, commandLine)
	}
	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := reg.Execute(stepCtx, commandLine)
		done <- result{out: out, err: err}
	}()

	select {
	case r := <-done:
		return r.out, r.err
	case <-stepCtx.Done():
		return "", fmt.Errorf("command timed out after %s: %w", timeout, stepCtx.Err())
	}
}

func appendDeepBrowserStep(sb *strings.Builder, name, commandLine, output string, err error) {
	sb.WriteString("\n## ")
	sb.WriteString(name)
	sb.WriteString("\nCommand: `")
	sb.WriteString(commandLine)
	sb.WriteString("`\n")
	if err != nil {
		sb.WriteString("Error: ")
		sb.WriteString(err.Error())
		sb.WriteString("\n")
	}
	output = strings.TrimSpace(output)
	if output != "" {
		sb.WriteString(truncateDeepBrowserArtifact(output, 8000))
		sb.WriteString("\n")
	}
}

func quoteCommandArg(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\r\n'\"\\") {
		return value
	}
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func truncateDeepBrowserArtifact(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + fmt.Sprintf("\n\n[truncated: showing %d of %d bytes]", max, len(value))
}

func makeMaxTurnStop(maxTurns int) func(context.Context, agent.ShouldStopAfterTurnContext) (bool, error) {
	turn := 0
	return func(_ context.Context, _ agent.ShouldStopAfterTurnContext) (bool, error) {
		turn++
		return turn >= maxTurns, nil
	}
}

func scanVerifyBeforeToolCall(_ context.Context, call agent.BeforeToolCallContext) (*agent.BeforeToolCallResult, error) {
	if call.ToolCall.Function.Name != "bash" {
		return nil, nil
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(call.ToolCall.Function.Arguments), &args); err != nil {
		return nil, err
	}
	if !scanVerifyBlocksCommand(args.Command) {
		return nil, nil
	}
	return &agent.BeforeToolCallResult{
		Block:  true,
		Reason: "scan verification may use search (tavily/fetch), but scanner pseudo-commands are blocked to avoid recursive or active scanning",
	}, nil
}

func scanVerifyBlocksCommand(commandLine string) bool {
	tokens, err := command.SplitCommandLine(commandLine)
	if err != nil {
		tokens = strings.Fields(commandLine)
	}
	if len(tokens) == 0 {
		return false
	}
	if isScanVerifyBlockedCommand(tokens[0]) {
		return true
	}
	return strings.EqualFold(tokens[0], "aiscan") && len(tokens) > 1 && isScanVerifyBlockedCommand(tokens[1])
}

func isScanVerifyBlockedCommand(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "scan", "passive", "gogo", "spray", "zombie", "neutron":
		return true
	default:
		return false
	}
}

func (a *App) InitIOA(ctx context.Context, cfg IOAConfig) error {
	client, err := newIOAClient(cfg)
	if err != nil {
		return err
	}
	a.IOAClient = client
	if streamClient, ok := client.(ioaclient.StreamAPI); ok {
		a.IOAStreamClient = streamClient
	}
	if cfg.RegisterTools && a.Commands != nil {
		deps := &command.Deps{
			IOAClient: client,
			NodeName:  cfg.NodeName,
			NodeMeta:  cfg.NodeMeta,
		}
		command.BuildGroup("ioa", deps, a.Commands)
	}
	if cfg.AutoRegister && client != nil && client.NodeID() == "" {
		_, err := client.RegisterNode(ctx, cfg.NodeName, cfg.NodeMeta)
		if err != nil {
			return err
		}
	}
	return nil
}

func newIOAClient(cfg IOAConfig) (ioaclient.API, error) {
	if cfg.URL == "" {
		return nil, nil
	}
	return ioaclient.NewClient(cfg.URL, cfg.NodeID)
}

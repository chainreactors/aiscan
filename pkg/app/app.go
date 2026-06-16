package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/truncate"
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/resources"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/scan"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	"github.com/chainreactors/aiscan/skills"
	ioaclient "github.com/chainreactors/ioa/client"
	"github.com/chainreactors/ioa/protocols"

	// Register scanner command factories with the unified command registry.
	_ "github.com/chainreactors/aiscan/pkg/tools"
)

type Config struct {
	Provider      ProviderConfig
	Scanner       ScannerConfig
	Tools         ToolConfig
	IOA           *IOAConfig
	Logger        telemetry.Logger
	CLISkillPaths []string
}

type ProviderConfig struct {
	Enabled   bool
	Config    agent.ProviderConfig
	Fallbacks []agent.ProviderConfig
	Optional  bool
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
	Space         string
	RegisterTools bool
	AutoRegister  bool
	NodeMeta      map[string]any
}

type App struct {
	Provider           agent.Provider
	ProviderConfig     agent.ProviderConfig
	ProviderFallbacks  []agent.ProviderEntry
	Commands           *commands.CommandRegistry
	Engines            *engine.Set
	Skills             *skills.Store
	SkillDiagnostics   []skills.Diagnostic
	IOAClient          protocols.ClientAPI
	IOAStreamClient    ioaclient.StreamAPI
}

func New(ctx context.Context, cfg Config) (*App, error) {
	app := &App{}
	logger := cfg.Logger
	if logger == nil {
		logger = telemetry.NopLogger()
	}

	store, diagnostics := skills.LoadAll(cfg.CLISkillPaths)
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
		for _, fbCfg := range cfg.Provider.Fallbacks {
			fbProvider, fbResolved, err := initProvider(fbCfg, logger)
			if err != nil {
				logger.Warnf("fallback provider %s init failed: %s", fbCfg.Provider, err)
				continue
			}
			app.ProviderFallbacks = append(app.ProviderFallbacks, agent.ProviderEntry{
				Provider: fbProvider,
				Model:    fbResolved.Model,
			})
			logger.Infof("fallback provider init provider=%s model=%s", fbResolved.Provider, fbResolved.Model)
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

func initProvider(cfg agent.ProviderConfig, logger telemetry.Logger) (agent.Provider, *agent.ProviderConfig, error) {
	resolved, err := agent.ResolveProvider(&cfg)
	if err != nil {
		return nil, nil, err
	}
	logger.Infof("provider init provider=%s model=%s", resolved.Provider, resolved.Model)
	llmProvider, err := agent.NewProviderFromResolved(resolved)
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

func initCommandRegistry(engineSet *engine.Set, scanCfg ScannerConfig, toolCfg ToolConfig, llmProvider agent.Provider, model string, skillStore *skills.Store, logger telemetry.Logger) *commands.CommandRegistry {
	cmdReg := commands.NewRegistry()

	workDir, _ := os.Getwd()

	var scanOpts []any
	if scanCfg.AIEnabled && llmProvider != nil {
		scanOpts = append(scanOpts, scan.WithParent(agent.NewAgent(agent.Config{
			Provider: llmProvider,
			Tools:    cmdReg,
			Model:    model,
			Logger:   logger,
		})))
		scanOpts = append(scanOpts, scan.WithDeepBrowserFunc(func(ctx context.Context, targetURL string) (string, error) {
			return collectDeepBrowserArtifacts(ctx, cmdReg, targetURL, logger)
		}))
	}
	scanOpts = append(scanOpts, scan.WithLogger(logger))

	deps := &commands.Deps{
		WorkDir:      workDir,
		BashTimeout:  toolCfg.BashTimeout,
		SkillStore:   skillStore,
		EngineSet:    engineSet,
		ScannerProxy: scanCfg.Proxy,
		ScanOpts:     scanOpts,
		Logger:       logger,
		Model:        model,
		TavilyKeys:   toolCfg.TavilyKeys,
	}
	if engineSet != nil {
		deps.Resources = engineSet.Resources
	}

	commands.BuildAll(deps, cmdReg)

	logger.Infof("commands=%s", fmt.Sprintf("%v", cmdReg.Names()))
	return cmdReg
}


func collectDeepBrowserArtifacts(ctx context.Context, reg *commands.CommandRegistry, targetURL string, logger telemetry.Logger) (string, error) {
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

	artifact := sb.String()
	if tr := truncate.Head(artifact, truncate.Options{}); tr.Truncated {
		artifact = tr.Content + fmt.Sprintf(
			"\n\n[deep browser truncated: showing %d/%d lines (%s of %s)]",
			tr.OutputLines, tr.TotalLines, truncate.FormatSize(tr.OutputBytes), truncate.FormatSize(tr.TotalBytes))
	}
	return artifact, nil
}

func executeRegistryCommand(ctx context.Context, reg *commands.CommandRegistry, commandLine string, timeout time.Duration) (string, error) {
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
		if tr := truncate.Head(output, truncate.Options{}); tr.Truncated {
			sb.WriteString(tr.Content)
			sb.WriteString(fmt.Sprintf("\n[step truncated: %d/%d lines]", tr.OutputLines, tr.TotalLines))
		} else {
			sb.WriteString(tr.Content)
		}
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
		deps := &commands.Deps{
			IOAClient: client,
			NodeName:  cfg.NodeName,
			NodeMeta:  cfg.NodeMeta,
		}
		commands.BuildGroup("ioa", deps, a.Commands)
	}
	if cfg.AutoRegister && client != nil && client.NodeID() == "" {
		_, err := client.RegisterNode(ctx, cfg.NodeName, "", cfg.NodeMeta)
		if err != nil {
			return err
		}
	}
	// Auto-join the configured space so ioa_send/ioa_read work immediately.
	if cfg.Space != "" && client != nil && client.NodeID() != "" {
		info, err := client.Space(ctx, cfg.Space, "aiscan agent")
		if err == nil {
			a.setIOASpace(info.ID)
		}
	}
	return nil
}

func (a *App) setIOASpace(spaceID string) {
	for _, cmd := range a.Commands.All() {
		if setter, ok := cmd.(interface{ SetDefaultSpace(string) }); ok {
			setter.SetDefaultSpace(spaceID)
		}
	}
}

func newIOAClient(cfg IOAConfig) (protocols.ClientAPI, error) {
	if cfg.URL == "" {
		return nil, nil
	}
	return ioaclient.NewClient(cfg.URL, cfg.NodeID)
}

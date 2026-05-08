package app

import (
	"context"
	"os"

	acpclient "github.com/chainreactors/aiscan/pkg/acp/client"
	"github.com/chainreactors/aiscan/pkg/provider"
	"github.com/chainreactors/aiscan/pkg/scanner"
	"github.com/chainreactors/aiscan/pkg/scanner/engines"
	"github.com/chainreactors/aiscan/pkg/scanner/resources"
	"github.com/chainreactors/aiscan/pkg/scanner/scan"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tool"
	"github.com/chainreactors/aiscan/skills"
)

type Config struct {
	Provider ProviderConfig
	Scanner  ScannerConfig
	Tools    ToolConfig
	ACP      *ACPConfig
	Logger   telemetry.Logger
}

type ProviderConfig struct {
	Enabled  bool
	Config   provider.ProviderConfig
	Optional bool
}

type ScannerConfig struct {
	CyberhubURL         string
	CyberhubKey         string
	CyberhubMode        string
	VerificationEnabled bool
	VerifyMinPriority   string
	VerifyMaxTurns      int
	VerifyTimeout       int
	VerifySystemPrompt  string
}

type ToolConfig struct {
	Enabled     bool
	BashTimeout int
}

type ACPConfig struct {
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
	Scanner          *scanner.ScannerRegistry
	Engines          *engines.Set
	Tools            *tool.ToolRegistry
	Skills           *skills.Store
	SkillDiagnostics []skills.Diagnostic
	ACPClient        acpclient.API
	ACPStreamClient  acpclient.StreamAPI
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

	app.Scanner, app.Engines = initScannerRegistry(ctx, cfg.Scanner, app.Provider, app.ProviderConfig.Model, logger)

	if cfg.Tools.Enabled {
		app.Tools = initToolRegistry(cfg.Tools, app.Skills, app.Scanner)
	}

	if cfg.ACP != nil {
		if err := app.InitACP(ctx, *cfg.ACP); err != nil {
			app.Close()
			return nil, err
		}
	}

	return app, nil
}

func (a *App) Close() {
	if a != nil && a.Engines != nil {
		a.Engines.Close()
	}
}

func initProvider(cfg provider.ProviderConfig, logger telemetry.Logger) (provider.Provider, *provider.ProviderConfig, error) {
	resolved, err := provider.Resolve(&cfg)
	if err != nil {
		return nil, nil, err
	}
	logger.Infof("provider init provider=%s model=%s", resolved.Provider, resolved.Model)
	llmProvider, err := provider.NewProvider(resolved)
	if err != nil {
		return nil, nil, err
	}
	return llmProvider, resolved, nil
}

func initScannerRegistry(ctx context.Context, cfg ScannerConfig, llmProvider provider.Provider, model string, logger telemetry.Logger) (*scanner.ScannerRegistry, *engines.Set) {
	scannerReg := scanner.NewScannerRegistry()
	engineSet, err := engines.InitWithOptions(ctx, resources.Options{
		CyberhubURL: cfg.CyberhubURL,
		APIKey:      cfg.CyberhubKey,
		Mode:        cfg.CyberhubMode,
	}, logger)
	if err != nil {
		logger.Warnf("scanner engines init error=%q action=continue_without_scanners", err)
		return scannerReg, nil
	}
	var opts []scan.Option
	if cfg.VerificationEnabled && llmProvider != nil {
		opts = append(opts, scan.WithVerificationConfig(scanVerificationConfig(cfg, llmProvider, model)))
	}
	opts = append(opts, scan.WithLogger(logger))
	scanner.RegisterAllWithLogger(scannerReg, engineSet, logger, opts...)
	return scannerReg, engineSet
}

func initToolRegistry(cfg ToolConfig, skillStore *skills.Store, scannerReg *scanner.ScannerRegistry) *tool.ToolRegistry {
	workDir, _ := os.Getwd()
	timeout := cfg.BashTimeout
	if timeout <= 0 {
		timeout = 300
	}
	toolReg := tool.NewToolRegistry()
	toolReg.Register(tool.NewReadTool(workDir, skillStore))
	toolReg.Register(tool.NewWriteTool(workDir))
	toolReg.Register(tool.NewGlobTool(workDir))
	toolReg.Register(tool.NewBashTool(workDir, timeout, scannerReg))
	return toolReg
}

func (a *App) InitACP(ctx context.Context, cfg ACPConfig) error {
	client, err := newACPClient(cfg)
	if err != nil {
		return err
	}
	a.ACPClient = client
	if streamClient, ok := client.(acpclient.StreamAPI); ok {
		a.ACPStreamClient = streamClient
	}
	if cfg.RegisterTools && a.Tools != nil {
		for _, acpTool := range acpclient.NewTools(client, acpclient.ToolOptions{NodeName: cfg.NodeName, NodeMeta: cfg.NodeMeta}) {
			a.Tools.Register(acpTool)
		}
	}
	if cfg.AutoRegister && client.NodeID() == "" {
		_, err := client.RegisterNode(ctx, cfg.NodeName, cfg.NodeMeta)
		if err != nil {
			return err
		}
	}
	return nil
}

func newACPClient(cfg ACPConfig) (acpclient.API, error) {
	if cfg.URL == "" {
		return nil, nil
	}
	return acpclient.NewClient(cfg.URL, cfg.NodeID)
}

func scanVerificationConfig(cfg ScannerConfig, llmProvider provider.Provider, model string) scan.VerificationConfig {
	maxTurns := cfg.VerifyMaxTurns
	if maxTurns <= 0 {
		maxTurns = 3
	}
	timeout := cfg.VerifyTimeout
	if timeout <= 0 {
		timeout = 120
	}
	return scan.VerificationConfig{
		Provider:     llmProvider,
		Model:        model,
		Enable:       cfg.VerificationEnabled,
		MinPriority:  cfg.VerifyMinPriority,
		MaxTurns:     maxTurns,
		Timeout:      timeout,
		SystemPrompt: cfg.VerifySystemPrompt,
	}
}

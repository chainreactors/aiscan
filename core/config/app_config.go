package config

import (
	"os"

	"github.com/chainreactors/aiscan/pkg/app"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

type RuntimeFeatures struct {
	ProviderEnabled  bool
	ProviderOptional bool
	ToolsEnabled     bool
	AIEnabled        bool
	ScannerAI        bool
	Warning          string
}

func AppConfig(option *Option, features RuntimeFeatures, logger telemetry.Logger) app.Config {
	return app.Config{
		Provider: app.ProviderConfig{
			Enabled:  features.ProviderEnabled,
			Config:   ProviderConfig(option),
			Optional: features.ProviderOptional,
		},
		Scanner: app.ScannerConfig{
			CyberhubURL:       option.CyberhubURL,
			CyberhubKey:       option.CyberhubKey,
			CyberhubMode:      option.CyberhubMode,
			CyberhubDraft:     option.CyberhubDraft,
			AIEnabled:         features.AIEnabled,
			EnableAllAISkills: option.AI,
			AITimeout:         DefaultInt(DefaultVerifyTimeout, 120),
			VerifyMode:        DefaultVerify,
			Proxy:             option.Proxy,
			FofaEmail:         ResolveString(option.FofaEmail, os.Getenv("FOFA_EMAIL")),
			FofaKey:           ResolveString(option.FofaKey, os.Getenv("FOFA_KEY")),
			HunterToken:       ResolveString(option.HunterToken, os.Getenv("HUNTER_TOKEN")),
			HunterAPIKey:      ResolveString(option.HunterAPIKey, os.Getenv("HUNTER_API_KEY")),
			ReconProxy:        ResolveString(option.ReconProxy, os.Getenv("RECON_PROXY")),
			ReconLimit:        intOptionValue(option.ReconLimit),
		},
		Tools: app.ToolConfig{
			Enabled:     features.ToolsEnabled,
			BashTimeout: 300,
			TavilyKeys:  DefaultTavilyKeys,
		},
		Logger: logger,
	}
}

func intOptionValue(p *int) int {
	if p != nil {
		return *p
	}
	return 0
}

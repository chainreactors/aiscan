package config

import (
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
			AIEnabled:         features.AIEnabled,
			EnableAllAISkills: option.AI,
			AITimeout:         DefaultInt(DefaultVerifyTimeout, 120),
			VerifyMode:        DefaultVerify,
			Proxy:             option.Proxy,
			FofaEmail:         option.FofaEmail,
			FofaKey:           option.FofaKey,
			HunterToken:       option.HunterToken,
			HunterAPIKey:      option.HunterAPIKey,
			ReconProxy:        option.ReconProxy,
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

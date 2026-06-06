package config

import "github.com/chainreactors/aiscan/pkg/agent"

var (
	DefaultProvider = "deepseek"
	DefaultBaseURL  = ""
	DefaultAPIKey   = ""
	DefaultModel    = "deepseek-v4-pro"

	DefaultScannerProxy = ""

	DefaultCyberhubURL  = ""
	DefaultCyberhubKey  = ""
	DefaultCyberhubMode = "merge"

	DefaultVerify        = "auto"
	DefaultVerifyTimeout = ""

	DefaultIOAURL      = ""
	DefaultIOANodeID   = ""
	DefaultIOANodeName = ""
	DefaultSpace       = ""

	DefaultTavilyKeys = ""
)

func defaultProviderConfig() agent.ProviderConfig {
	return agent.ProviderConfig{
		Provider: DefaultProvider,
		BaseURL:  DefaultBaseURL,
		APIKey:   DefaultAPIKey,
		Model:    DefaultModel,
	}
}

func ProviderConfig(option *Option) agent.ProviderConfig {
	cfg := defaultProviderConfig()
	if option.Provider != "" {
		cfg.Provider = option.Provider
	}
	if option.BaseURL != "" {
		cfg.BaseURL = option.BaseURL
		if option.Provider == "" {
			cfg.Provider = ""
		}
	}
	if option.APIKey != "" {
		cfg.APIKey = option.APIKey
	}
	if option.Model != "" {
		cfg.Model = option.Model
	}
	if option.LLMProxy != "" {
		cfg.Proxy = option.LLMProxy
	}
	cfg.Timeout = 120
	return cfg
}

func ApplyResolvedProviderOptions(option *Option, cfg agent.ProviderConfig) {
	option.Provider = cfg.Provider
	option.BaseURL = cfg.BaseURL
	option.APIKey = cfg.APIKey
	option.Model = cfg.Model
}

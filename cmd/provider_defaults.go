package cmd

import "github.com/chainreactors/aiscan/pkg/provider"

var (
	DefaultProvider = "openai"
	DefaultBaseURL  = ""
	DefaultAPIKey   = ""
	DefaultModel    = "gpt-4o"
	DefaultProxy    = ""

	DefaultCyberhubURL  = ""
	DefaultCyberhubKey  = ""
	DefaultCyberhubMode = "merge"

	DefaultVerify        = "auto"
	DefaultVerifyTimeout = ""

	DefaultACPURL      = ""
	DefaultACPNodeID   = ""
	DefaultACPNodeName = ""
	DefaultSpace       = ""
)

func defaultProviderConfig() provider.ProviderConfig {
	return provider.ProviderConfig{
		Provider: DefaultProvider,
		BaseURL:  DefaultBaseURL,
		APIKey:   DefaultAPIKey,
		Model:    DefaultModel,
		Proxy:    DefaultProxy,
	}
}

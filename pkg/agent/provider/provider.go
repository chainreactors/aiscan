package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type Provider interface {
	Name() string
	ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error)
	WebSearch(ctx context.Context, query string, maxUses int) (*WebSearchResponse, error)
}

type WebSearchResult struct {
	Title string
	URL   string
}

type WebSearchResponse struct {
	Results []WebSearchResult
	Summary string
}

const (
	defaultWebSearchMaxUses = 5
	maxWebSearchMaxUses     = 10
)

func normalizeWebSearchMaxUses(maxUses int) int {
	if maxUses <= 0 {
		return defaultWebSearchMaxUses
	}
	if maxUses > maxWebSearchMaxUses {
		return maxWebSearchMaxUses
	}
	return maxUses
}

type StreamingProvider interface {
	Provider
	ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan ChatCompletionStreamEvent, error)
}

type ProviderConfig struct {
	Provider string   `yaml:"provider" config:"provider"`
	BaseURL  string   `yaml:"base_url" config:"base_url"`
	APIKey   string   `yaml:"api_key"  config:"api_key"`
	APIKeys  []string `yaml:"api_keys" config:"api_keys"`
	Model    string   `yaml:"model"    config:"model"`
	Proxy    string   `yaml:"proxy"    config:"proxy"`
	Timeout  int      `yaml:"timeout"  config:"timeout"`
}

type providerPreset struct {
	BaseURL   string
	APIKeyEnv string
}

var presets = map[string]providerPreset{
	"openai":     {"https://api.openai.com/v1", "OPENAI_API_KEY"},
	"openrouter": {"https://openrouter.ai/api/v1", "OPENROUTER_API_KEY"},
	"deepseek":   {"https://api.deepseek.com/v1", "DEEPSEEK_API_KEY"},
	"groq":       {"https://api.groq.com/openai/v1", "GROQ_API_KEY"},
	"moonshot":   {"https://api.moonshot.cn/v1", "MOONSHOT_API_KEY"},
	"ollama":     {"http://localhost:11434/v1", ""},
	"anthropic":  {"https://api.anthropic.com/v1", "ANTHROPIC_API_KEY"},
}

func KnownProviders() []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func APIKeyEnvName(providerName string) string {
	preset, ok := presets[strings.ToLower(strings.TrimSpace(providerName))]
	if !ok {
		return ""
	}
	return preset.APIKeyEnv
}

func Resolve(cfg *ProviderConfig) (*ProviderConfig, error) {
	resolved := *cfg

	if resolved.Provider == "" {
		resolved.Provider = InferFromBaseURL(resolved.BaseURL)
		if resolved.Provider == "" {
			resolved.Provider = "openai"
		}
	}

	providerName := strings.ToLower(resolved.Provider)

	if resolved.BaseURL == "" {
		if preset, ok := presets[providerName]; ok {
			resolved.BaseURL = preset.BaseURL
		} else {
			return nil, fmt.Errorf("unknown provider %q and no base URL specified", resolved.Provider)
		}
	}

	keys := normalizeAPIKeys(resolved.APIKey, resolved.APIKeys)
	if len(keys) > 0 {
		resolved.APIKeys = keys
		if resolved.APIKey == "" {
			resolved.APIKey = keys[0]
		}
	}

	if len(keys) == 0 {
		if providerName != "ollama" {
			if envName := APIKeyEnvName(providerName); envName != "" {
				return nil, fmt.Errorf("no API key for provider %q: set --api-key/--api-keys, llm.api_key/llm.api_keys, %s, AISCAN_API_KEY, or AISCAN_API_KEYS",
					resolved.Provider, envName)
			}
			return nil, fmt.Errorf("no API key for provider %q: set --api-key/--api-keys, llm.api_key/llm.api_keys, AISCAN_API_KEY, or AISCAN_API_KEYS",
				resolved.Provider)
		}
	}

	if resolved.Timeout <= 0 {
		resolved.Timeout = 120
	}

	return &resolved, nil
}

func NewProvider(cfg *ProviderConfig) (Provider, error) {
	resolved, err := Resolve(cfg)
	if err != nil {
		return nil, err
	}
	return NewProviderFromResolved(resolved)
}

func InferFromBaseURL(baseURL string) string {
	baseURL = strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case strings.Contains(baseURL, "api.openai.com"):
		return "openai"
	case strings.Contains(baseURL, "api.anthropic.com"):
		return "anthropic"
	case strings.Contains(baseURL, "deepseek.com"):
		return "deepseek"
	case strings.Contains(baseURL, "openrouter.ai"):
		return "openrouter"
	case strings.Contains(baseURL, "groq.com"):
		return "groq"
	case strings.Contains(baseURL, "moonshot.cn"):
		return "moonshot"
	case strings.Contains(baseURL, "localhost:11434"), strings.Contains(baseURL, "127.0.0.1:11434"):
		return "ollama"
	default:
		return ""
	}
}

func NewProviderFromResolved(cfg *ProviderConfig) (Provider, error) {
	if strings.ToLower(cfg.Provider) == "anthropic" {
		return NewAnthropicProvider(cfg)
	}
	return NewOpenAIProvider(cfg)
}

package probe

import (
	"context"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent"
)

// LLMTestRequest carries the connection parameters the user wants to verify.
// It mirrors the LLM section of webproto.DistributeConfig. An empty APIKey
// means "use the key already stored in the config" (matching the settings UI
// where a configured key is left blank to keep it unchanged).
type LLMTestRequest struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
	Proxy    string `json:"proxy"`
}

// LLMTestResult reports whether a probe request reached the provider and
// returned a usable completion.
type LLMTestResult struct {
	OK        bool   `json:"ok"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	LatencyMs int64  `json:"latency_ms"`
	Reply     string `json:"reply,omitempty"`
	Error     string `json:"error,omitempty"`
}

// llmProbeTimeout bounds a single connectivity test so a misconfigured or
// unreachable endpoint fails fast instead of hanging the settings dialog.
const llmProbeTimeout = 30 * time.Second

// TestLLM issues a minimal chat completion against the supplied LLM settings
// and reports the outcome. It never returns a transport error to the caller —
// failures are captured inside LLMTestResult so the UI can render them. A nil
// error only signals the request was well-formed enough to attempt. When
// req.APIKey is blank, storedAPIKey is used (the settings UI leaves a configured
// key blank to keep it unchanged).
func TestLLM(ctx context.Context, req LLMTestRequest, storedAPIKey string) (LLMTestResult, error) {
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(storedAPIKey)
	}

	cfg := agent.ProviderConfig{
		Provider: strings.TrimSpace(req.Provider),
		BaseURL:  strings.TrimSpace(req.BaseURL),
		APIKey:   apiKey,
		Model:    strings.TrimSpace(req.Model),
		Proxy:    strings.TrimSpace(req.Proxy),
		Timeout:  int(llmProbeTimeout / time.Second),
	}

	result := LLMTestResult{Provider: cfg.Provider, Model: cfg.Model}

	if cfg.Model == "" {
		result.Error = "model is required"
		return result, nil
	}

	prov, err := agent.NewProvider(&cfg)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, llmProbeTimeout)
	defer cancel()

	maxTokens := 16
	start := time.Now()
	resp, err := prov.ChatCompletion(probeCtx, &agent.ChatCompletionRequest{
		Model:     cfg.Model,
		Messages:  []agent.ChatMessage{agent.NewTextMessage("user", "ping")},
		MaxTokens: maxTokens,
	})
	result.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	if len(resp.Choices) == 0 {
		result.Error = "provider returned no choices"
		return result, nil
	}

	result.OK = true
	if msg := resp.Choices[0].Message; msg.Content != nil {
		result.Reply = strings.TrimSpace(*msg.Content)
	}
	return result, nil
}

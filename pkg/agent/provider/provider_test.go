package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolveUsesBaseURL(t *testing.T) {
	cfg, err := Resolve(&ProviderConfig{
		Provider: "ollama",
		BaseURL:  "http://localhost:11434/v1",
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
}

func TestResolvePreservesExplicitBaseURL(t *testing.T) {
	cfg, err := Resolve(&ProviderConfig{
		Provider: "ollama",
		BaseURL:  "http://base-url.example/v1",
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.BaseURL != "http://base-url.example/v1" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
}

func TestResolveNormalizesCustomHeaders(t *testing.T) {
	cfg, err := Resolve(&ProviderConfig{
		Provider: "openai",
		APIKey:   "test-key",
		Headers: map[string]string{
			"user-agent": "Version: 5.10.0 openwarp",
			"X-Empty":    "",
		},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got := cfg.Headers["User-Agent"]; got != "Version: 5.10.0 openwarp" {
		t.Fatalf("User-Agent = %q", got)
	}
	if _, ok := cfg.Headers["X-Empty"]; ok {
		t.Fatalf("empty header should be ignored: %#v", cfg.Headers)
	}
}

func TestResolveRejectsInvalidCustomHeaders(t *testing.T) {
	tests := []map[string]string{
		{"User Agent": "bad"},
		{"X-Test": "bad\nvalue"},
		{"User-Agent": "one", "user-agent": "two"},
	}
	for _, headers := range tests {
		if _, err := Resolve(&ProviderConfig{Provider: "openai", APIKey: "test-key", Headers: headers}); err == nil {
			t.Fatalf("Resolve() error = nil for headers %#v", headers)
		}
	}
}

func TestInferFromBaseURLDefaultsToOpenAI(t *testing.T) {
	for _, baseURL := range []string{
		"https://api.openai.com/v1",
		"https://api.deepseek.com/v1",
		"https://openrouter.ai/api/v1",
		"http://localhost:11434/v1",
		"https://llm.example.com/v1",
	} {
		if got := InferFromBaseURL(baseURL); got != "openai" {
			t.Fatalf("InferFromBaseURL(%q) = %q, want openai", baseURL, got)
		}
	}
}

func TestResolveExplicitProvider(t *testing.T) {
	cfg, err := Resolve(&ProviderConfig{
		Provider: "anthropic",
		BaseURL:  "https://my-proxy.example.com/v1",
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.Provider != "anthropic" {
		t.Fatalf("Provider = %q, want anthropic", cfg.Provider)
	}
}

func TestNewProviderExplicitAnthropic(t *testing.T) {
	p, err := NewProvider(&ProviderConfig{
		Provider: "anthropic",
		BaseURL:  "https://my-proxy.example.com/v1",
		APIKey:   "test-key",
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if _, ok := p.(*AnthropicProvider); !ok {
		t.Fatalf("provider type = %T, want *AnthropicProvider", p)
	}
}

func TestAnthropicProviderChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Fatal("missing anthropic-version header")
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization header = %q, want empty", got)
		}

		var body struct {
			Model     string `json:"model"`
			System    string `json:"system"`
			MaxTokens int    `json:"max_tokens"`
			Tools     []struct {
				Type        string                 `json:"type"`
				Name        string                 `json:"name"`
				InputSchema map[string]interface{} `json:"input_schema"`
			} `json:"tools"`
			Messages []struct {
				Role    string                   `json:"role"`
				Content []map[string]interface{} `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "claude-test" {
			t.Fatalf("model = %q, want claude-test", body.Model)
		}
		if body.System != "system prompt" {
			t.Fatalf("system = %q, want system prompt", body.System)
		}
		if body.MaxTokens != defaultAnthropicMaxToken {
			t.Fatalf("max_tokens = %d, want %d", body.MaxTokens, defaultAnthropicMaxToken)
		}
		if len(body.Tools) != 1 || body.Tools[0].Name != "bash" {
			t.Fatalf("tools = %#v, want bash tool", body.Tools)
		}
		if len(body.Messages) != 1 || body.Messages[0].Role != "user" {
			t.Fatalf("messages = %#v, want one user message", body.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"scan ready"},{"type":"tool_use","id":"toolu_1","name":"bash","input":{"command":"id"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer server.Close()

	p, err := NewAnthropicProvider(&ProviderConfig{
		Provider: "anthropic",
		BaseURL:  server.URL + "/v1",
		APIKey:   "test-key",
		Timeout:  5,
	})
	if err != nil {
		t.Fatalf("NewAnthropicProvider() error = %v", err)
	}

	resp, err := p.ChatCompletion(context.Background(), &ChatCompletionRequest{
		Model: "claude-test",
		Messages: []ChatMessage{
			NewTextMessage("system", "system prompt"),
			NewTextMessage("user", "scan localhost"),
		},
		Tools: []ToolDefinition{{
			Type: "function",
			Function: FunctionDefinition{
				Name: "bash",
				Parameters: map[string]interface{}{
					"type": "object",
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	msg := resp.Choices[0].Message
	if msg.Role != "assistant" || msg.Content == nil || *msg.Content != "scan ready" {
		t.Fatalf("message = %#v, want assistant text", msg)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(msg.ToolCalls))
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != `{"command":"id"}` {
		t.Fatalf("tool arguments = %q, want command JSON", got)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 15 {
		t.Fatalf("usage = %#v, want total 15", resp.Usage)
	}
}

func TestAnthropicProviderParsesThinkingBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"thinking","thinking":"internal reasoning"},{"type":"text","text":"visible answer"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":20}}`)
	}))
	defer server.Close()

	p, err := NewAnthropicProvider(&ProviderConfig{
		Provider: "anthropic",
		BaseURL:  server.URL + "/v1",
		APIKey:   "test-key",
		Timeout:  5,
	})
	if err != nil {
		t.Fatalf("NewAnthropicProvider() error = %v", err)
	}

	resp, err := p.ChatCompletion(context.Background(), &ChatCompletionRequest{
		Model:    "claude-test",
		Messages: []ChatMessage{NewTextMessage("user", "think hard")},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	msg := resp.Choices[0].Message
	if msg.Content == nil || *msg.Content != "visible answer" {
		t.Fatalf("content = %v, want 'visible answer'", msg.Content)
	}
	if msg.ReasoningContent == nil || *msg.ReasoningContent != "internal reasoning" {
		t.Fatalf("reasoning = %v, want 'internal reasoning'", msg.ReasoningContent)
	}
}

func TestOpenAIProviderChatCompletionStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":""}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"reasoning_content":"think"},"finish_reason":""}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hel"},"finish_reason":""}]}`)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer server.Close()

	p, err := NewOpenAIProvider(&ProviderConfig{
		Provider: "test",
		BaseURL:  server.URL + "/v1",
		Timeout:  5,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	ch, err := p.ChatCompletionStream(context.Background(), &ChatCompletionRequest{Model: "test"})
	if err != nil {
		t.Fatalf("ChatCompletionStream() error = %v", err)
	}
	var text string
	var reasoning string
	var done bool
	for event := range ch {
		if event.Err != nil {
			t.Fatalf("stream error = %v", event.Err)
		}
		if event.Delta.Content != nil {
			text += *event.Delta.Content
		}
		if event.Delta.ReasoningContent != nil {
			reasoning += *event.Delta.ReasoningContent
		}
		if event.Done {
			done = true
		}
	}
	if text != "hello" {
		t.Fatalf("text = %q, want hello", text)
	}
	if reasoning != "think" {
		t.Fatalf("reasoning = %q, want think", reasoning)
	}
	if !done {
		t.Fatal("missing done event")
	}
}

func TestOpenAIProviderCustomHeadersForAllRequests(t *testing.T) {
	const userAgent = "Version: 5.10.0 openwarp"
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Fatalf("User-Agent = %q, want %q", got, userAgent)
		}
		if got := r.Header.Get("X-Test"); got != "yes" {
			t.Fatalf("X-Test = %q, want yes", got)
		}

		switch {
		case r.URL.Path == "/v1/chat/completions" && r.Header.Get("Accept") == "text/event-stream":
			seen["stream"] = true
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`)
			fmt.Fprintln(w, `data: [DONE]`)
		case r.URL.Path == "/v1/chat/completions":
			seen["chat"] = true
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
		case r.URL.Path == "/v1/responses":
			seen["websearch"] = true
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)
		default:
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	p, err := NewOpenAIProvider(&ProviderConfig{
		Provider: "openai",
		BaseURL:  server.URL + "/v1",
		Headers: map[string]string{
			"User-Agent": userAgent,
			"X-Test":     "yes",
		},
		Timeout: 5,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	if _, err := p.ChatCompletion(context.Background(), &ChatCompletionRequest{Model: "test"}); err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	ch, err := p.ChatCompletionStream(context.Background(), &ChatCompletionRequest{Model: "test"})
	if err != nil {
		t.Fatalf("ChatCompletionStream() error = %v", err)
	}
	for event := range ch {
		if event.Err != nil {
			t.Fatalf("stream error = %v", event.Err)
		}
	}
	if _, err := p.WebSearch(context.Background(), "query", 1); err != nil {
		t.Fatalf("WebSearch() error = %v", err)
	}
	for _, name := range []string{"chat", "stream", "websearch"} {
		if !seen[name] {
			t.Fatalf("missing %s request", name)
		}
	}
}

func TestAnthropicProviderChatCompletionStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "text/event-stream" {
			t.Fatalf("Accept = %q, want text/event-stream", got)
		}
		var body struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !body.Stream {
			t.Fatal("stream = false, want true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\n")
		fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"role\":\"assistant\",\"usage\":{\"input_tokens\":7}}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")
		fmt.Fprint(w, "event: content_block_start\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"bash\",\"input\":{}}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"command\\\":\\\"\"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"id\\\"}\"}}\n\n")
		fmt.Fprint(w, "event: message_delta\n")
		fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":5}}\n\n")
		fmt.Fprint(w, "event: message_stop\n")
		fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer server.Close()

	p, err := NewAnthropicProvider(&ProviderConfig{
		Provider: "anthropic",
		BaseURL:  server.URL + "/v1",
		APIKey:   "test-key",
		Timeout:  5,
	})
	if err != nil {
		t.Fatalf("NewAnthropicProvider() error = %v", err)
	}

	ch, err := p.ChatCompletionStream(context.Background(), &ChatCompletionRequest{
		Model:    "claude-test",
		Messages: []ChatMessage{NewTextMessage("user", "scan localhost")},
	})
	if err != nil {
		t.Fatalf("ChatCompletionStream() error = %v", err)
	}

	var role string
	var text string
	var done bool
	var finishReason string
	var usage *Usage
	toolCalls := make(map[int]ToolCall)
	for event := range ch {
		if event.Err != nil {
			t.Fatalf("stream error = %v", event.Err)
		}
		if event.Delta.Role != "" {
			role = event.Delta.Role
		}
		if event.Delta.Content != nil {
			text += *event.Delta.Content
		}
		for _, delta := range event.Delta.ToolCalls {
			tc := toolCalls[delta.Index]
			if delta.ID != "" {
				tc.ID = delta.ID
			}
			if delta.Type != "" {
				tc.Type = delta.Type
			}
			if delta.Function.Name != "" {
				tc.Function.Name = delta.Function.Name
			}
			if delta.Function.Arguments != "" {
				tc.Function.Arguments += delta.Function.Arguments
			}
			toolCalls[delta.Index] = tc
		}
		if event.FinishReason != "" {
			finishReason = event.FinishReason
		}
		if event.Usage != nil {
			usage = event.Usage
		}
		if event.Done {
			done = true
		}
	}
	if role != "assistant" {
		t.Fatalf("role = %q, want assistant", role)
	}
	if text != "hi" {
		t.Fatalf("text = %q, want hi", text)
	}
	if finishReason != "tool_calls" {
		t.Fatalf("finish reason = %q, want tool_calls", finishReason)
	}
	tc := toolCalls[1]
	if tc.ID != "toolu_1" || tc.Type != "function" || tc.Function.Name != "bash" {
		t.Fatalf("tool call = %#v, want bash tool call", tc)
	}
	if tc.Function.Arguments != `{"command":"id"}` {
		t.Fatalf("tool call arguments = %q, want command JSON", tc.Function.Arguments)
	}
	if usage == nil || usage.TotalTokens != 12 {
		t.Fatalf("usage = %#v, want total 12", usage)
	}
	if !done {
		t.Fatal("missing done event")
	}
}

func TestAnthropicProviderCustomHeadersPreserveProviderHeaders(t *testing.T) {
	const userAgent = "Version: 5.10.0 openwarp"
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != userAgent {
			t.Fatalf("User-Agent = %q, want %q", got, userAgent)
		}
		if got := r.Header.Get("X-Test"); got != "yes" {
			t.Fatalf("X-Test = %q, want yes", got)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("x-api-key = %q, want test-key", got)
		}
		if got := r.Header.Get("anthropic-version"); got != anthropicVersion {
			t.Fatalf("anthropic-version = %q, want %q", got, anthropicVersion)
		}

		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("anthropic-beta") != "" {
			seen["websearch"] = true
			fmt.Fprint(w, `{"content":[{"type":"text","text":"ok"}]}`)
			return
		}
		seen["chat"] = true
		fmt.Fprint(w, `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	p, err := NewAnthropicProvider(&ProviderConfig{
		Provider: "anthropic",
		BaseURL:  server.URL + "/v1",
		APIKey:   "test-key",
		Headers: map[string]string{
			"User-Agent": userAgent,
			"X-Test":     "yes",
		},
		Timeout: 5,
	})
	if err != nil {
		t.Fatalf("NewAnthropicProvider() error = %v", err)
	}

	if _, err := p.ChatCompletion(context.Background(), &ChatCompletionRequest{
		Model:    "claude-test",
		Messages: []ChatMessage{NewTextMessage("user", "hello")},
	}); err != nil {
		t.Fatalf("ChatCompletion() error = %v", err)
	}
	if _, err := p.WebSearch(context.Background(), "query", 1); err != nil {
		t.Fatalf("WebSearch() error = %v", err)
	}
	for _, name := range []string{"chat", "websearch"} {
		if !seen[name] {
			t.Fatalf("missing %s request", name)
		}
	}
}

func TestOpenAIProviderChatCompletionBodyTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":`)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	p, err := NewOpenAIProvider(&ProviderConfig{
		Provider: "test",
		BaseURL:  server.URL + "/v1",
		Timeout:  1,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	start := time.Now()
	_, err = p.ChatCompletion(context.Background(), &ChatCompletionRequest{Model: "test"})
	if err == nil {
		t.Fatal("ChatCompletion() error = nil, want timeout")
	}
	if !errors.Is(err, ErrCallTimeout) {
		t.Fatalf("ChatCompletion() error = %v, want ErrCallTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("ChatCompletion() took %s, want timeout near 1s", elapsed)
	}
}

func TestOpenAIProviderChatCompletionStreamErrorBodyTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "partial error")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	p, err := NewOpenAIProvider(&ProviderConfig{
		Provider: "test",
		BaseURL:  server.URL + "/v1",
		Timeout:  1,
	})
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	start := time.Now()
	_, err = p.ChatCompletionStream(context.Background(), &ChatCompletionRequest{Model: "test"})
	if err == nil {
		t.Fatal("ChatCompletionStream() error = nil, want timeout")
	}
	if !errors.Is(err, ErrCallTimeout) {
		t.Fatalf("ChatCompletionStream() error = %v, want ErrCallTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("ChatCompletionStream() took %s, want timeout near 1s", elapsed)
	}
}

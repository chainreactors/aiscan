package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
)

func TestWebSearchToolRequiresQuery(t *testing.T) {
	tool := NewWebSearchTool(nil)
	_, err := tool.Execute(context.Background(), `{}`)
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("expected query error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), `{"query":"   "}`)
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("expected blank query error, got %v", err)
	}
}

func TestWebSearchToolRejectsUnknownArguments(t *testing.T) {
	tool := NewWebSearchTool(nil)
	_, err := tool.Execute(context.Background(), `{"query":"test","limit":5}`)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestWebSearchToolNilProvider(t *testing.T) {
	tool := NewWebSearchTool(nil)
	_, err := tool.Execute(context.Background(), `{"query":"test"}`)
	if err == nil || !strings.Contains(err.Error(), "provider not configured") {
		t.Fatalf("expected provider error, got %v", err)
	}
}

func TestWebSearchToolViaAnthropicProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			t.Errorf("path = %q, want */messages", r.URL.Path)
		}
		if r.Header.Get("anthropic-beta") != "web-search-2025-03-05" {
			t.Error("missing beta header")
		}
		resp := map[string]any{
			"content": []map[string]any{
				{
					"type": "web_search_tool_result",
					"content": []map[string]string{
						{"type": "web_search_result", "title": "Result 1", "url": "https://example.com/1"},
						{"type": "web_search_result", "title": "Result 2", "url": "https://example.com/2"},
					},
				},
				{"type": "text", "text": "Summary of results."},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, err := provider.NewAnthropicProvider(&provider.ProviderConfig{
		Provider: "anthropic",
		BaseURL:  srv.URL,
		APIKey:   "test-key",
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	tool := NewWebSearchTool(p)
	result, err := tool.Execute(context.Background(), `{"query":"test query"}`)
	if err != nil {
		t.Fatal(err)
	}
	text := result.Text()
	if !strings.Contains(text, "Result 1") || !strings.Contains(text, "example.com/1") {
		t.Fatalf("unexpected result:\n%s", text)
	}
	if !strings.Contains(text, "Summary of results") {
		t.Fatalf("missing summary:\n%s", text)
	}
}

func TestWebSearchToolViaOpenAIProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			t.Errorf("path = %q, want */responses", r.URL.Path)
		}
		resp := map[string]any{
			"output": []map[string]any{
				{"type": "web_search_call", "status": "completed"},
				{
					"type": "message",
					"content": []map[string]any{
						{
							"type": "output_text",
							"text": "CVE-2024-1234 is a stored XSS vulnerability.",
							"annotations": []map[string]string{
								{
									"type":  "url_citation",
									"title": "NVD CVE-2024-1234",
									"url":   "https://nvd.nist.gov/vuln/detail/CVE-2024-1234",
								},
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, err := provider.NewOpenAIProvider(&provider.ProviderConfig{
		Provider: "openai",
		BaseURL:  srv.URL + "/v1",
		APIKey:   "test-key",
		Model:    "gpt-5.5",
	})
	if err != nil {
		t.Fatal(err)
	}

	tool := NewWebSearchTool(p)
	result, err := tool.Execute(context.Background(), `{"query":"test query"}`)
	if err != nil {
		t.Fatal(err)
	}
	text := result.Text()
	if !strings.Contains(text, "CVE-2024-1234") || !strings.Contains(text, "XSS") {
		t.Fatalf("unexpected result:\n%s", text)
	}
	if !strings.Contains(text, "NVD CVE-2024-1234") || !strings.Contains(text, "nvd.nist.gov") {
		t.Fatalf("missing citation result:\n%s", text)
	}
}

func TestWebSearchToolViaDeepSeekProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/messages" {
			t.Errorf("path = %q, want /anthropic/messages", r.URL.Path)
		}
		if r.Header.Get("anthropic-beta") != "web-search-2025-03-05" {
			t.Error("missing beta header")
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty", got)
		}

		var body struct {
			Tools []struct {
				Type    string `json:"type"`
				Name    string `json:"name"`
				MaxUses int    `json:"max_uses"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(body.Tools) != 1 || body.Tools[0].Type != "web_search_20250305" || body.Tools[0].MaxUses != 3 {
			t.Fatalf("tools = %#v", body.Tools)
		}

		resp := map[string]any{
			"content": []map[string]any{
				{
					"type": "web_search_tool_result",
					"content": []map[string]string{
						{"type": "web_search_result", "title": "DeepSeek Result", "url": "https://example.com/deepseek"},
					},
				},
				{"type": "text", "text": "DeepSeek summary."},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, err := provider.NewProvider(&provider.ProviderConfig{
		Provider: "deepseek",
		BaseURL:  srv.URL + "/v1",
		APIKey:   "test-key",
		Model:    "deepseek-test",
	})
	if err != nil {
		t.Fatal(err)
	}

	tool := NewWebSearchTool(p)
	result, err := tool.Execute(context.Background(), `{"query":"test query","num":3}`)
	if err != nil {
		t.Fatal(err)
	}
	text := result.Text()
	if !strings.Contains(text, "DeepSeek Result") || !strings.Contains(text, "DeepSeek summary") {
		t.Fatalf("unexpected result:\n%s", text)
	}
}

func TestWebSearchToolRejectsInvalidNum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("provider should not be called for invalid num; got %s", r.URL.Path)
	}))
	defer srv.Close()

	p, err := provider.NewAnthropicProvider(&provider.ProviderConfig{
		Provider: "anthropic",
		BaseURL:  srv.URL,
		APIKey:   "test-key",
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}

	tool := NewWebSearchTool(p)
	_, err = tool.Execute(context.Background(), `{"query":"test","num":99}`)
	if err == nil || !strings.Contains(err.Error(), "num must be between 1 and 10") {
		t.Fatalf("expected num range error, got %v", err)
	}

	_, err = tool.Execute(context.Background(), `{"query":"test","num":-1}`)
	if err == nil || !strings.Contains(err.Error(), "num must be between 1 and 10") {
		t.Fatalf("expected num range error, got %v", err)
	}
}

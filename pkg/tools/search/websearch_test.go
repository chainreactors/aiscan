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

func TestParseWebSearchArgs(t *testing.T) {
	q, n, err := parseWebSearchArgs([]string{"CVE-2024-1234", "exploit"})
	if err != nil {
		t.Fatal(err)
	}
	if q != "CVE-2024-1234 exploit" {
		t.Fatalf("query = %q", q)
	}
	if n != defaultMaxUses {
		t.Fatalf("maxUses = %d", n)
	}
}

func TestParseWebSearchArgsWithNum(t *testing.T) {
	q, n, err := parseWebSearchArgs([]string{"nginx", "--num", "8"})
	if err != nil {
		t.Fatal(err)
	}
	if q != "nginx" || n != 8 {
		t.Fatalf("got query=%q num=%d", q, n)
	}
}

func TestParseWebSearchArgsClampsNum(t *testing.T) {
	_, n, err := parseWebSearchArgs([]string{"test", "--num", "99"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Fatalf("expected clamped to 10, got %d", n)
	}
}

func TestParseWebSearchArgsRequiresQuery(t *testing.T) {
	_, _, err := parseWebSearchArgs(nil)
	if err == nil {
		t.Fatal("expected error for empty args")
	}
}

func TestWebSearchViaAnthropicProvider(t *testing.T) {
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

	ws := NewWebSearch(p)
	result, err := ws.Execute(context.Background(), []string{"test query"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Result 1") || !strings.Contains(result, "example.com/1") {
		t.Fatalf("unexpected result:\n%s", result)
	}
	if !strings.Contains(result, "Summary of results") {
		t.Fatalf("missing summary:\n%s", result)
	}
}

func TestWebSearchViaOpenAIProvider(t *testing.T) {
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

	ws := NewWebSearch(p)
	result, err := ws.Execute(context.Background(), []string{"test query"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "CVE-2024-1234") || !strings.Contains(result, "XSS") {
		t.Fatalf("unexpected result:\n%s", result)
	}
	if !strings.Contains(result, "NVD CVE-2024-1234") || !strings.Contains(result, "nvd.nist.gov") {
		t.Fatalf("missing citation result:\n%s", result)
	}
}

func TestWebSearchViaDeepSeekProviderUsesAnthropicEndpoint(t *testing.T) {
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

	ws := NewWebSearch(p)
	result, err := ws.Execute(context.Background(), []string{"test query", "--num", "3"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "DeepSeek Result") || !strings.Contains(result, "DeepSeek summary") {
		t.Fatalf("unexpected result:\n%s", result)
	}
}

func TestWebSearchNilProvider(t *testing.T) {
	ws := NewWebSearch(nil)
	_, err := ws.Execute(context.Background(), []string{"test"})
	if err == nil || !strings.Contains(err.Error(), "provider not configured") {
		t.Fatalf("expected provider error, got %v", err)
	}
}

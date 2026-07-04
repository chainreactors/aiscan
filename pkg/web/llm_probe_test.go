package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/probe"
	"github.com/chainreactors/aiscan/pkg/webproto"
)

// fakeConfigStore is a minimal in-memory ConfigStore for probe tests.
type fakeConfigStore struct {
	cfg webproto.DistributeConfig
}

func (f *fakeConfigStore) GetDistributeConfig(ctx context.Context) (string, bool, webproto.DistributeConfig, error) {
	return "config.yaml", true, f.cfg, nil
}

func (f *fakeConfigStore) SaveDistributeConfig(ctx context.Context, cfg webproto.DistributeConfig) error {
	f.cfg = cfg
	return nil
}

// stubLLMServer emulates an OpenAI-compatible /chat/completions endpoint and
// records the Authorization header it received.
func stubLLMServer(t *testing.T, reply string, gotAuth *string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "cmpl-1",
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": reply}, "finish_reason": "stop"},
			},
		})
	})
	return httptest.NewServer(mux)
}

func TestTestLLMSuccess(t *testing.T) {
	srv := stubLLMServer(t, "pong", nil)
	defer srv.Close()

	svc := NewService(ServiceConfig{ConfigStore: &fakeConfigStore{}})
	res, err := svc.TestLLM(context.Background(), probe.LLMTestRequest{
		Provider: "openai",
		BaseURL:  srv.URL + "/v1",
		APIKey:   "sk-test",
		Model:    "gpt-test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected ok, got error: %q", res.Error)
	}
	if res.Reply != "pong" {
		t.Fatalf("expected reply pong, got %q", res.Reply)
	}
	if res.LatencyMs < 0 {
		t.Fatalf("expected non-negative latency, got %d", res.LatencyMs)
	}
}

func TestTestLLMMissingModel(t *testing.T) {
	svc := NewService(ServiceConfig{ConfigStore: &fakeConfigStore{}})
	res, err := svc.TestLLM(context.Background(), probe.LLMTestRequest{Provider: "openai", APIKey: "sk-test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Fatal("expected failure when model is empty")
	}
	if !strings.Contains(res.Error, "model") {
		t.Fatalf("expected model error, got %q", res.Error)
	}
}

func TestTestLLMFallsBackToStoredKey(t *testing.T) {
	var gotAuth string
	srv := stubLLMServer(t, "ok", &gotAuth)
	defer srv.Close()

	store := &fakeConfigStore{}
	store.cfg.LLM.APIKey = "sk-stored"
	svc := NewService(ServiceConfig{ConfigStore: store})

	// APIKey left blank: the stored secret must be used.
	res, err := svc.TestLLM(context.Background(), probe.LLMTestRequest{
		Provider: "openai",
		BaseURL:  srv.URL + "/v1",
		Model:    "gpt-test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected ok, got error: %q", res.Error)
	}
	if gotAuth != "Bearer sk-stored" {
		t.Fatalf("expected stored key in Authorization header, got %q", gotAuth)
	}
}

func TestTestLLMReportsTransportError(t *testing.T) {
	svc := NewService(ServiceConfig{ConfigStore: &fakeConfigStore{}})
	// Unroutable port → connection refused, surfaced inside the result.
	res, err := svc.TestLLM(context.Background(), probe.LLMTestRequest{
		Provider: "openai",
		BaseURL:  "http://127.0.0.1:1/v1",
		APIKey:   "sk-test",
		Model:    "gpt-test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK {
		t.Fatal("expected failure against unreachable endpoint")
	}
	if res.Error == "" {
		t.Fatal("expected an error message")
	}
}

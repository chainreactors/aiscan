package provider

import (
	"context"
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
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if cfg.BaseURL != "http://base-url.example/v1" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
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

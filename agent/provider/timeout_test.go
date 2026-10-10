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

func TestProviderRequestTimeoutOverride(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", protocol, streaming), func(t *testing.T) {
				t.Parallel()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						return
					}
					if body["timeout"] != nil || body["Timeout"] != nil {
						t.Error("host fallback leaked into the vendor protocol")
					}
					// Return after the configured one-second provider deadline.
					select {
					case <-time.After(1200 * time.Millisecond):
					case <-r.Context().Done():
						return
					}
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						if protocol == "openai" {
							fmt.Fprint(w, "data: [DONE]\n\n")
						} else {
							fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
						}
					} else if protocol == "openai" {
						fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
					} else {
						fmt.Fprint(w, `{"role":"assistant","content":[{"type":"text","text":"done"}],"stop_reason":"end_turn"}`)
					}
				}))
				defer server.Close()
				p, err := NewProvider(&ProviderConfig{Provider: protocol, APIKey: "fixture", BaseURL: server.URL, Timeout: 1})
				if err != nil {
					t.Fatal(err)
				}
				for _, attempt := range []string{"configured", "longer_request", "parent_cancellation"} {
					ctx := t.Context()
					request := &ChatCompletionRequest{Model: "fixture"}
					if attempt != "configured" {
						request.Timeout = 30 * time.Minute
					}
					if attempt == "parent_cancellation" {
						var cancel context.CancelFunc
						ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
						defer cancel()
					}
					var completed bool
					if streaming {
						var events <-chan ChatCompletionStreamEvent
						events, err = p.(StreamingProvider).ChatCompletionStream(ctx, request)
						if err == nil {
							for event := range events {
								completed = completed || event.Done
								if event.Err != nil {
									err = event.Err
								}
							}
						}
					} else {
						var response *ChatCompletionResponse
						response, err = p.ChatCompletion(ctx, request)
						completed = response != nil && len(response.Choices) == 1 && MessageText(response.Choices[0].Message) == "done"
					}
					switch attempt {
					case "configured":
						if !errors.Is(err, ErrCallTimeout) {
							t.Fatalf("configured fallback: %v", err)
						}
					case "longer_request":
						if err != nil || !completed {
							t.Fatalf("request still capped by provider fallback: complete=%v err=%v", completed, err)
						}
					case "parent_cancellation":
						if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrCallTimeout) {
							t.Fatalf("parent cancellation lost: %v", err)
						}
					}
				}
			})
		}
	}
}

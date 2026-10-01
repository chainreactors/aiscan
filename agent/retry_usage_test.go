package agent

import (
	"context"
	"errors"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
)

func TestRetryRetainsBilledUsageAndLastContextSize(t *testing.T) {
	calls := 0
	p := &callbackProvider{fn: func(context.Context, *ChatCompletionRequest) (*ChatCompletionResponse, error) {
		calls++
		r := chatResponse(NewTextMessage("assistant", "done"))
		r.Usage = &aop.TokenUsage{InputTokens: 100, OutputTokens: 20, TotalTokens: 120, Detail: map[string]uint64{"reasoning": 10, "cache_read": 60}}
		if calls == 1 {
			return r, errors.New("API error (502): transient")
		}
		return r, nil
	}}
	r, err := NewAgent(Config{Loop: StandardLoop{}, Provider: p, MaxRetries: 1}).Run(t.Context(), TextInput("task"))
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalUsage.TotalTokens != 240 || r.TotalUsage.Detail["reasoning"] != 20 || r.TotalUsage.Detail["requests"] != 2 || r.ContextTokens != 120 {
		t.Fatalf("usage lost or context inflated: %v context=%d", r.TotalUsage, r.ContextTokens)
	}
}

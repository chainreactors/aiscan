package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"google.golang.org/protobuf/proto"
)

func TestBeforeModelAppendOnlyAndOnceAcrossRetry(t *testing.T) {
	registry := corehooks.New()
	calls := 0
	hooks.BeforeModel.On(registry, "test", func(_ context.Context, ev hooks.ContextEvent) ([]*aop.Message, error) {
		calls++
		ev.Messages[0].Content[0] = aop.Text("corrupted prefix")
		return []*aop.Message{provider.TextMessage("user", "controller observation"), provider.TextMessage("assistant", "fabricated reply"), provider.ToolResultMessage("orphan", &aop.ToolResult{})}, nil
	})
	var first []*aop.Message
	requests := 0
	llm := &callbackProvider{fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
		requests++
		if len(req.Messages) != 2 || provider.MessageText(req.Messages[0]) != "ordinary task" || provider.MessageText(req.Messages[1]) != "controller observation" {
			t.Fatalf("invalid history: %v", req.Messages)
		}
		if requests == 1 {
			for _, m := range req.Messages {
				first = append(first, proto.CloneOf(m))
			}
			return nil, errors.New("API error (502): transient")
		}
		for i, m := range req.Messages {
			if !proto.Equal(first[i], m) {
				t.Fatal("retry changed committed context")
			}
		}
		return chatResponse(NewTextMessage("assistant", "done")), nil
	}}
	result, err := NewAgent(Config{Loop: StandardLoop{}, Provider: llm, Hooks: registry, MaxRetries: 1}).Run(t.Context(), TextInput("ordinary task"))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || requests != 2 {
		t.Fatalf("hook=%d requests=%d", calls, requests)
	}
	if len(result.Messages) != 3 || result.Messages[1].Id == "" {
		t.Fatal("append was not committed to durable history")
	}
}

func TestBeforeModelFailureReturnsToModel(t *testing.T) {
	registry := corehooks.New()
	hooks.BeforeModel.On(registry, "failed-controller", func(context.Context, hooks.ContextEvent) ([]*aop.Message, error) {
		return nil, errors.New("unavailable")
	})
	llm := &callbackProvider{fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
		if len(req.Messages) != 1 {
			t.Fatal("failed hook mutated history")
		}
		return chatResponse(NewTextMessage("assistant", "done")), nil
	}}
	result, err := NewAgent(Config{Loop: StandardLoop{}, Provider: llm, Hooks: registry}).Run(t.Context(), TextInput("ordinary task"))
	if err != nil || result.Output != "done" {
		t.Fatalf("%v %v", result, err)
	}
}

func TestAfterModelObservesCompleteOutputAndCannotRewriteTranscript(t *testing.T) {
	registry := corehooks.New()
	echo := &recordingTool{name: "echo", output: "ok"}
	llm := &scriptedProvider{streamEventBatches: [][]ChatCompletionStreamEvent{
		{roleDelta("assistant"), toolCallDelta(0, "one", "echo", `{"value":`), toolCallDelta(0, "", "", `"x"}`), toolCallDelta(1, "two", "echo", `{"value":"y"}`), {Done: true}},
		{roleDelta("assistant"), textDelta("fi"), textDelta("nal"), {Done: true}},
	}}
	calls := 0
	hooks.AfterModel.On(registry, "observe", func(ctx context.Context, ev hooks.ContextEvent) (struct{}, error) {
		calls++
		cfg, ok := ToolAgentConfig(ctx)
		if !ok || cfg.Provider == nil {
			t.Error("hook has no borrowed model configuration")
		}
		last := ev.Messages[len(ev.Messages)-1]
		if calls == 1 && len(provider.MessageToolCalls(last)) != 2 {
			t.Error("tool batch was split or incomplete")
		}
		if calls == 2 && provider.MessageText(last) != "final" {
			t.Error("final text incomplete")
		}
		last.Content = []*aop.Content{aop.Text("corrupted")}
		return struct{}{}, errors.New("observer failure")
	})
	result, err := NewAgent(Config{Loop: StandardLoop{}, Provider: llm, Tools: newTestTools(t, echo), Hooks: registry, Stream: true}).Run(t.Context(), TextInput("Use both calls"))
	if err != nil || result.Output != "final" || calls != 2 || len(echo.callsSnapshot()) != 2 {
		t.Fatalf("result=%v error=%v hooks=%d", result, err, calls)
	}
}

func TestAfterModelDoesNotObserveFailedStreamRetry(t *testing.T) {
	registry := corehooks.New()
	calls := 0
	hooks.AfterModel.On(registry, "observe", func(context.Context, hooks.ContextEvent) (struct{}, error) { calls++; return struct{}{}, nil })
	llm := &flakyStreamProvider{events: []ChatCompletionStreamEvent{roleDelta("assistant"), textDelta("done"), {Done: true}}}
	_, err := NewAgent(Config{Loop: StandardLoop{}, Provider: llm, Hooks: registry, Stream: true, MaxRetries: 1}).Run(t.Context(), TextInput("task"))
	if err != nil || calls != 1 || llm.calls.Load() != 2 {
		t.Fatalf("err=%v hooks=%d requests=%d", err, calls, llm.calls.Load())
	}
}

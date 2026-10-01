package recap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/telemetry"
	"github.com/chainreactors/cyber/core/types"
)

type fakeProvider struct {
	list func(context.Context) ([]string, error)
	chat func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error)
}

func (*fakeProvider) Name() string { return "test" }
func (p *fakeProvider) ListModels(ctx context.Context) ([]string, error) {
	if p.list == nil {
		return nil, errors.New("no catalog")
	}
	return p.list(ctx)
}
func (p *fakeProvider) ChatCompletion(ctx context.Context, r *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	return p.chat(ctx, r)
}

func answer(text string) *provider.ChatCompletionResponse {
	return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: provider.TextMessage("assistant", text)}}}
}

func worker(t *testing.T, p provider.Provider) (*Worker, <-chan *aop.Event) {
	t.Helper()
	state := &provider.State{}
	state.Set(p, provider.ProviderConfig{Model: "gpt-large"})
	stream := events.New()
	output := make(chan *aop.Event, 64)
	sub := stream.Observe(func(e *aop.Event) { output <- e })
	t.Cleanup(sub.Cancel)
	return New(t.Context(), state, stream, telemetry.NopLogger()), output
}

func begin(w *Worker, session, turn, parentCall string) {
	w.Observe(&aop.Event{SessionId: session, Payload: &aop.Event_SessionStarted{SessionStarted: &aop.SessionStarted{ParentToolCallId: parentCall}}})
	w.Observe(&aop.Event{SessionId: session, TurnId: turn, Payload: &aop.Event_TurnStarted{TurnStarted: &aop.TurnStarted{}}})
}

func message(w *Worker, session, turn, role, text string) {
	w.Observe(&aop.Event{SessionId: session, TurnId: turn, Payload: &aop.Event_Message{Message: provider.TextMessage(role, text)}})
}

func end(w *Worker, session, turn, stop string) {
	w.Observe(&aop.Event{SessionId: session, TurnId: turn, Payload: &aop.Event_TurnEnded{TurnEnded: &aop.TurnEnded{StopReason: stop}}})
}

func TestCollectsOnlyThisTaskAndTriggersOnceAtTerminal(t *testing.T) {
	w, _ := worker(t, nil)
	begin(w, "s", "old", "")
	message(w, "s", "old", "assistant", "previous task")
	end(w, "s", "old", "completed")
	<-w.queue
	begin(w, "s", "current", "")
	message(w, "s", "current", "user", "fix the bug")
	w.Observe(&aop.Event{SessionId: "s", TurnId: "current", Payload: &aop.Event_Message{Message: &aop.Message{Role: "assistant", Content: []*aop.Content{
		aop.Text("checked the implementation"),
		{Value: &aop.Content_Reasoning{Reasoning: &aop.ReasoningContent{Text: "PRIVATE_THINKING"}}},
		{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: "call", Name: "DUPLICATE_EMBEDDED_CALL"}}},
	}}}})
	w.Observe(&aop.Event{SessionId: "s", TurnId: "current", Payload: &aop.Event_MessageDelta{MessageDelta: &aop.MessageDelta{Value: &aop.MessageDelta_Reasoning{Reasoning: "PRIVATE_DELTA"}}}})
	w.Observe(&aop.Event{SessionId: "s", TurnId: "current", Payload: &aop.Event_ToolCall{ToolCall: &aop.ToolCall{Id: "call", Name: "bash", Arguments: &aop.EncodedValue{Data: []byte(`{"command":"go test"}`)}}}})
	w.Observe(&aop.Event{SessionId: "s", TurnId: "current", Payload: &aop.Event_ToolResult{ToolResult: &aop.ToolResult{CallId: "call", Name: "bash", Output: []*aop.Content{aop.Text("PASS"), {Value: &aop.Content_Reasoning{Reasoning: &aop.ReasoningContent{Text: "PRIVATE_TOOL_REASONING"}}}}}}})
	for range 3 {
		w.Observe(&aop.Event{SessionId: "s", TurnId: "current", Payload: &aop.Event_Status{Status: &aop.Status{State: "eval_end"}}})
	}
	if len(w.queue) != 0 {
		t.Fatal("work or evaluation events triggered recap")
	}
	end(w, "s", "current", "completed")
	end(w, "s", "current", "completed")
	if len(w.queue) != 1 {
		t.Fatalf("queued %d recaps", len(w.queue))
	}
	input := (<-w.queue).input
	for _, forbidden := range []string{"previous task", "PRIVATE_", "DUPLICATE_EMBEDDED_CALL"} {
		if strings.Contains(input, forbidden) {
			t.Fatalf("input contains %q", forbidden)
		}
	}
	for _, want := range []string{"fix the bug", "checked the implementation", "go test", "PASS", "Task stopped: completed"} {
		if !strings.Contains(input, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestWindowIsBoundedAndKeepsGoalAndRecentWork(t *testing.T) {
	w, _ := worker(t, nil)
	begin(w, "s", "t", "")
	message(w, "s", "t", "user", "ORIGINAL_GOAL "+strings.Repeat("目标", 5000))
	for i := range 100 {
		message(w, "s", "t", "assistant", fmt.Sprintf("entry-%d %s end-%d", i, strings.Repeat("文本", 10_000), i))
	}
	for key, task := range w.turns {
		if task.bytes > workBytes || len(task.goal) > goalBytes {
			t.Fatalf("unbounded task %v", key)
		}
	}
	end(w, "s", "t", "completed")
	input := (<-w.queue).input
	if len(input)+len(instruction) > inputBytes || !utf8.ValidString(input) {
		t.Fatalf("invalid request budget or UTF-8: %d bytes", len(input)+len(instruction))
	}
	if !strings.Contains(input, "ORIGINAL_GOAL") || !strings.Contains(input, "entry-99") || !strings.Contains(input, "end-99") || strings.Contains(input, "entry-0 ") {
		t.Fatal("window lost the goal/recent work or retained old work")
	}
	for limit := range 100 {
		got := clip(strings.Repeat("中文", 100), limit)
		if len(got) > limit || !utf8.ValidString(got) {
			t.Fatalf("clip at %d returned invalid bytes", limit)
		}
	}
}

func TestSessionIsolationChildExclusionAndEmptyTasks(t *testing.T) {
	w, _ := worker(t, nil)
	begin(w, "root", "a", "")
	begin(w, "other", "a", "")
	begin(w, "child", "a", "parent-call")
	message(w, "root", "a", "assistant", "ROOT_ONLY")
	message(w, "other", "a", "assistant", "OTHER_ONLY")
	message(w, "child", "a", "assistant", "CHILD_ONLY")
	end(w, "root", "a", "error")
	end(w, "other", "a", "canceled")
	end(w, "child", "a", "completed")
	begin(w, "root", "empty", "")
	message(w, "root", "empty", "user", "not executed")
	end(w, "root", "empty", "error")
	if len(w.queue) != 2 {
		t.Fatalf("queued %d recaps", len(w.queue))
	}
	a, b := <-w.queue, <-w.queue
	if strings.Contains(a.input, "OTHER_ONLY") || strings.Contains(a.input, "CHILD_ONLY") || !strings.Contains(a.input, "stopped: error") || !strings.Contains(b.input, "stopped: canceled") {
		t.Fatal("task scope or stop reason was lost")
	}
}

func TestSlowProviderDoesNotBlockEventsAndShutdownCancels(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	p := &fakeProvider{chat: func(ctx context.Context, request *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	w, output := worker(t, p)
	ctx, cancel := context.WithCancel(t.Context())
	w.ctx = ctx
	done := make(chan struct{})
	go func() { defer close(done); w.Run() }()
	begin(w, "s", "t", "")
	message(w, "s", "t", "assistant", "finished work")
	end(w, "s", "t", "completed")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	for i := range queueSize + 5 {
		id := fmt.Sprint(i)
		begin(w, "s", id, "")
		message(w, "s", id, "assistant", "next work")
		end(w, "s", id, "completed")
	}
	if len(w.queue) != queueSize {
		t.Fatal("pending work is not bounded")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if calls.Load() != 1 || len(output) != 0 {
		t.Fatal("shutdown processed queued work or published a recap")
	}
}

func TestWorkerPublishesOnlyRecapAndSurvivesFailure(t *testing.T) {
	count := 0
	p := &fakeProvider{chat: func(ctx context.Context, request *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		count++
		if count == 1 {
			return nil, errors.New("temporary failure")
		}
		if request.Stream || len(request.Tools) != 0 || request.MaxTokens != 256 {
			t.Error("recap request enabled tools/streaming or wrong budget")
		}
		return answer("Updated code.\nTests passed."), nil
	}}
	w, output := worker(t, p)
	for _, id := range []string{"failed", "success"} {
		begin(w, "s", id, "")
		message(w, "s", id, "assistant", "work")
		end(w, "s", id, "completed")
		w.process(<-w.queue)
	}
	if len(output) != 1 {
		t.Fatalf("emitted %d events", len(output))
	}
	event := <-output
	var recap types.Recap
	if event.GetExtension().UnmarshalTo(&recap) != nil || recap.Text != "Updated code. Tests passed." || event.TurnId != "success" || event.SessionId != "s" || event.Emitter != "recap" {
		t.Fatalf("unexpected recap: %v", event)
	}
}

func TestSmallModelSelectionFallbackAndCache(t *testing.T) {
	for _, tc := range []struct {
		fallback string
		models   []string
		want     string
	}{
		{"gpt-large", []string{"gpt-mini", "gpt-nano", "text-embedding-mini", "gpt-nano-audio"}, "gpt-nano"},
		{"claude-sonnet", []string{"gpt-nano", "claude-haiku"}, "claude-haiku"},
		{"deepseek-v4-pro", []string{"deepseek-v4-flash", "gemini-flash-lite"}, "deepseek-v4-flash"},
		{"custom", []string{"unknown-mini", "gpt-image-mini", "gpt-mini-search"}, "custom"},
	} {
		if got := smallModel(tc.models, tc.fallback); got != tc.want {
			t.Errorf("smallModel(%q) = %q, want %q", tc.fallback, got, tc.want)
		}
	}
	lists := 0
	var called []string
	p := &fakeProvider{
		list: func(context.Context) ([]string, error) { lists++; return []string{"gpt-mini"}, nil },
		chat: func(_ context.Context, r *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
			called = append(called, r.Model)
			if r.Model == "gpt-mini" {
				return nil, &provider.APIError{StatusCode: 404}
			}
			return answer("done"), nil
		},
	}
	w, _ := worker(t, p)
	for range 2 {
		if _, err := w.summarize("work"); err != nil {
			t.Fatal(err)
		}
	}
	if lists != 1 || strings.Join(called, ",") != "gpt-mini,gpt-large,gpt-large" {
		t.Fatalf("lists=%d calls=%v", lists, called)
	}
	p2 := &fakeProvider{list: p.list, chat: p.chat}
	w.providers.Set(p2, provider.ProviderConfig{Model: "gpt-large"})
	if _, err := w.summarize("work"); err != nil || lists != 2 {
		t.Fatalf("cache did not reset: lists=%d err=%v", lists, err)
	}
}

func TestTransientFailureDoesNotRetryAndThinkingOnlyResponseIsEmpty(t *testing.T) {
	for _, code := range []int{429, 500, 401} {
		calls := 0
		p := &fakeProvider{list: func(context.Context) ([]string, error) { return []string{"gpt-mini"}, nil },
			chat: func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				calls++
				return nil, &provider.APIError{StatusCode: code}
			}}
		w, _ := worker(t, p)
		if _, err := w.summarize("work"); err == nil || calls != 1 {
			t.Errorf("HTTP %d was retried or ignored", code)
		}
	}
	response := answer("")
	response.Choices[0].Message.Content = []*aop.Content{{Value: &aop.Content_Reasoning{Reasoning: &aop.ReasoningContent{Text: "PRIVATE_THINKING"}}}}
	if _, err := responseText(response); err == nil {
		t.Fatal("thinking was used as the recap")
	}
}

func TestCatalogTimeoutFallsBackWithinRequestDeadline(t *testing.T) {
	p := &fakeProvider{
		list: func(ctx context.Context) ([]string, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 2*time.Second {
				t.Error("catalog lookup has no short deadline")
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
		chat: func(ctx context.Context, request *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 15*time.Second || ctx.Err() != nil {
				t.Error("catalog timeout canceled generation or lost the task deadline")
			}
			if request.Model != "gpt-large" {
				t.Errorf("catalog timeout selected %q", request.Model)
			}
			return answer("done"), nil
		},
	}
	w, _ := worker(t, p)
	if _, err := w.summarize("work"); err != nil {
		t.Fatal(err)
	}
}

func TestSmallDefaultDoesNotQueryCatalog(t *testing.T) {
	p := &fakeProvider{list: func(context.Context) ([]string, error) {
		t.Error("already-small default queried the catalog")
		return nil, nil
	}, chat: func(_ context.Context, request *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if request.Model != "claude-haiku" {
			t.Errorf("default model changed to %q", request.Model)
		}
		return answer("done"), nil
	}}
	w, _ := worker(t, p)
	w.providers.Set(p, provider.ProviderConfig{Model: "claude-haiku"})
	if _, err := w.summarize("work"); err != nil {
		t.Fatal(err)
	}
}

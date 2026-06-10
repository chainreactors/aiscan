package agent

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/eventbus"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

func testBus(handler func(Event)) *eventbus.Bus[Event] {
	b := eventbus.New[Event]()
	if handler != nil {
		b.Subscribe(handler)
	}
	return b
}

func TestRunWithoutToolsReturnsFinalText(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(NewTextMessage("assistant", "done")),
		},
	}

	result, err := (NewAgent(Config{
		Provider:     llm,
		Tools:        tools,
		Model:        "test",
		SystemPrompt: "system",
	})).Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "done" {
		t.Fatalf("result = %q, want done", result.Output)
	}
	requests := llm.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	if requests[0].Messages[0].Role != "system" || *requests[0].Messages[0].Content != "system" {
		t.Fatalf("system message not injected: %#v", requests[0].Messages)
	}
}

func TestRunExecutesToolLoop(t *testing.T) {
	tools := command.NewRegistry()
	echo := &recordingTool{name: "echo", output: "tool output"}
	tools.RegisterTool(echo)
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(ChatMessage{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: FunctionCall{
						Name:      "echo",
						Arguments: `{"value":"x"}`,
					},
				}},
			}),
			chatResponse(NewTextMessage("assistant", "final")),
		},
	}

	var events []EventType
	result, err := (NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
		Bus: testBus(func(e Event) { events = append(events, e.Type) }),
	})).Run(context.Background(), "use tool")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "final" {
		t.Fatalf("output = %q, want final", result.Output)
	}
	if got := echo.callsSnapshot(); !reflect.DeepEqual(got, []string{`{"value":"x"}`}) {
		t.Fatalf("tool calls = %#v", got)
	}
	requests := llm.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if !hasToolMessage(requests[1].Messages, "call-1", "tool output") {
		t.Fatalf("second request missing tool result: %#v", requests[1].Messages)
	}
	if !containsEvent(events, EventToolExecutionStart) || !containsEvent(events, EventToolExecutionEnd) {
		t.Fatalf("tool events missing: %#v", events)
	}
}

func TestRunEmitsTurnEndAfterToolResults(t *testing.T) {
	tools := command.NewRegistry()
	tools.RegisterTool(&recordingTool{name: "echo", output: "tool output"})
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(ChatMessage{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: FunctionCall{
						Name:      "echo",
						Arguments: `{"value":"x"}`,
					},
				}},
			}),
			chatResponse(NewTextMessage("assistant", "final")),
		},
	}

	var events []EventType
	result, err := (NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
		Bus: testBus(func(event Event) {
			events = append(events, event.Type)
		}),
	})).Run(context.Background(), "use tool")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2", result.Turns)
	}

	want := []EventType{
		EventAgentStart,
		EventTurnStart,
		EventMessageStart,
		EventMessageEnd,
		EventLLMRequest,
		EventMessageStart,
		EventMessageEnd,
		EventToolExecutionStart,
		EventToolExecutionEnd,
		EventMessageStart,
		EventMessageEnd,
		EventTurnEnd,
		EventTurnStart,
		EventLLMRequest,
		EventMessageStart,
		EventMessageEnd,
		EventTurnEnd,
		EventAgentEnd,
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestContinueRequiresNonAssistantLastMessage(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{}
	a := NewAgent(Config{Provider: llm, Tools: tools, Model: "test"})

	if _, err := a.Continue(context.Background()); err == nil || !strings.Contains(err.Error(), "no messages") {
		t.Fatalf("Continue() error = %v, want no messages", err)
	}

	a.state.Messages = []ChatMessage{NewTextMessage("assistant", "done")}
	if _, err := a.Continue(context.Background()); err == nil || !strings.Contains(err.Error(), "assistant") {
		t.Fatalf("Continue() error = %v, want assistant", err)
	}
}

func TestAgentReusesConversationAcrossPrompts(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(NewTextMessage("assistant", "first")),
			chatResponse(NewTextMessage("assistant", "second")),
		},
	}
	a := NewAgent(Config{Provider: llm, Tools: tools, Model: "test"})
	if _, err := a.Run(context.Background(), "one"); err != nil {
		t.Fatalf("first prompt error = %v", err)
	}
	if _, err := a.Run(context.Background(), "two"); err != nil {
		t.Fatalf("second prompt error = %v", err)
	}
	requests := llm.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if len(requests[1].Messages) != 3 {
		t.Fatalf("second request messages = %d, want 3: %#v", len(requests[1].Messages), requests[1].Messages)
	}
	if *requests[1].Messages[0].Content != "one" || *requests[1].Messages[1].Content != "first" || *requests[1].Messages[2].Content != "two" {
		t.Fatalf("unexpected reused context: %#v", requests[1].Messages)
	}
}

func TestAgentPromptReturnsRunScopedNewMessages(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(NewTextMessage("assistant", "next")),
		},
	}
	ag := NewAgent(Config{Provider: llm, Tools: tools, Model: "test"})
	ag.state.Messages = []ChatMessage{NewTextMessage("user", "base")}
	result, err := ag.Run(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if len(result.NewMessages) != 2 {
		t.Fatalf("new messages = %d, want 2: %#v", len(result.NewMessages), result.NewMessages)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("messages = %d, want 3: %#v", len(result.Messages), result.Messages)
	}
}

func TestTransformContextAppliesOnlyToProviderRequest(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(NewTextMessage("assistant", "one")),
			chatResponse(NewTextMessage("assistant", "two")),
		},
	}
	a := NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
		TransformContext: func(messages []ChatMessage) []ChatMessage {
			if len(messages) <= 1 {
				return messages
			}
			return messages[len(messages)-1:]
		},
	})
	if _, err := a.Run(context.Background(), "one"); err != nil {
		t.Fatalf("first prompt error = %v", err)
	}
	if _, err := a.Run(context.Background(), "two"); err != nil {
		t.Fatalf("second prompt error = %v", err)
	}
	requests := llm.requestsSnapshot()
	if len(requests[1].Messages) != 1 || *requests[1].Messages[0].Content != "two" {
		t.Fatalf("transform not applied to request: %#v", requests[1].Messages)
	}
	if got := len(a.state.Messages); got != 4 {
		t.Fatalf("agent state messages = %d, want 4", got)
	}
}

func TestProviderErrorEmitsAgentEndAndUpdatesState(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{err: fmt.Errorf("boom")}
	var events []Event
	a := NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
		Bus: testBus(func(event Event) {
			events = append(events, event)
		}),
	})

	result, err := a.Run(context.Background(), "hello")
	if err == nil {
		t.Fatal("Prompt() error = nil, want error")
	}
	if result == nil || result.Err == nil {
		t.Fatalf("result = %#v, want result with Err", result)
	}
	if got := eventTypes(events); !reflect.DeepEqual(got, []EventType{
		EventAgentStart,
		EventTurnStart,
		EventMessageStart,
		EventMessageEnd,
		EventLLMRequest,
		EventMessageStart,
		EventMessageEnd,
		EventTurnEnd,
		EventAgentEnd,
	}) {
		t.Fatalf("events = %#v", got)
	}
	if result.Turns != 1 {
		t.Fatalf("turns = %d, want 1", result.Turns)
	}
	if len(events) == 0 || events[len(events)-1].Type != EventAgentEnd || events[len(events)-1].Err == nil {
		t.Fatalf("last event = %#v, want agent_end with error", lastEvent(events))
	}
	if a.running {
		t.Fatal("running = true, want false")
	}
	if !strings.Contains(a.state.ErrorMessage, "boom") {
		t.Fatalf("state.ErrorMessage = %q, want boom", a.state.ErrorMessage)
	}
}

func TestMaxTurnsStopsBeforeNextModelCall(t *testing.T) {
	tools := command.NewRegistry()
	tools.RegisterTool(&recordingTool{name: "echo", output: "tool output"})
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(ChatMessage{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: FunctionCall{
						Name:      "echo",
						Arguments: `{"value":"x"}`,
					},
				}},
			}),
			chatResponse(NewTextMessage("assistant", "should not be called")),
		},
	}

	result, err := (NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
		MaxTurns: 1,
	})).Run(context.Background(), "use tool")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Turns != 1 {
		t.Fatalf("turns = %d, want 1", result.Turns)
	}
	if got := len(llm.requestsSnapshot()); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestStreamingProviderEmitsMessageUpdates(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		streamEvents: []ChatCompletionStreamEvent{
			{Delta: ChatMessageDelta{Role: "assistant"}},
			{Delta: ChatMessageDelta{Content: strPtr("hel")}},
			{Delta: ChatMessageDelta{Content: strPtr("lo")}},
			{Done: true},
		},
	}
	var updates int
	result, err := (NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
		Stream:   true,
		Bus: testBus(func(event Event) {
			if event.Type == EventMessageUpdate {
				updates++
			}
		}),
	})).Run(context.Background(), "stream")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "hello" {
		t.Fatalf("output = %q, want hello", result.Output)
	}
	if updates == 0 {
		t.Fatal("expected message_update events")
	}
}

func TestStatefulAgentTracksStreamingMessage(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		streamEvents: []ChatCompletionStreamEvent{
			{Delta: ChatMessageDelta{Role: "assistant"}},
			{Delta: ChatMessageDelta{Content: strPtr("hel")}},
			{Delta: ChatMessageDelta{Content: strPtr("lo")}},
			{Done: true},
		},
	}
	var sawUpdate bool
	a := NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
		Stream:   true,
		Bus: testBus(func(event Event) {
			if event.Type == EventMessageUpdate && messageContent(event.Message) != "" {
				sawUpdate = true
			}
		}),
	})

	result, err := a.Run(context.Background(), "stream")
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if result.Output != "hello" {
		t.Fatalf("output = %q, want hello", result.Output)
	}
	if !sawUpdate {
		t.Fatal("no message_update event during streaming")
	}
}

func TestResetDoesNotAllowConcurrentPrompt(t *testing.T) {
	tools := command.NewRegistry()
	llm := &blockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	a := NewAgent(Config{Provider: llm, Tools: tools, Model: "test"})

	done := make(chan error, 1)
	go func() {
		_, err := a.Run(context.Background(), "first")
		done <- err
	}()

	select {
	case <-llm.started:
	case <-time.After(time.Second):
		t.Fatal("provider was not called")
	}

	a.Reset()
	if _, err := a.Run(context.Background(), "second"); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second Prompt() error = %v, want already running", err)
	}

	close(llm.release)
	if err := <-done; err != nil {
		t.Fatalf("first Prompt() error = %v", err)
	}
}

func TestStreamingToolCallDeltasAreAggregated(t *testing.T) {
	tools := command.NewRegistry()
	echo := &recordingTool{name: "echo", output: "ok"}
	tools.RegisterTool(echo)
	llm := &scriptedProvider{
		streamEventBatches: [][]ChatCompletionStreamEvent{
			{
				{Delta: ChatMessageDelta{Role: "assistant"}},
				{Delta: ChatMessageDelta{ToolCalls: []ToolCallDelta{{
					Index: 0,
					ID:    "call-1",
					Type:  "function",
					Function: FunctionCallDelta{
						Name:      "echo",
						Arguments: `{"value":`,
					},
				}}}},
				{Delta: ChatMessageDelta{ToolCalls: []ToolCallDelta{{
					Index:    0,
					Function: FunctionCallDelta{Arguments: `"x"}`},
				}}}},
				{Done: true},
			},
			{
				{Delta: ChatMessageDelta{Role: "assistant"}},
				{Delta: ChatMessageDelta{Content: strPtr("final")}},
				{Done: true},
			},
		},
	}
	result, err := (NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
		Stream:   true,
	})).Run(context.Background(), "stream tool")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "final" {
		t.Fatalf("result = %q, want final", result.Output)
	}
	if got := echo.callsSnapshot(); !reflect.DeepEqual(got, []string{`{"value":"x"}`}) {
		t.Fatalf("tool calls = %#v", got)
	}
}

func TestToolHooksCanBlockRewriteAndTerminate(t *testing.T) {
	tools := command.NewRegistry()
	echo := &recordingTool{name: "echo", output: "raw"}
	tools.RegisterTool(echo)
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(ChatMessage{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Type: "function",
					Function: FunctionCall{
						Name:      "echo",
						Arguments: `{"value":"blocked"}`,
					},
				}},
			}),
		},
	}
	rewritten := "rewritten result"
	isError := false

	result, err := (NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
		BeforeToolCall: func(context.Context, BeforeToolCallContext) (*BeforeToolCallResult, error) {
			return &BeforeToolCallResult{Block: true, Reason: "blocked by test"}, nil
		},
		AfterToolCall: func(context.Context, AfterToolCallContext) (*AfterToolCallResult, error) {
			return &AfterToolCallResult{Result: &rewritten, IsError: &isError, Flow: ToolFlowTerminate}, nil
		},
	})).Run(context.Background(), "use tool")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := echo.callsSnapshot(); len(got) != 0 {
		t.Fatalf("tool calls = %#v, want blocked", got)
	}
	if len(llm.requestsSnapshot()) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(llm.requestsSnapshot()))
	}
	if !hasToolMessage(result.Messages, "call-1", rewritten) {
		t.Fatalf("result messages missing rewritten tool result: %#v", result.Messages)
	}
}

type recordingTool struct {
	name   string
	output string

	mu    sync.Mutex
	calls []string
}

func (t *recordingTool) Name() string { return t.name }

func (t *recordingTool) Description() string { return "recording tool" }

func (t *recordingTool) Definition() ToolDefinition {
	return ToolDefinition{
		Type: "function",
		Function: FunctionDefinition{
			Name:        t.name,
			Description: t.Description(),
			Parameters:  map[string]any{"type": "object"},
		},
	}
}

func (t *recordingTool) Execute(_ context.Context, arguments string) (command.ToolResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, arguments)
	if strings.Contains(arguments, "fail") {
		return command.ToolResult{}, fmt.Errorf("failed")
	}
	return command.TextResult(t.output), nil
}

func (t *recordingTool) callsSnapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.calls...)
}

type scriptedProvider struct {
	mu                 sync.Mutex
	responses          []*ChatCompletionResponse
	err                error
	streamEvents       []ChatCompletionStreamEvent
	streamEventBatches [][]ChatCompletionStreamEvent
	requests           []*ChatCompletionRequest
}

type blockingProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once

	mu       sync.Mutex
	requests []*ChatCompletionRequest
}

func (p *blockingProvider) Name() string { return "blocking" }
func (p *blockingProvider) WebSearch(_ context.Context, _ string, _ int) (*WebSearchResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *blockingProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	p.mu.Lock()
	p.requests = append(p.requests, cloneRequest(req))
	p.mu.Unlock()
	p.once.Do(func() { close(p.started) })
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return chatResponse(NewTextMessage("assistant", "done")), nil
}

func (p *scriptedProvider) Name() string { return "scripted" }
func (p *scriptedProvider) WebSearch(_ context.Context, _ string, _ int) (*WebSearchResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *scriptedProvider) ChatCompletion(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, cloneRequest(req))
	if p.err != nil {
		return nil, p.err
	}
	if len(p.responses) == 0 {
		return nil, fmt.Errorf("no scripted response left")
	}
	resp := p.responses[0]
	p.responses = p.responses[1:]
	return resp, nil
}

func (p *scriptedProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan ChatCompletionStreamEvent, error) {
	p.mu.Lock()
	p.requests = append(p.requests, cloneRequest(req))
	events := append([]ChatCompletionStreamEvent(nil), p.streamEvents...)
	if len(p.streamEventBatches) > 0 {
		events = append([]ChatCompletionStreamEvent(nil), p.streamEventBatches[0]...)
		p.streamEventBatches = p.streamEventBatches[1:]
	}
	p.mu.Unlock()

	ch := make(chan ChatCompletionStreamEvent)
	go func() {
		defer close(ch)
		for _, event := range events {
			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func (p *scriptedProvider) requestsSnapshot() []*ChatCompletionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*ChatCompletionRequest, 0, len(p.requests))
	for _, req := range p.requests {
		out = append(out, cloneRequest(req))
	}
	return out
}

func chatResponse(msg ChatMessage) *ChatCompletionResponse {
	return &ChatCompletionResponse{
		Choices: []Choice{{Message: msg}},
	}
}

func cloneRequest(req *ChatCompletionRequest) *ChatCompletionRequest {
	cloned := *req
	cloned.Messages = append([]ChatMessage(nil), req.Messages...)
	cloned.Tools = append([]ToolDefinition(nil), req.Tools...)
	return &cloned
}

func hasToolMessage(messages []ChatMessage, toolCallID, contains string) bool {
	for _, msg := range messages {
		if msg.Role != "tool" || msg.ToolCallID != toolCallID || msg.Content == nil {
			continue
		}
		if strings.Contains(*msg.Content, contains) {
			return true
		}
	}
	return false
}

func containsEvent(events []EventType, want EventType) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}

func eventTypes(events []Event) []EventType {
	out := make([]EventType, 0, len(events))
	for _, event := range events {
		out = append(out, event.Type)
	}
	return out
}

func lastEvent(events []Event) Event {
	if len(events) == 0 {
		return Event{}
	}
	return events[len(events)-1]
}

func strPtr(s string) *string {
	return &s
}

func TestRetryOnTransientError(t *testing.T) {
	tools := command.NewRegistry()
	callCount := 0
	llm := &callbackProvider{
		fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
			callCount++
			if callCount == 1 {
				return nil, fmt.Errorf("API error (502): bad gateway")
			}
			return chatResponse(NewTextMessage("assistant", "recovered")), nil
		},
	}

	result, err := (NewAgent(Config{
		Provider:   llm,
		Tools:      tools,
		Model:      "test",
		MaxRetries: 2,
	})).Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run() error = %v, want success after retry", err)
	}
	if result.Output != "recovered" {
		t.Fatalf("result = %q, want recovered", result.Output)
	}
	if callCount != 2 {
		t.Fatalf("call count = %d, want 2", callCount)
	}
}

func TestNoRetryOnAuthError(t *testing.T) {
	tools := command.NewRegistry()
	callCount := 0
	llm := &callbackProvider{
		fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
			callCount++
			return nil, fmt.Errorf("API error (401): invalid_api_key")
		},
	}

	_, err := (NewAgent(Config{
		Provider:   llm,
		Tools:      tools,
		Model:      "test",
		MaxRetries: 3,
	})).Run(context.Background(), "hello")
	if err == nil {
		t.Fatal("Run() error = nil, want auth error")
	}
	if callCount != 1 {
		t.Fatalf("call count = %d, want 1 (no retry for auth errors)", callCount)
	}
}

func TestRetryExhaustedReturnsLastError(t *testing.T) {
	tools := command.NewRegistry()
	callCount := 0
	llm := &callbackProvider{
		fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
			callCount++
			return nil, fmt.Errorf("API error (503): service unavailable")
		},
	}

	_, err := (NewAgent(Config{
		Provider:   llm,
		Tools:      tools,
		Model:      "test",
		MaxRetries: 2,
	})).Run(context.Background(), "hello")
	if err == nil {
		t.Fatal("Run() error = nil, want error after retries exhausted")
	}
	if callCount != 3 {
		t.Fatalf("call count = %d, want 3 (1 initial + 2 retries)", callCount)
	}
}

func TestRetryableProviderTimeoutAndStallErrors(t *testing.T) {
	if !isRetryableError(fmt.Errorf("wrapped: %w", ErrCallTimeout)) {
		t.Fatal("ErrCallTimeout should be retryable")
	}
	if !isRetryableError(fmt.Errorf("wrapped: %w", ErrStreamStalled)) {
		t.Fatal("ErrStreamStalled should be retryable")
	}
	if !isRetryableError(retryableTimeoutError{}) {
		t.Fatal("network timeout should be retryable")
	}
	if isRetryableError(fmt.Errorf("wrapped: %w", context.Canceled)) {
		t.Fatal("context.Canceled should not be retryable")
	}
	if isRetryableError(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)) {
		t.Fatal("context.DeadlineExceeded should not be retryable")
	}
}

func TestStreamAssistantMessageReturnsContextErrorOnClosedCanceledStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := streamAssistantMessageWithUsage(ctx,
		&scriptedProvider{},
		&ChatCompletionRequest{Model: "test"},
		newEmitter(eventbus.New[Event](), "test"),
		telemetry.NopLogger(),
		1,
	)
	if err != context.Canceled {
		t.Fatalf("streamAssistantMessageWithUsage() error = %v, want context.Canceled", err)
	}
}

func TestTokenBudgetWarning(t *testing.T) {
	tools := command.NewRegistry()
	llm := &callbackProvider{
		fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
			return &ChatCompletionResponse{
				Choices: []Choice{{Message: NewTextMessage("assistant", "done")}},
				Usage:   &Usage{PromptTokens: 700, CompletionTokens: 200, TotalTokens: 900},
			}, nil
		},
	}

	var sawWarning bool
	_, err := (NewAgent(Config{
		Provider:    llm,
		Tools:       tools,
		Model:       "test",
		TokenBudget: 1000,
		Bus: testBus(func(event Event) {
			if event.Type == EventTokenBudgetWarning {
				sawWarning = true
			}
		}),
	})).Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !sawWarning {
		t.Fatal("expected token_budget_warning event at 90% usage")
	}
}

func TestTokenBudgetExceeded(t *testing.T) {
	tools := command.NewRegistry()
	turn := 0
	llm := &callbackProvider{
		fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
			turn++
			if turn == 1 {
				return &ChatCompletionResponse{
					Choices: []Choice{{Message: ChatMessage{
						Role: "assistant",
						ToolCalls: []ToolCall{{
							ID:       "call-1",
							Type:     "function",
							Function: FunctionCall{Name: "echo", Arguments: `{}`},
						}},
					}}},
					Usage: &Usage{TotalTokens: 600},
				}, nil
			}
			return &ChatCompletionResponse{
				Choices: []Choice{{Message: NewTextMessage("assistant", "done")}},
				Usage:   &Usage{TotalTokens: 500},
			}, nil
		},
	}
	tools.RegisterTool(&recordingTool{name: "echo", output: "ok"})

	result, err := (NewAgent(Config{
		Provider:    llm,
		Tools:       tools,
		Model:       "test",
		TokenBudget: 1000,
	})).Run(context.Background(), "hello")
	if err == nil {
		t.Fatal("Run() error = nil, want budget exceeded error")
	}
	if !strings.Contains(err.Error(), "token budget exhausted") {
		t.Fatalf("error = %v, want token budget exhausted", err)
	}
	if result == nil || result.TotalUsage.TotalTokens == 0 {
		t.Fatal("result should contain accumulated usage")
	}
}

func TestTruncateResultIncludesSize(t *testing.T) {
	large := strings.Repeat("x", DefaultMaxResultSize+100)
	truncated := truncateResult(large)
	if !strings.Contains(truncated, "truncated:") {
		t.Fatalf("truncated result missing size info: %s", truncated[len(truncated)-100:])
	}
	if !strings.Contains(truncated, fmt.Sprintf("%d of %d bytes", DefaultMaxResultSize, len(large))) {
		t.Fatalf("truncated result missing byte counts: %s", truncated[len(truncated)-120:])
	}
}

func TestResultIncludesTotalUsage(t *testing.T) {
	tools := command.NewRegistry()
	llm := &callbackProvider{
		fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
			return &ChatCompletionResponse{
				Choices: []Choice{{Message: NewTextMessage("assistant", "done")}},
				Usage:   &Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
			}, nil
		},
	}

	result, err := (NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
	})).Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.TotalUsage.TotalTokens != 150 {
		t.Fatalf("TotalUsage.TotalTokens = %d, want 150", result.TotalUsage.TotalTokens)
	}
}

func TestResultIncludesPerTurnUsageAndContextTokens(t *testing.T) {
	tools := command.NewRegistry()
	tools.RegisterTool(&recordingTool{name: "echo", output: "ok"})

	turn := 0
	llm := &callbackProvider{
		fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
			turn++
			if turn == 1 {
				return &ChatCompletionResponse{
					Choices: []Choice{{Message: ChatMessage{
						Role: "assistant",
						ToolCalls: []ToolCall{{
							ID: "call-1", Type: "function",
							Function: FunctionCall{Name: "echo", Arguments: `{}`},
						}},
					}}},
					Usage: &Usage{PromptTokens: 200, CompletionTokens: 30, TotalTokens: 230},
				}, nil
			}
			return &ChatCompletionResponse{
				Choices: []Choice{{Message: NewTextMessage("assistant", "done")}},
				Usage:   &Usage{PromptTokens: 280, CompletionTokens: 20, TotalTokens: 300},
			}, nil
		},
	}

	result, err := (NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
	})).Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.TurnUsages) != 2 {
		t.Fatalf("TurnUsages length = %d, want 2", len(result.TurnUsages))
	}
	if result.TurnUsages[0].Turn != 1 || result.TurnUsages[0].TotalTokens != 230 {
		t.Errorf("TurnUsages[0] = %+v, want turn=1 total=230", result.TurnUsages[0])
	}
	if result.TurnUsages[1].Turn != 2 || result.TurnUsages[1].TotalTokens != 300 {
		t.Errorf("TurnUsages[1] = %+v, want turn=2 total=300", result.TurnUsages[1])
	}
	if result.TotalUsage.TotalTokens != 530 {
		t.Errorf("TotalUsage.TotalTokens = %d, want 530", result.TotalUsage.TotalTokens)
	}
	if result.TotalUsage.PromptTokens != 480 {
		t.Errorf("TotalUsage.PromptTokens = %d, want 480", result.TotalUsage.PromptTokens)
	}
	if result.ContextTokens != 280 {
		t.Errorf("ContextTokens = %d, want 280 (last turn prompt tokens)", result.ContextTokens)
	}
}

func TestTurnEndEventCarriesUsage(t *testing.T) {
	tools := command.NewRegistry()
	llm := &callbackProvider{
		fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
			return &ChatCompletionResponse{
				Choices: []Choice{{Message: NewTextMessage("assistant", "done")}},
				Usage:   &Usage{PromptTokens: 500, CompletionTokens: 40, TotalTokens: 540},
			}, nil
		},
	}

	var turnEndUsage *Usage
	var turnEndContext int
	_, err := (NewAgent(Config{
		Provider: llm,
		Tools:    tools,
		Model:    "test",
		Bus: testBus(func(event Event) {
			if event.Type == EventTurnEnd {
				turnEndUsage = event.Usage
				turnEndContext = event.ContextTokens
			}
		}),
	})).Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if turnEndUsage == nil {
		t.Fatal("EventTurnEnd.Usage is nil")
	}
	if turnEndUsage.TotalTokens != 540 {
		t.Errorf("EventTurnEnd Usage.TotalTokens = %d, want 540", turnEndUsage.TotalTokens)
	}
	if turnEndContext != 500 {
		t.Errorf("EventTurnEnd ContextTokens = %d, want 500", turnEndContext)
	}
}

type callbackProvider struct {
	fn func(context.Context, *ChatCompletionRequest) (*ChatCompletionResponse, error)
}

func (p *callbackProvider) Name() string { return "callback" }
func (p *callbackProvider) WebSearch(_ context.Context, _ string, _ int) (*WebSearchResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *callbackProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	return p.fn(ctx, req)
}

type retryableTimeoutError struct{}

func (retryableTimeoutError) Error() string   { return "timeout awaiting response headers" }
func (retryableTimeoutError) Timeout() bool   { return true }
func (retryableTimeoutError) Temporary() bool { return true }

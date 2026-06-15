package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/inbox"
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

func TestInboxDrainedBeforeFirstTurnLLMCall(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(NewTextMessage("assistant", "ack")),
		},
	}
	ib := inbox.NewBuffered(4)
	ib.Push(inbox.NewMessage(inbox.OriginPeer, "user", "[peer] hello"))
	ib.Push(inbox.NewMessage(inbox.OriginPeer, "user", "[peer] status?"))

	result, err := NewAgent(Config{
		Provider:     llm,
		Tools:        tools,
		Model:        "test",
		SystemPrompt: "system",
		Inbox:        ib,
	}).Run(context.Background(), "main task")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "ack" {
		t.Fatalf("result = %q, want ack", result.Output)
	}

	requests := llm.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requests))
	}
	msgs := requests[0].Messages
	if len(msgs) != 4 {
		t.Fatalf("messages = %d, want 4 (system + 2 peer + task): %#v", len(msgs), msgs)
	}
	if msgs[0].Role != "system" {
		t.Fatalf("msg[0].Role = %q, want system", msgs[0].Role)
	}
	if got := contentOf(msgs[1]); !strings.Contains(got, "[peer] hello") {
		t.Fatalf("msg[1] missing peer content: %q", got)
	}
	if got := contentOf(msgs[2]); !strings.Contains(got, "[peer] status?") {
		t.Fatalf("msg[2] missing peer content: %q", got)
	}
	if got := contentOf(msgs[3]); got != "main task" {
		t.Fatalf("msg[3] = %q, want main task", got)
	}
}

func TestInboxClosedDoesNotBlock(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(NewTextMessage("assistant", "done")),
		},
	}

	result, err := NewAgent(Config{
		Provider:     llm,
		Tools:        tools,
		Model:        "test",
		SystemPrompt: "system",
	}).Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "done" {
		t.Fatalf("result = %q, want done", result.Output)
	}
}

type pushingProvider struct {
	inner  Provider
	inbox  *inbox.Buffered
	pushed bool
	push   inbox.Message
}

func (p *pushingProvider) Name() string { return "pushing" }
func (p *pushingProvider) WebSearch(_ context.Context, _ string, _ int) (*WebSearchResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *pushingProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	if !p.pushed {
		p.pushed = true
		p.inbox.Push(p.push)
	}
	return p.inner.ChatCompletion(ctx, req)
}

func TestInboxDrainedBetweenTurns(t *testing.T) {
	tools := command.NewRegistry()
	tools.RegisterTool(&recordingTool{name: "echo", output: "tool output"})

	scripted := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(ChatMessage{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: FunctionCall{Name: "echo", Arguments: "{}"},
				}},
			}),
			chatResponse(NewTextMessage("assistant", "final")),
		},
	}

	ib := inbox.NewBuffered(4)
	pushing := &pushingProvider{
		inner: scripted,
		inbox: ib,
		push:  inbox.NewMessage(inbox.OriginPeer, "user", "[peer] watch out for example.com"),
	}

	result, err := NewAgent(Config{
		Provider:     pushing,
		Tools:        tools,
		Model:        "test",
		SystemPrompt: "system",
		Inbox:        ib,
	}).Run(context.Background(), "scan things")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "final" {
		t.Fatalf("result = %q, want final", result.Output)
	}

	requests := scripted.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}

	turn1Msgs := requests[0].Messages
	for _, m := range turn1Msgs {
		if strings.Contains(contentOf(m), "[peer] watch out for example.com") {
			t.Fatalf("turn 1 unexpectedly contains peer message: %#v", turn1Msgs)
		}
	}

	turn2Msgs := requests[1].Messages
	found := false
	for _, m := range turn2Msgs {
		if strings.Contains(contentOf(m), "[peer] watch out for example.com") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("turn 2 missing peer message: %#v", turn2Msgs)
	}
}

func TestRunWaitsWhenKeepAliveIsTrue(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(NewTextMessage("assistant", "waiting")),
			chatResponse(NewTextMessage("assistant", "final")),
		},
	}
	ib := inbox.NewBuffered(4)
	producer := ib.RegisterProducer("test-bg-task")

	go func() {
		defer producer.Done()
		time.Sleep(20 * time.Millisecond)
		ib.Push(inbox.NewMessage(inbox.OriginSession, "user", "<session_completion>scan done</session_completion>"))
	}()

	result, err := NewAgent(Config{
		Provider:     llm,
		Tools:        tools,
		Model:        "test",
		SystemPrompt: "system",
		Inbox:        ib,
	}).Run(context.Background(), "start background scan")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "final" {
		t.Fatalf("result = %q, want final", result.Output)
	}
	requests := llm.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	found := false
	for _, msg := range requests[1].Messages {
		if strings.Contains(contentOf(msg), "<session_completion>") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("second request missing task completion: %#v", requests[1].Messages)
	}
}

func TestRunReleasesWhenProducerCompletesWithoutMessage(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(NewTextMessage("assistant", "waiting")),
			chatResponse(NewTextMessage("assistant", "should-not-need-second-turn")),
		},
	}
	ib := inbox.NewBuffered(4)
	producer := ib.RegisterProducer("silent-bg-task")
	go func() {
		time.Sleep(20 * time.Millisecond)
		producer.Done()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := NewAgent(Config{
		Provider:     llm,
		Tools:        tools,
		Model:        "test",
		SystemPrompt: "system",
		Inbox:        ib,
	}).Run(ctx, "start silent background task")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "waiting" {
		t.Fatalf("result = %q, want waiting", result.Output)
	}
	if got := len(llm.requestsSnapshot()); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
}

func TestProducerBarrierBatchesSubagentCompletions(t *testing.T) {
	// Regression guard for the source fix: when several subagents (inbox
	// producers) finish at staggered times, the parent must batch their
	// completion messages and synthesize ONCE after all finish, instead of
	// re-summarizing on every trickle. Before the fix the parent re-announced
	// "complete" once per subagent, re-reading a huge context each time.
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(NewTextMessage("assistant", "ack")),
			chatResponse(NewTextMessage("assistant", "final synthesis")),
		},
	}
	ib := inbox.NewBuffered(8)
	var turnStarts, turnEnds int

	for i, name := range []string{"desk", "mall", "chat"} {
		producer := ib.RegisterProducer("subagent:" + name)
		i, name := i, name
		go func() {
			defer producer.Done()
			time.Sleep(time.Duration(20+i*20) * time.Millisecond)
			ib.Push(inbox.NewMessage(inbox.OriginSystem, "user",
				fmt.Sprintf("<subagent_completion name=%q>found %d</subagent_completion>", name, i+1)))
		}()
	}

	result, err := NewAgent(Config{
		Provider:     llm,
		Tools:        tools,
		Model:        "test",
		SystemPrompt: "system",
		Inbox:        ib,
		Bus: testBus(func(event Event) {
			switch event.Type {
			case EventTurnStart:
				turnStarts++
			case EventTurnEnd:
				turnEnds++
			}
		}),
	}).Run(context.Background(), "scan 3 subsystems")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "final synthesis" {
		t.Fatalf("result = %q, want final synthesis", result.Output)
	}
	if result.Turns != 2 {
		t.Fatalf("turns = %d, want 2 (waiting for producers must not consume turns)", result.Turns)
	}
	if turnStarts != 2 || turnEnds != 2 {
		t.Fatalf("turn events start/end = %d/%d, want 2/2", turnStarts, turnEnds)
	}

	requests := llm.requestsSnapshot()
	// Exactly 2 LLM calls: opening ack + a single batched final synthesis.
	// A broken barrier yields ~4 (ack + one re-synthesis per completion).
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2 (ack + single batched synthesis)", len(requests))
	}

	// The single final synthesis must contain all three completions.
	finalMsgs := requests[1].Messages
	for _, name := range []string{"desk", "mall", "chat"} {
		needle := fmt.Sprintf("name=%q", name)
		found := false
		for _, m := range finalMsgs {
			if strings.Contains(contentOf(m), needle) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("final synthesis missing %s completion: %#v", needle, finalMsgs)
		}
	}
}

func TestTerminatingToolResultTerminatesLoop(t *testing.T) {
	// A tool whose Execute returns command.TerminateResult should end the run
	// without paying for an automatic follow-up LLM turn.
	tools := command.NewRegistry()
	tools.RegisterTool(&terminatingTool{name: "finalize", output: "all subsystems reported"})
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(ChatMessage{
				Role: "assistant",
				ToolCalls: []ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: FunctionCall{Name: "finalize", Arguments: `{}`},
				}},
			}),
			// Must never be consumed: the terminating batch skips the follow-up LLM call.
			chatResponse(NewTextMessage("assistant", "should-not-reach")),
		},
	}

	result, err := NewAgent(Config{
		Provider:     llm,
		Tools:        tools,
		Model:        "test",
		SystemPrompt: "system",
		MaxTurns:     1,
	}).Run(context.Background(), "scan")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	requests := llm.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want 1 (terminating batch skips follow-up LLM call)", len(requests))
	}
	if result.Output != "all subsystems reported" {
		t.Fatalf("result output = %q, want terminating tool result", result.Output)
	}
	found := false
	for _, m := range result.Messages {
		if strings.Contains(contentOf(m), "all subsystems reported") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("transcript missing terminating tool result: %#v", result.Messages)
	}
}

func TestMixedTerminatingAndNonTerminatingToolCallsContinue(t *testing.T) {
	tools := command.NewRegistry()
	tools.RegisterTool(&recordingTool{name: "echo", output: "ok"})
	tools.RegisterTool(&terminatingTool{name: "finalize", output: "done"})
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(ChatMessage{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						ID:       "call_finalize",
						Type:     "function",
						Function: FunctionCall{Name: "finalize", Arguments: `{}`},
					},
					{
						ID:       "call_echo",
						Type:     "function",
						Function: FunctionCall{Name: "echo", Arguments: `{"value":"x"}`},
					},
				},
			}),
			chatResponse(NewTextMessage("assistant", "synthesized after mixed batch")),
		},
	}

	result, err := NewAgent(Config{
		Provider:     llm,
		Tools:        tools,
		Model:        "test",
		SystemPrompt: "system",
	}).Run(context.Background(), "scan")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got := len(llm.requestsSnapshot()); got != 2 {
		t.Fatalf("requests = %d, want 2 (mixed terminating/non-terminating batch must continue)", got)
	}
	if result.Output != "synthesized after mixed batch" {
		t.Fatalf("result output = %q, want follow-up synthesis", result.Output)
	}
}

type terminatingTool struct {
	name   string
	output string
}

func (t *terminatingTool) Name() string { return t.name }

func (t *terminatingTool) Description() string {
	return "test tool that returns a terminating result"
}

func (t *terminatingTool) Definition() ToolDefinition {
	return command.ToolDef(t.Name(), t.Description(), struct{}{})
}

func (t *terminatingTool) Execute(context.Context, string) (command.ToolResult, error) {
	return command.TerminateResult(t.output), nil
}

func TestRunWaitsForLoopSchedulerInboxFire(t *testing.T) {
	tools := command.NewRegistry()
	ib := inbox.NewBuffered(4)
	scheduler := NewLoopScheduler(ib, telemetry.NopLogger())
	scheduler.SetMinInterval(time.Millisecond)
	defer scheduler.Stop()

	var requestCount int
	llm := &callbackProvider{
		fn: func(_ context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
			requestCount++
			switch requestCount {
			case 1:
				return chatResponse(NewTextMessage("assistant", "waiting")), nil
			case 2:
				if err := scheduler.Remove("heartbeat"); err != nil {
					t.Errorf("Remove heartbeat: %v", err)
				}
				return chatResponse(NewTextMessage("assistant", "final")), nil
			default:
				return nil, fmt.Errorf("unexpected request %d: %#v", requestCount, req.Messages)
			}
		},
	}

	if err := scheduler.Add(context.Background(), LoopEntry{
		Name:     "heartbeat",
		Interval: 20 * time.Millisecond,
		Prompt:   "heartbeat check: read IOA context",
		Mode:     ModeInbox,
	}); err != nil {
		t.Fatalf("scheduler.Add() error = %v", err)
	}

	result, err := NewAgent(Config{
		Provider:      llm,
		Tools:         tools,
		Model:         "test",
		SystemPrompt:  "system",
		Inbox:         ib,
		LoopScheduler: scheduler,
	}).Run(context.Background(), "start loop")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "final" {
		t.Fatalf("result = %q, want final", result.Output)
	}
	if requestCount != 2 {
		t.Fatalf("requests = %d, want 2", requestCount)
	}

	found := false
	for _, msg := range result.Messages {
		if strings.Contains(contentOf(msg), "heartbeat check: read IOA context") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("result messages missing heartbeat prompt: %#v", result.Messages)
	}
}

func TestLoopSchedulerSkipsInboxWhenPromptFuncFails(t *testing.T) {
	ib := inbox.NewBuffered(4)
	scheduler := NewLoopScheduler(ib, telemetry.NopLogger())
	scheduler.SetMinInterval(time.Millisecond)
	defer scheduler.Stop()

	if err := scheduler.Add(context.Background(), LoopEntry{
		Name:     "heartbeat",
		Interval: time.Millisecond,
		Prompt:   "static prompt should not be injected",
		PromptFunc: func(context.Context, LoopEntry) (string, error) {
			return "", fmt.Errorf("ioa unavailable")
		},
		Mode:      ModeInbox,
		Immediate: true,
	}); err != nil {
		t.Fatalf("scheduler.Add() error = %v", err)
	}

	if got := ib.Len(); got != 0 {
		t.Fatalf("inbox messages = %d, want 0", got)
	}
}

func contentOf(m ChatMessage) string {
	if m.Content == nil {
		return ""
	}
	return *m.Content
}

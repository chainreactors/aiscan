package agent

import (
	"context"
	"testing"

	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/provider"
)

func TestInboxDrainedBeforeFirstTurnLLMCall(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*provider.ChatCompletionResponse{
			chatResponse(provider.NewTextMessage("assistant", "ack")),
		},
	}
	inbox := make(chan provider.ChatMessage, 4)
	inbox <- provider.NewTextMessage("user", "[peer] hello")
	inbox <- provider.NewTextMessage("user", "[peer] status?")

	result, err := Run(context.Background(), "main task", tools,
		WithProvider(llm),
		WithModel("test"),
		WithSystemPrompt("system"),
		WithInbox(inbox),
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != "ack" {
		t.Fatalf("result = %q, want ack", result)
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
	if got := contentOf(msgs[1]); got != "[peer] hello" {
		t.Fatalf("msg[1] = %q, want [peer] hello", got)
	}
	if got := contentOf(msgs[2]); got != "[peer] status?" {
		t.Fatalf("msg[2] = %q, want [peer] status?", got)
	}
	if got := contentOf(msgs[3]); got != "main task" {
		t.Fatalf("msg[3] = %q, want main task", got)
	}
}

func TestInboxClosedDoesNotBlock(t *testing.T) {
	tools := command.NewRegistry()
	llm := &scriptedProvider{
		responses: []*provider.ChatCompletionResponse{
			chatResponse(provider.NewTextMessage("assistant", "done")),
		},
	}
	inbox := make(chan provider.ChatMessage)
	close(inbox)

	result, err := Run(context.Background(), "task", tools,
		WithProvider(llm),
		WithModel("test"),
		WithSystemPrompt("system"),
		WithInbox(inbox),
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != "done" {
		t.Fatalf("result = %q, want done", result)
	}
}

// pushingProvider pushes a message into an inbox during the first LLM call,
// simulating a swarm peer that fires while the agent is mid-turn. Turn 2's
// drain should then pick it up.
type pushingProvider struct {
	inner  provider.Provider
	inbox  chan<- provider.ChatMessage
	pushed bool
	push   provider.ChatMessage
}

func (p *pushingProvider) Name() string { return "pushing" }

func (p *pushingProvider) ChatCompletion(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	if !p.pushed {
		p.pushed = true
		p.inbox <- p.push
	}
	return p.inner.ChatCompletion(ctx, req)
}

func TestInboxDrainedBetweenTurns(t *testing.T) {
	tools := command.NewRegistry()
	tools.RegisterTool(&recordingTool{name: "echo", output: "tool output"})

	scripted := &scriptedProvider{
		responses: []*provider.ChatCompletionResponse{
			chatResponse(provider.ChatMessage{
				Role: "assistant",
				ToolCalls: []provider.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: provider.FunctionCall{Name: "echo", Arguments: "{}"},
				}},
			}),
			chatResponse(provider.NewTextMessage("assistant", "final")),
		},
	}

	inbox := make(chan provider.ChatMessage, 4)
	pushing := &pushingProvider{
		inner: scripted,
		inbox: inbox,
		push:  provider.NewTextMessage("user", "[peer] watch out for example.com"),
	}

	result, err := Run(context.Background(), "scan things", tools,
		WithProvider(pushing),
		WithModel("test"),
		WithSystemPrompt("system"),
		WithInbox(inbox),
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != "final" {
		t.Fatalf("result = %q, want final", result)
	}

	requests := scripted.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}

	// Turn 1 should NOT have the peer message yet — it was pushed during this very call.
	turn1Msgs := requests[0].Messages
	for _, m := range turn1Msgs {
		if contentOf(m) == "[peer] watch out for example.com" {
			t.Fatalf("turn 1 unexpectedly contains peer message: %#v", turn1Msgs)
		}
	}

	// Turn 2 should have the peer message between tool result and the next user-or-assistant boundary.
	turn2Msgs := requests[1].Messages
	found := false
	for _, m := range turn2Msgs {
		if contentOf(m) == "[peer] watch out for example.com" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("turn 2 missing peer message: %#v", turn2Msgs)
	}
}

func contentOf(m provider.ChatMessage) string {
	if m.Content == nil {
		return ""
	}
	return *m.Content
}

package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/inbox"
	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/agent/task"
)

func TestTaskCompletionInjectedIntoAgentLoop(t *testing.T) {
	tools := command.NewRegistry()
	tools.RegisterTool(&recordingTool{name: "echo", output: "tool output"})

	// Turn 1: LLM calls tool "echo" → tool result returned.
	// Meanwhile, a background task completes and fires the OnComplete callback,
	// which pushes a completion message into the inbox.
	// Turn 2: LLM sees the task_completion message in its context and responds.

	ib := inbox.NewBuffered(8)
	dir := t.TempDir()
	taskMgr := task.NewManager(filepath.Join(dir, "tasks"))
	taskMgr.SetOnComplete(func(info task.Info, killed bool, cause string) {
		msg := inbox.NewMessage(inbox.OriginTask, "user", task.FormatCompletion(info, killed, cause))
		msg.Meta = map[string]any{"task_id": info.ID}
		ib.Push(msg)
	})

	// Spawn a quick background task that will complete before turn 2.
	_, err := taskMgr.Spawn(dir, "echo background-result", "bg-scan", 10*time.Second)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Wait for the task to complete so the inbox has the message.
	time.Sleep(500 * time.Millisecond)

	scripted := &scriptedProvider{
		responses: []*provider.ChatCompletionResponse{
			// Turn 1: call a tool
			chatResponse(provider.ChatMessage{
				Role: "assistant",
				ToolCalls: []provider.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: provider.FunctionCall{Name: "echo", Arguments: "{}"},
				}},
			}),
			// Turn 2: see task completion + respond
			chatResponse(provider.NewTextMessage("assistant", "saw the background task")),
		},
	}

	result, err := Run(context.Background(), "run a scan", tools,
		WithProvider(scripted),
		WithModel("test"),
		WithSystemPrompt("system"),
		WithInbox(ib),
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result != "saw the background task" {
		t.Fatalf("result = %q, want 'saw the background task'", result)
	}

	// Verify turn 2's request includes the task completion message.
	requests := scripted.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("expected 2 LLM requests, got %d", len(requests))
	}

	turn2Msgs := requests[1].Messages
	found := false
	for _, m := range turn2Msgs {
		if m.Content != nil && strings.Contains(*m.Content, "task_completion") {
			found = true
			if !strings.Contains(*m.Content, "background-result") {
				t.Errorf("task completion should contain stdout, got: %s", *m.Content)
			}
			break
		}
	}
	if !found {
		var contents []string
		for _, m := range turn2Msgs {
			if m.Content != nil {
				contents = append(contents, *m.Content)
			}
		}
		t.Fatalf("turn 2 missing task_completion message.\nMessages:\n%s", strings.Join(contents, "\n---\n"))
	}
}

func TestTaskCompletionMetadata(t *testing.T) {
	ib := inbox.NewBuffered(4)
	dir := t.TempDir()
	taskMgr := task.NewManager(filepath.Join(dir, "tasks"))

	var received []inbox.Message
	taskMgr.SetOnComplete(func(info task.Info, killed bool, cause string) {
		msg := inbox.NewMessage(inbox.OriginTask, "user", task.FormatCompletion(info, killed, cause))
		msg.Meta = map[string]any{
			"task_id":   info.ID,
			"task_name": info.Name,
			"exit_code": info.ExitCode,
		}
		ib.Push(msg)
	})

	_, err := taskMgr.Spawn(dir, "echo done", "test-task", 10*time.Second)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	received = ib.Drain()
	if len(received) == 0 {
		t.Fatal("expected at least 1 inbox message from task completion")
	}

	msg := received[0]
	if msg.Origin != inbox.OriginTask {
		t.Errorf("origin = %q, want %q", msg.Origin, inbox.OriginTask)
	}
	if msg.Meta["task_name"] != "test-task" {
		t.Errorf("task_name = %v, want test-task", msg.Meta["task_name"])
	}
	if msg.Meta["exit_code"] != 0 {
		t.Errorf("exit_code = %v, want 0", msg.Meta["exit_code"])
	}

	cms := msg.ToChatMessages()
	if len(cms) != 1 {
		t.Fatalf("expected 1 chat message, got %d", len(cms))
	}
	if !strings.Contains(*cms[0].Content, "task_completion") {
		t.Errorf("chat message should contain task_completion XML, got: %s", *cms[0].Content)
	}
}

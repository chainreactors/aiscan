package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/agent"
)

func TestRenderAgentMarkdownPlainFallback(t *testing.T) {
	got := renderAgentMarkdown("  ## Title\n\n- item  ", false)
	want := "## Title\n\n- item"
	if got != want {
		t.Fatalf("renderAgentMarkdown() = %q, want %q", got, want)
	}
}

func TestAgentOutputFinalWritesPlainMarkdownWithoutWrapper(t *testing.T) {
	var stdout bytes.Buffer
	output := &agentOutput{
		stdout:   &stdout,
		stderr:   &bytes.Buffer{},
		markdown: false,
	}

	output.Final("## Report\n\nDone.")

	got := stdout.String()
	if !strings.Contains(got, "## Report") || !strings.Contains(got, "Done.") {
		t.Fatalf("final output missing markdown content: %q", got)
	}
	if strings.Contains(got, "[assistant]") || strings.Contains(got, "[final_report]") {
		t.Fatalf("final output contains legacy wrapper: %q", got)
	}
}

func TestAgentOutputToolSummary(t *testing.T) {
	var stderr bytes.Buffer
	output := &agentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		tools:  make(map[string]agentToolSummary),
	}

	err := output.HandleEvent(context.Background(), agent.Event{
		Type:       agent.EventToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "bash",
		Arguments:  `{"command":"scan -i 127.0.0.1 --mode quick"}`,
	})
	if err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
	err = output.HandleEvent(context.Background(), agent.Event{
		Type:       agent.EventToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "bash",
		Result:     "ok",
	})
	if err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}

	got := stderr.String()
	if !strings.Contains(got, "- bash: scan -i 127.0.0.1 --mode quick") {
		t.Fatalf("stderr missing short tool summary: %q", got)
	}
	if strings.Contains(got, "args:") || strings.Contains(got, "result:") {
		t.Fatalf("balanced output should not include debug details: %q", got)
	}
}

func TestAgentOutputToolDebugDetails(t *testing.T) {
	var stderr bytes.Buffer
	output := &agentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		debug:  true,
		tools:  make(map[string]agentToolSummary),
	}

	_ = output.HandleEvent(context.Background(), agent.Event{
		Type:       agent.EventToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "read",
		Arguments:  `{"path":"docs/usage.md","limit":20}`,
	})
	_ = output.HandleEvent(context.Background(), agent.Event{
		Type:       agent.EventToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "read",
		Result:     "file content",
	})

	got := stderr.String()
	if !strings.Contains(got, "- read: docs/usage.md") {
		t.Fatalf("stderr missing read summary: %q", got)
	}
	if !strings.Contains(got, `args: {"path":"docs/usage.md","limit":20}`) {
		t.Fatalf("stderr missing compact args in debug mode: %q", got)
	}
	if !strings.Contains(got, "result: file content") {
		t.Fatalf("stderr missing result preview in debug mode: %q", got)
	}
}

func TestAgentOutputToolError(t *testing.T) {
	var stderr bytes.Buffer
	output := &agentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		tools:  make(map[string]agentToolSummary),
	}

	_ = output.HandleEvent(context.Background(), agent.Event{
		Type:       agent.EventToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "bash",
		Result:     "permission denied",
		IsError:    true,
	})

	if got := stderr.String(); !strings.Contains(got, "error: permission denied") {
		t.Fatalf("stderr missing tool error: %q", got)
	}
}

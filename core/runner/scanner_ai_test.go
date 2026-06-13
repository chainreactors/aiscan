package runner

import (
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/command"
)

func TestRegisterScannerCheckpointToolAddsVisibleTool(t *testing.T) {
	reg := command.NewRegistry()
	checkpoint := registerScannerCheckpointTool(reg)
	if checkpoint == nil {
		t.Fatal("checkpoint tool is nil")
	}
	if _, ok := reg.GetTool("checkpoint"); !ok {
		t.Fatal("checkpoint tool was not registered")
	}

	prompt := BuildSystemPrompt(&PromptConfig{
		Tools:            reg,
		ScannerAgentMode: true,
		ScannerName:      "scan",
	}, &agent.Config{Tools: reg})
	if !strings.Contains(prompt, "### checkpoint") {
		t.Fatalf("scanner prompt does not expose checkpoint tool:\n%s", prompt)
	}
	if strings.Contains(prompt, "call the `finish` tool") {
		t.Fatalf("scanner prompt should not instruct finish tool termination:\n%s", prompt)
	}
	if !strings.Contains(prompt, "call the `checkpoint` tool exactly once") {
		t.Fatalf("scanner prompt should instruct checkpoint termination:\n%s", prompt)
	}
}

func TestGeneralPromptUsesFinishTerminationWhenToolRegistered(t *testing.T) {
	reg := command.NewRegistry()
	reg.RegisterTool(agent.NewFinishTool())

	prompt := BuildSystemPrompt(&PromptConfig{
		Tools: reg,
	}, &agent.Config{Tools: reg})

	if !strings.Contains(prompt, "### finish") {
		t.Fatalf("prompt does not expose finish tool:\n%s", prompt)
	}
	if !strings.Contains(prompt, "call the `finish` tool exactly once") {
		t.Fatalf("prompt should instruct finish termination:\n%s", prompt)
	}
}

func TestFormatCheckpointMarkdown(t *testing.T) {
	got := formatCheckpointMarkdown(&command.CheckpointResult{
		Kind:    "verify",
		Title:   "CORS check",
		Target:  "https://example.test",
		Status:  "confirmed",
		Labels:  []string{"high", "cors"},
		Options: []string{"save-report"},
		Content: "Evidence: credentialed cross-origin request succeeded.",
	})

	for _, want := range []string{
		"## [verify] CORS check",
		"- target: https://example.test",
		"- status: confirmed",
		"- labels: high, cors",
		"- options: save-report",
		"Evidence: credentialed cross-origin request succeeded.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("checkpoint markdown missing %q:\n%s", want, got)
		}
	}
}

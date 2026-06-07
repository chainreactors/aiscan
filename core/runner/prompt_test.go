package runner

import (
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/skills"
)

func TestBuildSystemPromptIncludesSkills(t *testing.T) {
	tools := command.NewRegistry()
	loaded, diagnostics := skills.LoadEmbedded()
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}

	prompt := BuildSystemPrompt(&PromptConfig{
		Tools:  tools,
		Skills: loaded,
	})
	for _, want := range []string{
		"## Available Skills",
		"<available_skills>",
		"<name>aiscan</name>",
		"aiscan://skills/aiscan/SKILL.md",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, internal := range []string{"scan", "gogo", "spray", "katana", "fuzz", "zombie", "neutron"} {
		if strings.Contains(prompt, "<name>"+internal+"</name>") {
			t.Fatalf("prompt includes internal skill %q:\n%s", internal, prompt)
		}
	}
}

func TestBuildSystemPromptAllowsNilConfig(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	if !strings.Contains(prompt, "## Available Tools") {
		t.Fatalf("prompt missing tools section:\n%s", prompt)
	}
}

package runner

import (
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/agent"
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
	}, nil)
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
	prompt := BuildSystemPrompt(nil, nil)
	if !strings.Contains(prompt, "## Environment") {
		t.Fatalf("prompt missing environment section:\n%s", prompt)
	}
	if !strings.Contains(prompt, "## Key Principles") {
		t.Fatalf("prompt missing principles section:\n%s", prompt)
	}
}

func TestSystemPromptFuncAdaptsToTools(t *testing.T) {
	cfg := &PromptConfig{}
	fn := SystemPromptFunc(cfg)

	result := fn(nil)
	if strings.Contains(result, "## Available Tools") {
		t.Fatal("should not have tools section with empty registry")
	}
}

func TestBuildSystemPromptUsesSessionFindingsPath(t *testing.T) {
	prompt := BuildSystemPrompt(&PromptConfig{}, &agent.Config{SessionID: "sess/evil"})
	if strings.Contains(prompt, "/tmp/findings.md") {
		t.Fatalf("prompt still uses global findings path:\n%s", prompt)
	}
	if !strings.Contains(prompt, "aiscan-findings-sess-evil.md") {
		t.Fatalf("prompt missing session findings path:\n%s", prompt)
	}
	if !strings.Contains(prompt, "reproducible command or PoC evidence") {
		t.Fatalf("prompt missing generalized evidence wording:\n%s", prompt)
	}
}

func TestBuildSystemPromptIncludesDecisionBoundaries(t *testing.T) {
	prompt := BuildSystemPrompt(&PromptConfig{}, nil)
	for _, want := range []string{
		"No PoC means not confirmed",
		"P3/low/informational",
		"self-XSS",
		"Test 3-5 observed",
		"enumerate all reachable script sources",
		"remaining limits are clear",
		"Decision routing",
		"Login/auth boundary -> authorization",
		"API service -> unauthenticated access",
		"sort/orderBy",
		"about 20 minutes",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing decision boundary %q:\n%s", want, prompt)
		}
	}
}

func TestBuildSystemPromptLoadsSkillBody(t *testing.T) {
	prompt := BuildSystemPrompt(&PromptConfig{
		LoadedSkills: []LoadedSkill{
			{Name: "scan/verify", Body: "Verify all high-priority findings with active probing."},
			{Name: "scan/sniper", Body: "Search public CVEs for fingerprints."},
		},
	}, nil)

	for _, want := range []string{
		"## Skill: scan/verify",
		"Verify all high-priority findings with active probing.",
		"## Skill: scan/sniper",
		"Search public CVEs for fingerprints.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	// Loaded skills should appear before Key Principles
	skillIdx := strings.Index(prompt, "## Skill: scan/verify")
	principlesIdx := strings.Index(prompt, "## Key Principles")
	if skillIdx > principlesIdx {
		t.Fatal("loaded skills should appear before principles")
	}
}

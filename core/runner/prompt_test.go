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
}

func TestSystemPromptFuncAdaptsToTools(t *testing.T) {
	cfg := &PromptConfig{}
	fn := SystemPromptFunc(cfg)

	result := fn(nil)
	if strings.Contains(result, "## Available Tools") {
		t.Fatal("should not have tools section with empty registry")
	}
}

func TestBuildSystemPromptAlwaysLoadsAiscanSkill(t *testing.T) {
	prompt := BuildSystemPrompt(&PromptConfig{}, nil)
	if !strings.Contains(prompt, "## Skill: aiscan") {
		t.Fatalf("prompt missing base aiscan skill:\n%s", prompt)
	}
	// Key content from the aiscan skill should be present
	for _, want := range []string{
		"Platform Context",
		"Response Style",
		"Verification Standard",
		"Operating Rules",
		"Findings Log",
		"Termination",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing aiscan skill section %q:\n%s", want, prompt)
		}
	}
}

func TestBuildSystemPromptUsesSessionFindingsPath(t *testing.T) {
	prompt := BuildSystemPrompt(&PromptConfig{}, &agent.Config{SessionID: "sess/evil"})
	if !strings.Contains(prompt, "aiscan-findings-sess-evil.md") {
		t.Fatalf("prompt missing session findings path:\n%s", prompt)
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
		"Default ROI routing",
		"login or account boundary -> authorization",
		"API or Swagger/OpenAPI -> unauthenticated access",
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
}

func TestBuildSystemPromptDeduplicatesAiscan(t *testing.T) {
	prompt := BuildSystemPrompt(&PromptConfig{
		LoadedSkills: []LoadedSkill{
			{Name: "aiscan", Body: "duplicate body that should be skipped"},
		},
	}, nil)
	if strings.Contains(prompt, "duplicate body that should be skipped") {
		t.Fatal("should not include duplicate aiscan skill body from LoadedSkills")
	}
	if strings.Count(prompt, "## Skill: aiscan") != 1 {
		t.Fatal("aiscan skill should appear exactly once")
	}
}

func TestBuildSystemPromptScannerMode(t *testing.T) {
	prompt := BuildSystemPrompt(&PromptConfig{
		ScannerAgentMode: true,
		ScannerName:      "gogo",
	}, nil)
	if !strings.Contains(prompt, "Scanner Agent Constraints") {
		t.Fatalf("scanner mode prompt missing constraints:\n%s", prompt)
	}
	// Should still contain the base aiscan skill
	if !strings.Contains(prompt, "## Skill: aiscan") {
		t.Fatalf("scanner mode prompt missing base aiscan skill:\n%s", prompt)
	}
}

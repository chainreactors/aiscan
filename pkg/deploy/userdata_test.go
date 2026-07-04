package deploy

import (
	"strings"
	"testing"
)

func TestGenerateUserData(t *testing.T) {
	script := GenerateUserData(UserDataParams{
		PublicURL: "http://38.76.191.84:3000",
		IOAURL:    "http://tok123@38.76.191.84:3000/ioa",
		Space:     "redteam",
		NodeName:  "dep-abc-0",
		Overrides: map[string]string{"provider": "openai"},
	})
	for _, want := range []string{
		"#!/bin/bash",
		"/api/agent/binary?os=linux&arch=${ARCH}",
		"--web-url 'http://38.76.191.84:3000'",
		"--ioa-url 'http://tok123@38.76.191.84:3000/ioa'",
		"--space 'redteam'",
		"--ioa-node-name 'dep-abc-0'",
		"--provider 'openai'",
		"systemctl enable --now aiscan-agent",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q\n---\n%s", want, script)
		}
	}
	// The client must not receive a server-side --ioa-token flag.
	if strings.Contains(script, "--ioa-token") {
		t.Errorf("script should not pass --ioa-token (token is in the IOA URL)\n%s", script)
	}
}

func TestGenerateUserData_Progress(t *testing.T) {
	script := GenerateUserData(UserDataParams{
		PublicURL:   "http://38.76.191.84:3000",
		IOAURL:      "http://tok123@38.76.191.84:3000/ioa",
		NodeName:    "dep-abc-0",
		ProgressURL: "http://38.76.191.84:3000/api/agent/progress?token=tok123&node=dep-abc-0",
	})
	if strings.Contains(script, "%!") {
		t.Fatalf("script has a fmt verb error:\n%s", script)
	}
	for _, want := range []string{
		"PROGRESS=\"http://38.76.191.84:3000/api/agent/progress?token=tok123&node=dep-abc-0\"",
		"report booting",
		"report downloading 0 \"$TOTAL\"",
		"report installing",
		"report starting",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q\n---\n%s", want, script)
		}
	}
	// Phases must appear in bootstrap order.
	booting := strings.Index(script, "report booting")
	downloading := strings.Index(script, "report downloading 0")
	installing := strings.Index(script, "report installing")
	starting := strings.Index(script, "report starting")
	if !(booting < downloading && downloading < installing && installing < starting) {
		t.Fatalf("phase order wrong: booting=%d downloading=%d installing=%d starting=%d",
			booting, downloading, installing, starting)
	}
}

// An empty ProgressURL must render a no-op reporter, never a broken script.
func TestGenerateUserData_NoProgressURL(t *testing.T) {
	script := GenerateUserData(UserDataParams{PublicURL: "http://h:3000", NodeName: "n0"})
	if strings.Contains(script, "%!") {
		t.Fatalf("script has a fmt verb error:\n%s", script)
	}
	if !strings.Contains(script, `PROGRESS=""`) {
		t.Errorf("expected empty PROGRESS assignment (reporting disabled)\n%s", script)
	}
}

func TestShellQuoteEscapes(t *testing.T) {
	got := shellQuote("a'b")
	if got != `'a'\''b'` {
		t.Fatalf("shellQuote escaping wrong: %s", got)
	}
}

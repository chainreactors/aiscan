//go:build !full

package cmd

import (
	"strings"
	"testing"
)

func TestParseCLIBaseRejectsFullOnlyScannerCommand(t *testing.T) {
	_, err := parseCLI([]string{"katana"})
	if err == nil {
		t.Fatal("parseCLI() error = nil, want unavailable scanner command error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown command "katana"`) {
		t.Fatalf("error = %q, want unknown command for katana", msg)
	}
	if strings.Contains(msg, "passive") {
		t.Fatalf("error leaked full-only command in summary: %q", msg)
	}
}

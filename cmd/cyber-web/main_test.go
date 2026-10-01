package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		var stdout, stderr bytes.Buffer
		if err := run(t.Context(), args, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "cyber-web") {
			t.Fatalf("output: %s", stdout.String())
		}
	}
}

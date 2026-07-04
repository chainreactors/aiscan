package web

import "testing"

func TestParseSlashCommand(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantCmd string
		wantArg string
		wantOK  bool
	}{
		{"scan with target", "/scan example.com", "scan", "example.com", true},
		{"scan with flags", "/scan example.com --mode full --deep", "scan", "example.com --mode full --deep", true},
		{"verb only", "/agents", "agents", "", true},
		{"lowercased verb", "/SCAN Example.com", "scan", "Example.com", true},
		{"extra spaces", "/scan    a.com   b.com", "scan", "a.com   b.com", true},
		{"tab separator", "/shell\tls -la", "shell", "ls -la", true},
		{"plain message", "hello there", "", "", false},
		{"bare slash", "/", "", "", false},
		{"slash then spaces", "/   ", "", "", false},
		{"path-like, not a command", "/etc/passwd", "etc/passwd", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, arg, ok := parseSlashCommand(tc.in)
			if ok != tc.wantOK || cmd != tc.wantCmd || arg != tc.wantArg {
				t.Fatalf("parseSlashCommand(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.in, cmd, arg, ok, tc.wantCmd, tc.wantArg, tc.wantOK)
			}
		})
	}
}

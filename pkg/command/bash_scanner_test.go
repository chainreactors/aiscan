package command_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/command"
)

type simpleCommand struct{ name string }

func (c *simpleCommand) Name() string                                          { return c.name }
func (c *simpleCommand) Usage() string                                         { return c.name }
func (c *simpleCommand) Execute(_ context.Context, _ []string) (string, error) { return "ok", nil }

type capturingCommand struct {
	name     string
	captured *[]string
}

func (c *capturingCommand) Name() string  { return c.name }
func (c *capturingCommand) Usage() string { return c.name }
func (c *capturingCommand) Execute(_ context.Context, args []string) (string, error) {
	*c.captured = append([]string(nil), args...)
	return "ok", nil
}
func (c *capturingCommand) InProcess() {}

func TestScannerRejectsShellPipeAndFileRedir(t *testing.T) {
	registry := command.NewRegistry()
	registry.Register(&simpleCommand{name: "spray"}, "")
	bash := command.NewBashTool(t.TempDir(), 5, registry)

	tests := []struct {
		name     string
		cmd      string
		wantHint string
	}{
		{"pipe to head", `spray -u http://x | head -30`, "shell pipes"},
		{"double pipe", `spray -u http://x || echo done`, "shell pipes"},
		{"file redirection >", `spray -u http://x > out.txt`, "file redirection"},
		{"file redirection >>", `spray -u http://x >> out.txt`, "file redirection"},
		{"stderr to file", `spray -u http://x 2>err.log`, "file redirection"},
		{"combined to file", `spray -u http://x &> all.log`, "file redirection"},
		{"chained with &&", `spray -u http://x && spray -u http://y`, "chaining"},
		{"chained with ;", `spray -u http://x ; echo done`, "chaining"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := bash.Execute(context.Background(), bashArgs(tt.cmd))
			if err == nil {
				t.Fatalf("expected error, got output %q", res.Text())
			}
			if !strings.Contains(err.Error(), tt.wantHint) {
				t.Fatalf("error = %v, want hint containing %q", err, tt.wantHint)
			}
		})
	}
}

func TestBashProxyEnvInjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	proxy := "socks5://127.0.0.1:1080"
	bash := command.NewBashTool(t.TempDir(), 5, nil).WithScannerProxy(proxy)

	res, err := bash.Execute(context.Background(), bashArgs(`printf '%s\n' \
		"ALL_PROXY=$ALL_PROXY" \
		"all_proxy=$all_proxy" \
		"HTTP_PROXY=$HTTP_PROXY" \
		"http_proxy=$http_proxy" \
		"HTTPS_PROXY=$HTTPS_PROXY" \
		"https_proxy=$https_proxy"`))
	if err != nil {
		t.Fatalf("bash env: %v", err)
	}
	out := res.Text()
	for _, envVar := range []string{"ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"} {
		if !strings.Contains(out, envVar+"="+proxy) {
			t.Errorf("env output missing %s", envVar)
		}
	}
}

func TestBashNoProxyEnvWhenEmpty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	bash := command.NewBashTool(t.TempDir(), 5, nil)

	res, err := bash.Execute(context.Background(), bashArgs("env"))
	if err != nil {
		t.Fatalf("bash env: %v", err)
	}
	if strings.Contains(res.Text(), "ALL_PROXY=socks5://") {
		t.Errorf("should not inject proxy when empty")
	}
}

func TestScannerStripsNoColor(t *testing.T) {
	// --no-color should be silently stripped from pseudo-commands, not cause
	// an "unknown flag" error.
	var gotArgs []string
	registry := command.NewRegistry()
	registry.Register(&capturingCommand{name: "neutron", captured: &gotArgs}, "")
	bash := command.NewBashTool(t.TempDir(), 5, registry)

	_, err := bash.Execute(context.Background(), bashArgs(`neutron -u http://x --no-color`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, arg := range gotArgs {
		if arg == "--no-color" || strings.HasPrefix(arg, "--no-color=") {
			t.Fatalf("--no-color was not stripped; args = %v", gotArgs)
		}
	}
}

func TestScannerPreservesNoColorForScan(t *testing.T) {
	// scan owns --no-color itself. The registry must not strip it before
	// scan can disable ANSI in streamed output.
	var gotArgs []string
	registry := command.NewRegistry()
	registry.Register(&capturingCommand{name: "scan", captured: &gotArgs}, "")
	bash := command.NewBashTool(t.TempDir(), 5, registry)

	_, err := bash.Execute(context.Background(), bashArgs(`scan -i http://x --no-color`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, arg := range gotArgs {
		if arg == "--no-color" {
			return
		}
	}
	t.Fatalf("--no-color was stripped from scan args: %v", gotArgs)
}

func TestScannerSubprocessReceivesRootProxy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}

	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	stub := filepath.Join(dir, "aiscan-stub")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(argsFile) + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	registry := command.NewRegistry()
	registry.Register(&simpleCommand{name: "gogo"}, "")
	proxy := "socks5://127.0.0.1:9999"
	bash := command.NewBashTool(dir, 5, registry).WithScannerProxy(proxy)
	bash.SetSelfBinary(stub)

	_, err := bash.ExecuteTokens(context.Background(), []string{"gogo", "-i", "127.0.0.1"})
	if err != nil {
		t.Fatalf("ExecuteTokens gogo: %v", err)
	}

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"--proxy", proxy, "gogo", "-i", "127.0.0.1"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func bashArgs(cmd string) string {
	data, _ := json.Marshal(map[string]string{"command": cmd})
	return string(data)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

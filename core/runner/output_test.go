package runner

import (
	"bytes"
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/app"
	outpkg "github.com/chainreactors/aiscan/pkg/output"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func TestRenderAgentMarkdownPlainFallback(t *testing.T) {
	got := renderAgentMarkdown("  ## Title\n\n- item  ", false)
	want := "## Title\n\n- item"
	if got != want {
		t.Fatalf("renderAgentMarkdown() = %q, want %q", got, want)
	}
}

func TestTrimANSIVisibleRightKeepsResetAfterContent(t *testing.T) {
	line := "\x1b[38;5;252m漏洞验证\x1b[0m\x1b[38;5;252m \x1b[0m\x1b[38;5;252m \x1b[0m"
	want := "\x1b[38;5;252m漏洞验证\x1b[0m"
	if got := trimANSIVisibleRight(line); got != want {
		t.Fatalf("trimANSIVisibleRight() = %q, want %q", got, want)
	}
}

func TestRenderAgentMarkdownPrettyTrimsPaddedLineEnds(t *testing.T) {
	got := renderAgentMarkdown(`我是 aiscan，一个自主安全研究助手，主要能帮你做这些事：

- 端口与服务发现 — 扫描目标开放端口、识别服务指纹（gogo/scan）
- Web 探测与指纹识别 — 探测 Web 应用、目录爆破、爬虫（spray）
- 漏洞验证（PoC） — 基于 CVE/默认配置/错误配置的模板化漏洞检测（neutron）
- 漏洞情报搜索 — 查询 CVE、利用细节、产品文档（web_search/fetch）
- 多阶段综合扫描 — 一条命令完成端口→Web→指纹→弱口令→PoC 全流程（scan）`, true)

	for _, line := range strings.Split(stripANSI(got), "\n") {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			t.Fatalf("pretty markdown line has trailing whitespace: %q\nfull output:\n%q", line, got)
		}
	}
}

func TestAgentConsoleBannerHonorsNoColor(t *testing.T) {
	repl := &AgentConsole{
		application: &app.App{
			ProviderConfig: agent.ProviderConfig{Provider: "test-provider", Model: "test-model"},
		},
		output: &AgentOutput{stderr: &bytes.Buffer{}, color: outpkg.NewColor(false)},
	}

	got := repl.bannerOutput()
	if strings.Contains(got, "\x1b") {
		t.Fatalf("banner should not contain ANSI escapes when color is disabled: %q", got)
	}
	for _, want := range []string{"aiscan v", "test-provider / test-model", "/help"} {
		if !strings.Contains(got, want) {
			t.Fatalf("banner missing %q: %q", want, got)
		}
	}
}

func TestAgentConsoleDisablesNoisyAutoHints(t *testing.T) {
	repl := NewAgentConsole(nil, nil, &app.App{}, nil, &AgentOutput{})
	cfg := repl.console.Shell().Config
	for _, name := range []string{"autocomplete", "usage-hint-always", "history-autosuggest"} {
		if cfg.GetBool(name) {
			t.Fatalf("%s should be disabled for the agent REPL", name)
		}
	}
}

func TestAgentConsoleFastInputMode(t *testing.T) {
	t.Setenv("AISCAN_REPL", "")
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, nil, &AgentOutput{})
	if fastInputEnabledForMode("", true) {
		t.Fatal("interactive TTY should default to readline input")
	}
	if fastInputEnabledForMode("", false) {
		t.Fatal("non-TTY stdin should default to readline input unless AISCAN_REPL=fast is explicit")
	}

	t.Setenv("AISCAN_REPL", "rich")
	if repl.fastInputEnabled() {
		t.Fatal("AISCAN_REPL=rich should use readline input")
	}
	if !fastInputEnabledForMode("fast", true) {
		t.Fatal("AISCAN_REPL=fast should force fast line input")
	}
	if !fastInputEnabledForMode("fast", false) {
		t.Fatal("AISCAN_REPL=fast should force fast line input even for non-TTY stdin")
	}
	if fastInputEnabledForMode("readline", false) {
		t.Fatal("AISCAN_REPL=readline should force readline input")
	}
}

func TestAgentConsoleReadlineKeepsManualCompletionOnly(t *testing.T) {
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, nil, &AgentOutput{})
	cfg := repl.console.Shell().Config
	for _, name := range []string{"autocomplete", "usage-hint-always", "history-autosuggest", "enable-bracketed-paste"} {
		if cfg.GetBool(name) {
			t.Fatalf("%s should be disabled for smoother readline typing", name)
		}
	}
	if cfg.GetBool("disable-completion") {
		t.Fatal("manual Tab completion should remain enabled")
	}
}

func TestAgentConsoleHandleExitLine(t *testing.T) {
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, nil, &AgentOutput{})
	for _, line := range []string{"/exit\n", "/quit\n"} {
		done, err := repl.handleInputLine(line)
		if err != nil {
			t.Fatalf("handleInputLine(%q) error = %v", line, err)
		}
		if !done {
			t.Fatalf("%q should terminate the REPL", strings.TrimSpace(line))
		}
	}
}

func TestAgentOutputFinalWritesPlainMarkdownWithoutWrapper(t *testing.T) {
	var stdout bytes.Buffer
	output := &AgentOutput{
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

func TestAgentTipsEnabledHonorsEnv(t *testing.T) {
	cases := map[string]bool{
		"":         false,
		"1":        true,
		"true":     true,
		"on":       true,
		"off":      false,
		"false":    false,
		"0":        false,
		"disabled": false,
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("AISCAN_TIPS", value)
			if got := agentTipsEnabled(); got != want {
				t.Fatalf("agentTipsEnabled() = %v, want %v", got, want)
			}
		})
	}
}

func TestChooseAgentTipRotatesByHistory(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AISCAN_TIPS", "")
	t.Setenv("AISCAN_TIPS_OVERRIDE", "alpha|beta")

	first, ok := chooseAgentTip()
	if !ok || first.Text != "alpha" {
		t.Fatalf("first tip = %#v, %v; want alpha", first, ok)
	}
	recordAgentTipShown(first)

	second, ok := chooseAgentTip()
	if !ok || second.Text != "beta" {
		t.Fatalf("second tip = %#v, %v; want beta after alpha was recorded", second, ok)
	}
}

func TestAgentOutputFinalShowsTipForInteractiveTTY(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AISCAN_TIPS", "1")
	t.Setenv("AISCAN_TIPS_OVERRIDE", "Use /status for a quick health check.")

	var stdout, stderr bytes.Buffer
	output := &AgentOutput{
		stdout:   &stdout,
		stderr:   &stderr,
		color:    outpkg.NewColor(false),
		mode:     ModeInteractive,
		tty:      true,
		markdown: false,
	}

	output.Final("Done.")

	if got := stdout.String(); !strings.Contains(got, "Done.") {
		t.Fatalf("stdout missing final content: %q", got)
	}
	got := stripANSI(stderr.String())
	if !strings.Contains(got, "Tip: Use /status for a quick health check.") {
		t.Fatalf("stderr missing tip: %q", got)
	}
}

func TestAgentOutputStartSkipsSingleLinePromptEcho(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &stdout,
		stderr: &stderr,
		color:  outpkg.NewColor(false),
	}

	output.Start("prompt", "你好呀")
	if got := stderr.String(); got != "" {
		t.Fatalf("single-line prompt should not be echoed, got %q", got)
	}

	output.Start("prompt", "line one\nline two")
	if got := stripANSI(stderr.String()); !strings.Contains(got, "line one") || !strings.Contains(got, "line two") {
		t.Fatalf("multi-line prompt echo missing body: %q", got)
	}
}

func TestAgentOutputTipSuppressedOutsideInteractiveTTY(t *testing.T) {
	cases := []struct {
		name    string
		mode    RenderMode
		tty     bool
		quiet   bool
		tipsEnv string
	}{
		{name: "forwarded", mode: ModeForwarded, tty: true},
		{name: "non-tty", mode: ModeInteractive, tty: false},
		{name: "quiet", mode: ModeInteractive, tty: true, quiet: true},
		{name: "disabled", mode: ModeInteractive, tty: true, tipsEnv: "off"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("AISCAN_TIPS", tc.tipsEnv)
			t.Setenv("AISCAN_TIPS_OVERRIDE", "hidden tip")

			var stdout, stderr bytes.Buffer
			output := &AgentOutput{
				stdout:   &stdout,
				stderr:   &stderr,
				color:    outpkg.NewColor(false),
				mode:     tc.mode,
				tty:      tc.tty,
				Quiet:    tc.quiet,
				markdown: false,
			}

			output.Final("Done.")
			if got := stripANSI(stderr.String()); strings.Contains(got, "Tip:") {
				t.Fatalf("tip should be suppressed, stderr = %q", got)
			}
		})
	}
}

func TestAgentOutputToolSummary(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		tools:  make(map[string]agentToolSummary),
	}

	output.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "bash",
		Arguments:  `{"command":"scan -i 127.0.0.1 --mode quick"}`,
	})
	output.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "bash",
		Result:     "ok",
	})

	got := stripANSI(stderr.String())
	if !strings.Contains(got, "bash started") || !strings.Contains(got, "scan -i 127.0.0.1 --mode quick") {
		t.Fatalf("stderr missing tool summary: %q", got)
	}
	if !strings.Contains(got, "⎿") {
		t.Fatalf("stderr missing ⎿ marker: %q", got)
	}
}

func TestAgentOutputToolDebugDetails(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		debug:  true,
		tools:  make(map[string]agentToolSummary),
	}

	output.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "read",
		Arguments:  `{"path":"docs/usage.md","limit":20}`,
	})
	output.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "read",
		Result:     "file content",
	})

	got := stripANSI(stderr.String())
	if !strings.Contains(got, "read") || !strings.Contains(got, "docs/usage.md") {
		t.Fatalf("stderr missing read summary: %q", got)
	}
	if !strings.Contains(got, `args: {"path":"docs/usage.md","limit":20}`) {
		t.Fatalf("stderr missing compact args in debug mode: %q", got)
	}
	if !strings.Contains(got, "file content") {
		t.Fatalf("stderr missing result content in debug mode: %q", got)
	}
}

func TestAgentOutputToolError(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		tools:  make(map[string]agentToolSummary),
	}

	output.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "bash",
		Result:     "permission denied",
		IsError:    true,
	})

	got := stripANSI(stderr.String())
	if !strings.Contains(got, "permission denied") {
		t.Fatalf("stderr missing tool error: %q", got)
	}
}

func TestAgentOutputTurnUsageHiddenByDefault(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
	}

	output.HandleEvent(agent.Event{
		Type: agent.EventTurnEnd,
		Turn: 7,
		Usage: &agent.Usage{
			PromptTokens:     100,
			CompletionTokens: 20,
			TotalTokens:      120,
		},
		ContextTokens: 100,
	})

	got := stripANSI(stderr.String())
	if strings.Contains(got, "[turn") || strings.Contains(got, "prompt=") {
		t.Fatalf("turn usage should be hidden by default: %q", got)
	}
}

func TestAgentOutputTurnUsageShowsInDebug(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		debug:  true,
	}

	output.HandleEvent(agent.Event{
		Type: agent.EventTurnEnd,
		Turn: 7,
		Usage: &agent.Usage{
			PromptTokens:     100,
			CompletionTokens: 20,
			TotalTokens:      120,
		},
		ContextTokens: 100,
	})

	got := stripANSI(stderr.String())
	if !strings.Contains(got, "[turn 7]") || !strings.Contains(got, "prompt=100") {
		t.Fatalf("debug turn usage missing: %q", got)
	}
}

func TestAgentOutputTurnEndShowsStopReasonInDebug(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		debug:  true,
	}
	msg := agent.NewTextMessage("assistant", "partial")
	msg.StopReason = "length"

	output.HandleEvent(agent.Event{
		Type:    agent.EventTurnEnd,
		Turn:    7,
		Message: msg,
	})

	got := stripANSI(stderr.String())
	if !strings.Contains(got, "stop_reason=length") {
		t.Fatalf("debug stop reason missing: %q", got)
	}
	if strings.Contains(got, "finish_reason") {
		t.Fatalf("debug output should use stop_reason, got: %q", got)
	}
}

func TestAgentOutputWriteEditSummary(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		tools:  make(map[string]agentToolSummary),
	}

	output.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "write",
		Arguments:  `{"path":"src/main.go","edits":[{"old_text":"foo","new_text":"bar"},{"old_text":"baz","new_text":"qux"}]}`,
	})

	got := stripANSI(stderr.String())
	if !strings.Contains(got, "src/main.go") {
		t.Fatalf("stderr missing file path: %q", got)
	}
	if !strings.Contains(got, "2 change(s)") {
		t.Fatalf("stderr missing edit count: %q", got)
	}
}

func TestAgentOutputMultiLineResult(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		tools:  make(map[string]agentToolSummary),
	}

	result := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	output.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "bash",
		Result:     result,
	})

	got := stripANSI(stderr.String())
	if !strings.Contains(got, "line1") {
		t.Fatalf("stderr missing first line: %q", got)
	}
	if !strings.Contains(got, "+") && !strings.Contains(got, "lines") {
		t.Fatalf("stderr missing truncation hint for multi-line result: %q", got)
	}
}

func TestAgentOutputFetchPreviewIncludesBody(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		tools:  make(map[string]agentToolSummary),
	}

	result := strings.Join([]string{
		"Fetched: https://example.test/app.js",
		"Status: 200 OK",
		"Content-Type: text/javascript",
		"Size: 4096 bytes",
		"---",
		"",
		"const routes = ['/api/users', '/api/admin'];",
		"fetch('/api/desk/webhooks')",
		"fetch('/api/mall/checkout')",
		"fetch('/api/ads/creatives')",
		"fetch('/api/hidden')",
	}, "\n")
	output.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "fetch",
		Result:     result,
	})

	got := stripANSI(stderr.String())
	for _, want := range []string{
		"fetch result",
		"Fetched: https://example.test/app.js",
		"Content-Type: text/javascript",
		"const routes",
		"/api/desk/webhooks",
		"+1 lines hidden",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stderr missing %q:\n%s", want, got)
		}
	}
}

func TestAgentOutputToolPreviewTruncatesUTF8Safely(t *testing.T) {
	result := strings.Repeat("漏洞", toolResultPreviewWidth+10)
	preview := buildToolResultPreview("bash", result, false)
	if len(preview.lines) != 1 {
		t.Fatalf("preview lines = %d, want 1", len(preview.lines))
	}
	if !utf8.ValidString(preview.lines[0]) {
		t.Fatalf("preview contains invalid UTF-8: %q", preview.lines[0])
	}
	if !strings.HasSuffix(preview.lines[0], "…") {
		t.Fatalf("preview should show ellipsis after truncation: %q", preview.lines[0])
	}
}

func contentMsg(s string) agent.ChatMessage {
	return agent.ChatMessage{Content: &s}
}

// Streaming emits only the freshly-arrived suffix of the cumulative message, and
// Final must not re-render text that was already streamed.
func TestAgentOutputStreamsDeltasAndSkipsFinal(t *testing.T) {
	var stdout bytes.Buffer
	o := &AgentOutput{stdout: &stdout, stderr: &bytes.Buffer{}, stream: true, markdown: false}

	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg("Hel")})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg("Hello!")})
	o.HandleEvent(agent.Event{Type: agent.EventTurnEnd, Turn: 1})
	o.Final("Hello!")

	got := stdout.String()
	if c := strings.Count(got, "Hello!"); c != 1 {
		t.Fatalf("expected streamed text once, got %d in %q", c, got)
	}
	if !strings.HasSuffix(got, "Hello!\n") {
		t.Fatalf("expected trailing newline after stream, got %q", got)
	}
}

func TestAgentOutputStreamsDeltasButKeepsUnstreamedFinal(t *testing.T) {
	var stdout bytes.Buffer
	o := &AgentOutput{stdout: &stdout, stderr: &bytes.Buffer{}, stream: true, markdown: false}

	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg("I'll check that.")})
	o.HandleEvent(agent.Event{Type: agent.EventTurnEnd, Turn: 1})
	o.Final("task complete: done")

	got := stdout.String()
	if !strings.Contains(got, "I'll check that.") {
		t.Fatalf("stdout missing streamed progress: %q", got)
	}
	if !strings.Contains(got, "task complete: done") {
		t.Fatalf("stdout missing unstreamed final: %q", got)
	}
}

// streamPrinted resets per turn so multi-turn runs stream each turn's text once.
func TestAgentOutputStreamResetsPerTurn(t *testing.T) {
	var stdout bytes.Buffer
	o := &AgentOutput{stdout: &stdout, stderr: &bytes.Buffer{}, stream: true, markdown: false}

	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg("AB")})
	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 2})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 2, Message: contentMsg("CD")})

	if got, want := stdout.String(), "ABCD"; got != want {
		t.Fatalf("streamed = %q, want %q", got, want)
	}
}

// Updates with no visible content (reasoning / tool-call arg deltas) print nothing.
func TestAgentOutputStreamIgnoresEmptyContent(t *testing.T) {
	var stdout bytes.Buffer
	o := &AgentOutput{stdout: &stdout, stderr: &bytes.Buffer{}, stream: true, markdown: false}

	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: agent.ChatMessage{}}) // nil Content

	if got := stdout.String(); got != "" {
		t.Fatalf("expected no output for empty content, got %q", got)
	}
	if o.didStream {
		t.Fatalf("didStream should stay false when nothing was streamed")
	}
}

// Tool/status side-channel output shares the terminal cursor with streamed
// stdout, so it must first close an unfinished assistant line.
func TestAgentOutputStreamNewlineBeforeToolStart(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := &AgentOutput{stdout: &stdout, stderr: &stderr, stream: true, markdown: false}

	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg("thinking")})
	o.HandleEvent(agent.Event{
		Type:      agent.EventToolExecutionStart,
		Turn:      1,
		ToolName:  "bash",
		Arguments: `{"command":"id"}`,
	})

	if got, want := stdout.String(), "thinking\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := stripANSI(stderr.String()); !strings.Contains(got, "⎿ bash") {
		t.Fatalf("stderr missing tool start: %q", got)
	}
	if o.streamLineOpen {
		t.Fatalf("streamLineOpen should be false after tool start")
	}
}

func TestAgentOutputStreamNewlineBeforeToolStartDoesNotDoubleSpace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := &AgentOutput{stdout: &stdout, stderr: &stderr, stream: true, markdown: false}

	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg("thinking\n")})
	o.HandleEvent(agent.Event{Type: agent.EventToolExecutionStart, Turn: 1, ToolName: "bash"})

	if got, want := stdout.String(), "thinking\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestAgentOutputStartResetsStreamStateForAnyRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := &AgentOutput{stdout: &stdout, stderr: &stderr, stream: true, markdown: false}

	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg("partial")})
	o.Start("continue", "")

	if got, want := stdout.String(), "partial\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if o.didStream || o.streamPrinted != 0 || o.streamLineOpen || o.lastStreamed != "" {
		t.Fatalf("stream state not reset: didStream=%v streamPrinted=%d streamLineOpen=%v lastStreamed=%q",
			o.didStream, o.streamPrinted, o.streamLineOpen, o.lastStreamed)
	}
}

func TestAgentOutputAbortCurrentRunSuppressesLateEventsAndFinal(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := &AgentOutput{
		stdout:   &stdout,
		stderr:   &stderr,
		color:    outpkg.NewColor(false),
		tools:    make(map[string]agentToolSummary),
		stream:   true,
		markdown: false,
	}

	o.Start("prompt", "first")
	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg("partial")})
	o.AbortCurrentRun()
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg("partial leak")})
	o.HandleEvent(agent.Event{Type: agent.EventToolExecutionStart, Turn: 1, ToolName: "bash", Arguments: `{"command":"id"}`})
	o.Final("late final")

	if got, want := stdout.String(), "partial\n"; got != want {
		t.Fatalf("stdout after abort = %q, want %q", got, want)
	}
	if got := stripANSI(stderr.String()); strings.Contains(got, "bash") || strings.Contains(got, "late final") {
		t.Fatalf("stderr contains late output after abort: %q", got)
	}

	o.Start("prompt", "next")
	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 2})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 2, Message: contentMsg("next")})

	if got, want := stdout.String(), "partial\nnext"; got != want {
		t.Fatalf("stdout after next run = %q, want %q", got, want)
	}
}

// resolveRenderMode honours AISCAN_RENDER for the forwarded-PTY path.
func TestResolveRenderMode(t *testing.T) {
	cases := map[string]RenderMode{
		"":            ModeInteractive,
		"forwarded":   ModeForwarded,
		"FORWARD":     ModeForwarded,
		"remote":      ModeForwarded,
		"interactive": ModeInteractive,
		"garbage":     ModeInteractive,
	}
	for val, want := range cases {
		t.Setenv("AISCAN_RENDER", val)
		if got := resolveRenderMode(); got != want {
			t.Errorf("AISCAN_RENDER=%q: got %v, want %v", val, got, want)
		}
	}
}

// hyperlink emits OSC 8 wrapping; pathHyperlink resolves to a file:// URL.
func TestHyperlinkAndPathHyperlink(t *testing.T) {
	got := hyperlink("https://example.com/x", "go")
	if !strings.Contains(got, "\x1b]8;;https://example.com/x\x1b\\") || !strings.Contains(got, "go") {
		t.Fatalf("hyperlink malformed: %q", got)
	}
	if h := hyperlink("", "x"); h != "x" {
		t.Fatalf("empty-url hyperlink should be plain: %q", h)
	}

	dir := t.TempDir() // absolute
	got = pathHyperlink(dir, "label")
	if !strings.Contains(got, "file://"+dir) || !strings.Contains(got, "label") {
		t.Fatalf("pathHyperlink malformed: %q", got)
	}
	// an unresolvable-but-present path still yields a file:// link (Abs rarely errors)
	if h := pathHyperlink(filepath.Join(dir, "sub"), "ok"); !strings.Contains(h, "file://") {
		t.Fatalf("expected file:// link, got %q", h)
	}
}

// The spinner is PTY-forward safe: it repaints the current line only and Stop()
// always collapses it (carriage-return + erase), so the recorded transcript
// never holds a dangling transient frame. Runs under -race to validate the
// ticker goroutine's coordination with Start/Stop.
func TestSpinnerCollapsesOnStop(t *testing.T) {
	var buf bytes.Buffer
	s := newSpinner(&buf, "")
	s.Start("working")
	time.Sleep(150 * time.Millisecond) // let ≥1 frame render
	s.Stop()

	out := buf.String()
	if !strings.Contains(out, "working") {
		t.Fatalf("spinner never rendered its label: %q", out)
	}
	if !strings.Contains(out, carriage) || !strings.Contains(out, eraseLine) {
		t.Fatalf("Stop did not collapse the spinner line: %q", out)
	}
	// second Stop on a stopped spinner is a safe no-op
	s.Stop()
}

// Start while running retargets the label without spawning a second goroutine.
func TestSpinnerRetargetsLabel(t *testing.T) {
	var buf bytes.Buffer
	s := newSpinner(&buf, "")
	s.Start("first")
	s.Start("second") // must not panic / leak a goroutine
	s.Stop()
}

package runner

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/app"
	cmdpkg "github.com/chainreactors/aiscan/pkg/command"
	outpkg "github.com/chainreactors/aiscan/pkg/output"
	"github.com/reeflective/readline/inputrc"
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

func TestAgentConsoleSessionsOutput(t *testing.T) {
	reg := cmdpkg.NewRegistry()
	bash := cmdpkg.NewBashTool(t.TempDir(), 1)
	t.Cleanup(bash.Close)
	reg.RegisterTool(bash)

	session := agent.NewAgent(agent.Config{Tools: reg})
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, session, &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &bytes.Buffer{},
		color:  outpkg.NewColor(false),
	})

	if got := stripANSI(repl.sessionsOutput()); !strings.Contains(got, "no PTY sessions") {
		t.Fatalf("empty sessions output = %q", got)
	}

	_, err := bash.Manager().CreateFunc("worker", time.Second, func(ctx context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, "hello from worker\n")
		return nil
	})
	if err != nil {
		t.Fatalf("create func session: %v", err)
	}
	<-bash.Manager().Done("worker")

	got := stripANSI(repl.sessionsOutput())
	for _, want := range []string{"sessions", "worker", "completed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sessions output missing %q: %q", want, got)
		}
	}
	if summary := repl.sessionCountSummary(); !strings.Contains(summary, "completed=1") {
		t.Fatalf("session count summary = %q, want completed=1", summary)
	}

	tail := stripANSI(repl.tailOutput("worker", "hello from worker\n", false, true))
	for _, want := range []string{"tail", "worker", "hello from worker"} {
		if !strings.Contains(tail, want) {
			t.Fatalf("tail output missing %q: %q", want, tail)
		}
	}
}

func TestAgentConsolePromptPlainFallback(t *testing.T) {
	repl := &AgentConsole{
		application: &app.App{
			ProviderConfig: agent.ProviderConfig{Provider: "anthropic", Model: "glm-5.2"},
		},
		output: &AgentOutput{color: outpkg.NewColor(false)},
	}

	if got := repl.promptString(); got != "aiscan> " {
		t.Fatalf("plain prompt = %q, want %q", got, "aiscan> ")
	}
}

func TestAgentConsolePromptRendersCompactInputLine(t *testing.T) {
	repl := &AgentConsole{
		application: &app.App{
			ProviderConfig: agent.ProviderConfig{Provider: "anthropic", Model: "glm-5.2"},
		},
		output: &AgentOutput{color: outpkg.NewColor(true)},
	}

	got := stripANSI(repl.promptString())
	if got != "aiscan ❯ " {
		t.Fatalf("compact prompt = %q, want %q", got, "aiscan ❯ ")
	}
	if strings.Contains(got, "glm-5.2") || strings.Contains(got, "space default") {
		t.Fatalf("compact prompt should not include old inline context: %q", got)
	}
	for _, border := range []string{"╭", "╰", "─", "╮"} {
		if strings.Contains(got, border) {
			t.Fatalf("compact prompt should not render input borders: %q", got)
		}
	}
}

func TestAgentConsolePromptSecondary(t *testing.T) {
	if got := stripANSI(agentPromptSecondary(&AgentOutput{color: outpkg.NewColor(true)})); got != "... " {
		t.Fatalf("colored secondary prompt = %q, want %q", got, "... ")
	}
	if got := agentPromptSecondary(&AgentOutput{color: outpkg.NewColor(false)}); got != "> " {
		t.Fatalf("plain secondary prompt = %q, want %q", got, "> ")
	}
}

func TestAgentConsoleEnablesCommandPreviewWithoutHistoryGhostText(t *testing.T) {
	repl := NewAgentConsole(nil, nil, &app.App{}, nil, &AgentOutput{})
	cfg := repl.console.Shell().Config
	for _, name := range []string{"autocomplete", "usage-hint-always"} {
		if !cfg.GetBool(name) {
			t.Fatalf("%s should be enabled for slash-command preview", name)
		}
	}
	if cfg.GetBool("history-autosuggest") {
		t.Fatal("history-autosuggest should be disabled for the agent REPL")
	}
}

func TestAgentConsoleFastInputMode(t *testing.T) {
	t.Setenv("AISCAN_REPL", "")
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, nil, &AgentOutput{})
	if fastInputEnabledForMode("", true) {
		t.Fatal("interactive TTY should default to readline input")
	}
	if !fastInputEnabledForMode("", false) {
		t.Fatal("non-TTY stdin should default to fast line input")
	}

	t.Setenv("AISCAN_REPL", "rich")
	if repl.fastInputEnabled() {
		t.Fatal("AISCAN_REPL=rich should use readline input")
	}
	if !fastInputEnabledForMode("fast", true) {
		t.Fatal("AISCAN_REPL=fast should force fast line input")
	}
	if fastInputEnabledForMode("readline", false) {
		t.Fatal("AISCAN_REPL=readline should force readline input")
	}
}

func TestAgentOutputStreamingModeEnv(t *testing.T) {
	cases := []struct {
		name             string
		value            string
		stdoutIsTerminal bool
		want             bool
	}{
		{name: "default stream tty", value: "", stdoutIsTerminal: true, want: true},
		{name: "default non tty", value: "", stdoutIsTerminal: false, want: false},
		{name: "stream enabled tty", value: "1", stdoutIsTerminal: true, want: true},
		{name: "stream enabled non tty", value: "1", stdoutIsTerminal: false, want: false},
		{name: "stream word", value: "stream", stdoutIsTerminal: true, want: true},
		{name: "pretty word", value: "pretty", stdoutIsTerminal: true, want: false},
		{name: "disabled", value: "off", stdoutIsTerminal: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := streamingEnabledForEnv(tc.value, tc.stdoutIsTerminal); got != tc.want {
				t.Fatalf("streamingEnabledForEnv(%q, %v) = %v, want %v",
					tc.value, tc.stdoutIsTerminal, got, tc.want)
			}
		})
	}
}

func TestAgentConsoleReadlineKeepsCompletionEnabled(t *testing.T) {
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, nil, &AgentOutput{})
	cfg := repl.console.Shell().Config
	if cfg.GetBool("history-autosuggest") {
		t.Fatal("history-autosuggest should be disabled for smoother readline typing")
	}
	if !cfg.GetBool("enable-bracketed-paste") {
		t.Fatal("bracketed paste should be enabled so large pastes insert in one redraw")
	}
	for _, name := range []string{"autocomplete", "usage-hint-always"} {
		if !cfg.GetBool(name) {
			t.Fatalf("%s should remain enabled", name)
		}
	}
	if cfg.GetBool("disable-completion") {
		t.Fatal("manual Tab completion should remain enabled")
	}
}

func TestAgentConsoleReadlineBracketedPasteBinding(t *testing.T) {
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, nil, &AgentOutput{})
	cfg := repl.console.Shell().Config
	for _, keymap := range []string{"emacs", "emacs-standard", "vi-insert"} {
		got := cfg.Binds[keymap][inputrc.Unescape(`\M-[200~`)].Action
		if got != "bracketed-paste-begin" {
			t.Fatalf("%s bracketed paste binding = %q, want bracketed-paste-begin", keymap, got)
		}
	}
}

func TestAgentConsoleReadlineKeepsNoisyPasteFallbacksDisabled(t *testing.T) {
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, nil, &AgentOutput{})
	cfg := repl.console.Shell().Config
	for _, name := range []string{"show-all-if-ambiguous", "show-all-if-unmodified", "page-completions"} {
		if cfg.GetBool(name) {
			t.Fatalf("%s should be disabled to avoid noisy redraws while typing or pasting", name)
		}
	}
}

func TestAgentConsoleReadlineSlashArrowBindings(t *testing.T) {
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, nil, &AgentOutput{})
	shell := repl.console.Shell()
	commands := shell.Keymap.Commands()
	for _, name := range []string{agentSlashMenuCompleteCommand, agentSlashMenuCompleteBackwardCommand} {
		if commands[name] == nil {
			t.Fatalf("readline command %s should be registered", name)
		}
	}

	tests := []struct {
		keymap string
		seq    string
		want   string
	}{
		{keymap: "emacs", seq: `\M-[A`, want: agentSlashMenuCompleteBackwardCommand},
		{keymap: "emacs", seq: `\M-[B`, want: agentSlashMenuCompleteCommand},
		{keymap: "vi-insert", seq: `\M-[A`, want: agentSlashMenuCompleteBackwardCommand},
		{keymap: "vi-insert", seq: `\M-[B`, want: agentSlashMenuCompleteCommand},
	}
	for _, tt := range tests {
		t.Run(tt.keymap+"/"+tt.seq, func(t *testing.T) {
			got := shell.Config.Binds[tt.keymap][inputrc.Unescape(tt.seq)].Action
			if got != tt.want {
				t.Fatalf("binding = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentReadlineCursorInSlashCommand(t *testing.T) {
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, nil, &AgentOutput{})
	shell := repl.console.Shell()
	tests := []struct {
		name   string
		line   string
		cursor int
		want   bool
	}{
		{name: "slash prefix", line: "/re", cursor: 3, want: true},
		{name: "leading spaces", line: "  /re", cursor: 5, want: true},
		{name: "bare slash", line: "/", cursor: 1, want: true},
		{name: "cursor before slash", line: "/re", cursor: 0, want: false},
		{name: "plain text", line: "scan /re", cursor: 8, want: false},
		{name: "after slash command arg", line: "/report now", cursor: 11, want: false},
		{name: "inside command before arg", line: "/report now", cursor: 3, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell.Line().Set([]rune(tt.line)...)
			shell.Cursor().Set(tt.cursor)
			if got := agentReadlineCursorInSlashCommand(shell); got != tt.want {
				t.Fatalf("agentReadlineCursorInSlashCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunAgentSlashSelectionCommandFallsBackOutsideSlashCommand(t *testing.T) {
	repl := NewAgentConsole(context.Background(), nil, &app.App{}, nil, &AgentOutput{})
	shell := repl.console.Shell()
	var slashCalls, fallbackCalls int

	shell.Line().Set([]rune("/re")...)
	shell.Cursor().Set(3)
	runAgentSlashSelectionCommand(shell, func() { slashCalls++ }, func() { fallbackCalls++ })
	if slashCalls != 1 || fallbackCalls != 0 {
		t.Fatalf("slash prefix calls = (%d slash, %d fallback), want (1, 0)", slashCalls, fallbackCalls)
	}

	shell.Line().Set([]rune("scan target")...)
	shell.Cursor().Set(4)
	runAgentSlashSelectionCommand(shell, func() { slashCalls++ }, func() { fallbackCalls++ })
	if slashCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("plain text calls = (%d slash, %d fallback), want (1, 1)", slashCalls, fallbackCalls)
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

func TestAgentOutputStartRendersPromptEcho(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &stdout,
		stderr: &stderr,
		color:  outpkg.NewColor(false),
	}

	output.Start("prompt", "你好呀")
	if got := stripANSI(stderr.String()); !strings.Contains(got, "user") || !strings.Contains(got, "你好呀") {
		t.Fatalf("single-line prompt echo missing body: %q", got)
	}

	output.Start("prompt", "line one\nline two")
	if got := stripANSI(stderr.String()); !strings.Contains(got, "line one") || !strings.Contains(got, "line two") {
		t.Fatalf("multi-line prompt echo missing body: %q", got)
	}

	stderr.Reset()
	output.Start("prompt", "   ")
	if got := stderr.String(); got != "" {
		t.Fatalf("empty prompt should not be echoed, got %q", got)
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
	if !strings.Contains(got, "bash") || !strings.Contains(got, "scan -i 127.0.0.1 --mode quick") {
		t.Fatalf("stderr missing tool summary: %q", got)
	}
	if !strings.Contains(got, "⎿") {
		t.Fatalf("stderr missing ⎿ marker: %q", got)
	}
}

func TestAgentOutputToolDebugDoesNotRenderRawArgs(t *testing.T) {
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
	if strings.Contains(got, `args: {"path":"docs/usage.md","limit":20}`) {
		t.Fatalf("stderr should not include raw args in debug mode: %q", got)
	}
	if !strings.Contains(got, "file content") {
		t.Fatalf("stderr missing result content in debug mode: %q", got)
	}
}

func TestAgentOutputCompactsEmbeddedSkillRead(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		tools:  make(map[string]agentToolSummary),
	}

	output.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionStart,
		ToolCallID: "call-1",
		ToolName:   "read",
		Arguments:  `{"path":"aiscan://skills/aiscan/SKILL.md"}`,
	})
	output.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionEnd,
		ToolCallID: "call-1",
		ToolName:   "read",
		Arguments:  `{"path":"aiscan://skills/aiscan/SKILL.md"}`,
		Result:     "---\nname: aiscan\ndescription: internal rules\n---\nbody",
	})

	got := stripANSI(stderr.String())
	if !strings.Contains(got, "skill: aiscan") {
		t.Fatalf("stderr missing compact skill summary: %q", got)
	}
	if !strings.Contains(got, "loaded built-in skill aiscan (5 lines)") {
		t.Fatalf("stderr missing compact skill load result: %q", got)
	}
	for _, raw := range []string{"description: internal rules", "---"} {
		if strings.Contains(got, raw) {
			t.Fatalf("embedded skill body should not be previewed, leaked %q in %q", raw, got)
		}
	}
}

func TestAgentOutputDoesNotHyperlinkVirtualReadPath(t *testing.T) {
	output := &AgentOutput{mode: ModeInteractive, tty: true}
	got := output.hyperlinkSummary("read", `{"path":"aiscan://skills/aiscan/SKILL.md"}`, "skill: aiscan")
	if strings.Contains(got, "\x1b]8;;") || strings.Contains(got, "file://") {
		t.Fatalf("virtual read path should not be hyperlinked: %q", got)
	}
	if got != "skill: aiscan" {
		t.Fatalf("virtual read path display changed: %q", got)
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

func TestAgentOutputAgentEndShowsInDebug(t *testing.T) {
	var stderr bytes.Buffer
	output := &AgentOutput{
		stdout: &bytes.Buffer{},
		stderr: &stderr,
		debug:  true,
	}

	output.HandleEvent(agent.Event{
		Type:   agent.EventAgentEnd,
		Stop:   agent.StopReasonCompleted,
		Detail: "assistant response had no tool calls and no pending work",
	})

	got := stripANSI(stderr.String())
	if !strings.Contains(got, "[agent] stop=completed") || !strings.Contains(got, "no tool calls") {
		t.Fatalf("debug agent end missing stop detail: %q", got)
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

	result := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8"
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

func TestAgentOutputStreamsMarkdownBlocksAndFinalTail(t *testing.T) {
	var stdout bytes.Buffer
	o := &AgentOutput{stdout: &stdout, stderr: &bytes.Buffer{}, stream: true, markdown: true}

	first := "Intro\n\n**能力**\n- one\n"
	final := first + "- two\n\nTail **done**"

	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg(first)})
	if got := stripANSI(stdout.String()); !strings.Contains(got, "Intro") ||
		!strings.Contains(got, "能力") || !strings.Contains(got, "one") {
		t.Fatalf("streamed markdown block missing expected content: %q", got)
	}

	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg(final)})
	o.Final(final)

	got := stripANSI(stdout.String())
	for _, want := range []string{"Intro", "能力", "one", "two", "Tail", "done"} {
		if !strings.Contains(got, want) {
			t.Fatalf("streamed markdown output missing %q: %q", want, got)
		}
	}
	for _, raw := range []string{"**能力**", "**done**"} {
		if strings.Contains(got, raw) {
			t.Fatalf("streamed markdown output leaked raw markdown %q: %q", raw, got)
		}
	}
	if c := strings.Count(got, "Intro"); c != 1 {
		t.Fatalf("streamed markdown duplicated rendered prefix %d times: %q", c, got)
	}
}

func TestAgentOutputMarkdownStreamWaitsForClosedFence(t *testing.T) {
	var stdout bytes.Buffer
	o := &AgentOutput{stdout: &stdout, stderr: &bytes.Buffer{}, stream: true, markdown: true}

	open := "```go\nfmt.Println(1)\n\n"
	closed := open + "```\nAfter"

	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg(open)})
	if got := stdout.String(); got != "" {
		t.Fatalf("open code fence should not stream early, got %q", got)
	}

	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg(closed)})
	o.Final(closed)

	got := stripANSI(stdout.String())
	for _, want := range []string{"fmt.Println(1)", "After"} {
		if !strings.Contains(got, want) {
			t.Fatalf("streamed markdown output missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "```") {
		t.Fatalf("streamed markdown output leaked raw fence: %q", got)
	}
	if c := strings.Count(got, "fmt.Println(1)"); c != 1 {
		t.Fatalf("streamed code block duplicated %d times: %q", c, got)
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

func TestAgentOutputSuppressesScannerToolSpinner(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := &AgentOutput{
		stdout:   &stdout,
		stderr:   &stderr,
		stream:   true,
		markdown: false,
		mode:     ModeInteractive,
		tty:      true,
		spinner:  newSpinner(&stderr, ""),
	}

	o.HandleEvent(agent.Event{
		Type:      agent.EventToolExecutionStart,
		Turn:      1,
		ToolName:  "bash",
		Arguments: `{"command":"spray -l /tmp/subs.txt -t 50"}`,
	})

	o.spinner.mu.Lock()
	running := o.spinner.running
	o.spinner.mu.Unlock()
	if running {
		o.spinner.Stop()
		t.Fatalf("scanner-like bash calls should not start an activity spinner")
	}
	if got := stripANSI(stderr.String()); !strings.Contains(got, "⎿ bash") || strings.Contains(got, "⠼ bash spray") {
		t.Fatalf("unexpected stderr for scanner-like bash call: %q", got)
	}
}

func TestAgentOutputKeepsSpinnerForPlainBash(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := &AgentOutput{
		stdout:   &stdout,
		stderr:   &stderr,
		stream:   true,
		markdown: false,
		mode:     ModeInteractive,
		tty:      true,
		spinner:  newSpinner(&stderr, ""),
	}
	defer o.spinner.Stop()

	o.HandleEvent(agent.Event{
		Type:      agent.EventToolExecutionStart,
		Turn:      1,
		ToolName:  "bash",
		Arguments: `{"command":"sleep 1"}`,
	})

	o.spinner.mu.Lock()
	running := o.spinner.running
	o.spinner.mu.Unlock()
	if !running {
		t.Fatalf("plain bash calls should keep the activity spinner")
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
	if o.didStream || o.streamPrinted != 0 || o.streamLineOpen {
		t.Fatalf("stream state not reset: didStream=%v streamPrinted=%d streamLineOpen=%v",
			o.didStream, o.streamPrinted, o.streamLineOpen)
	}
}

func TestAgentConsolePrintResultDoesNotSayNoOutputAfterStreaming(t *testing.T) {
	var stdout, stderr bytes.Buffer
	o := &AgentOutput{
		stdout:   &stdout,
		stderr:   &stderr,
		stream:   true,
		markdown: false,
		color:    outpkg.NewColor(false),
	}
	repl := &AgentConsole{output: o}

	o.beginRun()
	o.HandleEvent(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	o.HandleEvent(agent.Event{Type: agent.EventMessageUpdate, Turn: 1, Message: contentMsg("visible")})
	repl.printResult(&agent.Result{})

	if got := stdout.String(); got != "visible\n" {
		t.Fatalf("stdout = %q, want streamed content plus newline", got)
	}
	if got := stripANSI(stderr.String()); strings.Contains(got, "No output") {
		t.Fatalf("stderr should not report No output after streamed content: %q", got)
	}
	if o.didStream {
		t.Fatal("printResult should reset stream state after finalizing streamed content")
	}
}

func TestAgentConsoleEnableDebugEventsWritesJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	session := agent.NewAgent(agent.Config{SessionID: "debug-test"})
	repl := &AgentConsole{
		session: session,
		output:  &AgentOutput{color: outpkg.NewColor(false)},
	}
	if err := repl.enableDebugEvents(path); err != nil {
		t.Fatalf("enableDebugEvents() error = %v", err)
	}
	defer repl.closeDebugEvents()

	session.Cfg.Bus.Emit(agent.Event{
		Type:   agent.EventAgentEnd,
		Stop:   agent.StopReasonCompleted,
		Detail: "assistant response had no tool calls",
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read debug events file: %v", err)
	}
	got := string(data)
	for _, want := range []string{`"type":"agent_end"`, `"stop":"completed"`, `"detail":"assistant response had no tool calls"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("events file missing %s: %s", want, got)
		}
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

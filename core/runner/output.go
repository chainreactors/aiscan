package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/output"
	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

const (
	agentStatusPreviewLimit = 180
	toolResultPreviewLines  = 3
	toolResultPreviewWidth  = 160
)

type AgentOutput struct {
	stdout   io.Writer
	stderr   io.Writer
	markdown bool
	color    output.Color
	debug    bool
	Quiet    bool
	tools    map[string]agentToolSummary

	// live streaming of assistant text deltas (Claude-Code-style): when stream
	// is true, EventMessageUpdate writes the freshly-arrived suffix to stdout so
	// the answer appears token-by-token instead of buffering the whole turn.
	stream         bool
	streamPrinted  int  // bytes of the current turn's content already flushed
	streamLineOpen bool // streamed text left the shared TTY cursor mid-line
	didStream      bool // this Run streamed assistant text; skip Final re-render
	streamContent  string

	// Pretty-render state. The REPL runs inside a PTY that may be forwarded to a
	// remote agent (aider), so transient chrome is gated by mode+tty: spinners,
	// OSC 8 hyperlinks and synchronized output only render for a local human.
	mode    RenderMode
	tty     bool
	spinner *spinner
}

type agentToolSummary struct {
	name      string
	summary   string
	arguments string
	started   time.Time
}

func NewAgentOutput(option *cfg.Option) *AgentOutput {
	markdown := stdoutMarkdownEnabled(option)
	debug := false
	quiet := false
	noColor := false
	if option != nil {
		debug = option.Debug
		quiet = option.Quiet
		noColor = option.NoColor
	}
	useColor := !noColor && term.IsTerminal(int(os.Stderr.Fd()))
	color := output.NewColor(useColor)
	tty := term.IsTerminal(int(os.Stderr.Fd()))
	return &AgentOutput{
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		markdown: markdown,
		color:    color,
		debug:    debug,
		Quiet:    quiet,
		tools:    make(map[string]agentToolSummary),
		stream:   interactiveStreamingEnabled(option),
		mode:     resolveRenderMode(),
		tty:      tty,
		spinner:  newSpinner(os.Stderr, color.Code(output.ANSICyan)),
	}
}

func stdoutMarkdownEnabled(option *cfg.Option) bool {
	if option != nil && option.NoColor {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// interactiveStreamingEnabled gates live assistant output. Interactive TTYs
// default to Markdown-aware streaming: stable blocks render as they arrive, and
// the final incomplete block renders at turn end. Set AISCAN_STREAM=0 (or
// "pretty") to force fully buffered final rendering.
func interactiveStreamingEnabled(option *cfg.Option) bool {
	return streamingEnabledForEnv(os.Getenv("AISCAN_STREAM"), term.IsTerminal(int(os.Stdout.Fd())))
}

func streamingEnabledForEnv(value string, stdoutIsTerminal bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes", "enabled", "stream", "streaming":
		return stdoutIsTerminal
	case "0", "false", "off", "no", "disabled", "pretty", "buffered":
		return false
	default:
		return stdoutIsTerminal
	}
}

func (o *AgentOutput) Start(label, text string) {
	if o == nil {
		return
	}
	o.spinner.Stop()
	o.ensureStreamNewline()
	o.beginRun()
	if o.Quiet {
		return
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "task"
	}

	// Interactive prompt echo: render like Claude Code's user-message bullet,
	// preserving the full (possibly multi-line) intent instead of compacting it.
	if label == "prompt" {
		body := strings.TrimRight(text, "\n")
		if shouldRenderUserIntent(body) {
			o.renderUserIntent(body)
		}
		return
	}

	text = compactAgentLine(text, agentStatusPreviewLimit)
	if text == "" {
		fmt.Fprintf(o.stderr, "%s> %s%s\n",
			o.color.Code(output.ANSIBold), label, o.color.Code(output.ANSIReset))
		return
	}
	fmt.Fprintf(o.stderr, "%s> %s:%s %s\n",
		o.color.Code(output.ANSIBold), label, o.color.Code(output.ANSIReset), text)
}

func (o *AgentOutput) Empty() {
	if o == nil || o.Quiet {
		return
	}
	o.spinner.Stop()
	o.ensureStreamNewline()
	fmt.Fprintf(o.stderr, "%sNo output.%s\n",
		o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset))
	o.maybeTip()
}

func (o *AgentOutput) Final(content string) {
	if o == nil {
		return
	}
	o.spinner.Stop()
	if o.didStream {
		if o.stream && o.markdown {
			o.streamMarkdownContent(content, true)
		}
		// assistant text was already streamed live — don't re-render/duplicate.
		// just ensure the cursor sits on a fresh line for the next prompt.
		o.ensureStreamNewline()
		o.maybeTip()
		o.resetStreamState()
		return
	}
	rendered := renderAgentMarkdown(content, o.markdown)
	if rendered == "" {
		return
	}
	fmt.Fprintln(o.stdout, rendered)
	o.maybeTip()
}

func (o *AgentOutput) HandleEvent(event agent.Event) {
	if o == nil {
		return
	}
	switch event.Type {
	case agent.EventTurnStart:
		// each assistant turn starts a fresh cumulative message
		o.streamPrinted = 0
		o.streamLineOpen = false
		o.streamContent = ""
		if o.canAnimate() {
			o.spinner.Start(o.thinkingLabel())
		}
	case agent.EventMessageUpdate:
		// First visible token settles any in-flight spinner before streaming.
		// Reasoning/tool-call deltas can arrive before user-visible text; keep
		// the activity indicator alive for those empty updates.
		o.streamDelta(event)
	case agent.EventToolExecutionStart:
		o.spinner.Stop()
		o.toolStart(event)
		if o.canAnimate() && !suppressToolSpinner(event) {
			o.spinner.Start(o.toolSpinnerLabel(event))
		}
	case agent.EventToolExecutionEnd:
		o.spinner.Stop()
		o.toolEnd(event)
	case agent.EventTurnEnd:
		o.spinner.Stop()
		o.turnEnd(event)
	case agent.EventAgentEnd:
		o.spinner.Stop()
		o.agentEnd(event)
	}
}

// streamDelta prints only the newly-arrived suffix of the assistant's visible
// content. The bus delivers the full cumulative message on each update, so we
// track how much we have already flushed and emit the remainder. Reasoning and
// in-flight tool-call argument deltas carry no visible content and are skipped.
func (o *AgentOutput) streamDelta(event agent.Event) {
	if o.Quiet || !o.stream || o.stdout == nil {
		return
	}
	content := ""
	if event.Message.Content != nil {
		content = *event.Message.Content
	}
	if o.markdown {
		o.streamMarkdownContent(content, false)
		return
	}
	o.streamRawDelta(content)
}

func (o *AgentOutput) streamRawDelta(content string) {
	if len(content) <= o.streamPrinted {
		return
	}
	delta := content[o.streamPrinted:]
	o.spinner.Stop()
	fmt.Fprint(o.stdout, delta)
	o.streamPrinted = len(content)
	o.streamLineOpen = !strings.HasSuffix(content, "\n")
	o.didStream = true
}

func (o *AgentOutput) streamMarkdownContent(content string, final bool) {
	o.streamContent = content
	if o.streamPrinted > len(content) {
		o.streamPrinted = 0
	}
	flushTo := markdownStreamFlushIndex(content, o.streamPrinted, final)
	if flushTo <= o.streamPrinted {
		return
	}
	chunk := content[o.streamPrinted:flushTo]
	o.streamPrinted = flushTo
	rendered := renderAgentMarkdown(chunk, o.markdown)
	if rendered == "" {
		return
	}
	o.spinner.Stop()
	fmt.Fprintln(o.stdout, rendered)
	o.streamLineOpen = false
	o.didStream = true
}

func (o *AgentOutput) beginRun() {
	o.resetStreamState()
}

func (o *AgentOutput) resetStreamState() {
	o.didStream = false
	o.streamPrinted = 0
	o.streamLineOpen = false
	o.streamContent = ""
}

func (o *AgentOutput) ensureStreamNewline() {
	if o == nil || !o.streamLineOpen || o.stdout == nil {
		return
	}
	fmt.Fprintln(o.stdout)
	o.streamLineOpen = false
}

// canAnimate gates transient chrome (spinners). Forwarded PTY sessions and
// non-TTY pipes get no spinner — a perpetually repainting line would corrupt
// the line-oriented stream a remote agent (aider) consumes.
func (o *AgentOutput) canAnimate() bool {
	return o != nil && o.mode == ModeInteractive && o.tty && !o.Quiet
}

// canHyperlink gates OSC 8 clickable paths. Same boundary as the spinner: only
// for a local human. Forwarded/piped output degrades to plain text.
func (o *AgentOutput) canHyperlink() bool {
	return o != nil && o.mode == ModeInteractive && o.tty
}

func (o *AgentOutput) maybeTip() {
	if o == nil || o.Quiet || o.stderr == nil || o.mode != ModeInteractive || !o.tty || !agentTipsEnabled() {
		return
	}
	tip, ok := chooseAgentTip()
	if !ok || strings.TrimSpace(tip.Text) == "" {
		return
	}
	fmt.Fprintf(o.stderr, "  %sTip: %s%s\n",
		o.color.Code(output.ANSIDim), tip.Text, o.color.Code(output.ANSIReset))
	recordAgentTipShown(tip)
}

func (o *AgentOutput) thinkingLabel() string {
	return "thinking"
}

func (o *AgentOutput) toolSpinnerLabel(event agent.Event) string {
	name := strings.TrimSpace(event.ToolName)
	if name == "" {
		name = "tool"
	}
	summary := compactAgentLine(summarizeToolArguments(name, event.Arguments), 48)
	if summary == "" {
		return name
	}
	return name + " " + summary
}

func suppressToolSpinner(event agent.Event) bool {
	if strings.TrimSpace(event.ToolName) != "bash" {
		return false
	}
	cmd := strings.TrimSpace(stringArg(decodeToolArguments(event.Arguments), "command"))
	if cmd == "" {
		return false
	}
	tokens := strings.Fields(cmd)
	for i := 0; i < len(tokens); i++ {
		token := strings.TrimSpace(tokens[i])
		if token == "" {
			continue
		}
		if strings.Contains(token, "=") && !strings.HasPrefix(token, "-") {
			if key, _, ok := strings.Cut(token, "="); ok && key != "" && !strings.ContainsAny(key, `/\`) {
				continue
			}
		}
		name := shellCommandBase(token)
		if isScannerLikeCommand(name) {
			return true
		}
		if name == "aiscan" && i+1 < len(tokens) {
			return isScannerLikeCommand(shellCommandBase(tokens[i+1]))
		}
		return false
	}
	return false
}

func shellCommandBase(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, `"'`)
	token = strings.TrimPrefix(token, "command:")
	if idx := strings.LastIndexAny(token, `/\`); idx >= 0 {
		token = token[idx+1:]
	}
	for strings.HasPrefix(token, "./") {
		token = strings.TrimPrefix(token, "./")
	}
	return token
}

func isScannerLikeCommand(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "scan", "spray", "gogo", "zombie", "neutron", "katana":
		return true
	default:
		return false
	}
}

// hyperlinkSummary wraps a path-bearing tool's summary in an OSC 8 file:// link
// so a local user can click straight to the file. No-op outside interactive
// TTY sessions (tests and forwarded PTYs get the plain summary).
func (o *AgentOutput) hyperlinkSummary(name, arguments, summary string) string {
	if !o.canHyperlink() || summary == "" {
		return summary
	}
	var path string
	if args := decodeToolArguments(arguments); args != nil {
		switch name {
		case "read", "write", "glob":
			path = stringArg(args, "path")
		}
	}
	if path == "" {
		return summary
	}
	if strings.Contains(path, "://") {
		return summary
	}
	return pathHyperlink(path, summary)
}

func (o *AgentOutput) turnEnd(event agent.Event) {
	o.ensureStreamNewline()
	if o.Quiet || !o.debug || event.Usage == nil {
		return
	}
	cache := ""
	if event.Usage.CacheReadTokens > 0 || event.Usage.CacheWriteTokens > 0 {
		cache = fmt.Sprintf(" cache_read=%d cache_write=%d (%.0f%%)",
			event.Usage.CacheReadTokens, event.Usage.CacheWriteTokens,
			event.Usage.CacheHitRatio()*100)
	}
	fmt.Fprintf(o.stderr, "%s[turn %d] prompt=%d completion=%d total=%d context=%d%s%s\n",
		o.color.Code(output.ANSIDim), event.Turn,
		event.Usage.PromptTokens, event.Usage.CompletionTokens, event.Usage.TotalTokens,
		event.ContextTokens, cache,
		o.color.Code(output.ANSIReset))
}

func (o *AgentOutput) agentEnd(event agent.Event) {
	o.ensureStreamNewline()
	if o.Quiet || !o.debug {
		return
	}
	stop := strings.TrimSpace(string(event.Stop))
	if stop == "" {
		stop = "unknown"
	}
	detail := compactAgentLine(event.Detail, agentStatusPreviewLimit)
	if detail == "" && event.Err != nil {
		detail = compactAgentLine(event.Err.Error(), agentStatusPreviewLimit)
	}
	if detail == "" {
		detail = "-"
	}
	fmt.Fprintf(o.stderr, "%s[agent] stop=%s detail=%s%s\n",
		o.color.Code(output.ANSIDim), stop, detail, o.color.Code(output.ANSIReset))
}

func (o *AgentOutput) toolStart(event agent.Event) {
	name := strings.TrimSpace(event.ToolName)
	if name == "" {
		name = "tool"
	}
	summary := summarizeToolArguments(name, event.Arguments)
	if o.tools == nil {
		o.tools = make(map[string]agentToolSummary)
	}
	if event.ToolCallID != "" {
		o.tools[event.ToolCallID] = agentToolSummary{name: name, summary: summary, arguments: event.Arguments, started: time.Now()}
	}
	if o.Quiet {
		return
	}
	o.ensureStreamNewline()

	display := o.hyperlinkSummary(name, event.Arguments, summary)
	header := fmt.Sprintf("  %s⎿ %s%s%s %sstarted%s",
		o.color.Code(output.ANSIDim), o.color.Code(output.ANSICyan), name, o.color.Code(output.ANSIReset),
		o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset))
	if display != "" {
		header += fmt.Sprintf("%s  %s%s",
			o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset), display)
	}
	fmt.Fprintln(o.stderr, header)

}

func (o *AgentOutput) toolEnd(event agent.Event) {
	if o.Quiet {
		return
	}

	summary := o.toolSummaryForEvent(event)
	if event.IsError || event.Err != nil {
		o.ensureStreamNewline()
		errText := strings.TrimSpace(event.Result)
		if event.Err != nil {
			errText = event.Err.Error()
		}
		if errText == "" {
			errText = "tool execution failed"
		}
		if summary.summary != "" {
			errText = summary.summary + ": " + errText
		}
		name := event.ToolName
		if strings.TrimSpace(name) == "" {
			name = summary.name
		}
		if strings.TrimSpace(name) == "" {
			name = "tool"
		}
		fmt.Fprintf(o.stderr, "  %s⎿ %s%s failed%s  %s\n",
			o.color.Code(output.ANSIDim), o.color.Code(output.ANSIRed),
			name, o.color.Code(output.ANSIReset), compactAgentLine(errText, agentStatusPreviewLimit))
		o.forgetTool(event.ToolCallID)
		return
	}

	result := strings.TrimSpace(event.Result)
	if result == "" {
		o.ensureStreamNewline()
		name := event.ToolName
		if strings.TrimSpace(name) == "" {
			name = summary.name
		}
		if strings.TrimSpace(name) == "" {
			name = "tool"
		}
		if elapsed := elapsedToolText(summary.started); elapsed != "" {
			fmt.Fprintf(o.stderr, "  %s⎿%s %s done %s\n",
				o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset), name, elapsed)
		} else {
			fmt.Fprintf(o.stderr, "  %s⎿%s %s done\n",
				o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset), name)
		}
		o.forgetTool(event.ToolCallID)
		return
	}

	o.ensureStreamNewline()
	arguments := event.Arguments
	if strings.TrimSpace(arguments) == "" {
		arguments = summary.arguments
	}
	name := event.ToolName
	if strings.TrimSpace(name) == "" {
		name = summary.name
	}
	o.renderToolResult(name, arguments, result)
	o.forgetTool(event.ToolCallID)
}

func (o *AgentOutput) renderToolResult(toolName, arguments, result string) {
	if toolName == "read" && !o.debug {
		if skillName, ok := aiscanSkillFromReadArguments(arguments); ok {
			fmt.Fprintf(o.stderr, "  %s⎿%s loaded built-in skill %s (%d lines)\n",
				o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset),
				skillName, countPreviewLines(result))
			return
		}
	}

	lines := strings.Split(result, "\n")

	maxLines := toolResultPreviewLines

	switch toolName {
	case "read":
		maxLines = 5
	case "write":
		maxLines = 6
	}

	showLines := lines
	truncated := false
	if len(showLines) > maxLines {
		showLines = showLines[:maxLines]
		truncated = true
	}

	for _, line := range showLines {
		display := line
		if len(display) > toolResultPreviewWidth {
			display = display[:toolResultPreviewWidth] + "…"
		}
		fmt.Fprintf(o.stderr, "  %s⎿%s %s\n",
			o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset), display)
	}

	if truncated {
		remaining := len(lines) - maxLines
		fmt.Fprintf(o.stderr, "  %s⎿ … +%d lines%s\n",
			o.color.Code(output.ANSIDim), remaining, o.color.Code(output.ANSIReset))
	}
}

func (o *AgentOutput) renderUserIntent(body string) {
	if o == nil || o.stderr == nil {
		return
	}
	title := "user"
	top := o.color.Code(output.ANSIDim) + "╭─ " + o.color.Code(output.ANSIReset) +
		o.color.Code(output.ANSIBold) + title + o.color.Code(output.ANSIReset)
	fmt.Fprintln(o.stderr, top)
	if strings.TrimSpace(body) == "" {
		fmt.Fprintf(o.stderr, "%s│%s\n", o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset))
	} else {
		for _, line := range strings.Split(body, "\n") {
			fmt.Fprintf(o.stderr, "%s│%s %s\n",
				o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset), line)
		}
	}
	fmt.Fprintf(o.stderr, "%s╰─%s\n", o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset))
}

func shouldRenderUserIntent(body string) bool {
	return strings.TrimSpace(body) != ""
}

func (o *AgentOutput) toolSummaryForEvent(event agent.Event) agentToolSummary {
	if o == nil || event.ToolCallID == "" || o.tools == nil {
		return agentToolSummary{name: event.ToolName, summary: summarizeToolArguments(event.ToolName, event.Arguments), arguments: event.Arguments}
	}
	if summary, ok := o.tools[event.ToolCallID]; ok {
		return summary
	}
	return agentToolSummary{name: event.ToolName, summary: summarizeToolArguments(event.ToolName, event.Arguments), arguments: event.Arguments}
}

func (o *AgentOutput) forgetTool(id string) {
	if o == nil || id == "" || o.tools == nil {
		return
	}
	delete(o.tools, id)
}

func elapsedToolText(started time.Time) string {
	if started.IsZero() {
		return ""
	}
	elapsed := time.Since(started)
	if elapsed < time.Second {
		return fmt.Sprintf("· %dms", elapsed.Milliseconds())
	}
	return fmt.Sprintf("· %.1fs", elapsed.Seconds())
}

func renderAgentMarkdown(content string, enabled bool) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if !enabled {
		return content
	}
	r, err := getAgentMarkdownRenderer()
	if err != nil {
		return content
	}
	rendered, err := r.Render(content)
	if err != nil {
		return content
	}
	rendered = trimRenderedMarkdownLineEnds(rendered)
	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return content
	}
	return trimRenderedMarkdownLineEnds(rendered)
}

func markdownStreamFlushIndex(content string, start int, final bool) int {
	if final {
		return len(content)
	}
	if start < 0 {
		start = 0
	}
	if start > len(content) {
		return len(content)
	}

	boundary := start
	inFence := false
	var fenceChar byte
	fenceLen := 0

	for lineStart := start; lineStart < len(content); {
		rel := strings.IndexByte(content[lineStart:], '\n')
		if rel < 0 {
			break
		}
		lineEnd := lineStart + rel
		next := lineEnd + 1
		line := content[lineStart:lineEnd]
		trimmedLeft := strings.TrimLeft(line, " \t")

		if ch, count, ok := markdownFenceInfo(trimmedLeft); ok {
			if !inFence {
				inFence = true
				fenceChar = ch
				fenceLen = count
			} else if ch == fenceChar && count >= fenceLen {
				inFence = false
				boundary = next
			}
			lineStart = next
			continue
		}
		if inFence {
			lineStart = next
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || markdownLineFlushable(trimmed) {
			boundary = next
		}
		lineStart = next
	}
	return boundary
}

func markdownFenceInfo(line string) (byte, int, bool) {
	if len(line) < 3 {
		return 0, 0, false
	}
	ch := line[0]
	if ch != '`' && ch != '~' {
		return 0, 0, false
	}
	count := 0
	for count < len(line) && line[count] == ch {
		count++
	}
	return ch, count, count >= 3
}

func markdownLineFlushable(trimmed string) bool {
	return isATXHeading(trimmed) ||
		isMarkdownListItem(trimmed) ||
		isMarkdownBlockquote(trimmed) ||
		isMarkdownThematicBreak(trimmed) ||
		isStandaloneStrongLine(trimmed)
}

func isATXHeading(s string) bool {
	count := 0
	for count < len(s) && s[count] == '#' {
		count++
	}
	return count > 0 && count <= 6 && (count == len(s) || s[count] == ' ' || s[count] == '\t')
}

func isMarkdownListItem(s string) bool {
	if len(s) >= 2 {
		switch s[0] {
		case '-', '+', '*':
			if s[1] == ' ' || s[1] == '\t' {
				return true
			}
		}
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i > 9 || i+1 >= len(s) {
		return false
	}
	return (s[i] == '.' || s[i] == ')') && (s[i+1] == ' ' || s[i+1] == '\t')
}

func isMarkdownBlockquote(s string) bool {
	return strings.HasPrefix(s, ">")
}

func isMarkdownThematicBreak(s string) bool {
	var marker byte
	count := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t':
			continue
		case '-', '_', '*':
			if marker == 0 {
				marker = s[i]
			}
			if s[i] != marker {
				return false
			}
			count++
		default:
			return false
		}
	}
	return count >= 3
}

func isStandaloneStrongLine(s string) bool {
	if len(s) <= 4 {
		return false
	}
	return (strings.HasPrefix(s, "**") && strings.HasSuffix(s, "**")) ||
		(strings.HasPrefix(s, "__") && strings.HasSuffix(s, "__"))
}

func trimRenderedMarkdownLineEnds(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	start := 0
	for start < len(s) {
		rel := strings.IndexByte(s[start:], '\n')
		if rel < 0 {
			b.WriteString(trimANSIVisibleRight(s[start:]))
			break
		}
		end := start + rel
		b.WriteString(trimANSIVisibleRight(s[start:end]))
		b.WriteByte('\n')
		start = end + 1
	}
	return b.String()
}

func trimANSIVisibleRight(line string) string {
	cut := 0
	extendCutWithANSI := false
	for i := 0; i < len(line); {
		if end, ok := ansiEscapeEnd(line, i); ok {
			if extendCutWithANSI && ansiClosesStyle(line[i:end]) {
				cut = end
			}
			i = end
			continue
		}

		r, size := utf8.DecodeRuneInString(line[i:])
		if r == utf8.RuneError && size == 1 {
			cut = i + size
			extendCutWithANSI = true
			i += size
			continue
		}

		end := i + size
		if unicode.IsSpace(r) {
			extendCutWithANSI = false
		} else {
			cut = end
			extendCutWithANSI = true
		}
		i = end
	}
	return line[:cut]
}

func ansiClosesStyle(seq string) bool {
	if strings.HasPrefix(seq, "\x1b]8;;") {
		return true
	}
	if len(seq) < 3 || seq[0] != '\x1b' || seq[1] != '[' || seq[len(seq)-1] != 'm' {
		return false
	}
	params := seq[2 : len(seq)-1]
	if params == "" {
		return true
	}
	for _, param := range strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' }) {
		switch param {
		case "0", "22", "23", "24", "25", "27", "28", "29", "39", "49", "59":
			return true
		}
	}
	return false
}

func ansiEscapeEnd(s string, start int) (int, bool) {
	if start >= len(s) || s[start] != '\x1b' {
		return 0, false
	}
	if start+1 >= len(s) {
		return start + 1, true
	}

	switch s[start+1] {
	case '[':
		for i := start + 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1, true
			}
		}
		return len(s), true
	case ']':
		for i := start + 2; i < len(s); i++ {
			switch {
			case s[i] == '\a':
				return i + 1, true
			case s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '\\':
				return i + 2, true
			}
		}
		return len(s), true
	default:
		return start + 2, true
	}
}

var (
	agentMarkdownRenderer     *glamour.TermRenderer
	agentMarkdownRendererErr  error
	agentMarkdownRendererOnce sync.Once
)

func getAgentMarkdownRenderer() (*glamour.TermRenderer, error) {
	agentMarkdownRendererOnce.Do(func() {
		opts := []glamour.TermRendererOption{
			// Use a fixed terminal style. Glamour's auto style falls back to the
			// no-TTY style under tests/forwarded sessions, which can preserve
			// inline markers like **bold** instead of rendering them.
			glamour.WithStandardStyle("dark"),
			// Auto-detect the richest color profile the terminal advertises
			// (truecolor → 256 → ANSI) instead of pinning 16-color ANSI.
			glamour.WithColorProfile(termenv.ColorProfile()),
			glamour.WithEmoji(),
		}
		if w := terminalWidth(); w > 0 {
			opts = append(opts, glamour.WithWordWrap(w))
		}
		agentMarkdownRenderer, agentMarkdownRendererErr = glamour.NewTermRenderer(opts...)
	})
	return agentMarkdownRenderer, agentMarkdownRendererErr
}

// terminalWidth returns the stdout column count, or 0 when unknown (piped /
// forwarded sessions) so the markdown renderer skips width-bounded wrapping.
func terminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 0
}

func summarizeToolArguments(name, arguments string) string {
	args := decodeToolArguments(arguments)
	if len(args) == 0 {
		return ""
	}
	switch name {
	case "bash":
		return compactAgentLine(stringArg(args, "command"), agentStatusPreviewLimit)
	case "read":
		path := stringArg(args, "path")
		if skillName, ok := aiscanSkillName(path); ok {
			path = "skill: " + skillName
		}
		if offset := stringArg(args, "offset"); offset != "" && offset != "0" {
			path += fmt.Sprintf(" (offset=%s)", offset)
		}
		return compactAgentLine(path, agentStatusPreviewLimit)
	case "write":
		path := stringArg(args, "path")
		if edits, ok := args["edits"]; ok && edits != nil {
			if arr, ok := edits.([]any); ok {
				path += fmt.Sprintf(" (edit: %d change(s))", len(arr))
			}
		}
		return compactAgentLine(path, agentStatusPreviewLimit)
	case "glob":
		return compactAgentLine(joinAgentSummaryParts(
			stringArg(args, "pattern"),
			prefixedArg("in ", stringArg(args, "path")),
		), agentStatusPreviewLimit)
	case "subagent":
		action := stringArg(args, "action")
		if action == "" || action == "create" {
			mode := stringArg(args, "mode")
			typeName := stringArg(args, "type")
			prompt := compactAgentLine(stringArg(args, "prompt"), 80)
			return joinAgentSummaryParts(typeName, prefixedArg("mode=", mode), prompt)
		}
		return joinAgentSummaryParts(action, stringArg(args, "name"))
	case "ioa_space":
		return compactAgentLine(stringArg(args, "name"), agentStatusPreviewLimit)
	case "ioa_send":
		return compactAgentLine(prefixedArg("space ", stringArg(args, "space_id")), agentStatusPreviewLimit)
	case "ioa_read":
		return compactAgentLine(joinAgentSummaryParts(
			prefixedArg("space ", stringArg(args, "space_id")),
			prefixedArg("message ", stringArg(args, "message_id")),
			prefixedArg("after ", stringArg(args, "after")),
		), agentStatusPreviewLimit)
	default:
		return compactAgentLine(firstNonEmptyArg(args, "target", "url", "input", "path", "name"), agentStatusPreviewLimit)
	}
}

func aiscanSkillFromReadArguments(arguments string) (string, bool) {
	args := decodeToolArguments(arguments)
	if len(args) == 0 {
		return "", false
	}
	return aiscanSkillName(stringArg(args, "path"))
}

func aiscanSkillName(path string) (string, bool) {
	path = strings.TrimSpace(path)
	const prefix = "aiscan://skills/"
	const suffix = "/SKILL.md"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

func countPreviewLines(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func decodeToolArguments(arguments string) map[string]any {
	var args map[string]any
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return nil
	}
	return args
}

func stringArg(args map[string]any, key string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64, bool:
		return fmt.Sprint(typed)
	default:
		data, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(data)
	}
}

func firstNonEmptyArg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringArg(args, key); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func prefixedArg(prefix, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return prefix + value
}

func joinAgentSummaryParts(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

func compactAgentLine(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit > 0 && len(value) > limit {
		return value[:limit] + "…"
	}
	return value
}

func compactAgentJSON(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(value)); err == nil {
		value = buf.String()
	}
	return compactAgentLine(value, limit)
}

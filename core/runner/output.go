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

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/output"
	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

const (
	agentStatusPreviewLimit = 180
	agentDebugPreviewLimit  = 320
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

	// Pretty-render state. The REPL runs inside a PTY that may be forwarded to a
	// remote agent (aider), so transient chrome is gated by mode+tty: spinners,
	// OSC 8 hyperlinks and synchronized output only render for a local human.
	mode    RenderMode
	tty     bool
	spinner *spinner
}

type agentToolSummary struct {
	name    string
	summary string
	started time.Time
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

// interactiveStreamingEnabled gates live token streaming. Pretty buffered
// output is the default in the REPL so final Markdown can be rendered as tables,
// headings, and wrapped prose. Set AISCAN_STREAM=1 when token-by-token output is
// preferred over final rendering.
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
		return false
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
		if o.canAnimate() {
			o.spinner.Start(o.thinkingLabel())
		}
	case agent.EventMessageUpdate:
		// first visible token settles any in-flight spinner before streaming
		o.spinner.Stop()
		o.streamDelta(event)
	case agent.EventToolExecutionStart:
		o.spinner.Stop()
		o.toolStart(event)
		if o.canAnimate() {
			o.spinner.Start(o.toolSpinnerLabel(event))
		}
	case agent.EventToolExecutionEnd:
		o.spinner.Stop()
		o.toolEnd(event)
	case agent.EventTurnEnd:
		o.spinner.Stop()
		o.turnEnd(event)
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
	if len(content) <= o.streamPrinted {
		return
	}
	delta := content[o.streamPrinted:]
	fmt.Fprint(o.stdout, delta)
	o.streamPrinted = len(content)
	o.streamLineOpen = !strings.HasSuffix(content, "\n")
	o.didStream = true
}

func (o *AgentOutput) beginRun() {
	o.resetStreamState()
}

func (o *AgentOutput) resetStreamState() {
	o.didStream = false
	o.streamPrinted = 0
	o.streamLineOpen = false
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
		o.tools[event.ToolCallID] = agentToolSummary{name: name, summary: summary, started: time.Now()}
	}
	if o.Quiet {
		return
	}
	o.ensureStreamNewline()

	display := o.hyperlinkSummary(name, event.Arguments, summary)
	header := fmt.Sprintf("  %s⎿ %s%s%s",
		o.color.Code(output.ANSIDim), o.color.Code(output.ANSICyan), name, o.color.Code(output.ANSIReset))
	if display != "" {
		header += fmt.Sprintf("%s  %s%s",
			o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset), display)
	}
	fmt.Fprintln(o.stderr, header)

	if o.debug {
		if args := compactAgentJSON(event.Arguments, agentDebugPreviewLimit); args != "" {
			fmt.Fprintf(o.stderr, "  %sargs: %s%s\n",
				o.color.Code(output.ANSIDim), args, o.color.Code(output.ANSIReset))
		}
	}
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
		fmt.Fprintf(o.stderr, "  %s⎿ %s%s%s\n",
			o.color.Code(output.ANSIDim), o.color.Code(output.ANSIRed),
			compactAgentLine(errText, agentStatusPreviewLimit), o.color.Code(output.ANSIReset))
		o.forgetTool(event.ToolCallID)
		return
	}

	result := strings.TrimSpace(event.Result)
	if result == "" {
		o.ensureStreamNewline()
		if elapsed := elapsedToolText(summary.started); elapsed != "" {
			fmt.Fprintf(o.stderr, "  %s⎿ done %s%s\n",
				o.color.Code(output.ANSIDim), elapsed, o.color.Code(output.ANSIReset))
		}
		o.forgetTool(event.ToolCallID)
		return
	}

	o.ensureStreamNewline()
	o.renderToolResult(event.ToolName, result)
	o.forgetTool(event.ToolCallID)
}

func (o *AgentOutput) renderToolResult(toolName, result string) {
	lines := strings.Split(result, "\n")

	maxLines := toolResultPreviewLines
	if o.debug {
		maxLines = 20
	}

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
	return strings.Contains(strings.TrimRight(body, "\n"), "\n")
}

func (o *AgentOutput) toolSummaryForEvent(event agent.Event) agentToolSummary {
	if o == nil || event.ToolCallID == "" || o.tools == nil {
		return agentToolSummary{name: event.ToolName, summary: summarizeToolArguments(event.ToolName, event.Arguments)}
	}
	if summary, ok := o.tools[event.ToolCallID]; ok {
		return summary
	}
	return agentToolSummary{name: event.ToolName, summary: summarizeToolArguments(event.ToolName, event.Arguments)}
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
	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return content
	}
	return rendered
}

var (
	agentMarkdownRenderer     *glamour.TermRenderer
	agentMarkdownRendererErr  error
	agentMarkdownRendererOnce sync.Once
)

func getAgentMarkdownRenderer() (*glamour.TermRenderer, error) {
	agentMarkdownRendererOnce.Do(func() {
		opts := []glamour.TermRendererOption{
			glamour.WithAutoStyle(),
			// Auto-detect the richest profile the terminal advertises (truecolor
			// → 256 → ANSI) instead of pinning 16-color ANSI, so markdown answers
			// render with real depth on modern terminals.
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

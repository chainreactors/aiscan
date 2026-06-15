package tui

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

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	agentStatusPreviewLimit = 180
	agentDebugPreviewLimit  = 320
	toolResultPreviewLines  = 6
	toolResultPreviewWidth  = 140
	toolFetchBodyLines      = 4
)

// ---------------------------------------------------------------------------
// AgentOutput
// ---------------------------------------------------------------------------

// AgentOutput renders agent events and assistant content to the terminal. It is
// safe for concurrent use: a sync.Mutex serialises HandleEvent, streamDelta,
// and lifecycle methods so streaming tokens and bus events from different
// goroutines never interleave output.
type AgentOutput struct {
	mu       sync.Mutex
	stdout   io.Writer
	stderr   io.Writer
	markdown bool
	color    output.Color
	debug    bool
	Quiet    bool
	tools    map[string]agentToolSummary

	// Live streaming of assistant text deltas (Claude-Code-style): when stream
	// is true, EventMessageUpdate writes the freshly-arrived suffix to stdout so
	// the answer appears token-by-token instead of buffering the whole turn.
	stream         bool
	streamPrinted  int    // bytes of the current turn's content already flushed
	streamLineOpen bool   // streamed text left the shared TTY cursor mid-line
	didStream      bool   // this Run streamed assistant text; Final may skip duplicate re-render
	lastStreamed   string // full cumulative content of the streamed turn
	aborted        bool   // current run was interrupted; drop late bus events/finals

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

// NewAgentOutput constructs an AgentOutput wired to os.Stdout/os.Stderr with
// rendering decisions derived from the supplied option and terminal state.
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
		stream:   stdoutDeltaStreamingEnabled(option),
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

// AgentStreamingEnabled keeps the agent/provider path event-streamed by default,
// matching the streamSimple-first model. Rendering decisions are separate:
// non-TTY stdout can stay buffered while subscribers still receive
// message_update events.
func AgentStreamingEnabled(_ *cfg.Option) bool {
	return true
}

// stdoutDeltaStreamingEnabled gates direct stdout rendering of assistant deltas.
// The agent still streams events when this is false; Final() renders the
// completed answer for non-interactive callers.
func stdoutDeltaStreamingEnabled(_ *cfg.Option) bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// ---------------------------------------------------------------------------
// Lifecycle methods
// ---------------------------------------------------------------------------

func (o *AgentOutput) Start(label, text string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.spinner.Stop()
	o.ensureStreamNewlineLocked()
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
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.aborted {
		return
	}
	o.spinner.Stop()
	o.ensureStreamNewlineLocked()
	fmt.Fprintf(o.stderr, "%sNo output.%s\n",
		o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset))
}

func (o *AgentOutput) Final(content string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.aborted {
		return
	}
	o.spinner.Stop()
	if o.didStream && sameRenderedAgentText(content, o.lastStreamed) {
		// Assistant text was already streamed live — don't re-render/duplicate.
		// Just ensure the cursor sits on a fresh line for the next prompt.
		o.ensureStreamNewlineLocked()
		o.resetStreamState()
		return
	}
	rendered := renderAgentMarkdown(content, o.markdown)
	if rendered == "" {
		return
	}
	fmt.Fprintln(o.stdout, rendered)
}

func (o *AgentOutput) Queued(text string) {
	if o == nil || o.Quiet {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.spinner.Stop()
	o.ensureStreamNewlineLocked()
	text = compactAgentLine(text, agentStatusPreviewLimit)
	if text == "" {
		fmt.Fprintf(o.stderr, "%squeued%s\n",
			o.color.Code(output.ANSIBold), o.color.Code(output.ANSIReset))
		return
	}
	fmt.Fprintf(o.stderr, "%squeued:%s %s\n",
		o.color.Code(output.ANSIBold), o.color.Code(output.ANSIReset), text)
}

func (o *AgentOutput) QueuedFollowUp(text string) {
	if o == nil || o.Quiet {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.spinner.Stop()
	o.ensureStreamNewlineLocked()
	text = compactAgentLine(text, agentStatusPreviewLimit)
	if text == "" {
		fmt.Fprintf(o.stderr, "%squeued follow-up%s\n",
			o.color.Code(output.ANSIBold), o.color.Code(output.ANSIReset))
		return
	}
	fmt.Fprintf(o.stderr, "%squeued follow-up:%s %s\n",
		o.color.Code(output.ANSIBold), o.color.Code(output.ANSIReset), text)
}

func (o *AgentOutput) Stopping() {
	if o == nil || o.Quiet {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.spinner.Stop()
	o.ensureStreamNewlineLocked()
	fmt.Fprintf(o.stderr, "%sStopping current task...%s\n",
		o.color.Code(output.ANSIYellow), o.color.Code(output.ANSIReset))
}

func (o *AgentOutput) Stopped() {
	if o == nil || o.Quiet {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.spinner.Stop()
	o.ensureStreamNewlineLocked()
	fmt.Fprintf(o.stderr, "%sTask stopped.%s\n",
		o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset))
}

func (o *AgentOutput) Error(err error) {
	if o == nil || err == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.aborted {
		return
	}
	o.spinner.Stop()
	o.ensureStreamNewlineLocked()
	fmt.Fprintf(o.stderr, "error: %s\n", err)
}

func (o *AgentOutput) AbortCurrentRun() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.spinner.Stop()
	o.ensureStreamNewlineLocked()
	o.resetStreamState()
	o.aborted = true
	if o.tools != nil {
		for id := range o.tools {
			delete(o.tools, id)
		}
	}
}

// EnsureStreamNewline closes any open streamed line (mid-line cursor) by
// emitting a newline. Thread-safe wrapper for the locked variant.
func (o *AgentOutput) EnsureStreamNewline() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.ensureStreamNewlineLocked()
}

// ---------------------------------------------------------------------------
// Event handling
// ---------------------------------------------------------------------------

func (o *AgentOutput) HandleEvent(event agent.Event) {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.aborted {
		return
	}
	switch event.Type {
	case agent.EventTurnStart:
		// Each assistant turn starts a fresh cumulative message.
		o.streamPrinted = 0
		if o.canAnimate() {
			o.spinner.Start(o.thinkingLabel())
		}
	case agent.EventMessageUpdate:
		// First visible token settles any in-flight spinner before streaming.
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
	case agent.EventAgentEnd:
		o.spinner.Stop()
		o.agentEnd(event)
	case agent.EventGoalEvalStart:
		o.spinner.Stop()
		o.goalEvalStart(event)
	case agent.EventGoalEvalEnd:
		o.spinner.Stop()
		o.goalEvalEnd(event)
	case agent.EventGoalEvalError:
		o.spinner.Stop()
		o.goalEvalError(event)
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
	o.lastStreamed = content
}

// ---------------------------------------------------------------------------
// Animation / hyperlink gating
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Internal run state
// ---------------------------------------------------------------------------

func (o *AgentOutput) beginRun() {
	o.resetStreamState()
	o.aborted = false
	if o.tools == nil {
		o.tools = make(map[string]agentToolSummary)
		return
	}
	for id := range o.tools {
		delete(o.tools, id)
	}
}

func (o *AgentOutput) resetStreamState() {
	o.didStream = false
	o.streamPrinted = 0
	o.streamLineOpen = false
	o.lastStreamed = ""
}

func (o *AgentOutput) ensureStreamNewlineLocked() {
	if o == nil || !o.streamLineOpen || o.stdout == nil {
		return
	}
	fmt.Fprintln(o.stdout)
	o.streamLineOpen = false
}

// ---------------------------------------------------------------------------
// Spinner label helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Tool execution rendering
// ---------------------------------------------------------------------------

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
	o.ensureStreamNewlineLocked()

	display := o.hyperlinkSummary(name, event.Arguments, summary)
	header := fmt.Sprintf("  %s⎿ %s%s started%s",
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
		o.ensureStreamNewlineLocked()
		errText := strings.TrimSpace(event.Result)
		if event.Err != nil {
			errText = event.Err.Error()
		}
		if errText == "" {
			errText = "tool execution failed"
		}
		name := firstNonEmptyString(summary.name, event.ToolName, "tool")
		if summary.summary != "" {
			errText = summary.summary + ": " + errText
		}
		fmt.Fprintf(o.stderr, "  %s⎿ %s%s failed%s  %s\n",
			o.color.Code(output.ANSIDim), o.color.Code(output.ANSIRed), name,
			o.color.Code(output.ANSIReset),
			compactAgentLine(errText, agentStatusPreviewLimit))
		o.forgetTool(event.ToolCallID)
		return
	}

	result := strings.TrimSpace(event.Result)
	if result == "" {
		o.ensureStreamNewlineLocked()
		if elapsed := elapsedToolText(summary.started); elapsed != "" {
			fmt.Fprintf(o.stderr, "  %s⎿ %s done %s%s\n",
				o.color.Code(output.ANSIDim),
				firstNonEmptyString(summary.name, event.ToolName, "tool"),
				elapsed, o.color.Code(output.ANSIReset))
		}
		o.forgetTool(event.ToolCallID)
		return
	}

	o.ensureStreamNewlineLocked()
	o.renderToolResult(firstNonEmptyString(summary.name, event.ToolName), result, elapsedToolText(summary.started))
	o.forgetTool(event.ToolCallID)
}

func (o *AgentOutput) renderToolResult(toolName, result, elapsed string) {
	preview := buildToolResultPreview(toolName, result, o.debug)
	if len(preview.lines) == 0 {
		return
	}

	if len(preview.lines) == 1 && !preview.truncated {
		fmt.Fprintf(o.stderr, "  %s⎿%s %s\n",
			o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset), preview.lines[0])
		return
	}

	fmt.Fprintf(o.stderr, "  %s⎿%s %s\n",
		o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset), toolResultTitle(toolName, elapsed))
	for _, line := range preview.lines {
		if line == "" {
			fmt.Fprintf(o.stderr, "    %s│%s\n",
				o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset))
			continue
		}
		fmt.Fprintf(o.stderr, "    %s│%s %s\n",
			o.color.Code(output.ANSIDim), o.color.Code(output.ANSIReset), line)
	}

	if preview.truncated {
		fmt.Fprintf(o.stderr, "    %s… +%d lines hidden%s\n",
			o.color.Code(output.ANSIDim), preview.hidden, o.color.Code(output.ANSIReset))
	}
}

// ---------------------------------------------------------------------------
// Tool result preview
// ---------------------------------------------------------------------------

type toolResultPreview struct {
	lines     []string
	truncated bool
	hidden    int
}

func buildToolResultPreview(toolName, result string, debug bool) toolResultPreview {
	lines := normalizeToolResultLines(result)
	if len(lines) == 0 {
		return toolResultPreview{}
	}

	if toolName == "fetch" {
		return buildFetchToolResultPreview(lines, debug)
	}

	maxLines := toolResultPreviewLines
	if debug {
		maxLines = 20
	}

	switch toolName {
	case "read":
		maxLines = 8
	case "write":
		maxLines = 6
	case "bash":
		maxLines = 8
	}

	return selectToolResultLines(lines, maxLines)
}

func buildFetchToolResultPreview(lines []string, debug bool) toolResultPreview {
	sep := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			sep = i
			break
		}
	}
	if sep < 0 {
		return selectToolResultLines(lines, toolResultPreviewLines)
	}

	bodyLines := toolFetchBodyLines
	if debug {
		bodyLines = 16
	}

	selected := make([]string, 0, sep+1+bodyLines)
	selected = append(selected, lines[:sep+1]...)

	bodyKept := 0
	lastSelected := sep
	for i := sep + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		selected = append(selected, line)
		lastSelected = i
		bodyKept++
		if bodyKept >= bodyLines {
			break
		}
	}

	hidden := len(lines) - lastSelected - 1
	if hidden < 0 {
		hidden = 0
	}
	return finalizeToolResultPreviewWithHidden(selected, hidden)
}

func selectToolResultLines(lines []string, maxLines int) toolResultPreview {
	if maxLines <= 0 || maxLines >= len(lines) {
		return finalizeToolResultPreview(lines, lines)
	}
	return finalizeToolResultPreview(lines, lines[:maxLines])
}

func finalizeToolResultPreview(all, selected []string) toolResultPreview {
	return finalizeToolResultPreviewWithHidden(selected, len(all)-len(selected))
}

func finalizeToolResultPreviewWithHidden(selected []string, hidden int) toolResultPreview {
	display := make([]string, 0, len(selected))
	for _, line := range selected {
		display = append(display, truncateToolResultLine(line, toolResultPreviewWidth))
	}
	if hidden < 0 {
		hidden = 0
	}
	return toolResultPreview{
		lines:     display,
		truncated: hidden > 0,
		hidden:    hidden,
	}
}

func normalizeToolResultLines(result string) []string {
	result = strings.ReplaceAll(result, "\r\n", "\n")
	result = strings.ReplaceAll(result, "\r", "\n")

	rawLines := strings.Split(result, "\n")
	lines := make([]string, 0, len(rawLines))
	lastBlank := false
	for _, line := range rawLines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if len(lines) == 0 || lastBlank {
				continue
			}
			lines = append(lines, "")
			lastBlank = true
			continue
		}
		lines = append(lines, line)
		lastBlank = false
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func truncateToolResultLine(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	var b strings.Builder
	b.Grow(limit + len("…"))
	count := 0
	for _, r := range value {
		if count >= limit {
			break
		}
		b.WriteRune(r)
		count++
	}
	b.WriteString("…")
	return b.String()
}

func toolResultTitle(toolName, elapsed string) string {
	title := ""
	switch toolName {
	case "fetch":
		title = "fetch result"
	case "read":
		title = "read preview"
	case "bash":
		title = "bash output"
	case "":
		title = "tool output"
	default:
		title = toolName + " output"
	}
	if elapsed != "" {
		title += " " + elapsed
	}
	return title
}

// ---------------------------------------------------------------------------
// Turn / agent end (debug diagnostics)
// ---------------------------------------------------------------------------

func (o *AgentOutput) turnEnd(event agent.Event) {
	o.ensureStreamNewlineLocked()
	if o.Quiet || !o.debug {
		return
	}
	role, contentLen, toolCalls, reasoningLen, preview := summarizeChatMessage(event.Message)
	if role != "" || contentLen > 0 || toolCalls > 0 || reasoningLen > 0 {
		fmt.Fprintf(o.stderr, "%s[debug] [turn %d] assistant role=%s content=%d reasoning=%d tool_calls=%d preview=%q%s\n",
			o.color.Code(output.ANSIDim),
			event.Turn,
			role,
			contentLen,
			reasoningLen,
			toolCalls,
			preview,
			o.color.Code(output.ANSIReset))
	}
	if event.Usage == nil {
		return
	}
	cache := ""
	if event.Usage.CacheReadTokens > 0 || event.Usage.CacheWriteTokens > 0 {
		cache = fmt.Sprintf(" cache_read=%d cache_write=%d (%.0f%%)",
			event.Usage.CacheReadTokens, event.Usage.CacheWriteTokens,
			event.Usage.CacheHitRatio()*100)
	}
	fmt.Fprintf(o.stderr, "%s[debug] [turn %d] prompt=%d completion=%d total=%d context=%d%s%s\n",
		o.color.Code(output.ANSIDim), event.Turn,
		event.Usage.PromptTokens, event.Usage.CompletionTokens, event.Usage.TotalTokens,
		event.ContextTokens, cache,
		o.color.Code(output.ANSIReset))
}

func (o *AgentOutput) agentEnd(event agent.Event) {
	o.ensureStreamNewlineLocked()
	if o.Quiet || !o.debug {
		return
	}
	errText := ""
	if event.Err != nil {
		errText = fmt.Sprintf(" err=%q", event.Err.Error())
	}
	lastRole, lastContentLen, lastToolCalls, lastReasoningLen, lastPreview := lastMessageSummary(event.Messages)
	noToolAssistant := lastRole == "assistant" && lastToolCalls == 0
	hint := ""
	if event.Stop == agent.StopReasonCompleted && noToolAssistant {
		hint = " hint=no_tool_calls_no_pending_work"
	}
	fmt.Fprintf(o.stderr, "%s[debug] [agent] stop=%s turns=%d messages=%d new_messages=%d last_role=%s last_content=%d last_reasoning=%d last_tool_calls=%d last_assistant_no_tool=%v last_preview=%q%s%s%s\n",
		o.color.Code(output.ANSIDim),
		event.Stop,
		event.Turn,
		len(event.Messages),
		len(event.NewMessages),
		lastRole,
		lastContentLen,
		lastReasoningLen,
		lastToolCalls,
		noToolAssistant,
		lastPreview,
		hint,
		errText,
		o.color.Code(output.ANSIReset))
}

// ---------------------------------------------------------------------------
// Goal evaluation output
// ---------------------------------------------------------------------------

func (o *AgentOutput) goalEvalStart(event agent.Event) {
	if o.Quiet || o.stderr == nil {
		return
	}
	label := fmt.Sprintf("Evaluating goal completion (round %d)...", event.EvalRound+1)
	if o.canAnimate() {
		o.spinner.Start(label)
	} else {
		fmt.Fprintf(o.stderr, "%s\n", label)
	}
}

func (o *AgentOutput) goalEvalEnd(event agent.Event) {
	if o.Quiet || o.stderr == nil {
		return
	}
	if event.EvalPass {
		fmt.Fprintf(o.stderr, "%s[eval] pass — %s%s\n",
			o.color.Code(output.ANSIGreen), event.EvalReason, o.color.Code(output.ANSIReset))
	} else {
		fmt.Fprintf(o.stderr, "%s[eval] fail (round %d) — %s%s\n",
			o.color.Code(output.ANSIYellow), event.EvalRound+1, event.EvalReason, o.color.Code(output.ANSIReset))
	}
}

func (o *AgentOutput) goalEvalError(event agent.Event) {
	if o.Quiet || o.stderr == nil {
		return
	}
	fmt.Fprintf(o.stderr, "%s[eval] error (round %d) — evaluator LLM call failed, retrying...%s\n",
		o.color.Code(output.ANSIYellow), event.EvalRound+1, o.color.Code(output.ANSIReset))
}

// ---------------------------------------------------------------------------
// Hyperlink helper
// ---------------------------------------------------------------------------

// hyperlinkSummary wraps a path-bearing tool's summary in an OSC 8 file:// link
// so a local user can click straight to the file. No-op outside interactive TTY
// sessions (tests and forwarded PTYs get the plain summary).
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

// ---------------------------------------------------------------------------
// User intent rendering
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Tool summary tracking
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Chat message summarisation helpers
// ---------------------------------------------------------------------------

func lastMessageSummary(messages []agent.ChatMessage) (role string, contentLen int, toolCalls int, reasoningLen int, preview string) {
	if len(messages) == 0 {
		return "", 0, 0, 0, ""
	}
	return summarizeChatMessage(messages[len(messages)-1])
}

func summarizeChatMessage(msg agent.ChatMessage) (role string, contentLen int, toolCalls int, reasoningLen int, preview string) {
	role = msg.Role
	if msg.Content != nil {
		contentLen = len(*msg.Content)
		preview = compactAgentLine(*msg.Content, agentDebugPreviewLimit)
	}
	if msg.ReasoningContent != nil {
		reasoningLen = len(*msg.ReasoningContent)
	}
	toolCalls = len(msg.ToolCalls)
	return role, contentLen, toolCalls, reasoningLen, preview
}

func sameRenderedAgentText(left, right string) bool {
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}

// ---------------------------------------------------------------------------
// Markdown rendering
// ---------------------------------------------------------------------------

var (
	agentMarkdownRenderer     *glamour.TermRenderer
	agentMarkdownRendererErr  error
	agentMarkdownRendererOnce sync.Once
)

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
	rendered = strings.TrimSpace(trimRenderedMarkdownLineEnds(rendered))
	if rendered == "" {
		return content
	}
	return rendered
}

func getAgentMarkdownRenderer() (*glamour.TermRenderer, error) {
	agentMarkdownRendererOnce.Do(func() {
		opts := []glamour.TermRendererOption{
			glamour.WithAutoStyle(),
			// Auto-detect the richest profile the terminal advertises (truecolor
			// -> 256 -> ANSI) instead of pinning 16-color ANSI, so markdown answers
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

// trimRenderedMarkdownLineEnds strips trailing visible whitespace from each
// line while preserving ANSI escape sequences that follow the last visible
// character. This avoids the "invisible trailing spaces" artefact from glamour
// padding without clobbering reset sequences that close styled spans.
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

// ---------------------------------------------------------------------------
// Tool argument summarisation
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// JSON / string helpers
// ---------------------------------------------------------------------------

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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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

package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/app"
	"github.com/chainreactors/aiscan/pkg/eventbus"
	outputpkg "github.com/chainreactors/aiscan/pkg/output"
	"github.com/chainreactors/aiscan/pkg/repl"
	ioaclient "github.com/chainreactors/ioa/client"
	"github.com/reeflective/console"
	"github.com/reeflective/readline/inputrc"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const agentPromptCommandName = "__prompt"
const agentConsoleInterruptCommandName = "aiscan-interrupt"
const agentConsoleEscapeSequenceWait = 10 * time.Millisecond

var errAgentConsoleExit = errors.New("agent console exit")

type AgentConsole struct {
	ctx         context.Context
	option      *cfg.Option
	application *app.App
	agent       *agent.Agent
	console     *console.Console
	menu        *console.Menu
	output      *AgentOutput
	controller  *interactiveRunController
	bus         *eventbus.Bus[agent.Event]
	// readlineActive is true only while the foreground goroutine is blocked in
	// Readline. Async agent output can then refresh the prompt without changing
	// the input buffer or creating a duplicate prompt between reads.
	readlineActive atomic.Bool
	// startupNotice, when set, is rendered once below the welcome banner (e.g.
	// an IOA-unavailable degradation warning). Set by the caller before Start.
	startupNotice string
	evalCriteria  string
}

func NewAgentConsole(ctx context.Context, option *cfg.Option, application *app.App, session *agent.Agent, output *AgentOutput, bus ...*eventbus.Bus[agent.Event]) *AgentConsole {
	c := console.New("aiscan")
	c.NewlineAfter = true
	configureAgentReadline(c)
	if output == nil {
		output = NewAgentOutput(option)
	}

	menu := c.NewMenu("agent")
	menu.Prompt().Primary = func() string {
		return agentPromptString(output)
	}
	menu.AddHistorySourceFile("history", agentConsoleHistoryPath())
	menu.ErrorHandler = func(err error) error {
		if errors.Is(err, errAgentConsoleExit) {
			return errAgentConsoleExit
		}
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		return nil
	}

	repl := &AgentConsole{
		ctx:         ctx,
		option:      option,
		application: application,
		agent:       session,
		console:     c,
		menu:        menu,
		output:      output,
	}
	if len(bus) > 0 && bus[0] != nil {
		repl.bus = bus[0]
	}
	if option != nil && option.EvalCriteria != "" {
		repl.evalCriteria = option.EvalCriteria
	}
	repl.controller = newInteractiveRunController(ctx, repl.agent, output)
	repl.controller.SetOnFinish(repl.refreshPromptAfterAsyncRun)
	repl.configureInterruptKey()
	menu.SetCommands(repl.rootCommand)
	menu.Command = repl.rootCommand()
	c.SwitchMenu("agent")
	return repl
}

func configureAgentReadline(c *console.Console) {
	if c == nil {
		return
	}
	cfg := c.Shell().Config
	// Keep readline history and Tab completion, but avoid expensive/noisy
	// as-you-type panels that recalculate and redraw on every keystroke.
	_ = cfg.Set("autocomplete", false)
	_ = cfg.Set("usage-hint-always", false)
	_ = cfg.Set("history-autosuggest", false)
	_ = cfg.Set("show-all-if-ambiguous", false)
	_ = cfg.Set("show-all-if-unmodified", false)
	_ = cfg.Set("menu-complete-display-prefix", false)
	_ = cfg.Set("page-completions", false)
	_ = cfg.Set("completion-query-items", 1000)
	_ = cfg.Set("bell-style", "none")
	_ = cfg.Set("enable-bracketed-paste", false)
}

func (r *AgentConsole) configureInterruptKey() {
	if r == nil || r.console == nil || r.console.Shell() == nil {
		return
	}
	shell := r.console.Shell()
	shell.Keymap.Register(map[string]func(){
		agentConsoleInterruptCommandName: func() {
			r.handleEscapeInterruptKey()
		},
	})
	escape := inputrc.Unescape(`\e`)
	for _, keymap := range []string{"emacs", "emacs-standard"} {
		_ = shell.Config.Bind(keymap, escape, agentConsoleInterruptCommandName, false)
	}
}

func (r *AgentConsole) handleEscapeInterruptKey() {
	if r == nil || r.console == nil || r.console.Shell() == nil {
		return
	}
	shell := r.console.Shell()
	pending := string(shell.Keys.Read())
	if pending == "" {
		pending = readPendingTerminalBytes(agentConsoleEscapeSequenceWait)
	}
	keymap := string(shell.Keymap.Main())
	if feed, ok := agentConsoleEscapeSequenceFeed(shell.Config.Binds[keymap], pending); ok {
		shell.Keys.Feed(true, []rune(feed)...)
		return
	}
	if pending != "" {
		shell.Keys.Feed(true, []rune(pending)...)
		return
	}
	shell.Display.AcceptLine()
	shell.History.Accept(false, false, errors.New(os.Interrupt.String()))
}

func agentConsoleEscapeSequenceFeed(binds map[string]inputrc.Bind, pending string) (string, bool) {
	if len(binds) == 0 || pending == "" {
		return "", false
	}
	sequence := inputrc.Unescape(`\e`) + pending
	matches := make([]string, 0, 4)
	for seq := range binds {
		readlineSeq := agentConsoleReadlineSequence(seq)
		if len(readlineSeq) <= 1 || !strings.HasPrefix(readlineSeq, inputrc.Unescape(`\e`)) {
			continue
		}
		if strings.HasPrefix(sequence, readlineSeq) {
			matches = append(matches, seq)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		left := agentConsoleReadlineSequence(matches[i])
		right := agentConsoleReadlineSequence(matches[j])
		if len(left) == len(right) {
			return left < right
		}
		return len(left) > len(right)
	})
	for _, seq := range matches {
		bind := binds[seq]
		replacement, ok := agentConsoleEquivalentNonEscapeBind(binds, bind)
		if !ok {
			continue
		}
		return replacement + sequence[len(agentConsoleReadlineSequence(seq)):], true
	}
	return "", false
}

func agentConsoleEquivalentNonEscapeBind(binds map[string]inputrc.Bind, target inputrc.Bind) (string, bool) {
	if target.Action == "" {
		return "", false
	}
	candidates := make([]string, 0, 4)
	for seq, bind := range binds {
		if bind.Action != target.Action || bind.Macro != target.Macro || strings.HasPrefix(agentConsoleReadlineSequence(seq), inputrc.Unescape(`\e`)) {
			continue
		}
		candidates = append(candidates, seq)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if len(candidates[i]) == len(candidates[j]) {
			return candidates[i] < candidates[j]
		}
		return len(candidates[i]) < len(candidates[j])
	})
	if len(candidates) == 0 {
		return agentConsoleFallbackNonEscapeBind(target)
	}
	return candidates[0], true
}

func agentConsoleFallbackNonEscapeBind(target inputrc.Bind) (string, bool) {
	switch target.Action {
	case "previous-history", "history-search-backward":
		return inputrc.Unescape(`\C-p`), true
	case "next-history", "history-search-forward":
		return inputrc.Unescape(`\C-n`), true
	case "backward-char", "vi-backward-char":
		return inputrc.Unescape(`\C-b`), true
	case "forward-char", "vi-forward-char":
		return inputrc.Unescape(`\C-f`), true
	case "beginning-of-line":
		return inputrc.Unescape(`\C-a`), true
	case "end-of-line":
		return inputrc.Unescape(`\C-e`), true
	default:
		return "", false
	}
}

func agentConsoleReadlineSequence(seq string) string {
	if seq == "" {
		return ""
	}
	converted := make([]rune, 0, len(seq))
	for _, r := range seq {
		if inputrc.IsMeta(r) {
			converted = append(converted, inputrc.Esc, inputrc.Demeta(r))
			continue
		}
		converted = append(converted, r)
	}
	return string(converted)
}

func (r *AgentConsole) Start() error {
	r.renderBanner()
	defer r.stopController()
	if r.fastInputEnabled() {
		return r.startFastInput()
	}
	return r.startReadline()
}

func (r *AgentConsole) startFastInput() error {
	reader := bufio.NewReader(os.Stdin)
	for {
		if err := r.ctx.Err(); err != nil {
			return nil
		}

		fmt.Fprint(os.Stderr, r.promptString())
		line, err := readFastInputLine(r.ctx, reader)
		if err != nil && !errors.Is(err, io.EOF) {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stdout)
				return nil
			}
			fmt.Fprintf(os.Stderr, "error: read interactive input: %s\n", err)
			continue
		}
		if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
			fmt.Fprintln(os.Stdout)
			return nil
		}

		done, execErr := r.handleInputLine(line)
		if execErr != nil {
			if errors.Is(execErr, context.Canceled) && r.ctx.Err() != nil {
				fmt.Fprintln(os.Stdout)
				return nil
			}
			fmt.Fprintf(os.Stderr, "error: %s\n", execErr)
		}
		if done || errors.Is(err, io.EOF) {
			return nil
		}
	}
}

type fastInputResult struct {
	line string
	err  error
}

// readFastInputLine reads one line from reader, cancellable via ctx.
// NOTE: on context cancellation the blocked ReadString goroutine leaks
// until stdin is closed — Go blocking I/O has no cancellation mechanism.
func readFastInputLine(ctx context.Context, reader *bufio.Reader) (string, error) {
	resultCh := make(chan fastInputResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		resultCh <- fastInputResult{line: line, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-resultCh:
		return result.line, result.err
	}
}

func (r *AgentConsole) startReadline() error {
	for {
		if err := r.ctx.Err(); err != nil {
			return nil
		}

		r.readlineActive.Store(true)
		line, err := r.console.Shell().Readline()
		r.readlineActive.Store(false)
		if err != nil {
			switch {
			case errors.Is(err, io.EOF):
				fmt.Fprintln(os.Stdout)
				return nil
			case err.Error() == os.Interrupt.String():
				if r.stopCurrentRun() {
					continue
				}
				fmt.Fprintln(os.Stdout)
				return nil
			default:
				fmt.Fprintf(os.Stderr, "error: read interactive input: %s\n", err)
				continue
			}
		}

		done, err := r.handleInputLine(line)
		if err != nil {
			if errors.Is(err, context.Canceled) && r.ctx.Err() != nil {
				fmt.Fprintln(os.Stdout)
				return nil
			}
			fmt.Fprintf(os.Stderr, "error: %s\n", err)
		}
		if done {
			return nil
		}
	}
}

func (r *AgentConsole) handleInputLine(line string) (bool, error) {
	args, err := AgentConsoleArgsForLine(line)
	if err != nil {
		return false, err
	}
	if len(args) == 0 {
		return false, nil
	}

	if err := r.executeArgs(r.ctx, args); err != nil {
		if errors.Is(err, errAgentConsoleExit) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func (r *AgentConsole) promptString() string {
	return agentPromptString(r.ensureOutput())
}

func agentPromptString(output *AgentOutput) string {
	if output != nil && output.color.Enabled {
		return output.color.Code(outputpkg.ANSIBold+outputpkg.ANSICyan) + "aiscan" +
			output.color.Code(outputpkg.ANSIReset) + " " + output.color.Dim("❯") + " "
	}
	return "aiscan> "
}

func (r *AgentConsole) fastInputEnabled() bool {
	return fastInputEnabledForMode(os.Getenv("AISCAN_REPL"), term.IsTerminal(int(os.Stdin.Fd())))
}

func fastInputEnabledForMode(mode string, _ bool) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "rich", "readline", "console":
		return false
	case "fast", "plain", "simple":
		return true
	}
	return false
}

func (r *AgentConsole) executeArgs(ctx context.Context, args []string) error {
	root := r.rootCommand()
	root.SetArgs(args)
	root.SetContext(ctx)
	return root.Execute()
}

// renderBanner prints a compact welcome block to stderr: title/version,
// resolved model, the session mode, and a short next-step hint. It uses fixed
// ANSI tokens so redirected or recorded sessions do not receive terminal
// background probes. stderr-TTY-only and skipped in quiet mode so redirected
// logs stay clean. Printed once into the scrollback (PTY-forward safe).
func (r *AgentConsole) renderBanner() {
	if r.output == nil || r.output.Quiet || r.output.stderr == nil {
		return
	}
	if !writerIsTerminal(r.output.stderr) {
		return
	}
	fmt.Fprint(r.output.stderr, r.bannerOutput())
}

func writerIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func (r *AgentConsole) bannerOutput() string {
	colorEnabled := r.output != nil && r.output.color.Enabled
	provider, model := r.providerModel()
	modelText := "not configured - run `aiscan --init`"
	modelStyle := ansiWarn
	switch {
	case provider != "" && model != "":
		modelText = provider + " / " + model
		modelStyle = ansiAccent
	case provider != "":
		modelText = provider
		modelStyle = ansiAccent
	}

	width := r.bannerWidth()
	header := ansiTitle("aiscan", colorEnabled) + " " + ansiDim("v"+cfg.Version, colorEnabled)

	var lines []string
	lines = append(lines, header)
	lines = append(lines, bannerKV("model", modelStyle(modelText, colorEnabled), colorEnabled))
	lines = append(lines, bannerKV("mode", ansiDim(r.sessionSummary(), colorEnabled), colorEnabled))
	lines = append(lines, bannerKV("help", renderInlineCommands([]string{"/help", "/status", "/exit"}, colorEnabled), colorEnabled))

	box := renderFixedBox(strings.Join(lines, "\n"), width, colorEnabled)
	intent := ansiDim("输入目标或任务即可；例如：扫描 192.168.1.10 的 Web 风险", colorEnabled)

	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, box)
	fmt.Fprintln(&b, "  "+intent)
	if notice := strings.TrimSpace(r.startupNotice); notice != "" {
		fmt.Fprintln(&b, "  "+ansiWarn("⚠ "+notice, colorEnabled))
	}
	fmt.Fprintln(&b)
	return b.String()
}

func (r *AgentConsole) bannerWidth() int {
	const (
		minWidth     = 44
		defaultWidth = 64
		maxWidth     = 78
	)
	width := defaultWidth
	if r != nil && r.output != nil && r.output.stderr != nil {
		if columns := writerTerminalWidth(r.output.stderr); columns > 0 {
			width = columns - 4
		}
	}
	if width < minWidth {
		return minWidth
	}
	if width > maxWidth {
		return maxWidth
	}
	return width
}

func writerTerminalWidth(w io.Writer) int {
	file, ok := w.(*os.File)
	if !ok {
		return 0
	}
	width, _, err := term.GetSize(int(file.Fd()))
	if err != nil || width <= 0 {
		return 0
	}
	return width
}

func bannerKV(label, value string, colorEnabled bool) string {
	return ansiDim(fmt.Sprintf("%-9s", label), colorEnabled) + value
}

func renderFixedBox(body string, width int, colorEnabled bool) string {
	const minInnerWidth = 16
	innerWidth := width - 4
	if innerWidth < minInnerWidth {
		innerWidth = minInnerWidth
	}
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if n := visibleRuneLen(line); n > innerWidth {
			innerWidth = n
		}
	}

	border := func(s string) string { return ansiDim(s, colorEnabled) }
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", border("╭"+strings.Repeat("─", innerWidth+2)+"╮"))
	for _, line := range lines {
		padding := innerWidth - visibleRuneLen(line)
		if padding < 0 {
			padding = 0
		}
		fmt.Fprintf(&b, "%s %s%s %s\n",
			border("│"),
			line,
			strings.Repeat(" ", padding),
			border("│"))
	}
	fmt.Fprint(&b, border("╰"+strings.Repeat("─", innerWidth+2)+"╯"))
	return b.String()
}

func visibleRuneLen(s string) int {
	return len([]rune(outputpkg.StripANSI(s)))
}

func ansiWrap(s, code string, enabled bool) string {
	if !enabled {
		return s
	}
	return code + s + outputpkg.ANSIReset
}

func ansiTitle(s string, enabled bool) string {
	return ansiWrap(s, outputpkg.ANSIBold+outputpkg.ANSICyan, enabled)
}

func ansiAccent(s string, enabled bool) string {
	return ansiWrap(s, outputpkg.ANSICyan, enabled)
}

func ansiOK(s string, enabled bool) string {
	return ansiWrap(s, outputpkg.ANSIGreen, enabled)
}

func ansiWarn(s string, enabled bool) string {
	return ansiWrap(s, outputpkg.ANSIYellow, enabled)
}

func ansiDim(s string, enabled bool) string {
	return ansiWrap(s, "\033[90m", enabled)
}

func renderInlineCommands(commands []string, colorEnabled bool) string {
	parts := make([]string, 0, len(commands))
	for _, command := range commands {
		parts = append(parts, ansiAccent(command, colorEnabled))
	}
	return strings.Join(parts, ansiDim("  ", colorEnabled))
}

func (r *AgentConsole) sessionSummary() string {
	var parts []string
	if r != nil && r.output != nil {
		switch r.output.mode {
		case ModeForwarded:
			parts = append(parts, "forwarded")
		default:
			parts = append(parts, "pty")
		}
		if r.output.stream {
			parts = append(parts, "stream")
		} else if r.output.markdown {
			parts = append(parts, "pretty")
		} else {
			parts = append(parts, "plain")
		}
	}
	if r != nil && r.option != nil {
		if space := strings.TrimSpace(r.option.Space); space != "" {
			parts = append(parts, "space "+space)
		}
	}
	if len(parts) == 0 {
		return "pty"
	}
	return strings.Join(parts, " · ")
}

func (r *AgentConsole) providerModel() (string, string) {
	if r.application == nil {
		return "", ""
	}
	pc := r.application.ProviderConfig
	return pc.Provider, pc.Model
}

// skillSlashNames lists user-facing skills as slash commands, capped so the
// banner stays tidy when many skills are loaded.
func (r *AgentConsole) skillSlashNames() string {
	if r.application == nil || r.application.Skills == nil {
		return ""
	}
	names := make([]string, 0, len(r.application.Skills.Skills))
	for _, s := range r.application.Skills.Skills {
		if strings.TrimSpace(s.Name) == "" || s.Internal {
			continue
		}
		names = append(names, "/"+s.Name)
	}
	if len(names) == 0 {
		return ""
	}
	const max = 6
	if len(names) > max {
		return strings.Join(names[:max], "  ") + fmt.Sprintf("  +%d", len(names)-max)
	}
	return strings.Join(names, "  ")
}

func (r *AgentConsole) replSession() *repl.Session {
	s := &repl.Session{
		Ctx:          r.ctx,
		Option:       r.option,
		App:          r.application,
		Agent:        r.agent,
		Controller:   r.ensureController(),
		EvalCriteria: r.evalCriteria,
	}
	s.OnEvalChange = func(criteria string) {
		r.evalCriteria = criteria
		r.syncEvalToController()
	}
	return s
}

func (r *AgentConsole) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use: "agent", Short: "aiscan interactive agent",
		SilenceUsage: true, SilenceErrors: true,
	}
	root.CompletionOptions.HiddenDefaultCmd = true
	root.SetHelpCommand(&cobra.Command{Use: "help", Hidden: true})
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)

	root.AddCommand(&cobra.Command{
		Use: agentPromptCommandName, Hidden: true, Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return repl.RunPrompt(r.replSession(), "prompt", args[0])
		},
	})

	for _, cmd := range repl.BuiltinCommands() {
		root.AddCommand(r.wrapCommand(cmd))
	}
	for _, cmd := range repl.SkillCommands(r.replSession()) {
		root.AddCommand(r.wrapCommand(cmd))
	}
	root.AddCommand(r.providerCommand())
	root.AddCommand(r.ioaCommands()...)
	return root
}

func (r *AgentConsole) wrapCommand(cmd repl.Command) *cobra.Command {
	cc := &cobra.Command{
		Use:   cmd.Name,
		Short: cmd.Description,
	}
	if len(cmd.Aliases) > 0 {
		cc.Aliases = cmd.Aliases
	}
	if cmd.Hidden {
		cc.Hidden = true
	}
	switch cmd.Args {
	case repl.ArgsNone:
		cc.Args = cobra.NoArgs
	case repl.ArgsExact1:
		cc.Args = cobra.ExactArgs(1)
		cc.DisableFlagParsing = true
	case repl.ArgsOptional:
		cc.DisableFlagParsing = true
	}

	switch cmd.Name {
	case "/help":
		cc.RunE = func(_ *cobra.Command, _ []string) error {
			fmt.Fprint(os.Stdout, r.renderHelp())
			return nil
		}
	case "/status":
		cc.Run = func(_ *cobra.Command, _ []string) {
			fmt.Fprint(os.Stdout, r.renderStatus())
		}
	case "/stop":
		cc.Run = func(_ *cobra.Command, _ []string) {
			if !r.stopCurrentRun() {
				fmt.Fprintln(os.Stderr, "No running task.")
			}
		}
	case "/reset":
		run := cmd.Run
		cc.RunE = func(c *cobra.Command, _ []string) error {
			if err := run(c.Context(), r.replSession(), nil); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return nil
			}
			fmt.Fprintln(os.Stdout, "Context reset.")
			return nil
		}
	case "/exit":
		cc.RunE = func(_ *cobra.Command, _ []string) error { return errAgentConsoleExit }
	default:
		if cmd.Run == nil {
			break
		}
		run := cmd.Run
		cc.RunE = func(c *cobra.Command, args []string) error {
			return run(c.Context(), r.replSession(), args)
		}
	}
	return cc
}

// ---------------------------------------------------------------------------
// Rendering: help, status, panels
// ---------------------------------------------------------------------------

func (r *AgentConsole) renderHelp() string {
	colorEnabled := r.output != nil && r.output.color.Enabled
	cmds := repl.BuiltinCommands()
	rows := make([]helpRow, 0, len(cmds)+4)
	for _, c := range cmds {
		rows = append(rows, helpRow{Command: c.Name, Detail: c.Description})
	}
	rows = append(rows, helpRow{})
	rows = append(rows, helpRow{Command: "普通文本", Detail: "直接发送自然语言任务"})
	rows = append(rows, helpRow{Command: "/<skill> 任务", Detail: "用指定 skill 处理后面的任务"})
	rows = append(rows, helpRow{Command: "/provider", Detail: "查看/管理 LLM provider 链"})
	rows = append(rows, helpRow{Command: "/spaces /nodes", Detail: "配置 IOA 时查看协作状态"})
	return r.renderPanel("commands", renderHelpRows(rows, colorEnabled), colorEnabled)
}

func (r *AgentConsole) renderStatus() string {
	colorEnabled := r.output != nil && r.output.color.Enabled
	info := repl.CollectStatus(r.replSession(), r.sessionSummary(), agentConsoleHistoryPath())
	rows := []helpRow{
		{Command: "model", Detail: info.Provider + " / " + info.Model},
		{Command: "render", Detail: info.Mode},
		{Command: "task", Detail: info.Task},
		{Command: "ioa", Detail: info.IOA},
		{Command: "history", Detail: info.History},
	}
	if info.Skills != "" {
		rows = append(rows, helpRow{Command: "skills", Detail: info.Skills})
	}
	return r.renderPanel("status", renderHelpRows(rows, colorEnabled), colorEnabled)
}

type helpRow struct {
	Command string
	Detail  string
}

const helpRowCommandWidth = 18

func renderHelpRows(rows []helpRow, colorEnabled bool) string {
	var b strings.Builder
	for _, row := range rows {
		if row.Command == "" && row.Detail == "" {
			b.WriteByte('\n')
			continue
		}
		command := ansiAccent(fmt.Sprintf("%-*s", helpRowCommandWidth, row.Command), colorEnabled)
		detail := ansiDim(row.Detail, colorEnabled)
		fmt.Fprintf(&b, "%s%s\n", command, detail)
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r *AgentConsole) renderPanel(title, body string, colorEnabled bool) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "aiscan"
	}
	header := ansiTitle(title, colorEnabled)
	return "\n" + renderFixedBox(header+"\n"+body, r.bannerWidth(), colorEnabled) + "\n\n"
}

func (r *AgentConsole) ensureOutput() *AgentOutput {
	if r.output == nil {
		r.output = NewAgentOutput(r.option)
	}
	return r.output
}

func (r *AgentConsole) ensureController() *interactiveRunController {
	if r.controller == nil {
		r.controller = newInteractiveRunController(r.ctx, r.agent, r.ensureOutput())
		r.controller.SetOnFinish(r.refreshPromptAfterAsyncRun)
	}
	r.syncEvalToController()
	return r.controller
}

func (r *AgentConsole) syncEvalToController() {
	if r.controller == nil {
		return
	}
	if r.evalCriteria == "" {
		r.controller.Eval = nil
		return
	}
	model := ""
	if r.option != nil {
		model = r.option.EvalModel
	}
	if model == "" && r.application != nil {
		model = r.application.ProviderConfig.Model
	}
	var prov agent.Provider
	if r.application != nil {
		prov = r.application.Provider
	}
	r.controller.Eval = &EvalSettings{
		Criteria: r.evalCriteria,
		Model:    model,
		Provider: prov,
		Bus:      r.bus,
	}
}

func (r *AgentConsole) refreshPromptAfterAsyncRun() {
	if r == nil || !r.readlineActive.Load() {
		return
	}
	if r.ctx != nil && r.ctx.Err() != nil {
		return
	}
	if r.output != nil && r.output.mode != ModeInteractive {
		return
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return
	}
	if r.console == nil || r.console.Shell() == nil || r.console.Shell().Display == nil {
		return
	}
	r.console.Shell().Display.Refresh()
}

func (r *AgentConsole) stopCurrentRun() bool {
	if r.controller == nil || !r.controller.Stop() {
		return false
	}
	r.ensureOutput().Stopping()
	return true
}

func (r *AgentConsole) stopController() {
	if r.controller != nil {
		r.controller.StopAndWait()
	}
}

func (r *AgentConsole) ioaClient() (*ioaclient.Client, error) {
	ioaURL := r.option.IOAURL
	if ioaURL == "" {
		return nil, fmt.Errorf("IOA not configured: use --ioa-url")
	}
	return ioaclient.NewClient(ioaURL, "")
}

func (r *AgentConsole) providerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "/provider",
		Short: "查看/管理 LLM provider 链",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Fprint(os.Stdout, r.renderProviders())
				return nil
			}
			switch args[0] {
			case "list":
				fmt.Fprint(os.Stdout, r.renderProviders())
			default:
				fmt.Fprintf(os.Stderr, "unknown subcommand: %s (use: list)\n", args[0])
			}
			return nil
		},
	}
	cmd.DisableFlagParsing = true
	return cmd
}

func (r *AgentConsole) renderProviders() string {
	colorEnabled := r.output != nil && r.output.color.Enabled
	info := repl.CollectStatus(r.replSession(), "", "")
	if len(info.Providers) == 0 {
		return "\n  No providers configured.\n\n"
	}
	var rows []helpRow
	for i, p := range info.Providers {
		status := "○ standby"
		if p.Active {
			status = "● active"
		}
		label := fmt.Sprintf("#%d  %s", i+1, p.Name)
		detail := fmt.Sprintf("%-24s %s", p.Model, status)
		rows = append(rows, helpRow{Command: label, Detail: detail})
	}
	return r.renderPanel("providers", renderHelpRows(rows, colorEnabled), colorEnabled)
}

func (r *AgentConsole) ioaCommands() []*cobra.Command {
	return []*cobra.Command{
		{
			Use:   "/spaces",
			Short: "List all IOA spaces",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				client, err := r.ioaClient()
				if err != nil {
					return err
				}
				return RunIOASpaces(cmd.Context(), client, r.option)
			},
		},
		{
			Use:   "/messages <space>",
			Short: "List start messages in a space",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := r.ioaClient()
				if err != nil {
					return err
				}
				return RunIOAMessages(cmd.Context(), client, r.option, cfg.IOAClientArgs{Space: args[0]})
			},
		},
		{
			Use:   "/context <space> <message-id>",
			Short: "View message thread/context",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := r.ioaClient()
				if err != nil {
					return err
				}
				return RunIOAContext(cmd.Context(), client, r.option, cfg.IOAClientArgs{Space: args[0], MessageID: args[1]})
			},
		},
		{
			Use:   "/nodes [space]",
			Short: "List nodes (optionally scoped to a space)",
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				client, err := r.ioaClient()
				if err != nil {
					return err
				}
				var a cfg.IOAClientArgs
				if len(args) > 0 {
					a.Space = args[0]
				}
				return RunIOANodes(cmd.Context(), client, r.option, a)
			},
		},
	}
}

var ioaConsoleCommands = map[string]bool{
	"/spaces": true, "/messages": true, "/context": true, "/nodes": true,
}

func AgentConsoleArgsForLine(line string) ([]string, error) {
	text := strings.TrimSpace(line)
	if text == "" {
		return nil, nil
	}
	if text == "/" {
		return []string{"/help"}, nil
	}
	if !strings.HasPrefix(text, "/") || strings.HasPrefix(text, "/skill:") {
		return []string{agentPromptCommandName, text}, nil
	}
	command, rest, ok := strings.Cut(text, " ")
	if !ok {
		return []string{text}, nil
	}
	if ioaConsoleCommands[command] {
		result := []string{command}
		result = append(result, strings.Fields(rest)...)
		return result, nil
	}
	return []string{command, strings.TrimSpace(rest)}, nil
}

func agentConsoleHistoryPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(configDir) == "" {
		return ".aiscan_agent_history"
	}
	dir := filepath.Join(configDir, "aiscan")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ".aiscan_agent_history"
	}
	return filepath.Join(dir, "agent_history")
}

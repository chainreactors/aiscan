package runner

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	tmuxpkg "github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/app"
	outputpkg "github.com/chainreactors/aiscan/pkg/output"
	skillpkg "github.com/chainreactors/aiscan/skills"
	ioaclient "github.com/chainreactors/ioa/client"
	"github.com/reeflective/console"
	"github.com/reeflective/readline"
	"github.com/reeflective/readline/inputrc"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	agentPromptCommandName                = "__prompt"
	agentSlashMenuCompleteCommand         = "aiscan-slash-menu-complete"
	agentSlashMenuCompleteBackwardCommand = "aiscan-slash-menu-complete-backward"
)

var errAgentConsoleExit = errors.New("agent console exit")

type AgentConsole struct {
	ctx         context.Context
	option      *cfg.Option
	application *app.App
	session     *agent.Agent
	console     *console.Console
	menu        *console.Menu
	output      *AgentOutput
	// startupNotice, when set, is rendered once below the welcome banner (e.g.
	// an IOA-unavailable degradation warning). Set by the caller before Start.
	startupNotice string
	debugEvents   *eventsFileSubscriber
	debugUnsub    func()
	debugPath     string
}

func NewAgentConsole(ctx context.Context, option *cfg.Option, application *app.App, session *agent.Agent, output *AgentOutput) *AgentConsole {
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
	menu.Prompt().Secondary = func() string {
		return agentPromptSecondary(output)
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
		session:     session,
		console:     c,
		menu:        menu,
		output:      output,
	}
	menu.SetCommands(repl.rootCommand)
	menu.Command = repl.rootCommand()
	c.SwitchMenu("agent")
	return repl
}

func configureAgentReadline(c *console.Console) {
	if c == nil {
		return
	}
	shell := c.Shell()
	readCfg := shell.Config
	// Keep readline history, Tab completion, and the lightweight slash-command
	// preview. Avoid only the history ghost text and paged completion prompts
	// that interfere with streamed agent output.
	_ = readCfg.Set("autocomplete", true)
	_ = readCfg.Set("usage-hint-always", true)
	_ = readCfg.Set("history-autosuggest", false)
	_ = readCfg.Set("show-all-if-ambiguous", false)
	_ = readCfg.Set("show-all-if-unmodified", false)
	_ = readCfg.Set("menu-complete-display-prefix", false)
	_ = readCfg.Set("page-completions", false)
	_ = readCfg.Set("completion-query-items", 1000)
	_ = readCfg.Set("bell-style", "none")
	_ = readCfg.Set("enable-bracketed-paste", true)
	configureAgentSlashCommandNavigation(shell)
}

func configureAgentSlashCommandNavigation(shell *readline.Shell) {
	if shell == nil || shell.Keymap == nil || shell.Config == nil {
		return
	}

	commands := shell.Keymap.Commands()
	menuNext := commands["menu-complete"]
	menuPrevious := commands["menu-complete-backward"]
	historyDown := commands["down-line-or-history"]
	historyUp := commands["up-line-or-history"]

	shell.Keymap.Register(map[string]func(){
		agentSlashMenuCompleteCommand: func() {
			runAgentSlashSelectionCommand(shell, menuNext, historyDown)
		},
		agentSlashMenuCompleteBackwardCommand: func() {
			runAgentSlashSelectionCommand(shell, menuPrevious, historyUp)
		},
	})

	bindAgentReadlineKeys(shell.Config, agentSlashMenuCompleteBackwardCommand,
		`\e[A`, `\M-[A`, `\M-OA`)
	bindAgentReadlineKeys(shell.Config, agentSlashMenuCompleteCommand,
		`\e[B`, `\M-[B`, `\M-OB`)
}

func bindAgentReadlineKeys(readCfg *inputrc.Config, action string, sequences ...string) {
	if readCfg == nil {
		return
	}
	for _, keymapName := range []string{"emacs", "emacs-standard", "vi-insert"} {
		for _, sequence := range sequences {
			_ = readCfg.Bind(keymapName, inputrc.Unescape(sequence), action, false)
		}
	}
}

func runAgentSlashSelectionCommand(shell *readline.Shell, slashCommand, fallback func()) {
	if agentReadlineCursorInSlashCommand(shell) {
		if slashCommand != nil {
			slashCommand()
		}
		return
	}
	if fallback != nil {
		fallback()
	}
}

func agentReadlineCursorInSlashCommand(shell *readline.Shell) bool {
	if shell == nil || shell.Line() == nil || shell.Cursor() == nil {
		return false
	}

	line := []rune(*shell.Line())
	cursor := shell.Cursor().Pos()
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(line) {
		cursor = len(line)
	}

	start := 0
	for start < len(line) && unicode.IsSpace(line[start]) {
		start++
	}
	if start >= len(line) || line[start] != '/' || cursor <= start {
		return false
	}
	for i := start; i < cursor && i < len(line); i++ {
		if unicode.IsSpace(line[i]) {
			return false
		}
	}
	return true
}

func (r *AgentConsole) Start() error {
	defer r.closeDebugEvents()
	r.renderBanner()
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

		line, err := r.console.Shell().Readline()
		if err != nil {
			switch {
			case errors.Is(err, io.EOF):
				fmt.Fprintln(os.Stdout)
				return nil
			case err.Error() == os.Interrupt.String():
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
	if output == nil || !output.color.Enabled {
		return "aiscan> "
	}

	reset := output.color.Code(outputpkg.ANSIReset)
	title := output.color.Code(outputpkg.ANSIBold+outputpkg.ANSICyan) + "aiscan" + reset
	caret := output.color.Code(outputpkg.ANSIBold+outputpkg.ANSIGreen) + "❯" + reset
	return title + " " + caret + " "
}

func agentPromptSecondary(output *AgentOutput) string {
	if output == nil || !output.color.Enabled {
		return "> "
	}
	return output.color.Dim("... ")
}

func (r *AgentConsole) fastInputEnabled() bool {
	return fastInputEnabledForMode(os.Getenv("AISCAN_REPL"), term.IsTerminal(int(os.Stdin.Fd())))
}

func fastInputEnabledForMode(mode string, stdinIsTerminal bool) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "rich", "readline", "console":
		return false
	case "fast", "plain", "simple":
		return true
	}
	return !stdinIsTerminal
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
	lines = append(lines, bannerKV("mode", ansiValue(r.sessionSummary(), colorEnabled), colorEnabled))
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
	return bannerTag(fmt.Sprintf("%-9s", label), colorEnabled) + value
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

	border := func(s string) string { return ansiBorder(s, colorEnabled) }
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
	return terminalDisplayWidth(s)
}

func ansiTitle(s string, enabled bool) string {
	if !enabled {
		return s
	}
	return outputpkg.ANSIBold + outputpkg.ANSICyan + s + outputpkg.ANSIReset
}

func ansiAccent(s string, enabled bool) string {
	if !enabled {
		return s
	}
	return outputpkg.ANSICyan + s + outputpkg.ANSIReset
}

func ansiValue(s string, enabled bool) string {
	return s
}

func ansiMaybeDash(s string, enabled bool) string {
	if strings.TrimSpace(s) == "-" {
		return ansiDim(s, enabled)
	}
	return ansiValue(s, enabled)
}

func ansiSwitch(enabledValue bool, enabled bool) string {
	return ansiBoolLabel(enabledValue, "on", "off", enabled)
}

func ansiBoolLabel(value bool, trueText, falseText string, enabled bool) string {
	if value {
		return ansiOK(trueText, enabled)
	}
	return ansiDim(falseText, enabled)
}

func ansiOK(s string, enabled bool) string {
	if !enabled {
		return s
	}
	return outputpkg.ANSIGreen + s + outputpkg.ANSIReset
}

func ansiWarn(s string, enabled bool) string {
	if !enabled {
		return s
	}
	return outputpkg.ANSIYellow + s + outputpkg.ANSIReset
}

func ansiRed(s string, enabled bool) string {
	if !enabled {
		return s
	}
	return outputpkg.ANSIRed + s + outputpkg.ANSIReset
}

func ansiDim(s string, enabled bool) string {
	if !enabled {
		return s
	}
	return outputpkg.ANSIDim + s + outputpkg.ANSIReset
}

func ansiBorder(s string, enabled bool) string {
	if !enabled {
		return s
	}
	return outputpkg.ANSIBorder + s + outputpkg.ANSIReset
}

// bannerTag renders a dim left-aligned label inside the banner box.
func bannerTag(s string, colorEnabled bool) string {
	return ansiDim(s, colorEnabled)
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

func (r *AgentConsole) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "agent",
		Short:         "aiscan interactive agent",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.HiddenDefaultCmd = true
	root.SetHelpCommand(&cobra.Command{Use: "help", Hidden: true})
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)

	root.AddCommand(
		r.promptCommand(),
		r.helpCommand(),
		r.statusCommand(),
		r.debugCommand(),
		r.resetCommand(),
		r.continueCommand(),
		r.exitCommand(),
	)
	root.AddCommand(r.sessionCommands()...)
	root.AddCommand(r.ioaCommands()...)
	root.AddCommand(r.skillCommands()...)
	return root
}

func (r *AgentConsole) promptCommand() *cobra.Command {
	return &cobra.Command{
		Use:    agentPromptCommandName,
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.runPrompt(cmd.Context(), args[0])
		},
	}
}

func (r *AgentConsole) helpCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "/help",
		Short: "Show interactive commands",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Fprint(os.Stdout, r.helpOutput())
			return nil
		},
	}
}

func (r *AgentConsole) statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "/status",
		Short: "Show current agent status",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Fprint(os.Stdout, r.statusOutput())
		},
	}
}

func (r *AgentConsole) helpOutput() string {
	colorEnabled := r.output != nil && r.output.color.Enabled
	sections := []terminalSection{
		{
			Title: "常用",
			Rows: []terminalRow{
				{Label: "/help", Value: ansiValue("查看这份命令面板", colorEnabled)},
				{Label: "/status", Value: ansiValue("查看模型、渲染模式、IOA、skills 和 PTY 会话", colorEnabled)},
				{Label: "/debug", Value: ansiValue("查看停止原因；/debug on 开启事件 JSONL", colorEnabled)},
				{Label: "/continue", Value: ansiValue("不追加输入，继续上一轮任务", colorEnabled)},
				{Label: "/reset", Value: ansiValue("清空当前会话上下文", colorEnabled)},
				{Label: "/exit", Value: ansiValue("退出交互模式", colorEnabled)},
			},
		},
		{
			Title: "任务",
			Rows: []terminalRow{
				{Label: "普通文本", Value: ansiValue("直接发送自然语言任务", colorEnabled)},
				{Label: "/<skill> 任务", Value: ansiValue("用指定 skill 处理后面的任务", colorEnabled)},
				{Label: "/scan 目标", Value: ansiValue("常用扫描入口；可继续追加 verify/deep/report 需求", colorEnabled)},
			},
		},
		{
			Title: "PTY",
			Rows: []terminalRow{
				{Label: "/sessions", Value: ansiValue("查看后台 PTY 会话", colorEnabled)},
				{Label: "/tail <id>", Value: ansiValue("读取会话新增输出；加 --full 查看 tail", colorEnabled)},
				{Label: "/send <id> <text>", Value: ansiValue("向交互式会话发送文本", colorEnabled)},
				{Label: "/kill <id>", Value: ansiValue("终止会话", colorEnabled)},
			},
		},
		{
			Title: "IOA",
			Rows: []terminalRow{
				{Label: "/spaces", Value: ansiValue("查看协作空间", colorEnabled)},
				{Label: "/nodes", Value: ansiValue("查看协作节点", colorEnabled)},
				{Label: "/messages", Value: ansiValue("查看空间起始消息", colorEnabled)},
				{Label: "/context", Value: ansiValue("查看消息上下文", colorEnabled)},
			},
		},
	}
	return r.renderSectionsPanel("commands", sections, colorEnabled)
}

func (r *AgentConsole) statusOutput() string {
	colorEnabled := r.output != nil && r.output.color.Enabled
	provider, model := r.providerModel()
	if provider == "" {
		provider = "not configured"
	}
	if model == "" {
		model = "-"
	}

	ioa := "disabled"
	if r != nil && r.option != nil && strings.TrimSpace(r.option.IOAURL) != "" {
		ioa = strings.TrimSpace(r.option.IOAURL)
		if r.option.Space != "" {
			ioa += " · space " + r.option.Space
		}
	}

	rows := []helpRow{
		{Command: "model", Detail: provider + " / " + model},
		{Command: "render", Detail: r.sessionSummary()},
		{Command: "debug", Detail: r.debugStatusSummary()},
		{Command: "ioa", Detail: ioa},
		{Command: "history", Detail: agentConsoleHistoryPath()},
	}
	if skills := r.skillSlashNames(); skills != "" {
		rows = append(rows, helpRow{Command: "skills", Detail: skills})
	}
	if summary := r.sessionCountSummary(); summary != "" {
		rows = append(rows, helpRow{Command: "sessions", Detail: summary})
	}
	sectionRows := make([]terminalRow, 0, len(rows))
	for _, row := range rows {
		sectionRows = append(sectionRows, terminalRow{Label: row.Command, Value: ansiValue(row.Detail, colorEnabled)})
	}
	return r.renderSectionsPanel("status", []terminalSection{{Rows: sectionRows}}, colorEnabled)
}

type helpRow struct {
	Command string
	Detail  string
}

func renderHelpRows(rows []helpRow, colorEnabled bool) string {
	var b strings.Builder
	for _, row := range rows {
		if row.Command == "" && row.Detail == "" {
			b.WriteByte('\n')
			continue
		}
		command := ansiAccent(fmt.Sprintf("%-18s", row.Command), colorEnabled)
		detail := ansiValue(row.Detail, colorEnabled)
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

func (r *AgentConsole) renderSectionsPanel(title string, sections []terminalSection, colorEnabled bool) string {
	return "\n" + renderTerminalSections(title, sections, r.bannerWidth(), colorEnabled) + "\n\n"
}

func (r *AgentConsole) resetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "/reset",
		Short: "Clear conversation context",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			r.session.Reset()
			fmt.Fprintln(os.Stdout, "Context reset.")
		},
	}
}

func (r *AgentConsole) continueCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "/continue",
		Short: "Continue without a new prompt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			r.ensureOutput().Start("continue", "")
			result, err := r.session.Continue(cmd.Context())
			if err != nil {
				r.ensureOutput().ensureStreamNewline()
				return err
			}
			r.printResult(result)
			return nil
		},
	}
}

func (r *AgentConsole) debugCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "/debug [on|off] [events-file]",
		Short: "Show debug state and configure event logging",
		Args:  cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				fmt.Fprint(os.Stdout, r.debugOutput())
				return nil
			}
			action := strings.ToLower(strings.TrimSpace(args[0]))
			switch action {
			case "on", "enable", "start":
				path := ""
				if len(args) > 1 {
					path = strings.Join(args[1:], " ")
				}
				if err := r.enableDebugEvents(path); err != nil {
					return err
				}
				fmt.Fprint(os.Stdout, r.debugOutput())
				return nil
			case "off", "disable", "stop":
				r.closeDebugEvents()
				fmt.Fprint(os.Stdout, r.debugOutput())
				return nil
			default:
				return fmt.Errorf("unknown debug action %q: use /debug, /debug on [file], or /debug off", args[0])
			}
		},
	}
}

func (r *AgentConsole) debugOutput() string {
	colorEnabled := r.output != nil && r.output.color.Enabled
	snap := agent.DebugSnapshot{}
	if r != nil && r.session != nil {
		snap = r.session.DebugSnapshot()
	}
	lastStop := string(snap.LastStop)
	if lastStop == "" {
		lastStop = "-"
	}
	rows := []terminalRow{
		{Label: "cli_debug", Value: ansiSwitch(r.option != nil && r.option.Debug, colorEnabled)},
		{Label: "events", Value: ansiMaybeDash(valueOrDash(r.currentDebugEventsPath()), colorEnabled)},
		{Label: "session", Value: ansiMaybeDash(valueOrDash(snap.SessionID), colorEnabled)},
		{Label: "running", Value: ansiBoolLabel(snap.Running, "true", "false", colorEnabled)},
		{Label: "messages", Value: ansiValue(fmt.Sprintf("%d", snap.MessageCount), colorEnabled)},
		{Label: "last_stop", Value: ansiMaybeDash(lastStop, colorEnabled)},
		{Label: "last_detail", Value: ansiMaybeDash(valueOrDash(snap.LastDetail), colorEnabled)},
		{Label: "last_error", Value: ansiMaybeDash(valueOrDash(snap.LastError), colorEnabled)},
		{Label: "turns", Value: ansiValue(fmt.Sprintf("%d", snap.LastTurns), colorEnabled)},
		{Label: "tokens", Value: ansiValue(fmt.Sprintf("prompt=%d completion=%d total=%d context=%d",
			snap.LastUsage.PromptTokens, snap.LastUsage.CompletionTokens, snap.LastUsage.TotalTokens, snap.LastContext), colorEnabled)},
		{Label: "inbox", Value: ansiValue(fmt.Sprintf("queued=%d producers=%d loops=%d",
			snap.InboxLen, snap.ActiveProducers, snap.ActiveLoops), colorEnabled)},
	}
	if r.currentDebugEventsPath() == "" {
		rows = append(rows, terminalRow{
			Label: "hint",
			Value: ansiValue("run /debug on to write event JSONL for this session", colorEnabled),
		})
	}
	return r.renderSectionsPanel("debug", []terminalSection{{Rows: rows}}, colorEnabled)
}

func (r *AgentConsole) debugStatusSummary() string {
	state := "off"
	if r != nil && r.option != nil && r.option.Debug {
		state = "on"
	}
	if path := r.currentDebugEventsPath(); path != "" {
		return state + " · events " + path
	}
	return state + " · events off"
}

func (r *AgentConsole) enableDebugEvents(path string) error {
	if r == nil || r.session == nil || r.session.Cfg.Bus == nil {
		return fmt.Errorf("debug events unavailable: agent session is not initialized")
	}
	if strings.TrimSpace(path) == "" {
		if envPath := strings.TrimSpace(os.Getenv("AISCAN_EVENTS_FILE")); envPath != "" && r.debugEvents == nil {
			r.debugPath = envPath
			return nil
		}
		path = defaultDebugEventsPath(r.session.Cfg.SessionID)
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if r.debugEvents != nil {
		r.closeDebugEvents()
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create debug events dir: %w", err)
		}
	}
	writer, err := newEventsFileSubscriber(path)
	if err != nil {
		return err
	}
	unsub := r.session.Cfg.Bus.Subscribe(writer.HandleEvent)
	r.debugEvents = writer
	r.debugUnsub = unsub
	r.debugPath = path
	return nil
}

func (r *AgentConsole) closeDebugEvents() {
	if r == nil {
		return
	}
	if r.debugUnsub != nil {
		r.debugUnsub()
		r.debugUnsub = nil
	}
	if r.debugEvents != nil {
		r.debugEvents.Close()
		r.debugEvents = nil
	}
	if strings.TrimSpace(os.Getenv("AISCAN_EVENTS_FILE")) == "" {
		r.debugPath = ""
	}
}

func (r *AgentConsole) currentDebugEventsPath() string {
	if r == nil {
		return ""
	}
	if strings.TrimSpace(r.debugPath) != "" {
		return strings.TrimSpace(r.debugPath)
	}
	return strings.TrimSpace(os.Getenv("AISCAN_EVENTS_FILE"))
}

func defaultDebugEventsPath(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = "session"
	}
	return filepath.Join(os.TempDir(), "aiscan-events-"+sessionID+".jsonl")
}

func enabledText(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func (r *AgentConsole) exitCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "/exit",
		Aliases: []string{"/quit"},
		Short:   "Exit",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errAgentConsoleExit
		},
	}
}

func (r *AgentConsole) sessionCommands() []*cobra.Command {
	return []*cobra.Command{
		r.sessionsCommand(),
		r.tailCommand(),
		r.sendCommand(),
		r.killCommand(),
	}
}

func (r *AgentConsole) sessionsCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "/sessions",
		Aliases: []string{"/session"},
		Short:   "List PTY sessions",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Fprint(os.Stdout, r.sessionsOutput())
			return nil
		},
	}
}

func (r *AgentConsole) tailCommand() *cobra.Command {
	var full bool
	var lines int
	cmd := &cobra.Command{
		Use:   "/tail <id|name>",
		Short: "Show new PTY session output",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			mgr := r.sessionManager()
			if mgr == nil {
				return fmt.Errorf("PTY sessions unavailable: bash tool is not registered")
			}
			id := args[0]
			var (
				text string
				more bool
				err  error
			)
			if full {
				text, err = mgr.Peek(id, lines)
			} else {
				text, more, err = mgr.PeekNew(id, 0)
			}
			if err != nil {
				return err
			}
			fmt.Fprint(os.Stdout, r.tailOutput(id, text, more, full))
			return nil
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "show session tail instead of new output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 30, "lines to show with --full")
	return cmd
}

func (r *AgentConsole) sendCommand() *cobra.Command {
	var raw bool
	cmd := &cobra.Command{
		Use:   "/send <id|name> <text>",
		Short: "Send text to a PTY session",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			mgr := r.sessionManager()
			if mgr == nil {
				return fmt.Errorf("PTY sessions unavailable: bash tool is not registered")
			}
			text := strings.Join(args[1:], " ")
			if !raw {
				text += "\n"
			}
			if err := mgr.Write(args[0], []byte(text)); err != nil {
				return err
			}
			colorEnabled := r.output != nil && r.output.color.Enabled
			rows := []terminalRow{
				{Label: "session", Value: ansiValue(args[0], colorEnabled)},
				{Label: "bytes", Value: ansiValue(fmt.Sprintf("%d", len(text)), colorEnabled)},
				{Label: "mode", Value: ansiValue(map[bool]string{true: "raw", false: "line"}[raw], colorEnabled)},
			}
			fmt.Fprint(os.Stdout, r.renderSectionsPanel("send", []terminalSection{{Rows: rows}}, colorEnabled))
			return nil
		},
	}
	cmd.Flags().BoolVar(&raw, "raw", false, "send text without appending newline")
	return cmd
}

func (r *AgentConsole) killCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "/kill <id|name>",
		Short: "Kill a PTY session",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			mgr := r.sessionManager()
			if mgr == nil {
				return fmt.Errorf("PTY sessions unavailable: bash tool is not registered")
			}
			if err := mgr.Kill(args[0]); err != nil {
				return err
			}
			colorEnabled := r.output != nil && r.output.color.Enabled
			rows := []terminalRow{{Label: "session", Value: ansiWarn(args[0], colorEnabled)}}
			fmt.Fprint(os.Stdout, r.renderSectionsPanel("killed", []terminalSection{{Rows: rows}}, colorEnabled))
			return nil
		},
	}
}

func (r *AgentConsole) sessionManager() *tmuxpkg.Manager {
	if r == nil || r.session == nil {
		return nil
	}
	return bashSessionManager(r.session.Cfg.Tools)
}

func (r *AgentConsole) sessionsOutput() string {
	colorEnabled := r.output != nil && r.output.color.Enabled
	mgr := r.sessionManager()
	if mgr == nil {
		return r.renderSectionsPanel("sessions", []terminalSection{{
			Rows: []terminalRow{{Label: "state", Value: ansiWarn("unavailable - bash tool is not registered", colorEnabled)}},
		}}, colorEnabled)
	}
	items := mgr.List()
	if len(items) == 0 {
		return r.renderSectionsPanel("sessions", []terminalSection{{
			Rows: []terminalRow{{Label: "state", Value: ansiValue("no PTY sessions", colorEnabled)}},
		}}, colorEnabled)
	}
	rows := make([][]string, 0, len(items))
	for _, it := range items {
		rows = append(rows, []string{
			it.ID,
			valueOrDash(it.Name),
			sessionStateLabel(it.State, colorEnabled),
			sessionAge(it),
			compactTerminalCell(it.Command, 56),
		})
	}
	table := renderTerminalTable([]string{"id", "name", "state", "age", "command"}, rows, colorEnabled)
	return r.renderPanel("sessions", table, colorEnabled)
}

func (r *AgentConsole) tailOutput(id, text string, more, full bool) string {
	colorEnabled := r.output != nil && r.output.color.Enabled
	mode := "new"
	if full {
		mode = "tail"
	}
	rows := []terminalRow{
		{Label: "session", Value: ansiValue(id, colorEnabled)},
		{Label: "mode", Value: ansiValue(mode, colorEnabled)},
	}
	var b strings.Builder
	b.WriteString(r.renderSectionsPanel("tail", []terminalSection{{Rows: rows}}, colorEnabled))
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		if full {
			text = "(no output yet)"
		} else {
			text = "(no new output since last read; use /tail --full <id> to re-read tail)"
		}
	}
	b.WriteString(text)
	b.WriteString("\n")
	if more {
		b.WriteString(ansiDim("more output available; run /tail "+id+" again", colorEnabled))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

func (r *AgentConsole) sessionCountSummary() string {
	mgr := r.sessionManager()
	if mgr == nil {
		return ""
	}
	items := mgr.List()
	if len(items) == 0 {
		return "0"
	}
	counts := map[tmuxpkg.State]int{}
	for _, it := range items {
		counts[it.State]++
	}
	parts := make([]string, 0, 4)
	for _, state := range []tmuxpkg.State{tmuxpkg.StateRunning, tmuxpkg.StateCompleted, tmuxpkg.StateFailed, tmuxpkg.StateKilled} {
		if n := counts[state]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", state, n))
		}
	}
	return strings.Join(parts, " ")
}

func sessionStateLabel(state tmuxpkg.State, colorEnabled bool) string {
	switch state {
	case tmuxpkg.StateRunning:
		return ansiAccent(string(state), colorEnabled)
	case tmuxpkg.StateCompleted:
		return ansiOK(string(state), colorEnabled)
	case tmuxpkg.StateKilled:
		return ansiWarn(string(state), colorEnabled)
	case tmuxpkg.StateFailed:
		return ansiRed(string(state), colorEnabled)
	default:
		return string(state)
	}
}

func sessionAge(info tmuxpkg.Info) string {
	end := info.EndedAt
	if info.State == tmuxpkg.StateRunning || end.IsZero() {
		end = time.Now()
	}
	if info.StartedAt.IsZero() {
		return "-"
	}
	d := end.Sub(info.StartedAt).Round(time.Second)
	if d < 0 {
		return "-"
	}
	return d.String()
}

func valueOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func (r *AgentConsole) skillCommands() []*cobra.Command {
	if r.application == nil || r.application.Skills == nil {
		return nil
	}
	commands := make([]*cobra.Command, 0, len(r.application.Skills.Skills))
	for _, skill := range r.application.Skills.Skills {
		skill := skill
		if strings.TrimSpace(skill.Name) == "" {
			continue
		}
		commands = append(commands, r.skillCommand(skill))
	}
	return commands
}

func (r *AgentConsole) skillCommand(skill skillpkg.Skill) *cobra.Command {
	return &cobra.Command{
		Use:                "/" + skill.Name + " [prompt]",
		Short:              skill.Description,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.runSkill(cmd.Context(), skill, strings.Join(args, " "))
		},
	}
}

func (r *AgentConsole) runPrompt(ctx context.Context, input string) error {
	prompt := skillpkg.ExpandCommand(input, r.application.Skills)
	prompt, err := cfg.ApplySelectedSkills(prompt, r.option.Skills, r.application.Skills)
	if err != nil {
		return err
	}
	r.ensureOutput().Start("prompt", input)
	result, err := r.session.Run(ctx, prompt)
	if err != nil {
		r.ensureOutput().ensureStreamNewline()
		return err
	}
	r.printResult(result)
	return nil
}

func (r *AgentConsole) runSkill(ctx context.Context, skill skillpkg.Skill, input string) error {
	prompt := r.application.Skills.FormatInvocation(skill, input)
	prompt, err := cfg.ApplySelectedSkills(prompt, r.option.Skills, r.application.Skills)
	if err != nil {
		return err
	}
	r.ensureOutput().Start("skill "+skill.Name, input)
	result, err := r.session.Run(ctx, prompt)
	if err != nil {
		r.ensureOutput().ensureStreamNewline()
		return err
	}
	r.printResult(result)
	return nil
}

func (r *AgentConsole) printResult(result *agent.Result) {
	output := r.ensureOutput()
	if result == nil || strings.TrimSpace(result.Output) == "" {
		if output.didStream {
			output.Final("")
			return
		}
		output.Empty()
		return
	}
	output.Final(result.Output)
}

func (r *AgentConsole) ensureOutput() *AgentOutput {
	if r.output == nil {
		r.output = NewAgentOutput(r.option)
	}
	return r.output
}

func (r *AgentConsole) ioaClient() (*ioaclient.Client, error) {
	ioaURL := r.option.IOAURL
	if ioaURL == "" {
		return nil, fmt.Errorf("IOA not configured: use --ioa-url")
	}
	return ioaclient.NewClient(ioaURL, "")
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
	"/sessions": true, "/session": true, "/tail": true, "/send": true, "/kill": true,
	"/debug": true,
}

func AgentConsoleArgsForLine(line string) ([]string, error) {
	text := strings.TrimSpace(line)
	if text == "" {
		return nil, nil
	}
	switch strings.ToLower(text) {
	case "continue":
		return []string{"/continue"}, nil
	}
	switch text {
	case "继续":
		return []string{"/continue"}, nil
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

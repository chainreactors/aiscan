package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/skills"
	goflags "github.com/jessevdk/go-flags"
)

const Version = "0.1.0"

type Option struct {
	LLMOptions     `group:"LLM Options"`
	ScannerOptions `group:"Scanner Options"`
	AgentOptions   `group:"Agent Options"`
	ACPOptions     `group:"ACP Options"`
	MiscOptions    `group:"Miscellaneous Options"`
}

type LLMOptions struct {
	Provider string `long:"llm-provider" description:"LLM provider name (openai, deepseek, openrouter, ollama, etc.)"`
	BaseURL  string `long:"llm-base-url" description:"LLM API base URL"`
	APIKey   string `long:"llm-api-key" description:"LLM API key (or set env: OPENAI_API_KEY, AISCAN_API_KEY)"`
	Model    string `long:"llm-model" description:"LLM model name"`
	Proxy    string `long:"llm-proxy" description:"HTTP proxy for LLM API"`
	AI       bool   `long:"ai" description:"Use the configured LLM to process direct scanner output with the relevant tool skill"`
}

type ScannerOptions struct {
	CyberhubURL  string `long:"cyberhub-url" description:"Cyberhub server URL for loading fingers/templates"`
	CyberhubKey  string `long:"cyberhub-key" description:"Cyberhub API key"`
	CyberhubMode string `long:"cyberhub-mode" description:"Cyberhub resource mode: merge or override"`
}

type AgentOptions struct {
	Prompt   string   `short:"p" long:"prompt" description:"Natural language task for the agent"`
	Inputs   []string `short:"i" long:"input" description:"Target input: IP, URL, IP:port, or CIDR. Can specify multiple"`
	Skills   []string `short:"s" long:"skill" description:"Embedded skill to apply. Can specify multiple"`
	TaskFile string   `long:"task-file" description:"File containing task description"`
	MaxTurns int      `long:"max-turns" description:"Maximum agent loop iterations" default:"50"`
	Timeout  int      `long:"timeout" description:"Overall timeout in seconds" default:"3600"`
}

type ACPOptions struct {
	ACPURL      string `long:"acp-url" description:"ACP server URL for agent tools"`
	ACPNodeID   string `long:"acp-node-id" description:"Existing ACP node id for agent tools"`
	ACPNodeName string `long:"acp-node-name" description:"ACP node name when auto-registering"`
	ACPDB       string `long:"acp-db" description:"ACP SQLite database path for 'aiscan acp serve'" default:"./acp.db"`
	Space       string `long:"space" description:"ACP space name for 'aiscan loop'" default:"default"`
}

type MiscOptions struct {
	Debug   bool `long:"debug" description:"Enable debug logging"`
	Quiet   bool `short:"q" long:"quiet" description:"Quiet mode"`
	NoColor bool `long:"no-color" description:"Disable ANSI colors in scanner output"`
	Version bool `long:"version" description:"Print version and exit"`
}

type cliOptions struct {
	RootOptions
	Agent   agentCommandOptions       `command:"agent" description:"Run the LLM agent"`
	Loop    loopCommandOptions        `command:"loop" description:"Run an ACP loop worker"`
	ACP     acpCommandOptions         `command:"acp" description:"ACP server commands"`
	Scan    scannerCommandOptions     `command:"scan" description:"Run the scan pipeline"`
	Gogo    passthroughCommandOptions `command:"gogo" description:"Run gogo scanner"`
	Spray   passthroughCommandOptions `command:"spray" description:"Run spray scanner"`
	Zombie  passthroughCommandOptions `command:"zombie" description:"Run zombie weakpass scanner"`
	Neutron passthroughCommandOptions `command:"neutron" description:"Run neutron POC scanner"`
}

type RootOptions struct {
	LLMOptions     `group:"LLM Options"`
	MiscOptions    `group:"Miscellaneous Options"`
	ScannerOptions `group:"Scanner Options"`
}

type agentCommandOptions struct {
	LLMOptions     `group:"LLM Options"`
	AgentOptions   `group:"Agent Options"`
	ACPToolOptions `group:"ACP Tool Options"`
}

type ACPToolOptions struct {
	ACPURL      string `long:"acp-url" description:"ACP server URL for agent tools"`
	ACPNodeID   string `long:"acp-node-id" description:"Existing ACP node id for agent tools"`
	ACPNodeName string `long:"acp-node-name" description:"ACP node name when auto-registering"`
}

type loopCommandOptions struct {
	LLMOptions     `group:"LLM Options"`
	AgentOptions   `group:"Agent Options"`
	LoopACPOptions `group:"Loop ACP Options"`
}

type LoopACPOptions struct {
	ACPURL      string `long:"acp-url" description:"ACP server URL"`
	ACPNodeID   string `long:"acp-node-id" description:"Existing ACP node id"`
	ACPNodeName string `long:"acp-node-name" description:"ACP node name"`
	Space       string `long:"space" description:"ACP space name" default:"default"`
}

type acpCommandOptions struct {
	Serve acpServeCommandOptions `command:"serve" description:"Run the ACP HTTP server"`
}

type acpServeCommandOptions struct {
	ACPURL  string `long:"acp-url" description:"ACP server listen URL" default:"http://127.0.0.1:8765"`
	ACPDB   string `long:"acp-db" description:"ACP SQLite database path" default:"./acp.db"`
	Timeout int    `long:"timeout" description:"Overall timeout in seconds" default:"3600"`
}

type scannerCommandOptions struct {
	LLMOptions `group:"LLM Options"`
}

type passthroughCommandOptions struct{}

type runMode string

const (
	runModeAgent     runMode = "agent"
	runModeLoop      runMode = "loop"
	runModeACPServe  runMode = "acp serve"
	runModeScanner   runMode = "scanner"
	runModeNoCommand runMode = ""
)

type parsedCLI struct {
	Option      Option
	Mode        runMode
	ScannerArgs []string
}

func AiScan() {
	parsed, err := parseCLI(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	option := parsed.Option
	if option.Version {
		fmt.Printf("aiscan v%s\n", Version)
		return
	}
	if parsed.Mode == runModeNoCommand {
		fmt.Fprintln(os.Stderr, "error: missing subcommand: use agent, loop, acp serve, scan, gogo, spray, zombie, or neutron")
		os.Exit(1)
	}

	logger := telemetry.GlobalLogger(telemetry.LogConfig{Debug: option.Debug, Quiet: option.Quiet, Output: os.Stderr})

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(option.Timeout)*time.Second)
	defer cancel()

	setupSignalHandler(cancel, logger)

	switch parsed.Mode {
	case runModeAgent:
		if err := runAgentMode(ctx, &option); err != nil {
			logger.Errorf("agent failed: %s", err)
			os.Exit(1)
		}
	case runModeLoop:
		if err := runLoop(ctx, &option); err != nil {
			logger.Errorf("loop failed: %s", err)
			os.Exit(1)
		}
	case runModeACPServe:
		if err := runACPServe(ctx, &option); err != nil {
			logger.Errorf("acp server failed: %s", err)
			os.Exit(1)
		}
	case runModeScanner:
		if err := runDirectScannerMode(ctx, &option, parsed.ScannerArgs); err != nil {
			logger.Errorf("scanner command failed: %s", err)
			os.Exit(1)
		}
	}
}

func parseCLI(args []string) (parsedCLI, error) {
	if scannerName, rootArgs, scannerRest, ok := splitScannerCommand(args); ok {
		return parseScannerCLI(scannerName, rootArgs, scannerRest)
	}

	var cli cliOptions
	parser := newCLIParser(&cli, parserOptionsForArgs(args))
	rest, err := parser.ParseArgs(args)
	if err != nil {
		if flagsErr, ok := err.(*goflags.Error); ok && flagsErr.Type == goflags.ErrHelp {
			if scannerName := firstCommandName(args, rootFlagValueArity); isScannerCommandName(scannerName) {
				option := baseOption(cli.RootOptions)
				option.Timeout = 3600
				scannerArgs := append([]string{scannerName}, argsAfterCommand(args, scannerName)...)
				return parsedCLI{Option: option, Mode: runModeScanner, ScannerArgs: scannerArgs}, nil
			}
			printHelp(parser)
			return parsedCLI{Mode: runModeNoCommand}, nil
		}
		return parsedCLI{}, err
	}

	option := baseOption(cli.RootOptions)
	if cli.Version {
		return parsedCLI{Option: option, Mode: runModeNoCommand}, nil
	}

	mode := selectedMode(parser)
	if mode == runModeNoCommand {
		return parsedCLI{Option: option, Mode: runModeNoCommand}, nil
	}

	scannerName := selectedScanner(parser)
	switch mode {
	case runModeAgent:
		option.ScannerOptions = mergeScannerOptions(option.ScannerOptions, cli.RootOptions.ScannerOptions)
		mergeAgentCommand(&option, cli.Agent)
	case runModeLoop:
		option.ScannerOptions = mergeScannerOptions(option.ScannerOptions, cli.RootOptions.ScannerOptions)
		mergeLoopCommand(&option, cli.Loop)
	case runModeACPServe:
		mergeACPServeCommand(&option, cli.ACP.Serve)
	case runModeScanner:
		if scannerName == "scan" {
			mergeLLMOptions(&option, cli.Scan.LLMOptions)
		}
		option.Timeout = 3600
		scannerRest, err := applyScannerRootArgs(rest, &option)
		if err != nil {
			return parsedCLI{}, err
		}
		scannerArgs := append([]string{scannerName}, scannerRest...)
		return parsedCLI{Option: option, Mode: mode, ScannerArgs: scannerArgs}, nil
	}
	return parsedCLI{Option: option, Mode: mode}, nil
}

func parseScannerCLI(scannerName string, rootArgs, scannerRest []string) (parsedCLI, error) {
	var manual Option
	filteredRootArgs, err := applyScannerCommandArgs(scannerName, rootArgs, &manual)
	if err != nil {
		return parsedCLI{}, err
	}
	var cli cliOptions
	parser := newCLIParser(&cli, goflags.Default&^goflags.PrintErrors)
	if scannerName == "scan" {
		parser = newCLIParser(&cli, (goflags.Default&^goflags.PrintErrors)|goflags.IgnoreUnknown)
	}
	if _, err := parser.ParseArgs(filteredRootArgs); err != nil {
		if flagsErr, ok := err.(*goflags.Error); ok && flagsErr.Type == goflags.ErrHelp {
			printHelp(parser)
			return parsedCLI{Mode: runModeNoCommand}, nil
		}
		return parsedCLI{}, err
	}

	option := baseOption(cli.RootOptions)
	mergeManualScannerOptions(&option, manual)
	if cli.Version {
		return parsedCLI{Option: option, Mode: runModeNoCommand}, nil
	}
	option.Timeout = 3600

	scannerArgs, err := applyScannerCommandArgs(scannerName, scannerRest, &option)
	if err != nil {
		return parsedCLI{}, err
	}
	return parsedCLI{
		Option:      option,
		Mode:        runModeScanner,
		ScannerArgs: append([]string{scannerName}, scannerArgs...),
	}, nil
}

func mergeManualScannerOptions(option *Option, manual Option) {
	option.LLMOptions = mergeLLMOptionValues(option.LLMOptions, manual.LLMOptions)
	option.ScannerOptions = mergeScannerOptions(option.ScannerOptions, manual.ScannerOptions)
	if manual.NoColor {
		option.NoColor = true
	}
	if manual.Prompt != "" {
		option.Prompt = manual.Prompt
	}
	if manual.TaskFile != "" {
		option.TaskFile = manual.TaskFile
	}
	if len(manual.Skills) > 0 {
		option.Skills = append(option.Skills, manual.Skills...)
	}
}

func mergeLLMOptionValues(base, override LLMOptions) LLMOptions {
	if override.Provider != "" {
		base.Provider = override.Provider
	}
	if override.BaseURL != "" {
		base.BaseURL = override.BaseURL
	}
	if override.APIKey != "" {
		base.APIKey = override.APIKey
	}
	if override.Model != "" {
		base.Model = override.Model
	}
	if override.Proxy != "" {
		base.Proxy = override.Proxy
	}
	if override.AI {
		base.AI = true
	}
	return base
}

func newCLIParser(cli *cliOptions, options goflags.Options) *goflags.Parser {
	parser := goflags.NewParser(cli, options)
	parser.SubcommandsOptional = true
	parser.Usage = `[OPTIONS] <command>

aiscan - Agentic Security Scanner powered by LLM

Commands:
  agent       Run the LLM agent
  loop        Run an ACP loop worker
  acp serve   Run the ACP HTTP server
  scan        Run the scan pipeline
  gogo        Run gogo scanner
  spray       Run spray scanner
  zombie      Run zombie weakpass scanner
  neutron     Run neutron POC scanner

Examples:
  aiscan agent -p "find web services and check vulnerabilities" -i 192.168.1.0/24
  aiscan agent --llm-provider deepseek --llm-model deepseek-chat -p "enumerate services" -i 10.0.0.0/24
  aiscan agent --llm-provider ollama --llm-model llama3 --llm-base-url http://localhost:11434/v1 -p "check this host" -i http://target.com
  aiscan scan -i 127.0.0.1 --mode quick --verify=high --llm-api-key KEY --llm-model gpt-4o
  aiscan scan -i 192.168.1.0/24 --mode full
  aiscan acp serve
  aiscan loop -p "localhost web scanner" -s aiscan --space case-1`
	return parser
}

func parserOptionsForArgs(args []string) goflags.Options {
	options := goflags.Options(goflags.Default &^ goflags.PrintErrors)
	if len(args) == 0 {
		return options
	}
	if isScannerCommandName(firstCommandName(args, rootFlagValueArity)) {
		options |= goflags.IgnoreUnknown
	}
	return options
}

func splitScannerCommand(args []string) (string, []string, []string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if isScannerCommandName(arg) {
			return arg, append([]string(nil), args[:i]...), append([]string(nil), args[i+1:]...), true
		}
		if shouldSkipRootFlagValue(arg) && i+1 < len(args) {
			i++
		}
	}
	return "", nil, nil, false
}

func shouldSkipRootFlagValue(arg string) bool {
	key, _, hasValue := strings.Cut(arg, "=")
	if hasValue {
		return false
	}
	return rootFlagValueArity[key] > 0
}

func firstCommandName(args []string, valueArity map[string]int) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return ""
		}
		if strings.HasPrefix(arg, "-") {
			key, _, hasValue := strings.Cut(arg, "=")
			if !hasValue {
				i += valueArity[key]
			}
			continue
		}
		return arg
	}
	return ""
}

var rootFlagValueArity = map[string]int{
	"--cyberhub-url":  1,
	"--cyberhub-key":  1,
	"--cyberhub-mode": 1,
	"--llm-provider":  1,
	"--llm-base-url":  1,
	"--llm-api-key":   1,
	"--llm-model":     1,
	"--llm-proxy":     1,
	"--prompt":        1,
	"--task-file":     1,
	"--skill":         1,
	"--max-turns":     1,
}

func argsAfterCommand(args []string, command string) []string {
	for i, arg := range args {
		if arg == command {
			return append([]string(nil), args[i+1:]...)
		}
	}
	return nil
}

func isScannerCommandName(name string) bool {
	switch name {
	case "scan", "gogo", "spray", "zombie", "neutron":
		return true
	}
	return false
}

func selectedMode(parser *goflags.Parser) runMode {
	active := parser.Active
	if active == nil {
		return runModeNoCommand
	}
	if active.Name == "acp" && active.Active != nil && active.Active.Name == "serve" {
		return runModeACPServe
	}
	switch active.Name {
	case "agent":
		return runModeAgent
	case "loop":
		return runModeLoop
	case "serve":
		return runModeACPServe
	case "scan", "gogo", "spray", "zombie", "neutron":
		return runModeScanner
	}
	return runModeNoCommand
}

func selectedScanner(parser *goflags.Parser) string {
	active := parser.Active
	if active == nil {
		return ""
	}
	switch active.Name {
	case "scan", "gogo", "spray", "zombie", "neutron":
		return active.Name
	}
	return ""
}

func baseOption(root RootOptions) Option {
	option := Option{
		LLMOptions:     root.LLMOptions,
		MiscOptions:    root.MiscOptions,
		ScannerOptions: root.ScannerOptions,
	}
	return option
}

func mergeAgentCommand(option *Option, cmd agentCommandOptions) {
	mergeLLMOptions(option, cmd.LLMOptions)
	option.AgentOptions = cmd.AgentOptions
	option.ACPURL = cmd.ACPURL
	option.ACPNodeID = cmd.ACPNodeID
	option.ACPNodeName = cmd.ACPNodeName
}

func mergeLoopCommand(option *Option, cmd loopCommandOptions) {
	mergeLLMOptions(option, cmd.LLMOptions)
	option.AgentOptions = cmd.AgentOptions
	option.ACPURL = cmd.ACPURL
	option.ACPNodeID = cmd.ACPNodeID
	option.ACPNodeName = cmd.ACPNodeName
	option.Space = cmd.Space
}

func mergeACPServeCommand(option *Option, cmd acpServeCommandOptions) {
	option.ACPURL = cmd.ACPURL
	option.ACPDB = cmd.ACPDB
	option.Timeout = cmd.Timeout
}

func mergeLLMOptions(option *Option, llm LLMOptions) {
	option.LLMOptions = llm
}

func mergeScannerOptions(base, override ScannerOptions) ScannerOptions {
	if override.CyberhubURL != "" {
		base.CyberhubURL = override.CyberhubURL
	}
	if override.CyberhubKey != "" {
		base.CyberhubKey = override.CyberhubKey
	}
	if override.CyberhubMode != "" {
		base.CyberhubMode = override.CyberhubMode
	}
	return base
}

func applyScannerRootArgs(args []string, option *Option) ([]string, error) {
	return applyScannerCommandArgs("", args, option)
}

func applyScannerCommandArgs(scannerName string, args []string, option *Option) ([]string, error) {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, hasValue := strings.Cut(arg, "=")
		nextValue := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", arg)
			}
			i++
			return args[i], nil
		}
		switch key {
		case "--cyberhub-url":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.CyberhubURL = v
		case "--cyberhub-key":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.CyberhubKey = v
		case "--cyberhub-mode":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.CyberhubMode = v
		case "--no-color":
			option.NoColor = true
		case "--ai":
			if hasValue {
				option.AI = truthyFlagValue(value)
			} else {
				option.AI = true
			}
		case "--prompt":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.Prompt = v
		case "--task-file":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.TaskFile = v
		case "--skill":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.Skills = append(option.Skills, v)
		case "--llm-provider", "--provider":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.Provider = v
		case "--llm-base-url", "--base-url":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.BaseURL = v
		case "--llm-api-key", "--api-key":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.APIKey = v
		case "--llm-model", "--model":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.Model = v
		case "--llm-proxy", "--proxy":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			option.Proxy = v
		case "--max-turns":
			v, err := nextValue()
			if err != nil {
				return nil, err
			}
			if n, e := strconv.Atoi(v); e == nil && n > 0 {
				option.MaxTurns = n
			}
		default:
			out = append(out, arg)
		}
	}
	return out, nil
}

func truthyFlagValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

func resolveTask(opt *Option) (string, error) {
	prompt := strings.TrimSpace(opt.Prompt)
	if prompt != "" {
		if len(opt.Inputs) > 0 {
			return fmt.Sprintf("%s\n\nTargets:\n%s", prompt, formatInputs(opt.Inputs)), nil
		}
		return prompt, nil
	}

	if opt.TaskFile != "" {
		data, err := os.ReadFile(opt.TaskFile)
		if err != nil {
			return "", fmt.Errorf("read task file: %w", err)
		}
		task := strings.TrimSpace(string(data))
		if len(opt.Inputs) > 0 {
			return fmt.Sprintf("%s\n\nTargets:\n%s", task, formatInputs(opt.Inputs)), nil
		}
		return task, nil
	}

	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		task := strings.TrimSpace(string(data))
		if task != "" {
			if len(opt.Inputs) > 0 {
				return fmt.Sprintf("%s\n\nTargets:\n%s", task, formatInputs(opt.Inputs)), nil
			}
			return task, nil
		}
	}

	if len(opt.Inputs) > 0 {
		return fmt.Sprintf("Scan the provided targets using scan and summarize findings.\n\nTargets:\n%s", formatInputs(opt.Inputs)), nil
	}

	return "", fmt.Errorf("no prompt specified: use -p, --prompt, --task-file, or pipe via stdin")
}

func isDirectScannerJSONOutput(rest []string) bool {
	if !isDirectScannerCommand(rest) {
		return false
	}

	for _, arg := range rest[1:] {
		if arg == "-j" || arg == "--json" {
			return true
		}
		if strings.HasPrefix(arg, "--json=") {
			value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "--json=")))
			return value != "false" && value != "0" && value != "no"
		}
	}
	return false
}

func isDirectScannerCommand(rest []string) bool {
	if len(rest) == 0 {
		return false
	}
	switch rest[0] {
	case "scan", "gogo", "spray", "zombie", "neutron":
		return true
	}
	return false
}

func shouldStreamScannerOutput(rest []string) bool {
	if len(rest) == 0 || rest[0] != "scan" {
		return false
	}
	if isDirectScannerJSONOutput(rest) {
		return false
	}
	for _, arg := range rest[1:] {
		if arg == "--report" {
			return false
		}
		if strings.HasPrefix(arg, "--report=") {
			value := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "--report=")))
			if value != "false" && value != "0" && value != "no" {
				return false
			}
		}
	}
	return true
}

func hasScannerFlag(args []string, long string) bool {
	for _, arg := range args {
		if arg == long || strings.HasPrefix(arg, long+"=") {
			return true
		}
	}
	return false
}

func applySelectedSkills(text string, selected []string, store *skills.Store) (string, error) {
	if len(selected) == 0 {
		return text, nil
	}
	var sb strings.Builder
	for _, name := range selected {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		skill, ok := store.ByName(name)
		if !ok {
			return "", fmt.Errorf("unknown skill %q", name)
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(skills.FormatInvocation(skill, ""))
	}
	if strings.TrimSpace(text) != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(strings.TrimSpace(text))
	}
	return sb.String(), nil
}

func formatInputs(inputs []string) string {
	var sb strings.Builder
	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(input)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func setupSignalHandler(cancel context.CancelFunc, logger telemetry.Logger) {
	if logger == nil {
		logger = telemetry.NopLogger()
	}
	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sigCount := 0
		for range sigChan {
			sigCount++
			if sigCount == 1 {
				logger.Warnf("signal=shutdown action=finish_current_turn")
				cancel()
			} else {
				logger.Warnf("signal=shutdown action=force_exit")
				os.Exit(1)
			}
		}
	}()
}

func printHelp(parser *goflags.Parser) {
	parser.WriteHelp(os.Stdout)
}

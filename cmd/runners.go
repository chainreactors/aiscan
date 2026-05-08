package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	acpclient "github.com/chainreactors/aiscan/pkg/acp/client"
	"github.com/chainreactors/aiscan/pkg/acp/servercmd"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/app"
	"github.com/chainreactors/aiscan/pkg/loop"
	"github.com/chainreactors/aiscan/pkg/scanner/scan"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tool"
	"github.com/chainreactors/aiscan/pkg/tool/results"
	"github.com/chainreactors/aiscan/skills"
)

func commandLogger(option *Option) telemetry.Logger {
	return telemetry.NewLogger(telemetry.LogConfig{
		Debug:  option.Debug,
		Quiet:  option.Quiet,
		Output: os.Stderr,
	})
}

func runAgentMode(ctx context.Context, option *Option) error {
	logger := commandLogger(option)
	application, err := app.New(ctx, appConfig(option, runtimeFeatures{
		ProviderEnabled:     true,
		ToolsEnabled:        true,
		VerificationEnabled: true,
		VerifyMinPriority:   "high",
	}))
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}
	defer application.Close()
	applyResolvedProviderOptions(option, application.ProviderConfig)

	for _, diagnostic := range application.SkillDiagnostics {
		logger.Warnf("skill %s: %s", diagnostic.Path, diagnostic.Message)
	}

	task, err := resolveTask(option)
	if err != nil {
		return err
	}
	task = skills.ExpandCommand(task, application.Skills)
	task, err = applySelectedSkills(task, option.Skills, application.Skills)
	if err != nil {
		return err
	}
	if err := registerACPTools(ctx, application, option); err != nil {
		return fmt.Errorf("init acp tools: %w", err)
	}

	systemPrompt := agent.BuildSystemPrompt(&agent.PromptConfig{
		Tools:       application.Tools,
		ScannerDocs: application.Scanner.UsageDocs(),
		Skills:      application.Skills.Skills,
	})
	logger.Debugf("system prompt length: %d chars", len(systemPrompt))
	logger.Importantf("agent status=starting max_turns=%d timeout=%ds", option.MaxTurns, option.Timeout)
	logger.Importantf("agent task=%q", task)

	result, err := agent.Run(ctx, task, application.Tools,
		agent.WithProvider(application.Provider),
		agent.WithMaxTurns(option.MaxTurns),
		agent.WithSystemPrompt(systemPrompt),
		agent.WithModel(resolvedModel(option)),
		agent.WithStream(true),
		agent.WithLogger(logger),
	)
	if err != nil {
		return err
	}
	if result != "" {
		printResultBlock("final_report", result)
	}
	return nil
}

func runDirectScannerMode(ctx context.Context, option *Option, rest []string) error {
	logger := commandLogger(option)
	features, scannerArgs, err := directScannerRuntimeFeatures(rest)
	if err != nil {
		return err
	}
	if option.AI {
		features.ProviderEnabled = true
		features.ProviderOptional = false
		features.ToolsEnabled = true
	}
	if isScannerHelpRequest(scannerArgs) {
		if usage, ok := staticScannerUsage(scannerArgs[0]); ok {
			fmt.Print(usage)
			if !strings.HasSuffix(usage, "\n") {
				fmt.Println()
			}
			return nil
		}
	}
	application, err := app.New(ctx, appConfig(option, features))
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}
	defer application.Close()
	applyResolvedProviderOptions(option, application.ProviderConfig)

	if !application.Scanner.Has(scannerArgs[0]) {
		return fmt.Errorf("unknown subcommand: %s", scannerArgs[0])
	}
	if option.AI && scannerArgs[0] != "scan" {
		return runScannerAgentMode(ctx, option, application, scannerArgs, logger)
	}
	var stream io.Writer
	if option.NoColor && scannerArgs[0] == "scan" && !hasScannerFlag(scannerArgs[1:], "--no-color") {
		scannerArgs = append(scannerArgs, "--no-color")
	}
	if shouldStreamScannerOutput(scannerArgs) {
		stream = os.Stdout
	}
	out, err := application.Scanner.ExecuteArgsStreaming(ctx, scannerArgs, stream)
	if err != nil {
		return err
	}
	fmt.Print(out)
	if option.AI {
		result, err := runScannerAIProcess(ctx, option, application, scannerArgs, out, logger)
		if err != nil {
			return err
		}
		if strings.TrimSpace(result) != "" {
			printResultBlock("ai_result", result)
		}
	}
	return nil
}

func runScannerAgentMode(ctx context.Context, option *Option, application *app.App, scannerArgs []string, logger telemetry.Logger) error {
	if application.Provider == nil {
		return fmt.Errorf("--ai requires a configured LLM provider")
	}

	toolReg := application.Tools
	if toolReg == nil {
		toolReg = tool.NewToolRegistry()
	}
	for _, t := range results.NewTools() {
		toolReg.Register(t)
	}

	command := scannerArgs[0]
	intent, err := resolveScannerAIIntent(option, application.Skills, command)
	if err != nil {
		return err
	}
	prompt := buildScannerAgentTaskPrompt(scannerArgs, intent)

	systemPrompt := agent.BuildSystemPrompt(&agent.PromptConfig{
		Tools:            toolReg,
		ScannerDocs:      application.Scanner.UsageDocs(),
		Skills:           application.Skills.Skills,
		ScannerAgentMode: true,
		ScannerName:      command,
	})

	maxTurns := option.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 20
	}

	logger.Debugf("system prompt length: %d chars", len(systemPrompt))
	logger.Importantf("agent mode=scanner scanner=%s status=starting max_turns=%d", command, maxTurns)
	logger.Importantf("agent task=%q", prompt)

	result, err := agent.Run(ctx, prompt, toolReg,
		agent.WithProvider(application.Provider),
		agent.WithMaxTurns(maxTurns),
		agent.WithSystemPrompt(systemPrompt),
		agent.WithModel(resolvedModel(option)),
		agent.WithStream(true),
		agent.WithLogger(logger),
	)
	if err != nil {
		return err
	}
	if result != "" {
		printResultBlock("agent_result", result)
	}
	return nil
}

func buildScannerAgentTaskPrompt(scannerArgs []string, intent string) string {
	command := strings.Join(scannerArgs, " ")
	if strings.TrimSpace(intent) == "" {
		return fmt.Sprintf("Execute: %s", command)
	}
	return fmt.Sprintf("Execute: %s\n\nUser intent: %s", command, strings.TrimSpace(intent))
}

func runScannerAIProcess(ctx context.Context, option *Option, application *app.App, scannerArgs []string, output string, logger telemetry.Logger) (string, error) {
	if application.Provider == nil {
		return "", fmt.Errorf("--ai requires a configured LLM provider")
	}
	if len(scannerArgs) == 0 {
		return "", nil
	}
	command := scannerArgs[0]
	intent, err := resolveScannerAIIntent(option, application.Skills, command)
	if err != nil {
		return "", err
	}
	timeout := resolvedDefaultInt(DefaultVerifyTimeout, 120)
	processCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	return agent.Run(processCtx, buildScannerAIProcessPrompt(command, scannerArgs[1:], intent, output), tool.NewToolRegistry(),
		agent.WithProvider(application.Provider),
		agent.WithModel(resolvedModel(option)),
		agent.WithMaxTurns(resolvedDefaultInt(DefaultVerifyTurns, 3)),
		agent.WithMaxTokens(1600),
		agent.WithSystemPrompt(scannerAIProcessSystemPrompt(command)),
		agent.WithLogger(telemetry.NopLogger()),
	)
}

func printResultBlock(name, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	fmt.Printf("\n[%s]\n%s\n", name, content)
}

func resolveScannerAIIntent(option *Option, store *skills.Store, command string) (string, error) {
	var sections []string
	if skill, ok := store.ByName(scannerAISkillName(command)); ok {
		sections = append(sections, skills.FormatInvocation(skill, ""))
	}

	intent := strings.TrimSpace(option.Prompt)
	if intent == "" && option.TaskFile != "" {
		data, err := os.ReadFile(option.TaskFile)
		if err != nil {
			return "", fmt.Errorf("read task file: %w", err)
		}
		intent = strings.TrimSpace(string(data))
	}
	if intent == "" {
		intent = defaultScannerAIIntent(command)
	}
	intent, err := applySelectedSkills(intent, scannerUserSkills(option.Skills, command), store)
	if err != nil {
		return "", err
	}
	sections = append(sections, intent)
	return strings.Join(sections, "\n\n"), nil
}

func scannerAISkillName(command string) string {
	switch command {
	case "gogo", "spray", "zombie", "neutron", "scan":
		return command
	default:
		return ""
	}
}

func scannerUserSkills(selected []string, command string) []string {
	auto := scannerAISkillName(command)
	if auto == "" {
		return selected
	}
	out := make([]string, 0, len(selected))
	for _, name := range selected {
		if strings.TrimSpace(name) == auto {
			continue
		}
		out = append(out, name)
	}
	return slices.Clip(out)
}

func defaultScannerAIIntent(command string) string {
	return "Process the scanner output according to the user's intent. If no specific intent is provided, briefly explain the important evidence in the output."
}

func buildScannerAIProcessPrompt(command string, args []string, intent, output string) string {
	return fmt.Sprintf(`User intent:
%s

Scanner command:
%s %s

Scanner output:
%s

Use the embedded scanner-output description to interpret the data, then follow the user intent.
`, strings.TrimSpace(intent), command, strings.Join(args, " "), scannerOutputForPrompt(output))
}

func scannerAIProcessSystemPrompt(command string) string {
	return "You are aiscan's scanner-output processor. Follow the supplied tool capability description and user intent. Use the scanner output as evidence and do not invent unsupported facts."
}

func scannerOutputForPrompt(output string) string {
	output = strings.TrimSpace(output)
	const maxPromptOutput = 60000
	if len(output) <= maxPromptOutput {
		return output
	}
	return output[:maxPromptOutput] + "\n... (scanner output truncated)"
}

func isScannerHelpRequest(args []string) bool {
	if len(args) < 2 {
		return false
	}
	for _, arg := range args[1:] {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func staticScannerUsage(name string) (string, bool) {
	switch name {
	case "scan":
		return scan.Usage(), true
	case "gogo":
		return "gogo - host, port, service, and banner discovery\nUsage: gogo [options]\n", true
	case "spray":
		return "spray - web probing, fingerprints, common files, and crawl checks\nUsage: spray [options]\n", true
	case "zombie":
		return "zombie - weak credential checks for supported services\nUsage: zombie [options]\n", true
	case "neutron":
		return "neutron - POC/vulnerability testing with nuclei-style options\nUsage: neutron -u <target> [options]\n", true
	default:
		return "", false
	}
}

func directScannerRuntimeFeatures(rest []string) (runtimeFeatures, []string, error) {
	if len(rest) == 0 {
		return runtimeFeatures{}, nil, fmt.Errorf("missing scanner command")
	}
	if rest[0] != "scan" {
		return runtimeFeatures{}, rest, nil
	}
	verifyMode, explicit := scannerVerifyMode(rest[1:])
	switch verifyMode {
	case "auto":
		return runtimeFeatures{
			ProviderEnabled:     true,
			ProviderOptional:    true,
			VerificationEnabled: true,
			VerifyMinPriority:   "high",
		}, removeScannerFlag(rest, "--verify"), nil
	case "off":
		return runtimeFeatures{}, replaceOrAppendScannerFlag(rest, "--verify", "off"), nil
	case "low", "medium", "high", "critical":
		return runtimeFeatures{
			ProviderEnabled:     true,
			VerificationEnabled: true,
			VerifyMinPriority:   verifyMode,
		}, rest, nil
	default:
		if explicit {
			return runtimeFeatures{}, nil, fmt.Errorf("invalid --verify value %q: expected auto, off, low, medium, high, or critical", verifyMode)
		}
		return runtimeFeatures{
			ProviderEnabled:     true,
			ProviderOptional:    true,
			VerificationEnabled: true,
			VerifyMinPriority:   "high",
		}, rest, nil
	}
}

func scannerVerifyMode(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, hasValue := strings.Cut(arg, "=")
		if key != "--verify" {
			continue
		}
		if hasValue {
			return strings.ToLower(strings.TrimSpace(value)), true
		}
		if i+1 < len(args) {
			return strings.ToLower(strings.TrimSpace(args[i+1])), true
		}
		return "", true
	}
	return resolvedDefaultVerify(), false
}

func replaceOrAppendScannerFlag(args []string, flag, value string) []string {
	out := append([]string(nil), args...)
	for i := 1; i < len(out); i++ {
		arg := out[i]
		key, _, hasValue := strings.Cut(arg, "=")
		if key != flag {
			continue
		}
		if hasValue {
			out[i] = flag + "=" + value
			return out
		}
		if i+1 < len(out) {
			out[i+1] = value
			return out
		}
		out = append(out, value)
		return out
	}
	return append(out, flag+"="+value)
}

func removeScannerFlag(args []string, flag string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, _, hasValue := strings.Cut(arg, "=")
		if key != flag {
			out = append(out, arg)
			continue
		}
		if !hasValue && i+1 < len(args) {
			i++
		}
	}
	return out
}

func runLoop(ctx context.Context, option *Option) error {
	logger := commandLogger(option)
	acpURL := resolvedACPURL(option)
	if acpURL == "" {
		acpURL = "http://127.0.0.1:8765"
	}
	cfg := appConfig(option, runtimeFeatures{
		ProviderEnabled:     true,
		ToolsEnabled:        true,
		VerificationEnabled: true,
		VerifyMinPriority:   "high",
	})
	cfg.ACP = &app.ACPConfig{
		URL:           acpURL,
		NodeID:        resolvedACPNodeID(option),
		NodeName:      resolvedACPNodeName(option),
		RegisterTools: true,
		AutoRegister:  false,
	}
	application, err := app.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}
	defer application.Close()
	applyResolvedProviderOptions(option, application.ProviderConfig)
	for _, diagnostic := range application.SkillDiagnostics {
		logger.Warnf("skill %s: %s", diagnostic.Path, diagnostic.Message)
	}

	intent := strings.TrimSpace(option.Prompt)
	if intent != "" && len(option.Inputs) > 0 {
		intent = fmt.Sprintf("%s\n\nTargets:\n%s", intent, formatInputs(option.Inputs))
	}
	rawPrompt := intent
	intent, err = applySelectedSkills(intent, option.Skills, application.Skills)
	if err != nil {
		return err
	}

	systemPrompt := agent.BuildSystemPrompt(&agent.PromptConfig{
		Tools:       application.Tools,
		ScannerDocs: application.Scanner.UsageDocs(),
		Skills:      application.Skills.Skills,
	})

	streamClient, ok := application.ACPStreamClient.(acpclient.StreamAPI)
	if !ok || streamClient == nil {
		return fmt.Errorf("loop requires streaming ACP client")
	}
	runner := loop.New(loop.Config{
		Client:           streamClient,
		Provider:         application.Provider,
		Tools:            application.Tools,
		SystemPrompt:     systemPrompt,
		Model:            resolvedModel(option),
		MaxTurns:         option.MaxTurns,
		Stream:           true,
		NodeName:         defaultACPNodeName(option),
		SpaceName:        resolvedSpace(option),
		SpaceDescription: "aiscan loop worker",
		PollInterval:     2 * time.Second,
		Prompt:           rawPrompt,
		Intent:           intent,
		Skills:           option.Skills,
		Logger:           logger,
	})
	return runner.Run(ctx)
}

func runACPServe(ctx context.Context, option *Option) error {
	return servercmd.Run(ctx, servercmd.Options{
		URL:     option.ACPURL,
		DB:      option.ACPDB,
		Timeout: option.Timeout,
		Debug:   option.Debug,
		Quiet:   option.Quiet,
	})
}

func registerACPTools(ctx context.Context, application *app.App, option *Option) error {
	acpURL := resolvedACPURL(option)
	if acpURL == "" {
		return nil
	}
	cfg := app.ACPConfig{
		URL:           acpURL,
		NodeID:        resolvedACPNodeID(option),
		NodeName:      resolvedACPNodeName(option),
		RegisterTools: true,
		AutoRegister:  true,
		NodeMeta:      map[string]any{"client": "aiscan"},
	}
	if cfg.NodeName == "" {
		cfg.NodeName = defaultACPNodeName(option)
	}
	return application.InitACP(ctx, cfg)
}

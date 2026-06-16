package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/evaluator"
	inboxpkg "github.com/chainreactors/aiscan/pkg/agent/inbox"
	tmuxpkg "github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/app"
	cmdpkg "github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/core/eventbus"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/toolargs"
	"github.com/chainreactors/aiscan/pkg/tui"
	"github.com/chainreactors/aiscan/skills"
)

// ---------------------------------------------------------------------------
// AgentRuntime — unified factory for all agent execution modes
// ---------------------------------------------------------------------------

type AgentRuntime struct {
	App          *app.App
	SystemPrompt string
	Config       agent.Config
	Bus          *eventbus.Bus[agent.Event]
	Output       *tui.AgentOutput
	ownsApp      bool
	cleanup      func()
}

type RuntimeConfig struct {
	ExistingApp  *app.App
	IOA          *app.IOAConfig
	PromptConfig *PromptConfig
	NoOutput     bool
}

func NewAgentRuntime(ctx context.Context, option *cfg.Option, logger telemetry.Logger, rc *RuntimeConfig) (*AgentRuntime, error) {
	rt := &AgentRuntime{}

	if rc != nil && rc.ExistingApp != nil {
		rt.App = rc.ExistingApp
	} else {
		appCfg := cfg.AppConfig(option, cfg.RuntimeFeatures{
			ProviderEnabled: true,
			ToolsEnabled:    true,
			AIEnabled:       true,
		}, logger)
		if rc != nil && rc.IOA != nil {
			appCfg.IOA = rc.IOA
		}
		application, err := app.New(ctx, appCfg)
		if err != nil {
			return nil, fmt.Errorf("init app: %w", err)
		}
		rt.App = application
		rt.ownsApp = true
		cfg.ApplyResolvedProviderOptions(option, application.ProviderConfig)

		for _, d := range application.SkillDiagnostics {
			logger.Warnf("skill %s: %s", d.Path, d.Message)
		}

		if rc == nil || rc.IOA == nil {
			if err := registerIOATools(ctx, application, option); err != nil {
				application.Close()
				return nil, fmt.Errorf("init ioa tools: %w", err)
			}
		}
	}

	pc := &PromptConfig{
		Tools:       rt.App.Commands,
		ScannerDocs: rt.App.Commands.UsageDocs(),
		Skills:      rt.App.Skills.Skills,
		NodeName:    ResolveIOANodeName(option),
		Space:       option.Space,
	}
	for _, name := range option.Skills {
		body := rt.App.Skills.ReadBody(name)
		if body == "" {
			body = skills.ReadFile("skills/" + name + ".md")
		}
		if body == "" {
			body = skills.ReadFile(name)
		}
		if body != "" {
			pc.LoadedSkills = append(pc.LoadedSkills, LoadedSkill{Name: name, Body: body})
		}
	}
	if rc != nil && rc.PromptConfig != nil {
		pc = rc.PromptConfig
	}
	rt.SystemPrompt = BuildSystemPrompt(pc, nil)
	logger.Debugf("system prompt length: %d chars", len(rt.SystemPrompt))

	if rc == nil || !rc.NoOutput {
		rt.Output = tui.NewAgentOutput(option)
	}

	agentBus := eventbus.New[agent.Event]()
	if rt.Output != nil {
		agentBus.Subscribe(rt.Output.HandleEvent)
	}
	var eventsCloser func()
	if eventsPath := os.Getenv("AISCAN_EVENTS_FILE"); eventsPath != "" {
		w, err := newEventsFileSubscriber(eventsPath)
		if err != nil {
			logger.Warnf("events file: %s", err)
		} else {
			unsub := agentBus.Subscribe(w.HandleEvent)
			eventsCloser = func() { unsub(); w.Close() }
		}
	}
	rt.Bus = agentBus

	ib := inboxpkg.NewBuffered(agent.DefaultInboxCapacity)

	sessMgr, bashTool := bashToolAndManager(rt.App.Commands)
	if bashTool != nil {
		bashTool.SetInbox(ib)
	}
	if sessMgr != nil {
		sessMgr.SetOnDone(func(info tmuxpkg.Info) {
			tail := sessMgr.PeekOrEmpty(info.ID, 20)
			msg := inboxpkg.NewMessage(inboxpkg.OriginSession, "user",
				tmuxpkg.FormatCompletion(info, tail))
			msg.Meta = map[string]any{
				"session_id":   info.ID,
				"session_name": info.Name,
				"exit_code":    info.ExitCode,
			}
			if err := ib.Push(msg); err != nil {
				logger.Warnf("inbox push session completion: %s", err)
			}
		})
	}

	scheduler := agent.NewLoopScheduler(ib, logger)

	if option.Heartbeat > 0 {
		_ = scheduler.Add(ctx, agent.LoopEntry{
			Name:     "heartbeat",
			Interval: time.Duration(option.Heartbeat) * time.Minute,
			Mode:     agent.ModeInbox,
			Prompt:   "Heartbeat: review current context, check on any running sessions, and decide if action is needed.",
		})
	}

	rt.Config = agent.Config{
		Provider:       rt.App.Provider,
		Fallbacks:      rt.App.ProviderFallbacks,
		Tools:          rt.App.Commands,
		Model:          option.Model,
		Logger:         logger,
		Inbox:          ib,
		LoopScheduler:  scheduler,
		CacheRetention: agent.CacheShort,
		Bus:            agentBus,
	}

	rt.App.Commands.RegisterTool(agent.NewLoopTool(scheduler))

	parentAgent := agent.NewAgent(rt.Config)
	subAgentTool := agent.NewSubAgentTool(parentAgent, ib, func(name string) (agent.AgentType, error) {
		if rt.App.Skills == nil {
			return agent.AgentType{}, fmt.Errorf("agent type %q not found", name)
		}
		s, ok := rt.App.Skills.ByName(name)
		if !ok {
			return agent.AgentType{}, fmt.Errorf("agent type %q not found", name)
		}
		if !s.Agent {
			return agent.AgentType{}, fmt.Errorf("skill %q is not configured as an agent type", name)
		}
		return agent.AgentType{
			FormattedPrompt: rt.App.Skills.FormatInvocation(s, ""),
			Model:           s.AgentModel,
			Background:      s.AgentBackground,
		}, nil
	})
	rt.App.Commands.RegisterTool(subAgentTool)

	rt.cleanup = func() {
		scheduler.Stop()
		if sessMgr != nil {
			sessMgr.Shutdown()
		}
		if eventsCloser != nil {
			eventsCloser()
		}
	}

	return rt, nil
}

func (rt *AgentRuntime) Close() {
	if rt.cleanup != nil {
		rt.cleanup()
	}
	if rt.ownsApp && rt.App != nil {
		rt.App.Close()
	}
}


// ---------------------------------------------------------------------------
// Mode dispatch
// ---------------------------------------------------------------------------

func RunAgentMode(ctx context.Context, option *cfg.Option, logger telemetry.Logger, setInterrupt ...func(func() bool)) error {
	if option.Loop {
		return runLoop(ctx, option, logger)
	}
	var si func(func() bool)
	if len(setInterrupt) > 0 {
		si = setInterrupt[0]
	}
	if !cfg.HasAgentOneShotInput(option) {
		return runInteractiveMode(ctx, option, logger, si)
	}
	return runOneShotMode(ctx, option, logger)
}

// ---------------------------------------------------------------------------
// Agent one-shot
// ---------------------------------------------------------------------------

func runOneShotMode(ctx context.Context, option *cfg.Option, logger telemetry.Logger) error {
	task, err := cfg.ResolveTask(option)
	if err != nil {
		return err
	}

	rt, err := NewAgentRuntime(ctx, option, logger, nil)
	if err != nil {
		return err
	}
	defer rt.Close()

	task = skills.ExpandCommand(task, rt.App.Skills)
	task, err = cfg.ApplySelectedSkills(task, option.Skills, rt.App.Skills)
	if err != nil {
		return err
	}

	rt.Output.Start("task", task)

	a := agent.NewAgent(rt.Config.
		WithSystemPrompt(rt.SystemPrompt).
		WithStream(false))

	var result *agent.Result
	if option.EvalCriteria != "" {
		evalCfg := buildGoalEvalConfig(option, rt, logger, task)
		result, _, err = evaluator.RunWithGoalEval(ctx, a, evalCfg)
	} else {
		result, err = a.Run(ctx, task)
	}
	if err != nil {
		return err
	}
	if result != nil && strings.TrimSpace(result.Output) != "" {
		rt.Output.Final(result.Output)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Agent interactive (REPL)
// ---------------------------------------------------------------------------

func runInteractiveMode(ctx context.Context, option *cfg.Option, logger telemetry.Logger, setInterrupt func(func() bool)) error {
	rt, err := NewAgentRuntime(ctx, option, logger, nil)
	if err != nil {
		return err
	}
	defer rt.Close()

	if _, err := cfg.ApplySelectedSkills("", option.Skills, rt.App.Skills); err != nil {
		return err
	}

	session := agent.NewAgent(rt.Config.
		WithSystemPrompt(rt.SystemPrompt).
		WithStream(tui.AgentStreamingEnabled(option)))

	repl := tui.NewAgentConsole(ctx, option, rt.App, session, rt.Output, rt.Bus)
	if setInterrupt != nil {
		setInterrupt(repl.InterruptCurrentRun)
	}
	return repl.Start()
}

// ---------------------------------------------------------------------------
// Agent loop (IOA swarm worker)
// ---------------------------------------------------------------------------

func runLoop(ctx context.Context, option *cfg.Option, logger telemetry.Logger) error {
	ioaURL := option.IOAURL
	if ioaURL == "" {
		ioaURL = "http://127.0.0.1:8765"
	}

	rt, err := NewAgentRuntime(ctx, option, logger, &RuntimeConfig{
		NoOutput: true,
		IOA: &app.IOAConfig{
			URL:           ioaURL,
			NodeID:        option.IOANodeID,
			NodeName:      option.IOANodeName,
			Space:         option.Space,
			RegisterTools: true,
			AutoRegister:  true,
		},
	})
	if err != nil {
		return err
	}
	defer rt.Close()

	prompt := strings.TrimSpace(option.Prompt)
	if prompt != "" && len(option.Inputs) > 0 {
		prompt = fmt.Sprintf("%s\n\nTargets:\n%s", prompt, cfg.FormatInputs(option.Inputs))
	}

	loopCfg := rt.Config.WithSystemPrompt(rt.SystemPrompt).WithStream(true)
	_, err = agent.NewAgent(loopCfg).Run(ctx, prompt)
	return err
}

// ---------------------------------------------------------------------------
// Scanner direct execution
// ---------------------------------------------------------------------------

func RunDirectScannerMode(ctx context.Context, option *cfg.Option, rest []string, logger telemetry.Logger) error {
	features, scannerArgs, err := DirectScannerRuntimeFeatures(rest)
	if err != nil {
		return err
	}
	if features.Warning != "" && !option.Quiet {
		fmt.Fprintf(os.Stderr, "warning: %s\n", features.Warning)
	}
	if option.AI || features.ScannerAI {
		features.ProviderEnabled = true
		features.ProviderOptional = false
		features.ToolsEnabled = true
		features.AIEnabled = true
	}
	if cfg.IsScannerHelpRequest(scannerArgs) {
		if usage, ok := cfg.StaticScannerUsage(scannerArgs[0]); ok {
			fmt.Print(usage)
			if !strings.HasSuffix(usage, "\n") {
				fmt.Println()
			}
			return nil
		}
	}

	scannerLogger := logger
	if !directScannerDebugEnabled(option, scannerArgs) {
		scannerLogger = telemetry.ErrorOnlyLogger(logger)
		restoreLogs := telemetry.SuppressGlobalNonErrors()
		defer restoreLogs()
	}

	application, err := app.New(ctx, cfg.AppConfig(option, features, scannerLogger))
	if err != nil {
		return fmt.Errorf("init app: %w", err)
	}
	defer application.Close()
	if err := application.WaitEngines(ctx); err != nil {
		return fmt.Errorf("engine init: %w", err)
	}
	cfg.ApplyResolvedProviderOptions(option, application.ProviderConfig)

	if !application.Commands.Has(scannerArgs[0]) {
		return fmt.Errorf("unknown subcommand: %s", scannerArgs[0])
	}
	if option.Debug && scannerCommandSupportsDebug(scannerArgs[0]) && !toolargs.BoolFlagEnabled(scannerArgs[1:], "--debug") {
		scannerArgs = append(scannerArgs, "--debug")
	}

	if option.AI && scannerArgs[0] != "scan" {
		return RunScannerWithAgent(ctx, option, application, scannerArgs, logger)
	}

	if option.NoColor && scannerArgs[0] == "scan" && !HasScannerFlag(scannerArgs[1:], "--no-color") {
		scannerArgs = append(scannerArgs, "--no-color")
	}
	var stream io.Writer
	if ShouldStreamScannerOutput(scannerArgs) {
		stream = os.Stdout
	}
	out, err := application.Commands.ExecuteArgsStreaming(ctx, scannerArgs, stream)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

func directScannerDebugEnabled(option *cfg.Option, scannerArgs []string) bool {
	if option != nil && option.Debug {
		return true
	}
	if len(scannerArgs) == 0 || !scannerCommandSupportsDebug(scannerArgs[0]) {
		return false
	}
	return toolargs.BoolFlagEnabled(scannerArgs[1:], "--debug")
}

func scannerCommandSupportsDebug(name string) bool {
	switch name {
	case "scan", "gogo", "spray", "zombie", "neutron":
		return true
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// Goal evaluation
// ---------------------------------------------------------------------------

func buildGoalEvalConfig(option *cfg.Option, rt *AgentRuntime, logger telemetry.Logger, task string) evaluator.GoalLoopConfig {
	model := option.Model
	if option.EvalModel != "" {
		model = option.EvalModel
	}
	maxRounds := option.EvalMaxRetries
	if maxRounds <= 0 {
		maxRounds = 3
	}
	return evaluator.GoalLoopConfig{
		Evaluator: evaluator.New(evaluator.Config{
			Provider: rt.App.Provider,
			Model:    model,
			Logger:   logger,
		}),
		MaxEvalRounds: maxRounds,
		Goal:          task,
		Criteria:      option.EvalCriteria,
		Bus:           rt.Bus,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func registerIOATools(ctx context.Context, application *app.App, option *cfg.Option) error {
	ioaURL := option.IOAURL
	if ioaURL == "" {
		return nil
	}
	ioaCfg := app.IOAConfig{
		URL:           ioaURL,
		NodeID:        option.IOANodeID,
		NodeName:      option.IOANodeName,
		Space:         option.Space,
		RegisterTools: true,
		AutoRegister:  true,
		NodeMeta:      map[string]any{"client": "aiscan"},
	}
	if ioaCfg.NodeName == "" {
		ioaCfg.NodeName = ResolveIOANodeName(option)
	}
	return application.InitIOA(ctx, ioaCfg)
}

func bashToolAndManager(reg interface {
	GetTool(string) (cmdpkg.AgentTool, bool)
}) (*tmuxpkg.Manager, *cmdpkg.BashTool) {
	if reg == nil {
		return nil, nil
	}
	tool, ok := reg.GetTool("bash")
	if !ok {
		return nil, nil
	}
	bt, ok := tool.(*cmdpkg.BashTool)
	if !ok {
		return nil, nil
	}
	return bt.Manager(), bt
}

package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	inboxpkg "github.com/chainreactors/aiscan/pkg/agent/inbox"
	tmuxpkg "github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/app"
	cmdpkg "github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/eventbus"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/toolargs"
	"github.com/chainreactors/aiscan/skills"
	"github.com/chainreactors/ioa/protocols"
)

// ---------------------------------------------------------------------------
// AgentRuntime — unified factory for all agent execution modes
// ---------------------------------------------------------------------------

type AgentRuntime struct {
	App          *app.App
	SystemPrompt string
	Config       agent.Config
	Bus          *eventbus.Bus[agent.Event]
	Output       *AgentOutput
	// IOAInitErr holds a non-fatal IOA init failure when the runtime was
	// constructed in standalone-tolerant mode (AllowStandaloneIOA). Callers
	// that want to surface it (e.g. the interactive REPL banner) can read it;
	// nil means IOA came up cleanly.
	IOAInitErr error
	ownsApp    bool
	cleanup    func()
}

type RuntimeConfig struct {
	ExistingApp  *app.App
	IOA          *app.IOAConfig
	PromptConfig *PromptConfig
	NoOutput     bool
	// AgentLogger overrides the logger used inside the agent loop/scheduler.
	// Runtime/application initialization still uses the top-level logger so
	// warnings remain visible, while UI modes can silence internal status lines.
	AgentLogger telemetry.Logger
	// AllowStandaloneIOA makes an IOA init failure non-fatal: the runtime is
	// returned without IOA tools instead of erroring out. Intended for the
	// interactive REPL, where a dead remote IOA server must not kill the local
	// agent. The failure is exposed via AgentRuntime.IOAInitErr.
	AllowStandaloneIOA bool
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
				if rc != nil && rc.AllowStandaloneIOA {
					// Degrade gracefully: keep going without IOA (swarm/collab)
					// tools instead of tearing down the whole runtime. The
					// interactive REPL surfaces this via IOAInitErr; a dead
					// remote server must not kill the local agent.
					rt.IOAInitErr = err
					logger.Warnf("ioa unavailable, running standalone: %s", err)
				} else {
					application.Close()
					return nil, fmt.Errorf("init ioa tools: %w", err)
				}
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
		pcCopy := *rc.PromptConfig
		pc = &pcCopy
	}

	if rc == nil || !rc.NoOutput {
		rt.Output = NewAgentOutput(option)
	}
	agentLogger := logger
	if rc != nil && rc.AgentLogger != nil {
		agentLogger = rc.AgentLogger
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

	sessMgr := bashSessionManager(rt.App.Commands)
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

	scheduler := agent.NewLoopScheduler(ib, agentLogger)

	rt.Config = agent.Config{
		Provider:       rt.App.Provider,
		Tools:          rt.App.Commands,
		Model:          option.Model,
		Logger:         agentLogger,
		Inbox:          ib,
		LoopScheduler:  scheduler,
		CacheRetention: agent.CacheShort,
		Bus:            agentBus,
	}
	rt.Config = agent.NewAgent(rt.Config).Cfg

	rt.App.Commands.RegisterTool(agent.NewLoopTool(scheduler))

	if pc.FindingsPath == "" {
		pc.FindingsPath = findingsLogPath(rt.Config.SessionID)
	}
	if err := initFindingsLog(pc.FindingsPath); err != nil {
		logger.Warnf("findings log %s: %s", pc.FindingsPath, err)
	}
	rt.SystemPrompt = BuildSystemPrompt(pc, &rt.Config)
	rt.Config.SystemPrompt = rt.SystemPrompt
	logger.Debugf("system prompt length: %d chars", len(rt.SystemPrompt))

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

func initFindingsLog(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return os.WriteFile(path, []byte("# aiscan findings\n\n"), 0600)
}

// ---------------------------------------------------------------------------
// Mode dispatch
// ---------------------------------------------------------------------------

func RunAgentMode(ctx context.Context, option *cfg.Option, logger telemetry.Logger) error {
	if option.Loop {
		return runLoop(ctx, option, logger)
	}
	if !cfg.HasAgentOneShotInput(option) {
		return runInteractiveMode(ctx, option, logger)
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

	rt, err := NewAgentRuntime(ctx, option, logger, &RuntimeConfig{
		AgentLogger: quietAgentStatusLogger(option, logger),
	})
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
	result, err := agent.NewAgent(rt.Config.
		WithSystemPrompt(rt.SystemPrompt).
		WithStream(agentStreamingEnabled(option))).
		Run(ctx, task)
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

func runInteractiveMode(ctx context.Context, option *cfg.Option, logger telemetry.Logger) error {
	// Interactive REPL: let the welcome banner be the first thing on screen.
	// The shared global logger defaults to a verbose level — chainreactors/logs
	// numbers WarnLevel=20 below InfoLevel=30, so WarnLevel surfaces Info too,
	// which dumps provider/engine "ready" lines over the banner. Drop to
	// error-only so only real failures surface; --debug keeps the full trace.
	if !option.Debug {
		restoreLogs := telemetry.SuppressGlobalNonErrors()
		defer restoreLogs()
	}
	rt, err := NewAgentRuntime(ctx, option, logger, &RuntimeConfig{
		AgentLogger:        quietAgentStatusLogger(option, logger),
		AllowStandaloneIOA: true,
	})
	if err != nil {
		return err
	}
	defer rt.Close()

	if _, err := cfg.ApplySelectedSkills("", option.Skills, rt.App.Skills); err != nil {
		return err
	}

	session := agent.NewAgent(rt.Config.
		WithSystemPrompt(rt.SystemPrompt).
		WithStream(agentStreamingEnabled(option)))

	// Reuse the runtime's bus-subscribed output so streaming deltas, tool
	// events, and the final render share one set of state (avoids a second
	// AgentOutput that would duplicate or fight the live stream).
	repl := NewAgentConsole(ctx, option, rt.App, session, rt.Output)
	if rt.IOAInitErr != nil {
		repl.startupNotice = fmt.Sprintf("ioa 不可用，已切换独立模式（无 swarm/协作工具）· %s", rt.IOAInitErr)
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
	if option.IOANodeName == "" {
		option.IOANodeName = ResolveIOANodeName(option)
	}

	initialTask, err := cfg.ResolveOptionalTask(option)
	if err != nil {
		return err
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

	prompt := initialTask
	if prompt != "" {
		prompt = skills.ExpandCommand(prompt, rt.App.Skills)
		prompt, err = cfg.ApplySelectedSkills(prompt, option.Skills, rt.App.Skills)
		if err != nil {
			return err
		}
	}

	if err := scheduleHeartbeat(ctx, rt.Config.LoopScheduler, option, initialTask, rt.App.IOAClient); err != nil {
		return err
	}

	loopCfg := rt.Config.WithSystemPrompt(rt.SystemPrompt).WithStream(true)
	_, err = agent.NewAgent(loopCfg).Run(ctx, prompt)
	return err
}

const heartbeatRecentMessageLimit = 50

func scheduleHeartbeat(ctx context.Context, scheduler *agent.LoopScheduler, option *cfg.Option, initialTask string, ioaClient protocols.ClientAPI) error {
	if option.Heartbeat < 0 {
		return fmt.Errorf("--heartbeat must be greater than or equal to 0")
	}
	if option.Heartbeat == 0 {
		return nil
	}
	if scheduler == nil {
		return fmt.Errorf("--heartbeat requires a loop scheduler")
	}
	if ioaClient == nil {
		return fmt.Errorf("--heartbeat requires an IOA client")
	}

	if err := scheduler.Add(ctx, agent.LoopEntry{
		Name:       "heartbeat",
		Interval:   time.Duration(option.Heartbeat) * time.Minute,
		Prompt:     heartbeatPromptPreview(option, initialTask),
		PromptFunc: heartbeatPromptFunc(option, initialTask, ioaClient),
		Mode:       agent.ModeInbox,
	}); err != nil {
		return fmt.Errorf("schedule heartbeat: %w", err)
	}
	return nil
}

func heartbeatPromptFunc(option *cfg.Option, initialTask string, ioaClient protocols.ClientAPI) func(context.Context, agent.LoopEntry) (string, error) {
	return func(ctx context.Context, _ agent.LoopEntry) (string, error) {
		return heartbeatPrompt(ctx, option, initialTask, ioaClient)
	}
}

func heartbeatPrompt(ctx context.Context, option *cfg.Option, initialTask string, ioaClient protocols.ClientAPI) (string, error) {
	if ioaClient == nil {
		return "", fmt.Errorf("IOA client is nil")
	}

	spaceName := heartbeatSpace(option)
	info, err := ioaClient.Space(ctx, spaceName, "aiscan heartbeat")
	if err != nil {
		return "", fmt.Errorf("read IOA space %q: %w", spaceName, err)
	}
	messages, err := ioaClient.Read(ctx, info.ID, protocols.ReadOptions{
		All:   true,
		Limit: heartbeatRecentMessageLimit,
	})
	if err != nil {
		return "", fmt.Errorf("read IOA messages for space %q: %w", info.Name, err)
	}

	return renderHeartbeatPrompt(option, initialTask, ioaClient.NodeID(), info, messages), nil
}

func heartbeatPromptPreview(option *cfg.Option, initialTask string) string {
	return heartbeatPromptHeader(option, initialTask, "", protocols.SpaceInfo{}, nil) +
		"\nRuntime reads IOA SpaceInfo and the latest messages before each heartbeat fire."
}

func renderHeartbeatPrompt(option *cfg.Option, initialTask, nodeID string, info protocols.SpaceInfo, messages []protocols.Message) string {
	var sb strings.Builder
	sb.WriteString(heartbeatPromptHeader(option, initialTask, nodeID, info, messages))
	sb.WriteString("\nSpace nodes:\n")
	if len(info.Nodes) == 0 {
		sb.WriteString("- none\n")
	} else {
		for _, node := range info.Nodes {
			sb.WriteString("- ")
			sb.WriteString(node.Name)
			if node.ID != "" {
				sb.WriteString(" (")
				sb.WriteString(node.ID)
				sb.WriteString(")")
			}
			if node.Description != "" {
				sb.WriteString(" desc=")
				sb.WriteString(node.Description)
			}
			if len(node.Meta) > 0 {
				sb.WriteString(" meta=")
				sb.WriteString(marshalJSON(node.Meta))
			}
			sb.WriteByte('\n')
		}
	}

	sb.WriteString("\nRecent messages (latest ")
	sb.WriteString(fmt.Sprint(heartbeatRecentMessageLimit))
	sb.WriteString("):\n")
	if len(messages) == 0 {
		sb.WriteString("- none\n")
	} else {
		for _, msg := range messages {
			sb.WriteString("- id=")
			sb.WriteString(msg.ID)
			if msg.CreatedAt != "" {
				sb.WriteString(" at=")
				sb.WriteString(msg.CreatedAt)
			}
			if msg.Sender != "" {
				sb.WriteString(" sender=")
				sb.WriteString(msg.Sender)
			}
			if len(msg.Refs.Nodes) > 0 || len(msg.Refs.Messages) > 0 {
				sb.WriteString(" refs=")
				sb.WriteString(marshalJSON(msg.Refs))
			}
			if msg.ContentType != "" {
				sb.WriteString(" content_type=")
				sb.WriteString(msg.ContentType)
			}
			sb.WriteString(" content=")
			sb.WriteString(marshalJSON(truncateMapValues(msg.Content, heartbeatFieldMaxLen)))
			sb.WriteByte('\n')
		}
	}

	sb.WriteString("\nDecide whether to dispatch work, reply, summarize, or stay idle. ")
	sb.WriteString("Use IOA tools for coordination and avoid repeating completed work.")
	return sb.String()
}

func heartbeatPromptHeader(option *cfg.Option, initialTask, nodeID string, info protocols.SpaceInfo, messages []protocols.Message) string {
	space := strings.TrimSpace(option.Space)
	if space == "" {
		space = "default"
	}

	var sb strings.Builder
	sb.WriteString("Heartbeat wake-up for aiscan agent --loop.\n")
	sb.WriteString("Space:\n")
	sb.WriteString("- name: ")
	if info.Name != "" {
		sb.WriteString(info.Name)
	} else {
		sb.WriteString(space)
	}
	sb.WriteByte('\n')
	if info.ID != "" {
		sb.WriteString("- id: ")
		sb.WriteString(info.ID)
		sb.WriteByte('\n')
	}
	if len(info.Tags) > 0 {
		sb.WriteString("- tags: ")
		sb.WriteString(strings.Join(info.Tags, ", "))
		sb.WriteByte('\n')
	}
	if info.MessageCount > 0 || messages != nil {
		sb.WriteString("- message_count: ")
		sb.WriteString(fmt.Sprint(info.MessageCount))
		sb.WriteByte('\n')
		sb.WriteString("- recent_messages_loaded: ")
		sb.WriteString(fmt.Sprint(len(messages)))
		sb.WriteByte('\n')
	}

	node := strings.TrimSpace(ResolveIOANodeName(option))
	if node != "" || nodeID != "" || len(option.Skills) > 0 {
		sb.WriteString("Current node:\n")
		if node != "" {
			sb.WriteString("- name: ")
			sb.WriteString(node)
			sb.WriteByte('\n')
		}
		if nodeID != "" {
			sb.WriteString("- id: ")
			sb.WriteString(nodeID)
			sb.WriteByte('\n')
		}
		if len(option.Skills) > 0 {
			sb.WriteString("- skills: ")
			sb.WriteString(strings.Join(option.Skills, ", "))
			sb.WriteByte('\n')
		}
	}

	task := strings.TrimSpace(initialTask)
	if task != "" {
		sb.WriteString("Initial task:\n")
		sb.WriteString(task)
		sb.WriteByte('\n')
	} else if len(option.Inputs) > 0 {
		sb.WriteString("Targets:\n")
		sb.WriteString(cfg.FormatInputs(option.Inputs))
		sb.WriteByte('\n')
	}

	return sb.String()
}

func heartbeatSpace(option *cfg.Option) string {
	space := strings.TrimSpace(option.Space)
	if space == "" {
		return "default"
	}
	return space
}

const heartbeatFieldMaxLen = 2000

func marshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

func truncateMapValues(m map[string]any, max int) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok && len(s) > max {
			out[k] = s[:max] + "...[truncated]"
		} else {
			out[k] = v
		}
	}
	return out
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
	cfg.ApplyResolvedProviderOptions(option, application.ProviderConfig)

	if !application.Commands.Has(scannerArgs[0]) {
		return fmt.Errorf("unknown subcommand: %s", scannerArgs[0])
	}
	if option.Debug && scannerCommandSupportsDebug(scannerArgs[0]) && !toolargs.BoolFlagEnabled(scannerArgs[1:], "--debug") {
		scannerArgs = append(scannerArgs, "--debug")
	}

	if (option.AI && scannerArgs[0] != "scan") || features.ScannerAI {
		if application.Provider == nil {
			if !features.ProviderOptional {
				return fmt.Errorf("--verify/--sniper/--deep requires a configured LLM provider")
			}
		} else {
			injectScanSubSkills(option, rest, scannerArgs)
			return RunScannerWithAgent(ctx, option, application, scannerArgs, logger)
		}
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
// Helpers
// ---------------------------------------------------------------------------

// ioaInitTimeout caps the synchronous IOA registration dial. InitIOA only does
// a couple of request/response round trips (register + optional space join) —
// not a long-lived stream — so a tight bound is safe. Without it the ioa client
// (http.DefaultClient, no Timeout) blocks on the OS TCP connect timeout, which
// is ~127s for a black-holing host and stalls the REPL banner the whole while.
// A healthy IOA registers in well under a second.
const ioaInitTimeout = 5 * time.Second

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
	ioaCtx, cancel := context.WithTimeout(ctx, ioaInitTimeout)
	defer cancel()
	return application.InitIOA(ioaCtx, ioaCfg)
}

func quietAgentStatusLogger(option *cfg.Option, logger telemetry.Logger) telemetry.Logger {
	if option != nil && option.Debug {
		return logger
	}
	return telemetry.SuppressImportantLogger(logger)
}

func bashSessionManager(reg interface {
	GetTool(string) (cmdpkg.AgentTool, bool)
}) *tmuxpkg.Manager {
	if reg == nil {
		return nil
	}
	tool, ok := reg.GetTool("bash")
	if !ok {
		return nil
	}
	bt, ok := tool.(*cmdpkg.BashTool)
	if !ok {
		return nil
	}
	return bt.Manager()
}

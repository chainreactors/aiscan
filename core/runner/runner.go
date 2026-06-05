package runner

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	inboxpkg "github.com/chainreactors/aiscan/pkg/agent/inbox"
	"github.com/chainreactors/aiscan/pkg/agent/provider"
	tmuxpkg "github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/app"
	cmdpkg "github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/eventbus"
	"github.com/chainreactors/aiscan/pkg/swarm"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/toolargs"
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
	Output       *AgentOutput
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
	if rc != nil && rc.PromptConfig != nil {
		pc = rc.PromptConfig
	}
	rt.SystemPrompt = BuildSystemPrompt(pc)
	logger.Debugf("system prompt length: %d chars", len(rt.SystemPrompt))

	if rc == nil || !rc.NoOutput {
		rt.Output = NewAgentOutput(option)
	}

	agentBus := eventbus.New[agent.Event]()
	if rt.Output != nil {
		agentBus.Subscribe(rt.Output.HandleEvent)
	}
	var eventsCloser func()
	if option.EventsFile != "" {
		w, err := newEventsFileSubscriber(option.EventsFile)
		if err != nil {
			if rt.ownsApp {
				rt.App.Close()
			}
			return nil, err
		}
		unsub := agentBus.Subscribe(w.HandleEvent)
		eventsCloser = func() { unsub(); w.Close() }
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

	scheduler := agent.NewLoopScheduler(ib, logger)

	rt.Config = agent.Config{
		Provider:       rt.App.Provider,
		Tools:          rt.App.Commands,
		Model:          option.Model,
		Logger:         logger,
		Inbox:          ib,
		LoopScheduler:  scheduler,
		CacheRetention: provider.CacheShort,
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
			FormattedPrompt: skills.FormatInvocation(s, ""),
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
	result, err := agent.NewAgent(rt.Config.
		WithSystemPrompt(rt.SystemPrompt).
		WithStream(false)).
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
		WithStream(false))

	repl := NewAgentConsole(ctx, option, rt.App, session)
	return repl.Start()
}

// ---------------------------------------------------------------------------
// Agent loop (IOA swarm worker)
// ---------------------------------------------------------------------------

func runLoop(ctx context.Context, option *cfg.Option, logger telemetry.Logger) error {
	if option.Heartbeat < 0 {
		return fmt.Errorf("--heartbeat must be greater than or equal to 0")
	}
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
			AutoRegister:  false,
		},
	})
	if err != nil {
		return err
	}
	defer rt.Close()

	streamClient := rt.App.IOAStreamClient
	if streamClient == nil {
		return fmt.Errorf("loop requires streaming IOA client")
	}

	rawPrompt := strings.TrimSpace(option.Prompt)
	if rawPrompt != "" && len(option.Inputs) > 0 {
		rawPrompt = fmt.Sprintf("%s\n\nTargets:\n%s", rawPrompt, cfg.FormatInputs(option.Inputs))
	}

	skillRefs, err := buildSkillRefs(option.Skills, rt.App.Skills)
	if err != nil {
		return err
	}

	loopCfg := rt.Config.WithSystemPrompt(rt.SystemPrompt).WithStream(true)
	runOnce := func(ctx context.Context, prompt string) (*agent.Result, error) {
		return agent.NewAgent(loopCfg).Run(ctx, prompt)
	}

	node := swarm.NewNode(swarm.NodeConfig{
		Client:                streamClient,
		NodeName:              ResolveIOANodeName(option),
		SpaceName:             option.Space,
		SpaceDescription:      "aiscan loop worker",
		PollInterval:          2 * time.Second,
		HeartbeatInterval:     time.Duration(option.Heartbeat) * time.Minute,
		HeartbeatContextLimit: 50,
		Prompt:                rawPrompt,
		Skills:                option.Skills,
		SkillRefs:             skillRefs,
		OnTask: func(ctx context.Context, st swarm.Task) (string, error) {
			result, err := runOnce(ctx, st.Prompt())
			if err != nil {
				return "", err
			}
			return result.Output, nil
		},
		OnPeer: func(peer swarm.PeerMessage) bool {
			return rt.Config.Inbox.Push(peerToInboxMessage(peer)) == nil
		},
		OnHeartbeat: func(ctx context.Context, prompt string) (string, error) {
			result, err := runOnce(ctx, prompt)
			if err != nil {
				return "", err
			}
			return result.Output, nil
		},
		Logger: logger,
	})

	if option.Heartbeat > 0 {
		_ = rt.Config.LoopScheduler.Add(ctx, agent.LoopEntry{
			Name:     "heartbeat",
			Interval: time.Duration(option.Heartbeat) * time.Minute,
			Mode:     agent.ModeIndependent,
			OnFire: func(fctx context.Context, e agent.LoopEntry) (string, error) {
				return "", node.RunHeartbeat(fctx)
			},
		})
	}

	return node.Run(ctx)
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
// Helpers
// ---------------------------------------------------------------------------

func buildSkillRefs(selected []string, store *skills.Store) ([]swarm.SkillRef, error) {
	if len(selected) == 0 {
		return nil, nil
	}
	refs := make([]swarm.SkillRef, 0, len(selected))
	seen := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		skill, ok := store.ByName(name)
		if !ok {
			return nil, fmt.Errorf("unknown skill %q", name)
		}
		seen[name] = struct{}{}
		refs = append(refs, swarm.SkillRef{
			Name:        skill.Name,
			Description: skill.Description,
			Location:    skill.Location,
		})
	}
	return refs, nil
}

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

func formatPeerForLLM(peer swarm.PeerMessage) string {
	var sb strings.Builder
	sb.WriteString("<swarm_peer")
	if peer.Sender != "" {
		writeXMLAttr(&sb, "sender", peer.Sender)
	}
	if peer.MessageID != "" {
		writeXMLAttr(&sb, "message_id", peer.MessageID)
	}
	sb.WriteString(">\n")
	_ = xml.EscapeText(&sb, []byte(peerPayload(peer)))
	sb.WriteString("\n</swarm_peer>")
	return sb.String()
}

func writeXMLAttr(sb *strings.Builder, name, value string) {
	sb.WriteByte(' ')
	sb.WriteString(name)
	sb.WriteString("=\"")
	_ = xml.EscapeText(sb, []byte(value))
	sb.WriteByte('"')
}

func peerPayload(peer swarm.PeerMessage) string {
	if strings.TrimSpace(peer.Content) != "" {
		return peer.Content
	}
	if len(peer.RawContent) == 0 {
		return ""
	}
	data, err := json.MarshalIndent(peer.RawContent, "", "  ")
	if err != nil {
		return fmt.Sprint(peer.RawContent)
	}
	return string(data)
}

func peerToInboxMessage(peer swarm.PeerMessage) inboxpkg.Message {
	msg := inboxpkg.NewMessage(inboxpkg.OriginPeer, "user", formatPeerForLLM(peer))
	msg.Meta = map[string]any{
		"sender":     peer.Sender,
		"message_id": peer.MessageID,
	}
	return msg
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

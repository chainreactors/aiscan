package runner

import (
	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	inboxpkg "github.com/chainreactors/aiscan/pkg/agent/inbox"
	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/app"
	tmuxpkg "github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

type AgentSession struct {
	Config  agent.Config
	cleanup func()
}

type SessionConfig struct {
	Application *app.App
	Option      *cfg.Option
	Logger      telemetry.Logger
	Events      *EventsWriter
}

func NewAgentSession(sc SessionConfig) *AgentSession {
	ib := inboxpkg.NewBuffered(64)

	sessMgr := bashSessionManager(sc.Application.Commands)
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
				sc.Logger.Warnf("inbox push session completion: %s", err)
			}
		})
	}

	scheduler := agent.NewLoopScheduler(ib, sc.Logger)

	agentCfg := agent.Config{
		Provider:       sc.Application.Provider,
		Tools:          sc.Application.Commands,
		Model:          sc.Option.Model,
		Logger:         sc.Logger,
		Inbox:          ib,
		LoopScheduler:  scheduler,
		CacheRetention: provider.CacheShort,
	}

	sc.Application.Commands.RegisterTool(agent.NewLoopTool(scheduler))

	subAgentTool := NewSubAgentTool(SubAgentConfig{
		ParentConfig: agentCfg,
		ParentInbox:  ib,
		SkillStore:   sc.Application.Skills,
	})
	sc.Application.Commands.RegisterTool(subAgentTool)

	if sc.Events != nil {
		agentCfg.Emit = sc.Events.HandleEvent
	}

	cleanup := func() {
		scheduler.Stop()
		if sessMgr != nil {
			sessMgr.Shutdown()
		}
	}

	return &AgentSession{Config: agentCfg, cleanup: cleanup}
}

func (s *AgentSession) Cleanup() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

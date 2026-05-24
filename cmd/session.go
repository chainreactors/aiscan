package cmd

import (
	"context"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent"
	inboxpkg "github.com/chainreactors/aiscan/pkg/agent/inbox"
	"github.com/chainreactors/aiscan/pkg/app"
	taskmod "github.com/chainreactors/aiscan/pkg/task"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

type agentSession struct {
	Config  agent.Config
	cleanup func()
}

type sessionConfig struct {
	Application *app.App
	Option      *Option
	Logger      telemetry.Logger
	Events      *eventsWriter
}

func newAgentSession(cfg sessionConfig) *agentSession {
	ib := inboxpkg.NewBuffered(64)

	taskMgr := bashTaskManager(cfg.Application.Commands)
	if taskMgr != nil {
		taskMgr.SetOnComplete(func(info taskmod.Info, killed bool, cause string) {
			msg := inboxpkg.NewMessage(inboxpkg.OriginTask, "user", taskmod.FormatCompletion(info, killed, cause))
			msg.Meta = map[string]any{"task_id": info.ID, "task_name": info.Name, "exit_code": info.ExitCode}
			ib.Push(msg)
		})
		taskMgr.StartReminder(30*time.Second, func(content string) {
			ib.Push(inboxpkg.NewMessage(inboxpkg.OriginTask, "user", content))
		})
	}

	cfg.Application.Commands.RegisterTool(NewSubAgentTool(SubAgentConfig{
		Base: agent.Config{
			Provider: cfg.Application.Provider,
			Tools:    cfg.Application.Commands,
			Model:    cfg.Option.Model,
			Logger:   cfg.Logger,
		},
		ParentInbox: ib,
		SkillStore:  cfg.Application.Skills,
	}))

	agentCfg := agent.Config{
		Provider: cfg.Application.Provider,
		Tools:    cfg.Application.Commands,
		Model:    cfg.Option.Model,
		Logger:   cfg.Logger,
		Inbox:    ib,
		ShouldWaitAfterTurn: func(_ context.Context, _ agent.ShouldWaitAfterTurnContext) (bool, error) {
			if taskMgr == nil {
				return false, nil
			}
			return taskMgr.RunningCount() > 0, nil
		},
	}
	if cfg.Events != nil {
		agentCfg.Emit = cfg.Events.HandleEvent
	}

	cleanup := func() {
		if taskMgr != nil {
			taskMgr.Shutdown()
		}
	}

	return &agentSession{Config: agentCfg, cleanup: cleanup}
}

func (s *agentSession) Cleanup() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

package cmd

import (
	"github.com/chainreactors/aiscan/pkg/agent"
	inboxpkg "github.com/chainreactors/aiscan/pkg/agent/inbox"
	"github.com/chainreactors/aiscan/pkg/app"
	taskmod "github.com/chainreactors/aiscan/pkg/task"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

type agentSession struct {
	Inbox   *inboxpkg.Buffered
	Opts    []agent.Option
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
	}

	cfg.Application.Commands.RegisterTool(NewSubAgentTool(SubAgentConfig{
		Provider:    cfg.Application.Provider,
		Tools:       cfg.Application.Commands,
		ParentInbox: ib,
		SkillStore:  cfg.Application.Skills,
		BaseOpts: []agent.Option{
			agent.WithProvider(cfg.Application.Provider),
			agent.WithModel(cfg.Option.Model),
			agent.WithLogger(cfg.Logger),
		},
	}))

	opts := []agent.Option{
		agent.WithProvider(cfg.Application.Provider),
		agent.WithModel(cfg.Option.Model),
		agent.WithLogger(cfg.Logger),
		agent.WithInbox(ib),
	}
	if cfg.Events != nil {
		opts = append(opts, agent.WithEventHandler(cfg.Events.HandleEvent))
	}

	cleanup := func() {
		if taskMgr != nil {
			taskMgr.ClearOnComplete()
		}
	}

	return &agentSession{Inbox: ib, Opts: opts, cleanup: cleanup}
}

func (s *agentSession) Cleanup() {
	if s.cleanup != nil {
		s.cleanup()
	}
}

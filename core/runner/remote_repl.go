package runner

import (
	"context"
	"fmt"
	"io"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/core/eventbus"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/tui"
	"github.com/chainreactors/utils/pty"
	rlterm "github.com/chainreactors/tui/readline/terminal"
)

func NewRemoteREPLOpener(rt *AgentRuntime, mgr *tmux.Manager) pty.OpenFunc {
	return func(ctx context.Context, spec pty.OpenSpec) (pty.OpenResult, error) {
		if rt == nil || rt.App == nil {
			return pty.OpenResult{}, fmt.Errorf("remote repl requires an agent runtime")
		}
		if mgr == nil {
			return pty.OpenResult{}, fmt.Errorf("pty manager unavailable")
		}
		option := rt.Option
		if option == nil {
			option = &cfg.Option{}
		}
		// Each REPL session emits onto its own bus so the console renders only
		// its own events (no cross-talk from concurrent chat sessions sharing
		// rt.Bus). Bridge that bus back into rt.Bus so agent.stats and any
		// connection-level forwarding still observe this session's activity.
		replBus := eventbus.New[agent.Event]()
		if rt.Bus != nil {
			replBus.Subscribe(rt.Bus.Emit)
		}
		session := agent.NewAgent(rt.Config.
			WithSystemPrompt(rt.SystemPrompt).
			WithStream(tui.AgentStreamingEnabled(option)).
			WithBus(replBus))
		appInfo := tui.AppInfo{
			Provider:          rt.App.Provider,
			ProviderConfig:    rt.App.ProviderConfig,
			ProviderFallbacks: rt.App.ProviderFallbacks,
			Commands:          rt.App.Commands,
			Skills:            rt.App.Skills,
			OnProviderChange: func(provider agent.Provider, providerConfig agent.ProviderConfig) {
				rt.App.Provider = provider
				rt.App.ProviderConfig = providerConfig
				rt.Config.Provider = provider
				rt.Config.Model = providerConfig.Model
			},
		}
		control := rlterm.NewControl(true, 80, 24)
		info, err := mgr.CreateInteractiveFunc(ctx, spec.Name, "aiscan remote repl", pty.DefaultSessionTimeout, false, func(replCtx context.Context, input io.Reader, output io.Writer) error {
			return tui.RunRemoteAgentConsoleWithControl(replCtx, option, appInfo, session, input, output, control, replBus)
		})
		if err != nil {
			return pty.OpenResult{}, err
		}
		mgr.SetKind(info.ID, "repl")
		info.Kind = "repl"
		return pty.OpenResult{
			Info: info,
			Resize: func(cols, rows int) {
				control.SetSize(cols, rows)
			},
		}, nil
	}
}

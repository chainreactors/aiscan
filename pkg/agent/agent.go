package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/chainreactors/aiscan/pkg/agent/inbox"
)

const continueNudgePrompt = "Continue."

type Agent struct {
	Cfg Config

	mu      sync.Mutex
	state   State
	running bool
}

// Run executes the agent with a prompt and returns the result.
// For one-shot usage, create an agent and call Run once.
// For multi-turn, call Run repeatedly — message history accumulates.
func (a *Agent) Run(ctx context.Context, prompt string) (*Result, error) {
	runCtx, cancel, err := a.startRun(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer a.finishRun()

	cfg := a.Cfg
	cfg = cfg.init()
	cfg.Messages = a.messagesSnapshot()
	if cfg.Inbox == nil {
		cfg.Inbox = inbox.NewBuffered(SubInboxCapacity)
	}
	if err := cfg.Inbox.Push(inbox.NewUserMessage(prompt)); err != nil {
		return nil, fmt.Errorf("push prompt: %w", err)
	}

	result, runErr := runLoop(runCtx, cfg)
	a.saveState(result, runErr)
	return result, runErr
}

// Continue resumes the agent without a new prompt (e.g. after tool results).
func (a *Agent) Continue(ctx context.Context) (*Result, error) {
	needsNudge, err := a.continueNeedsNudge()
	if err != nil {
		return nil, err
	}

	runCtx, cancel, err := a.startRun(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer a.finishRun()

	cfg := a.Cfg
	cfg = cfg.init()
	cfg.Messages = a.messagesSnapshot()
	if cfg.Inbox == nil {
		cfg.Inbox = inbox.NewBuffered(SubInboxCapacity)
	}
	if needsNudge {
		if err := cfg.Inbox.Push(inbox.NewUserMessage(continueNudgePrompt)); err != nil {
			return nil, fmt.Errorf("push continue prompt: %w", err)
		}
	}
	result, runErr := runLoop(runCtx, cfg)
	a.saveState(result, runErr)
	return result, runErr
}

// Derive creates a new Agent with the same infrastructure (provider, tools,
// model, logger) but clean state. Use for spawning independent agent tasks.
func (a *Agent) Derive() *Agent {
	return NewAgent(Config{
		Provider:       a.Cfg.Provider,
		Tools:          a.Cfg.Tools,
		Model:          a.Cfg.Model,
		Logger:         a.Cfg.Logger,
		MaxRetries:     a.Cfg.MaxRetries,
		Stream:         a.Cfg.Stream,
		Temperature:    a.Cfg.Temperature,
		CacheRetention: a.Cfg.CacheRetention,
		Bus:            a.Cfg.Bus,
	})
}

func (a *Agent) Reset() {
	a.mu.Lock()
	a.state.Messages = nil
	a.state.LastError = nil
	a.state.ErrorMessage = ""
	a.state.LastStop = ""
	a.state.LastDetail = ""
	a.state.LastTurns = 0
	a.state.LastUsage = Usage{}
	a.state.LastContext = 0
	a.mu.Unlock()

	if a.Cfg.LoopScheduler != nil {
		a.Cfg.LoopScheduler.Stop()
	}
	a.resetBackgroundTools()
	if a.Cfg.Inbox != nil {
		if resetter, ok := a.Cfg.Inbox.(interface{ Reset() }); ok {
			resetter.Reset()
		} else {
			a.Cfg.Inbox.Drain()
		}
	}
}

func (a *Agent) resetBackgroundTools() {
	if a.Cfg.Tools == nil {
		return
	}
	if tool, ok := a.Cfg.Tools.GetTool("subagent"); ok {
		if resetter, ok := tool.(interface{ Reset() }); ok {
			resetter.Reset()
		}
	}
	if tool, ok := a.Cfg.Tools.GetTool("bash"); ok {
		if closer, ok := tool.(interface{ Close() }); ok {
			closer.Close()
		}
	}
}

func (a *Agent) continueNeedsNudge() (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.state.Messages) == 0 {
		return false, fmt.Errorf("cannot continue: no messages in context")
	}
	return a.state.Messages[len(a.state.Messages)-1].Role == "assistant", nil
}

func (a *Agent) startRun(ctx context.Context) (context.Context, context.CancelFunc, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return nil, nil, fmt.Errorf("agent is already running")
	}
	runCtx, cancel := context.WithCancel(ctx)
	a.running = true
	a.state.LastError = nil
	a.state.ErrorMessage = ""
	return runCtx, cancel, nil
}

func (a *Agent) finishRun() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.running = false
}

func (a *Agent) messagesSnapshot() []ChatMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ChatMessage(nil), a.state.Messages...)
}

func (a *Agent) saveState(result *Result, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.state.LastError = err
		a.state.ErrorMessage = err.Error()
	}
	if result != nil {
		a.state.Messages = append([]ChatMessage(nil), result.Messages...)
		a.state.LastStop = result.Stop
		a.state.LastDetail = result.StopDetail
		a.state.LastTurns = result.Turns
		a.state.LastUsage = result.TotalUsage
		a.state.LastContext = result.ContextTokens
	}
}

func (a *Agent) DebugSnapshot() DebugSnapshot {
	if a == nil {
		return DebugSnapshot{}
	}
	a.mu.Lock()
	snapshot := DebugSnapshot{
		SessionID:    a.Cfg.SessionID,
		Running:      a.running,
		MessageCount: len(a.state.Messages),
		LastStop:     a.state.LastStop,
		LastDetail:   a.state.LastDetail,
		LastTurns:    a.state.LastTurns,
		LastUsage:    a.state.LastUsage,
		LastContext:  a.state.LastContext,
	}
	if a.state.LastError != nil {
		snapshot.LastError = a.state.LastError.Error()
	} else if a.state.ErrorMessage != "" {
		snapshot.LastError = a.state.ErrorMessage
	}
	a.mu.Unlock()

	if a.Cfg.Inbox != nil {
		snapshot.InboxLen = a.Cfg.Inbox.Len()
		snapshot.ActiveProducers = a.Cfg.Inbox.ActiveProducers()
	}
	if a.Cfg.LoopScheduler != nil {
		snapshot.ActiveLoops = a.Cfg.LoopScheduler.Active()
	}
	return snapshot
}

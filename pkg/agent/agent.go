package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/chainreactors/aiscan/pkg/agent/inbox"
)

type Agent struct {
	Cfg Config

	mu            sync.Mutex
	state         State
	running       bool
	steeringQueue *pendingMessageQueue
	followUpQueue *pendingMessageQueue
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
	cfg = a.withMessageQueues(cfg)
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
	if err := a.validateContinue(); err != nil {
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
	cfg = a.withMessageQueues(cfg)
	cfg.Messages = a.messagesSnapshot()
	result, runErr := runLoop(runCtx, cfg)
	a.saveState(result, runErr)
	return result, runErr
}

// Inject pushes a message into the currently running agent loop.
// It does not start a second run; the active loop drains the inbox at turn
// boundaries, preserving the single transcript/tool flow invariant.
func (a *Agent) Inject(msg inbox.Message) error {
	a.mu.Lock()
	running := a.running
	ib := a.Cfg.Inbox
	a.mu.Unlock()
	if !running {
		return fmt.Errorf("agent is not running")
	}
	if ib == nil {
		return fmt.Errorf("agent inbox is nil")
	}
	return ib.Push(msg)
}

// InjectUserMessage queues user input for the currently running agent loop.
func (a *Agent) InjectUserMessage(content string) error {
	msg := inbox.NewUserMessage(content)
	msg.Priority = inbox.PriorityHigh
	return a.Inject(msg)
}

// Steer queues a message to be injected at the next steering checkpoint.
// Steering is intended for human input submitted while the agent is working.
func (a *Agent) Steer(msg inbox.Message) {
	a.ensureQueues()
	a.steeringQueue.enqueue(msg)
}

// SteerUserMessage queues user input for the next steering checkpoint.
func (a *Agent) SteerUserMessage(content string) {
	msg := inbox.NewUserMessage(content)
	msg.Priority = inbox.PriorityHigh
	a.Steer(msg)
}

// FollowUp queues a message to run only after the agent would otherwise stop.
func (a *Agent) FollowUp(msg inbox.Message) {
	a.ensureQueues()
	a.followUpQueue.enqueue(msg)
}

// FollowUpUserMessage queues user input after the current work finishes.
func (a *Agent) FollowUpUserMessage(content string) {
	msg := inbox.NewUserMessage(content)
	msg.Priority = inbox.PriorityHigh
	a.FollowUp(msg)
}

// ClearSteeringQueue drops queued steering messages and returns what was removed.
func (a *Agent) ClearSteeringQueue() []inbox.Message {
	a.ensureQueues()
	return a.steeringQueue.clear()
}

// ClearFollowUpQueue drops queued follow-up messages and returns what was removed.
func (a *Agent) ClearFollowUpQueue() []inbox.Message {
	a.ensureQueues()
	return a.followUpQueue.clear()
}

// ClearAllQueues drops all human-in-the-loop queued messages.
func (a *Agent) ClearAllQueues() (steering []inbox.Message, followUp []inbox.Message) {
	return a.ClearSteeringQueue(), a.ClearFollowUpQueue()
}

// HasQueuedMessages reports whether steering or follow-up messages are waiting.
func (a *Agent) HasQueuedMessages() bool {
	steering, followUp := a.QueuedMessageCounts()
	return steering+followUp > 0
}

// QueuedMessageCounts returns pending steering and follow-up message counts.
func (a *Agent) QueuedMessageCounts() (steering int, followUp int) {
	a.ensureQueues()
	return a.steeringQueue.len(), a.followUpQueue.len()
}

func (a *Agent) SetSteeringMode(mode QueueMode) {
	a.ensureQueues()
	a.steeringQueue.setMode(mode)
}

func (a *Agent) SteeringMode() QueueMode {
	a.ensureQueues()
	return a.steeringQueue.getMode()
}

func (a *Agent) SetFollowUpMode(mode QueueMode) {
	a.ensureQueues()
	a.followUpQueue.setMode(mode)
}

func (a *Agent) FollowUpMode() QueueMode {
	a.ensureQueues()
	return a.followUpQueue.getMode()
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
	defer a.mu.Unlock()
	a.state.Messages = nil
	a.state.LastError = nil
	a.state.ErrorMessage = ""
	if a.steeringQueue != nil {
		a.steeringQueue.clear()
	}
	if a.followUpQueue != nil {
		a.followUpQueue.clear()
	}
}

func (a *Agent) validateContinue() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.state.Messages) == 0 {
		return fmt.Errorf("cannot continue: no messages in context")
	}
	if a.state.Messages[len(a.state.Messages)-1].Role == "assistant" {
		return fmt.Errorf("cannot continue from message role: assistant")
	}
	return nil
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
	}
}

func (a *Agent) ensureQueues() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ensureQueuesLocked()
}

func (a *Agent) ensureQueuesLocked() {
	if a.steeringQueue == nil {
		a.steeringQueue = newPendingMessageQueue(QueueModeOneAtATime)
	}
	if a.followUpQueue == nil {
		a.followUpQueue = newPendingMessageQueue(QueueModeOneAtATime)
	}
}

func (a *Agent) withMessageQueues(cfg Config) Config {
	a.ensureQueues()
	if cfg.GetSteeringMessages == nil {
		cfg.GetSteeringMessages = a.steeringQueue.drain
	}
	if cfg.GetFollowUpMessages == nil {
		cfg.GetFollowUpMessages = a.followUpQueue.drain
	}
	return cfg
}

package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/chainreactors/aiscan/pkg/agent"
)

type agentRunFunc func(context.Context) (*agent.Result, error)

type interactiveRunController struct {
	ctx     context.Context
	session *agent.Agent
	output  *AgentOutput

	mu       sync.Mutex
	running  bool
	stopping bool
	cancel   context.CancelFunc
	done     chan struct{}
	onFinish func()
}

func newInteractiveRunController(ctx context.Context, session *agent.Agent, output *AgentOutput) *interactiveRunController {
	if ctx == nil {
		ctx = context.Background()
	}
	return &interactiveRunController{
		ctx:     ctx,
		session: session,
		output:  output,
	}
}

func (c *interactiveRunController) SubmitPrompt(label, displayText, prompt string) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("agent session is not configured")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil
	}

	c.mu.Lock()
	if c.running {
		if c.stopping {
			c.mu.Unlock()
			return fmt.Errorf("agent task is stopping; wait for it to finish")
		}
		c.session.SteerUserMessage(prompt)
		c.mu.Unlock()
		c.output.Queued(displayText)
		return nil
	}
	c.mu.Unlock()

	return c.start(label, displayText, func(ctx context.Context) (*agent.Result, error) {
		return c.session.Run(ctx, prompt)
	})
}

func (c *interactiveRunController) SubmitPromptAndWait(label, displayText, prompt string) error {
	if err := c.SubmitPrompt(label, displayText, prompt); err != nil {
		return err
	}
	c.Wait()
	return nil
}

func (c *interactiveRunController) SubmitFollowUp(label, displayText, prompt string) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("agent session is not configured")
	}
	if strings.TrimSpace(prompt) == "" {
		return nil
	}

	c.mu.Lock()
	if c.running {
		if c.stopping {
			c.mu.Unlock()
			return fmt.Errorf("agent task is stopping; wait for it to finish")
		}
		c.session.FollowUpUserMessage(prompt)
		c.mu.Unlock()
		c.output.QueuedFollowUp(displayText)
		return nil
	}
	c.mu.Unlock()

	return c.start(label, displayText, func(ctx context.Context) (*agent.Result, error) {
		return c.session.Run(ctx, prompt)
	})
}

func (c *interactiveRunController) SubmitFollowUpAndWait(label, displayText, prompt string) error {
	if err := c.SubmitFollowUp(label, displayText, prompt); err != nil {
		return err
	}
	c.Wait()
	return nil
}

func (c *interactiveRunController) Continue() error {
	if c == nil || c.session == nil {
		return fmt.Errorf("agent session is not configured")
	}

	c.mu.Lock()
	if c.running {
		if c.stopping {
			c.mu.Unlock()
			return fmt.Errorf("agent task is stopping; wait for it to finish")
		}
		c.session.SteerUserMessage("Continue.")
		c.mu.Unlock()
		c.output.Queued("Continue.")
		return nil
	}
	c.mu.Unlock()

	return c.start("continue", "", c.session.Continue)
}

func (c *interactiveRunController) ContinueAndWait() error {
	if err := c.Continue(); err != nil {
		return err
	}
	c.Wait()
	return nil
}

func (c *interactiveRunController) start(label, displayText string, run agentRunFunc) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("agent session is not configured")
	}
	if run == nil {
		return fmt.Errorf("agent run function is nil")
	}

	runCtx, cancel := context.WithCancel(c.ctx)
	done := make(chan struct{})

	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		cancel()
		return fmt.Errorf("agent is already running")
	}
	c.running = true
	c.stopping = false
	c.cancel = cancel
	c.done = done
	c.mu.Unlock()

	c.output.Start(label, displayText)
	go c.run(runCtx, cancel, done, run)
	return nil
}

func (c *interactiveRunController) run(ctx context.Context, cancel context.CancelFunc, done chan struct{}, run agentRunFunc) {
	defer close(done)
	defer cancel()
	defer func() {
		c.finish()
		c.notifyFinish()
	}()

	result, err := run(ctx)
	if ctx.Err() != nil {
		c.output.ensureStreamNewline()
		c.output.Stopped()
		return
	}
	if err != nil {
		c.output.ensureStreamNewline()
		if errors.Is(err, context.Canceled) {
			c.output.Stopped()
			return
		}
		c.output.Error(err)
		return
	}
	c.printResult(result)
}

func (c *interactiveRunController) finish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = false
	c.stopping = false
	c.cancel = nil
}

func (c *interactiveRunController) SetOnFinish(fn func()) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onFinish = fn
}

func (c *interactiveRunController) notifyFinish() {
	if c == nil {
		return
	}
	c.mu.Lock()
	fn := c.onFinish
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (c *interactiveRunController) Stop() bool {
	c.mu.Lock()
	if !c.running || c.cancel == nil {
		c.mu.Unlock()
		return false
	}
	cancel := c.cancel
	c.stopping = true
	if c.session != nil {
		c.session.ClearAllQueues()
	}
	c.mu.Unlock()

	if c.output != nil {
		c.output.AbortCurrentRun()
	}
	cancel()
	return true
}

func (c *interactiveRunController) Running() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

func (c *interactiveRunController) Wait() {
	if c == nil {
		return
	}
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (c *interactiveRunController) StopAndWait() {
	if c == nil {
		return
	}
	c.Stop()
	c.Wait()
}

func (c *interactiveRunController) printResult(result *agent.Result) {
	if result == nil || strings.TrimSpace(result.Output) == "" {
		c.output.Empty()
		return
	}
	c.output.Final(result.Output)
}

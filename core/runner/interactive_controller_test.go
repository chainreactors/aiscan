package runner

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/app"
	"github.com/chainreactors/aiscan/pkg/command"
	outpkg "github.com/chainreactors/aiscan/pkg/output"
	"github.com/reeflective/readline/inputrc"
)

type controllerBlockingProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once

	mu       sync.Mutex
	requests []*agent.ChatCompletionRequest
}

func (p *controllerBlockingProvider) Name() string { return "blocking" }

func (p *controllerBlockingProvider) WebSearch(_ context.Context, _ string, _ int) (*agent.WebSearchResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (p *controllerBlockingProvider) ChatCompletion(ctx context.Context, req *agent.ChatCompletionRequest) (*agent.ChatCompletionResponse, error) {
	p.mu.Lock()
	cloned := *req
	cloned.Messages = append([]agent.ChatMessage(nil), req.Messages...)
	p.requests = append(p.requests, &cloned)
	p.mu.Unlock()

	p.once.Do(func() { close(p.started) })
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &agent.ChatCompletionResponse{
		Choices: []agent.Choice{{Message: agent.NewTextMessage("assistant", "done")}},
	}, nil
}

func (p *controllerBlockingProvider) requestsSnapshot() []*agent.ChatCompletionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*agent.ChatCompletionRequest, 0, len(p.requests))
	for _, req := range p.requests {
		cloned := *req
		cloned.Messages = append([]agent.ChatMessage(nil), req.Messages...)
		out = append(out, &cloned)
	}
	return out
}

func newTestAgentOutput(stdout, stderr *bytes.Buffer) *AgentOutput {
	return &AgentOutput{
		stdout: stdout,
		stderr: stderr,
		color:  outpkg.NewColor(false),
		tools:  make(map[string]agentToolSummary),
	}
}

func TestInteractiveRunControllerQueuesPromptWhileRunning(t *testing.T) {
	var stdout, stderr bytes.Buffer
	provider := &controllerBlockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	session := agent.NewAgent(agent.Config{
		Provider:   provider,
		Tools:      command.NewRegistry(),
		Model:      "test",
		MaxRetries: -1,
	})
	controller := newInteractiveRunController(context.Background(), session, newTestAgentOutput(&stdout, &stderr))

	if err := controller.SubmitPrompt("prompt", "first", "first"); err != nil {
		t.Fatalf("SubmitPrompt(first) error = %v", err)
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider was not called")
	}
	if !controller.Running() {
		t.Fatal("controller should be running")
	}

	if err := controller.SubmitPrompt("prompt", "follow up", "follow up"); err != nil {
		t.Fatalf("SubmitPrompt(follow up) error = %v", err)
	}
	close(provider.release)
	controller.Wait()

	requests := provider.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if !requestContainsUserText(requests[1], "follow up") {
		t.Fatalf("second request missing queued prompt: %#v", requests[1].Messages)
	}
	if !strings.Contains(stderr.String(), "queued: follow up") {
		t.Fatalf("stderr missing queued status: %q", stderr.String())
	}
}

func TestInteractiveRunControllerSubmitPromptAndWaitBlocksUntilDone(t *testing.T) {
	var stdout, stderr bytes.Buffer
	provider := &controllerBlockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	session := agent.NewAgent(agent.Config{
		Provider:   provider,
		Tools:      command.NewRegistry(),
		Model:      "test",
		MaxRetries: -1,
	})
	controller := newInteractiveRunController(context.Background(), session, newTestAgentOutput(&stdout, &stderr))

	done := make(chan error, 1)
	go func() {
		done <- controller.SubmitPromptAndWait("prompt", "first", "first")
	}()

	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider was not called")
	}
	select {
	case err := <-done:
		t.Fatalf("SubmitPromptAndWait returned before provider finished: %v", err)
	default:
	}

	close(provider.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SubmitPromptAndWait error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SubmitPromptAndWait did not return after provider finished")
	}
}

func TestInteractiveRunControllerFinishCallbackRunsAfterIdle(t *testing.T) {
	var stdout, stderr bytes.Buffer
	provider := &controllerBlockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	session := agent.NewAgent(agent.Config{
		Provider:   provider,
		Tools:      command.NewRegistry(),
		Model:      "test",
		MaxRetries: -1,
	})
	controller := newInteractiveRunController(context.Background(), session, newTestAgentOutput(&stdout, &stderr))
	finishStatus := make(chan bool, 1)
	controller.SetOnFinish(func() {
		finishStatus <- controller.Running()
	})

	if err := controller.SubmitPrompt("prompt", "first", "first"); err != nil {
		t.Fatalf("SubmitPrompt(first) error = %v", err)
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider was not called")
	}
	close(provider.release)
	controller.Wait()

	select {
	case running := <-finishStatus:
		if running {
			t.Fatal("finish callback ran while controller still reported running")
		}
	case <-time.After(time.Second):
		t.Fatal("finish callback was not called")
	}
}

func TestAgentConsoleHandleInputLineReturnsWhilePromptRuns(t *testing.T) {
	var stdout, stderr bytes.Buffer
	provider := &controllerBlockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	session := agent.NewAgent(agent.Config{
		Provider:   provider,
		Tools:      command.NewRegistry(),
		Model:      "test",
		MaxRetries: -1,
	})
	repl := NewAgentConsole(
		context.Background(),
		&cfg.Option{},
		&app.App{},
		session,
		newTestAgentOutput(&stdout, &stderr),
	)

	done, err := repl.handleInputLine("你好")
	if err != nil {
		t.Fatalf("handleInputLine(first) error = %v", err)
	}
	if done {
		t.Fatal("handleInputLine should not exit the REPL for a prompt")
	}

	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider was not called")
	}
	if !repl.controller.Running() {
		t.Fatal("controller should keep running after handleInputLine returns")
	}

	done, err = repl.handleInputLine("follow up")
	if err != nil {
		t.Fatalf("handleInputLine(follow up) error = %v", err)
	}
	if done {
		t.Fatal("handleInputLine should not exit the REPL for a queued prompt")
	}

	close(provider.release)
	repl.controller.Wait()

	requests := provider.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if !requestContainsUserText(requests[1], "follow up") {
		t.Fatalf("second request missing queued prompt: %#v", requests[1].Messages)
	}
	if !strings.Contains(stderr.String(), "queued: follow up") {
		t.Fatalf("stderr missing queued status: %q", stderr.String())
	}
}

func TestAgentConsoleEscapeInterruptDoesNotStealArrowKeys(t *testing.T) {
	repl := NewAgentConsole(
		context.Background(),
		&cfg.Option{},
		&app.App{},
		agent.NewAgent(agent.Config{Provider: &controllerBlockingProvider{}, Tools: command.NewRegistry()}),
		newTestAgentOutput(&bytes.Buffer{}, &bytes.Buffer{}),
	)

	escape := inputrc.Unescape(`\e`)
	upArrow := inputrc.Unescape(`\M-[A`)
	binds := repl.console.Shell().Config.Binds["emacs"]
	bind := binds[escape]
	if bind.Action != agentConsoleInterruptCommandName {
		t.Fatalf("emacs bare escape bind action = %q, want %q", bind.Action, agentConsoleInterruptCommandName)
	}
	upBind := binds[upArrow]
	if upBind.Action == "" {
		t.Fatal("emacs up-arrow bind should remain available")
	}
	if strings.Contains(upBind.Action, "interrupt") {
		t.Fatalf("emacs up-arrow bind action = %q, must not interrupt", upBind.Action)
	}

	feed, ok := agentConsoleEscapeSequenceFeed(binds, "[A")
	if !ok {
		t.Fatal("up-arrow suffix should be rewritten to an equivalent non-escape bind")
	}
	if want := inputrc.Unescape(`\C-p`); feed != want {
		t.Fatalf("up-arrow feed = %q, want %q", feed, want)
	}
}

func TestInteractiveRunControllerQueuesFollowUpWhileRunning(t *testing.T) {
	var stdout, stderr bytes.Buffer
	provider := &controllerBlockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	session := agent.NewAgent(agent.Config{
		Provider:   provider,
		Tools:      command.NewRegistry(),
		Model:      "test",
		MaxRetries: -1,
	})
	controller := newInteractiveRunController(context.Background(), session, newTestAgentOutput(&stdout, &stderr))

	if err := controller.SubmitPrompt("prompt", "first", "first"); err != nil {
		t.Fatalf("SubmitPrompt(first) error = %v", err)
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider was not called")
	}

	if err := controller.SubmitFollowUp("follow-up", "later", "later"); err != nil {
		t.Fatalf("SubmitFollowUp(later) error = %v", err)
	}
	close(provider.release)
	controller.Wait()

	requests := provider.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}
	if !requestContainsUserText(requests[1], "later") {
		t.Fatalf("second request missing queued follow-up: %#v", requests[1].Messages)
	}
	if !strings.Contains(stderr.String(), "queued follow-up: later") {
		t.Fatalf("stderr missing queued follow-up status: %q", stderr.String())
	}
}

func TestInteractiveRunControllerStopCancelsRunningPrompt(t *testing.T) {
	var stdout, stderr bytes.Buffer
	provider := &controllerBlockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	session := agent.NewAgent(agent.Config{
		Provider:   provider,
		Tools:      command.NewRegistry(),
		Model:      "test",
		MaxRetries: -1,
	})
	controller := newInteractiveRunController(context.Background(), session, newTestAgentOutput(&stdout, &stderr))

	if err := controller.SubmitPrompt("prompt", "first", "first"); err != nil {
		t.Fatalf("SubmitPrompt(first) error = %v", err)
	}
	select {
	case <-provider.started:
	case <-time.After(time.Second):
		t.Fatal("provider was not called")
	}

	if !controller.Stop() {
		t.Fatal("Stop() = false, want true")
	}
	controller.Wait()
	if controller.Running() {
		t.Fatal("controller should not be running after Wait")
	}
	if !strings.Contains(stderr.String(), "Task stopped.") {
		t.Fatalf("stderr missing stopped status: %q", stderr.String())
	}
}

func TestInteractiveRunControllerStopSuppressesLateSuccessfulResult(t *testing.T) {
	var stdout, stderr bytes.Buffer
	session := agent.NewAgent(agent.Config{
		Provider:   &controllerBlockingProvider{},
		Tools:      command.NewRegistry(),
		Model:      "test",
		MaxRetries: -1,
	})
	controller := newInteractiveRunController(context.Background(), session, newTestAgentOutput(&stdout, &stderr))
	started := make(chan struct{})

	err := controller.start("prompt", "first", func(ctx context.Context) (*agent.Result, error) {
		close(started)
		<-ctx.Done()
		return &agent.Result{Output: "late final"}, nil
	})
	if err != nil {
		t.Fatalf("start error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("run function did not start")
	}

	if !controller.Stop() {
		t.Fatal("Stop() = false, want true")
	}
	controller.Wait()

	if got := stdout.String(); strings.Contains(got, "late final") {
		t.Fatalf("stdout contains late final after stop: %q", got)
	}
	if !strings.Contains(stderr.String(), "Task stopped.") {
		t.Fatalf("stderr missing stopped status: %q", stderr.String())
	}
}

func requestContainsUserText(req *agent.ChatCompletionRequest, text string) bool {
	for _, msg := range req.Messages {
		if msg.Role != "user" || msg.Content == nil {
			continue
		}
		if strings.Contains(*msg.Content, text) {
			return true
		}
	}
	return false
}

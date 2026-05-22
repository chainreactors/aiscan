// Package task provides a background-task manager for long-running shell
// commands launched by the agent. Each task runs in its own process group
// (so kill cascades to descendants), tees stdout+stderr to a persistent
// file, and on completion pushes a follow-up ChatMessage onto an optional
// sink channel so the next agent turn can react to the result.
//
// Design follows pi-mono's tmux-bash extension (richardgill/pi-extensions)
// but uses plain os/exec + a filesystem signal pattern instead of tmux.
package task

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/pkg/provider"
)

type State string

const (
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateKilled    State = "killed"
	StateFailed    State = "failed"
)

const (
	// DefaultTimeout caps a single background task's wall-clock runtime.
	// 30 minutes covers a full scan→spray→neutron pipeline against one
	// target; longer bursts should pass an explicit timeout.
	DefaultTimeout = 30 * time.Minute

	// killGrace is the SIGTERM → SIGKILL grace window.
	killGrace = 5 * time.Second

	// shutdownGrace is how long Shutdown waits for tasks to flush after
	// SIGKILL before returning.
	shutdownGrace = 2 * time.Second
)

// Info is the externally-visible state of a task. Always returned by value
// so callers can't mutate the Manager's view.
type Info struct {
	ID         string    `json:"id"`
	Name       string    `json:"name,omitempty"`
	Command    string    `json:"command"`
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at,omitempty"`
	ExitCode   int       `json:"exit_code"`
	State      State     `json:"state"`
	StdoutFile string    `json:"stdout_file"`
}

// Manager owns a set of background tasks and an optional completion-message
// sink. Safe for concurrent use; all maps are protected by mu.
type Manager struct {
	mu     sync.Mutex
	tasks  map[string]*task
	outDir string
	sink   chan<- provider.ChatMessage
}

type task struct {
	Info
	cmd       *exec.Cmd
	stdoutFP  *os.File
	done      chan struct{}
	cancel    context.CancelFunc // populated for in-process tasks; nil for shell tasks
	killCause string             // protected by Manager.mu
}

// InProcessFn is the closure executed by SpawnInProcess. Implementations
// must respect ctx (used by Kill / Shutdown) and write progress to out.
type InProcessFn func(ctx context.Context, out io.Writer) error

// NewManager returns an empty Manager. outDir is the base directory under
// which each task's files are written (outDir/<id>/{cmd,stdout,signal,meta.json}).
// The directory is created lazily on first Spawn.
func NewManager(outDir string) *Manager {
	return &Manager{
		tasks:  make(map[string]*task),
		outDir: outDir,
	}
}

// SetSink configures the channel that receives task-completion notifications.
// Pass nil to disable injection. Replaces any previous sink. Existing
// in-flight tasks pick up the new sink when they complete.
func (m *Manager) SetSink(c chan<- provider.ChatMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sink = c
}

// ClearSink is shorthand for SetSink(nil).
func (m *Manager) ClearSink() { m.SetSink(nil) }

// Spawn launches cmdLine in its own process group. Returns immediately
// after exec.Start() succeeds; the supervising goroutine handles wait,
// timeout, and completion notification.
//
//   - workDir: child's cwd; usually the agent's working directory.
//   - cmdLine: shell command (passed to sh -c on Unix, cmd /c on Windows).
//   - name:    optional human label (shown in `task list`); defaults to first token of cmdLine.
//   - timeout: wall-clock kill deadline; 0 means DefaultTimeout.
func (m *Manager) Spawn(workDir, cmdLine, name string, timeout time.Duration) (Info, error) {
	if strings.TrimSpace(cmdLine) == "" {
		return Info{}, errors.New("empty command")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if name == "" {
		name = labelFromCommand(cmdLine)
	}

	id, err := genID()
	if err != nil {
		return Info{}, fmt.Errorf("generate id: %w", err)
	}
	taskDir := filepath.Join(m.outDir, id)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return Info{}, fmt.Errorf("mkdir %s: %w", taskDir, err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "cmd"), []byte(cmdLine+"\n"), 0o600); err != nil {
		return Info{}, fmt.Errorf("write cmd: %w", err)
	}
	stdoutPath := filepath.Join(taskDir, "stdout")
	stdoutFP, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Info{}, fmt.Errorf("open stdout file: %w", err)
	}

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/c", cmdLine)
	} else {
		c = exec.Command("sh", "-c", cmdLine)
	}
	c.Dir = workDir
	c.Stdin = nil
	c.Stdout = stdoutFP
	c.Stderr = stdoutFP
	configureTaskProcessGroup(c)

	if err := c.Start(); err != nil {
		_ = stdoutFP.Close()
		return Info{}, fmt.Errorf("start: %w", err)
	}

	now := time.Now()
	info := Info{
		ID:         id,
		Name:       name,
		Command:    cmdLine,
		PID:        c.Process.Pid,
		StartedAt:  now,
		State:      StateRunning,
		StdoutFile: stdoutPath,
	}
	t := &task{Info: info, cmd: c, stdoutFP: stdoutFP, done: make(chan struct{})}

	m.mu.Lock()
	m.tasks[id] = t
	m.mu.Unlock()

	writeMeta(taskDir, info)

	go m.supervise(t, timeout)
	return info, nil
}

// SpawnInProcess registers a task that runs a Go closure (typically a
// pseudo-command via CommandRegistry.ExecuteStreaming) instead of a shell
// subprocess. Output is tee'd to the same on-disk stdout file as Spawn so
// the LLM can `task peek` / `task wait` uniformly. Cancellation is by
// context.CancelFunc (no signals).
func (m *Manager) SpawnInProcess(label, cmdDisplay string, timeout time.Duration, fn InProcessFn) (Info, error) {
	if fn == nil {
		return Info{}, errors.New("nil function")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	name := label
	if name == "" {
		name = labelFromCommand(cmdDisplay)
	}

	id, err := genID()
	if err != nil {
		return Info{}, fmt.Errorf("generate id: %w", err)
	}
	taskDir := filepath.Join(m.outDir, id)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return Info{}, fmt.Errorf("mkdir %s: %w", taskDir, err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "cmd"), []byte(cmdDisplay+"\n"), 0o600); err != nil {
		return Info{}, fmt.Errorf("write cmd: %w", err)
	}
	stdoutPath := filepath.Join(taskDir, "stdout")
	stdoutFP, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Info{}, fmt.Errorf("open stdout file: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	now := time.Now()
	info := Info{
		ID:         id,
		Name:       name,
		Command:    cmdDisplay,
		PID:        0,
		StartedAt:  now,
		State:      StateRunning,
		StdoutFile: stdoutPath,
	}
	t := &task{Info: info, stdoutFP: stdoutFP, done: make(chan struct{}), cancel: cancel}

	m.mu.Lock()
	m.tasks[id] = t
	m.mu.Unlock()

	writeMeta(taskDir, info)

	go m.superviseInProcess(t, fn, ctx, timeout)
	return info, nil
}

// superviseInProcess is SpawnInProcess's equivalent of supervise().
func (m *Manager) superviseInProcess(t *task, fn InProcessFn, ctx context.Context, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	runErr := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				runErr <- fmt.Errorf("panic in background task: %v", r)
			}
		}()
		runErr <- fn(ctx, t.stdoutFP)
	}()

	var (
		fnErr     error
		killed    bool
		killCause string
	)
	select {
	case err := <-runErr:
		fnErr = err
	case <-timer.C:
		killed, killCause = true, fmt.Sprintf("timeout after %s", timeout)
		m.markKillCause(t, killCause)
		t.cancel()
		fnErr = <-runErr
	}

	_ = t.stdoutFP.Close()

	recordedKillCause := m.killCause(t)
	if recordedKillCause != "" {
		killed = true
		killCause = recordedKillCause
	}

	state, exitCode := StateCompleted, 0
	if fnErr != nil {
		if killed {
			state = StateKilled
			exitCode = -1
		} else {
			state = StateFailed
			exitCode = 1
			// Append the error message to stdout file so peek/wait see why.
			if data, err := os.OpenFile(t.StdoutFile, os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				fmt.Fprintf(data, "\n[task error] %v\n", fnErr)
				_ = data.Close()
			}
		}
	} else if killed {
		state = StateKilled
	}

	m.mu.Lock()
	t.EndedAt = time.Now()
	t.ExitCode = exitCode
	t.State = state
	sink := m.sink
	infoCopy := t.Info
	m.mu.Unlock()

	taskDir := filepath.Dir(t.StdoutFile)
	signalContent := strconv.Itoa(exitCode)
	if state == StateKilled {
		signalContent = "killed:" + killCause
	}
	_ = os.WriteFile(filepath.Join(taskDir, "signal"), []byte(signalContent+"\n"), 0o600)
	writeMeta(taskDir, infoCopy)

	close(t.done)

	sendCompletion(sink, infoCopy, state == StateKilled, killCause)
}

// supervise runs in its own goroutine; one per spawned task. It blocks on
// cmd.Wait, applies the timeout, writes the signal file, and pushes the
// completion notification to the sink (if any).
func (m *Manager) supervise(t *task, timeout time.Duration) {
	waitDone := make(chan error, 1)
	go func() { waitDone <- t.cmd.Wait() }()

	var (
		waitErr   error
		killed    bool
		killCause string
	)

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-waitDone:
		waitErr = err
	case <-timer.C:
		killed, killCause = true, fmt.Sprintf("timeout after %s", timeout)
		m.markKillCause(t, killCause)
		m.forceKillTask(t)
		waitErr = <-waitDone
	}

	_ = t.stdoutFP.Close()

	recordedKillCause := m.killCause(t)
	if recordedKillCause != "" {
		killed = true
		killCause = recordedKillCause
	}

	state, exitCode := StateCompleted, 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(waitErr, &exitErr):
			exitCode = exitErr.ExitCode()
			if killed {
				state = StateKilled
			} else {
				state = StateFailed
			}
		default:
			exitCode = -1
			state = StateFailed
		}
	} else if killed {
		state = StateKilled
	}

	m.mu.Lock()
	t.EndedAt = time.Now()
	t.ExitCode = exitCode
	t.State = state
	sink := m.sink
	infoCopy := t.Info
	m.mu.Unlock()

	taskDir := filepath.Dir(t.StdoutFile)
	signalContent := strconv.Itoa(exitCode)
	if state == StateKilled {
		signalContent = "killed:" + killCause
	}
	_ = os.WriteFile(filepath.Join(taskDir, "signal"), []byte(signalContent+"\n"), 0o600)
	writeMeta(taskDir, infoCopy)

	close(t.done)

	sendCompletion(sink, infoCopy, state == StateKilled, killCause)
}

// forceKillTask cancels in-process tasks via context, or sends SIGTERM →
// SIGKILL to shell tasks' process group. Either way the supervising
// goroutine still owns the wait/close-done sequence.
func (m *Manager) forceKillTask(t *task) {
	if t.cmd == nil {
		// In-process task: ctx cancellation is the only lever.
		if t.cancel != nil {
			t.cancel()
		}
		return
	}
	if t.cmd.Process == nil {
		return
	}
	_ = signalProcessGroup(t.cmd.Process.Pid, false)
	timer := time.NewTimer(killGrace)
	defer timer.Stop()
	select {
	case <-t.done:
		return
	case <-timer.C:
	}
	_ = signalProcessGroup(t.cmd.Process.Pid, true)
}

// Kill terminates a running task. Returns nil if the task is already done.
func (m *Manager) Kill(id string) error {
	m.mu.Lock()
	t, ok := m.tasks[id]
	if ok {
		select {
		case <-t.done:
		default:
			if t.killCause == "" {
				t.killCause = "killed by user"
			}
		}
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no such task: %s", id)
	}
	select {
	case <-t.done:
		return nil
	default:
	}
	go m.forceKillTask(t)
	return nil
}

// KillAll terminates every running task and waits up to shutdownGrace for
// them to finish. Useful between swarm tasks so background scans from a
// previous company don't leak into the next.
func (m *Manager) KillAll() {
	m.mu.Lock()
	running := make([]*task, 0, len(m.tasks))
	for _, t := range m.tasks {
		select {
		case <-t.done:
		default:
			running = append(running, t)
		}
	}
	m.mu.Unlock()

	for _, t := range running {
		m.markKillCause(t, "shutdown")
		if t.cmd != nil && t.cmd.Process != nil {
			_ = signalProcessGroup(t.cmd.Process.Pid, false)
		}
		if t.cancel != nil {
			t.cancel()
		}
	}
	deadline := time.After(killGrace)
	for _, t := range running {
		select {
		case <-t.done:
		case <-deadline:
			if t.cmd != nil && t.cmd.Process != nil {
				_ = signalProcessGroup(t.cmd.Process.Pid, true)
			}
		}
	}
	// Final drain.
	finalDeadline := time.After(shutdownGrace)
	for _, t := range running {
		select {
		case <-t.done:
		case <-finalDeadline:
		}
	}
}

// Shutdown is an alias for KillAll suitable for deferring at process exit.
func (m *Manager) Shutdown() { m.KillAll() }

// List returns a copy of every task currently tracked, oldest first.
func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, t.Info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

// Get returns a snapshot of one task's Info.
func (m *Manager) Get(id string) (Info, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return Info{}, false
	}
	return t.Info, true
}

// Peek returns the last n non-empty lines of the task's stdout file.
// If n <= 0, returns the last 30 lines.
func (m *Manager) Peek(id string, n int) (string, error) {
	m.mu.Lock()
	t, ok := m.tasks[id]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("no such task: %s", id)
	}
	if n <= 0 {
		n = 30
	}
	data, err := os.ReadFile(t.StdoutFile)
	if err != nil {
		return "", err
	}
	return tailLines(string(data), n), nil
}

// Wait blocks until the task completes or timeout elapses. ctx cancellation
// is honored. Returns the final Info; the State field discriminates between
// "still running" (timed out) and a terminal state.
func (m *Manager) Wait(ctx context.Context, id string, timeout time.Duration) (Info, error) {
	m.mu.Lock()
	t, ok := m.tasks[id]
	m.mu.Unlock()
	if !ok {
		return Info{}, fmt.Errorf("no such task: %s", id)
	}
	var timerC <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		timerC = timer.C
	}
	select {
	case <-t.done:
	case <-timerC:
	case <-ctx.Done():
		return t.Info, ctx.Err()
	}
	return m.snapshot(t), nil
}

func (m *Manager) snapshot(t *task) Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	return t.Info
}

func (m *Manager) markKillCause(t *task, cause string) {
	if cause == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.killCause == "" {
		t.killCause = cause
	}
}

func (m *Manager) killCause(t *task) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return t.killCause
}

// --- helpers ---

func genID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeMeta(dir string, info Info) {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "meta.json"), data, 0o600)
}

func labelFromCommand(cmdLine string) string {
	cmdLine = strings.TrimSpace(cmdLine)
	if i := strings.IndexAny(cmdLine, " \t\n"); i > 0 {
		cmdLine = cmdLine[:i]
	}
	if i := strings.LastIndex(cmdLine, "/"); i >= 0 {
		cmdLine = cmdLine[i+1:]
	}
	if cmdLine == "" {
		return "shell"
	}
	return cmdLine
}

// tailLines returns up to n trailing non-empty lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		kept = append(kept, ln)
	}
	if len(kept) > n {
		kept = kept[len(kept)-n:]
	}
	return strings.Join(kept, "\n")
}

func formatCompletion(info Info, killed bool, killCause string) string {
	duration := info.EndedAt.Sub(info.StartedAt).Round(time.Second)
	status := "completed"
	switch {
	case killed:
		status = "killed (" + killCause + ")"
	case info.ExitCode != 0:
		status = fmt.Sprintf("exited with code %d", info.ExitCode)
	}

	tail := ""
	if data, err := os.ReadFile(info.StdoutFile); err == nil {
		tail = tailLines(string(data), 20)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<task_completion id=%q name=%q exit_code=%d duration=%q stdout_file=%q>\n",
		info.ID, info.Name, info.ExitCode, duration.String(), info.StdoutFile)
	fmt.Fprintf(&sb, "Background task %s.\n", status)
	if tail != "" {
		sb.WriteString("--- last 20 lines ---\n")
		sb.WriteString(tail)
		sb.WriteString("\n")
	} else {
		sb.WriteString("(no output)\n")
	}
	sb.WriteString("</task_completion>")
	return sb.String()
}

func sendCompletion(sink chan<- provider.ChatMessage, info Info, killed bool, killCause string) {
	if sink == nil {
		return
	}
	msg := provider.NewTextMessage("user", formatCompletion(info, killed, killCause))
	defer func() {
		_ = recover()
	}()
	// Non-blocking send: if the inbox is full or has just been closed, the LLM
	// can still discover completion via `task list` / `task wait`.
	select {
	case sink <- msg:
	default:
	}
}

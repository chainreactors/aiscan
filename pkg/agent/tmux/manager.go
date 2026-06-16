// Package task provides a PTY-based session manager. Each session runs a
// command in a pseudo-terminal with buffered output, interactive input, and
// lifecycle management. The API mirrors tmux semantics: Create, List, Peek,
// Write, Kill, Wait.
package tmux

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

type State string

const (
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateKilled    State = "killed"
	StateFailed    State = "failed"
)

const (
	DefaultTimeout = 30 * time.Minute
	killGrace      = 5 * time.Second
	shutdownGrace  = 2 * time.Second
)

type Info struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Command   string    `json:"command"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	ExitCode  int       `json:"exit_code"`
	State     State     `json:"state"`
	KillCause string    `json:"kill_cause,omitempty"`
}

type session struct {
	Info
	cmd       *exec.Cmd          // nil for func sessions
	output    *OutputBuffer
	pty       *ptyHandle         // nil for func sessions
	pumpDone  <-chan struct{}     // nil for func sessions
	done      chan struct{}
	peekOff   int64
	cancel    context.CancelFunc // non-nil for func sessions
}

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*session
	onDone   func(Info)
	bufCap   int

	commands func(name string) (Command, bool)
	workDir  string
}

func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*session)}
}

func (m *Manager) SetOnDone(fn func(Info)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onDone = fn
}

// RunOpts controls how RunCommand creates a session.
type RunOpts struct {
	Name    string
	Timeout time.Duration
	WorkDir string
	Env     []string
	Ctx     context.Context
}

// SetCommands injects the lookup function used by RunCommand to detect
// in-process commands. The function is typically a closure over a
// CommandRegistry in the calling package.
func (m *Manager) SetCommands(fn func(name string) (Command, bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = fn
}

// SetWorkDir sets the default working directory for shell sessions created
// by RunCommand.
func (m *Manager) SetWorkDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workDir = dir
}

// CommandNames returns the names of all registered in-process commands,
// or nil if no command resolver is set. Used by BashTool to generate
// its LLM-visible description.
func (m *Manager) CommandNames() []string {
	m.mu.Lock()
	fn := m.commands
	m.mu.Unlock()
	if fn == nil {
		return nil
	}
	// The commands function is a point lookup, not an iterator.
	// Callers that need a full list should use the registry directly.
	return nil
}

// RunCommand creates a session for the given command line. If the first
// token matches a registered in-process Command, the command runs in a
// goroutine-based session (CreateFunc). Otherwise it runs as a shell
// command in a PTY session (Create).
//
// This is the SINGLE place where command → session wrapping occurs.
// Both BashTool (inline execution) and TmuxCommand (session management)
// delegate here.
func (m *Manager) RunCommand(cmdLine string, opts RunOpts) (Info, error) {
	cmdLine = stripCommentsAndBlanks(cmdLine)
	if strings.TrimSpace(cmdLine) == "" {
		return Info{}, errors.New("empty command")
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	workDir := opts.WorkDir
	if workDir == "" {
		m.mu.Lock()
		workDir = m.workDir
		m.mu.Unlock()
	}

	token := firstCommandToken(cmdLine)

	m.mu.Lock()
	resolve := m.commands
	m.mu.Unlock()

	if resolve != nil && token != "" {
		if cmd, ok := resolve(token); ok {
			tokens, err := SplitCommandLine(cmdLine)
			if err != nil {
				return Info{}, err
			}
			if len(tokens) > 1 {
				if _, valErr := stripShellSyntax(tokens[1:]); valErr != nil {
					return Info{}, valErr
				}
			}
			name := opts.Name
			if name == "" {
				name = token
			}
			args := tokens[1:]
			return m.CreateFunc(opts.Ctx, name, timeout, func(ctx context.Context, w io.Writer) error {
				return cmd.Execute(ctx, args, w)
			})
		}
	}

	return m.Create(workDir, cmdLine, opts.Name, timeout, opts.Env, "")
}

func stripCommentsAndBlanks(input string) string {
	lines := strings.Split(input, "\n")
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// Create starts a shell command in a PTY session.
func (m *Manager) Create(workDir, cmdLine, name string, timeout time.Duration, env []string, outputFile string) (Info, error) {
	if strings.TrimSpace(cmdLine) == "" {
		return Info{}, errors.New("empty command")
	}
	c := ShellCommand(cmdLine)
	c.Dir = workDir
	if len(env) > 0 {
		c.Env = mergeEnv(os.Environ(), env)
	}
	return m.start(c, cmdLine, name, timeout, outputFile)
}

// CreateFunc starts a goroutine-based session. The function fn runs in a
// goroutine; its output (written to w) is captured in the same OutputBuffer
// used by PTY sessions, so Peek/Kill/Wait/Done work identically.
// The session context is derived from parentCtx (or context.Background if nil).
func (m *Manager) CreateFunc(parentCtx context.Context, name string, timeout time.Duration, fn func(ctx context.Context, w io.Writer) error) (Info, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if name == "" {
		name = "func"
	}

	id, err := genID()
	if err != nil {
		return Info{}, err
	}

	buf, err := m.newBuffer("")
	if err != nil {
		return Info{}, err
	}

	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, timeout)

	info := Info{
		ID:        id,
		Name:      name,
		Command:   name,
		StartedAt: time.Now(),
		State:     StateRunning,
	}
	s := &session{Info: info, output: buf, done: make(chan struct{}), cancel: cancel}

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()

	go m.superviseFunc(s, ctx, fn)
	return info, nil
}

func (m *Manager) superviseFunc(s *session, ctx context.Context, fn func(context.Context, io.Writer) error) {
	defer s.cancel()

	var fnErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				fnErr = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
			}
		}()
		fnErr = fn(ctx, s.output)
	}()

	state, exitCode := StateCompleted, 0
	killCause := ""

	if fnErr != nil {
		if errors.Is(fnErr, context.DeadlineExceeded) {
			state = StateKilled
			killCause = "timeout"
		} else if errors.Is(fnErr, context.Canceled) {
			state = StateKilled
			killCause = m.getKillCause(s)
			if killCause == "" {
				killCause = "canceled"
			}
		} else {
			state = StateFailed
			exitCode = 1
			s.output.AppendError(fnErr.Error())
		}
	}

	m.mu.Lock()
	s.EndedAt = time.Now()
	s.ExitCode = exitCode
	s.State = state
	s.KillCause = killCause
	infoCopy := s.Info
	m.mu.Unlock()

	s.output.Close()
	close(s.done)

	m.mu.Lock()
	onDone := m.onDone
	m.mu.Unlock()
	if onDone != nil {
		defer func() { _ = recover() }()
		onDone(infoCopy)
	}
}

// CreateCmd starts a command with explicit binary and args in a PTY session.
func (m *Manager) CreateCmd(workDir, binary string, args []string, name string, timeout time.Duration, env []string, outputFile string) (Info, error) {
	c := exec.Command(binary, args...)
	c.Dir = workDir
	if len(env) > 0 {
		c.Env = mergeEnv(os.Environ(), env)
	}
	display := binary + " " + strings.Join(args, " ")
	return m.start(c, display, name, timeout, outputFile)
}

// mergeEnv merges override env vars into base, replacing any existing keys.
func mergeEnv(base, override []string) []string {
	overrideKeys := make(map[string]bool, len(override))
	for _, e := range override {
		if k, _, ok := strings.Cut(e, "="); ok {
			overrideKeys[k] = true
		}
	}
	result := make([]string, 0, len(base)+len(override))
	for _, e := range base {
		if k, _, ok := strings.Cut(e, "="); ok && overrideKeys[k] {
			continue
		}
		result = append(result, e)
	}
	return append(result, override...)
}

func (m *Manager) start(c *exec.Cmd, cmdDisplay, name string, timeout time.Duration, outputFile string) (Info, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if name == "" {
		name = labelFromCommand(cmdDisplay)
	}

	id, err := genID()
	if err != nil {
		return Info{}, err
	}

	buf, err := m.newBuffer(outputFile)
	if err != nil {
		return Info{}, err
	}

	p, err := startPTY(c)
	if err != nil {
		buf.Close()
		return Info{}, fmt.Errorf("start pty: %w", err)
	}

	pd := pumpOutput(p, buf)

	info := Info{
		ID:        id,
		Name:      name,
		Command:   cmdDisplay,
		PID:       c.Process.Pid,
		StartedAt: time.Now(),
		State:     StateRunning,
	}
	s := &session{Info: info, cmd: c, output: buf, pty: p, pumpDone: pd, done: make(chan struct{})}

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()

	go m.supervise(s, timeout)
	return info, nil
}

func (m *Manager) supervise(s *session, timeout time.Duration) {
	waitDone := make(chan error, 1)
	go func() { waitDone <- s.cmd.Wait() }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var (
		waitErr   error
		killCause string
	)

	select {
	case err := <-waitDone:
		waitErr = err
	case <-timer.C:
		killCause = fmt.Sprintf("timeout after %s", timeout)
		m.setKillCause(s, killCause)
		m.forceKill(s)
		waitErr = <-waitDone
	}

	if recorded := m.getKillCause(s); recorded != "" {
		killCause = recorded
	}

	state, exitCode := StateCompleted, 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		switch {
		case errors.As(waitErr, &exitErr):
			exitCode = exitErr.ExitCode()
			if killCause != "" {
				state = StateKilled
			} else {
				state = StateFailed
			}
		default:
			exitCode = -1
			state = StateFailed
		}
	} else if killCause != "" {
		state = StateKilled
	}

	if s.pty != nil {
		s.pty.Close()
	}
	if s.pumpDone != nil {
		<-s.pumpDone
	}

	m.mu.Lock()
	s.EndedAt = time.Now()
	s.ExitCode = exitCode
	s.State = state
	s.KillCause = killCause
	infoCopy := s.Info
	m.mu.Unlock()

	s.output.Close()
	close(s.done)

	m.mu.Lock()
	fn := m.onDone
	m.mu.Unlock()
	if fn != nil {
		defer func() { _ = recover() }()
		fn(infoCopy)
	}
}

func (m *Manager) forceKill(s *session) {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = signalProcessGroup(s.cmd.Process.Pid, false)
	timer := time.NewTimer(killGrace)
	defer timer.Stop()
	select {
	case <-s.done:
		return
	case <-timer.C:
	}
	_ = signalProcessGroup(s.cmd.Process.Pid, true)
}

func (m *Manager) Kill(id string) error {
	m.mu.Lock()
	s := m.resolve(id)
	ok := s != nil
	if ok {
		select {
		case <-s.done:
		default:
			if s.KillCause == "" {
				s.KillCause = "killed by user"
			}
		}
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("no such session: %s", id)
	}
	select {
	case <-s.done:
		return nil
	default:
	}
	if s.cancel != nil {
		s.cancel()
		return nil
	}
	go m.forceKill(s)
	return nil
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	running := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		select {
		case <-s.done:
		default:
			running = append(running, s)
		}
	}
	m.mu.Unlock()

	for _, s := range running {
		m.setKillCause(s, "shutdown")
		if s.cancel != nil {
			s.cancel()
		} else if s.cmd != nil && s.cmd.Process != nil {
			_ = signalProcessGroup(s.cmd.Process.Pid, false)
		}
	}
	deadline := time.After(killGrace)
	for _, s := range running {
		select {
		case <-s.done:
		case <-deadline:
			if s.cmd != nil && s.cmd.Process != nil {
				_ = signalProcessGroup(s.cmd.Process.Pid, true)
			}
		}
	}
	finalDeadline := time.After(shutdownGrace)
	for _, s := range running {
		select {
		case <-s.done:
		case <-finalDeadline:
		}
	}
}

// --- query methods ---

func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}

func (m *Manager) Get(id string) (Info, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.resolve(id)
	if s == nil {
		return Info{}, false
	}
	return s.Info, true
}

// resolve finds a session by ID first, then by name.
func (m *Manager) resolve(idOrName string) *session {
	if s, ok := m.sessions[idOrName]; ok {
		return s
	}
	for _, s := range m.sessions {
		if s.Name == idOrName {
			return s
		}
	}
	return nil
}

func (m *Manager) RunningCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.sessions {
		if s.State == StateRunning {
			n++
		}
	}
	return n
}

func (m *Manager) Done(id string) <-chan struct{} {
	m.mu.Lock()
	s := m.resolve(id)
	m.mu.Unlock()
	if s == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.done
}

func (m *Manager) Peek(id string, n int) (string, error) {
	m.mu.Lock()
	s := m.resolve(id)
	m.mu.Unlock()
	if s == nil {
		return "", fmt.Errorf("no such session: %s", id)
	}
	if n <= 0 {
		n = 30
	}
	return s.output.TailLines(n), nil
}

func (m *Manager) PeekBytes(id string, n int) (string, error) {
	m.mu.Lock()
	s := m.resolve(id)
	m.mu.Unlock()
	if s == nil {
		return "", fmt.Errorf("no such session: %s", id)
	}
	return s.output.TailBytes(n), nil
}

func (m *Manager) PeekOrEmpty(id string, n int) string {
	s, _ := m.Peek(id, n)
	return s
}

const defaultPeekNewMax int64 = 40 * 1024

func (m *Manager) PeekNew(id string, maxBytes int64) (string, bool, error) {
	m.mu.Lock()
	s := m.resolve(id)
	m.mu.Unlock()
	if s == nil {
		return "", false, fmt.Errorf("no such session: %s", id)
	}
	if maxBytes <= 0 {
		maxBytes = defaultPeekNewMax
	}

	m.mu.Lock()
	offset := s.peekOff
	m.mu.Unlock()

	data, newOff, more, err := s.output.ReadSinceLimit(offset, maxBytes)
	if err != nil {
		return "", false, err
	}

	m.mu.Lock()
	s.peekOff = newOff
	m.mu.Unlock()

	return string(data), more, nil
}

// ReadFrom reads output since the given offset without modifying session state.
// The caller tracks the returned offset for subsequent calls.
func (m *Manager) ReadFrom(id string, offset int64, maxBytes int64) (string, int64, error) {
	m.mu.Lock()
	s := m.resolve(id)
	m.mu.Unlock()
	if s == nil {
		return "", 0, fmt.Errorf("no such session: %s", id)
	}
	if maxBytes <= 0 {
		maxBytes = defaultPeekNewMax
	}
	data, newOff, _, err := s.output.ReadSinceLimit(offset, maxBytes)
	if err != nil {
		return "", offset, err
	}
	return string(data), newOff, nil
}

// Monitor starts a goroutine that periodically reads incremental output
// and calls push with new content. Stops automatically when the session ends.
func (m *Manager) Monitor(id string, interval time.Duration, push func(output string)) {
	m.mu.Lock()
	s := m.resolve(id)
	m.mu.Unlock()
	if s == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var offset int64
		for {
			select {
			case <-s.done:
				if text, _, _ := m.ReadFrom(id, offset, 0); text != "" {
					push(text)
				}
				return
			case <-ticker.C:
				text, newOff, err := m.ReadFrom(id, offset, 0)
				if err != nil {
					return
				}
				offset = newOff
				if text != "" {
					push(text)
				}
			}
		}
	}()
}

func (m *Manager) Write(id string, data []byte) error {
	m.mu.Lock()
	s := m.resolve(id)
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("no such session: %s", id)
	}
	select {
	case <-s.done:
		return fmt.Errorf("session %s already finished", id)
	default:
	}
	if s.pty == nil {
		return fmt.Errorf("session %s does not accept input", id)
	}
	_, err := s.pty.Write(data)
	return err
}

func (m *Manager) Wait(ctx context.Context, id string, timeout time.Duration) (Info, error) {
	m.mu.Lock()
	s := m.resolve(id)
	m.mu.Unlock()
	if s == nil {
		return Info{}, fmt.Errorf("no such session: %s", id)
	}
	var timerC <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		timerC = timer.C
	}
	select {
	case <-s.done:
	case <-timerC:
	case <-ctx.Done():
		return s.Info, ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return s.Info, nil
}

// --- helpers ---

func (m *Manager) setKillCause(s *session, cause string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.KillCause == "" {
		s.KillCause = cause
	}
}

func (m *Manager) getKillCause(s *session) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return s.KillCause
}

func (m *Manager) bufferCap() int {
	if m.bufCap > 0 {
		return m.bufCap
	}
	return DefaultBufferCap
}

func (m *Manager) newBuffer(outputFile string) (*OutputBuffer, error) {
	cap := m.bufferCap()
	buf := &OutputBuffer{
		buf:       make([]byte, 0, min(cap, 64*1024)),
		cap:       cap,
		stripANSI: true,
	}
	if outputFile != "" {
		f, err := os.OpenFile(outputFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open output file: %w", err)
		}
		buf.file = f
	}
	return buf, nil
}

func genID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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

func FormatCompletion(info Info, lastOutput string) string {
	duration := info.EndedAt.Sub(info.StartedAt).Round(time.Second)
	status := "completed"
	switch {
	case info.State == StateKilled:
		status = "killed"
		if info.KillCause != "" {
			status += " (" + info.KillCause + ")"
		}
	case info.ExitCode != 0:
		status = fmt.Sprintf("exited with code %d", info.ExitCode)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<session_completion id=%q name=%q exit_code=%d duration=%q>\n",
		info.ID, info.Name, info.ExitCode, duration.String())
	fmt.Fprintf(&sb, "Background session %s.\n", status)
	if lastOutput != "" {
		sb.WriteString("--- last 20 lines ---\n")
		sb.WriteString(lastOutput)
		sb.WriteString("\n")
	} else {
		sb.WriteString("(no output)\n")
	}
	sb.WriteString("</session_completion>")
	return sb.String()
}

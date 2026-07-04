// Package local launches and tracks `aiscan agent` subprocesses on the hub host
// (hub-hosted nodes), so they can be listed and stopped from the UI. Each child
// dials the hub's loopback address and registers over the normal agent
// WebSocket, so it shows up in the pool like any node.
package local

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/chainreactors/aiscan/pkg/deploy"
)

// agentProc holds the process handle for one launched `aiscan agent` child.
type agentProc struct {
	name string // --ioa-node-name, also the stable handle used to delete it
	pid  int
	cmd  *exec.Cmd
}

// View is the API-facing view of a local agent, cross-referenced with the pool.
type View struct {
	Name       string `json:"name"`
	PID        int    `json:"pid"`
	Registered bool   `json:"registered"` // connected to the hub AgentPool
	Busy       bool   `json:"busy,omitempty"`
}

// Service launches and tracks local `aiscan agent` subprocesses. hubURL supplies
// the (tunnel-owned) hub loopback address the child dials; lookup cross-refs a
// node name against the live pool for registration state. Both are injected so
// this package never imports pkg/web.
type Service struct {
	ioaToken   string
	binaryPath string
	hubURL     func() string
	lookup     func(nodeName string) (registered, busy bool)

	mu     sync.Mutex
	agents []*agentProc
	seq    int
}

// NewService builds a local-agent launcher. ioaToken is baked into the child's
// IOA URL; binaryPath overrides the served binary (empty => os.Executable()).
func NewService(ioaToken, binaryPath string, hubURL func() string, lookup func(nodeName string) (registered, busy bool)) *Service {
	return &Service{ioaToken: ioaToken, binaryPath: binaryPath, hubURL: hubURL, lookup: lookup}
}

// Launch spawns an `aiscan agent` on the hub host, wired to the hub's loopback
// web + IOA endpoints, and tracks it. The LLM provider/model/key arrive via the
// hub's config push on registration, so nothing about the model is passed here.
func (s *Service) Launch(ctx context.Context) (*View, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var hub string
	if s.hubURL != nil {
		hub = s.hubURL()
	}
	if hub == "" {
		return nil, fmt.Errorf("hub local address unknown; cannot launch a local agent (check the web --addr)")
	}
	ioaURL, err := deploy.NodeIOAURL(hub, s.ioaToken)
	if err != nil {
		return nil, fmt.Errorf("build IOA url: %w", err)
	}
	bin := s.binaryPath
	if bin == "" {
		if bin, err = os.Executable(); err != nil {
			return nil, fmt.Errorf("resolve agent binary: %w", err)
		}
	}

	// hubURL() is called before taking s.mu (never nested) to preserve the
	// original lock ordering between the tunnel lock and this roster lock.
	s.mu.Lock()
	s.seq++
	name := fmt.Sprintf("local-%d", s.seq)
	s.mu.Unlock()

	cmd := exec.Command(bin, "agent",
		"--web-url", hub,
		"--ioa-url", ioaURL,
		"--space", "default",
		"--ioa-node-name", name,
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start local agent: %w", err)
	}
	la := &agentProc{name: name, pid: cmd.Process.Pid, cmd: cmd}

	s.mu.Lock()
	s.agents = append(s.agents, la)
	s.mu.Unlock()

	// Drop the entry once the child exits (on its own or via Stop) so the list
	// never shows a dead node.
	go func() {
		_ = cmd.Wait()
		s.remove(la)
	}()

	view := s.view(la)
	return &view, nil
}

// List returns the tracked local agents (launch order), cross-referenced with
// the pool for connection state.
func (s *Service) List() []View {
	s.mu.Lock()
	all := make([]*agentProc, len(s.agents))
	copy(all, s.agents)
	s.mu.Unlock()

	// view()'s pool lookup runs outside s.mu so the roster lock never nests with
	// the pool lock.
	views := make([]View, 0, len(all))
	for _, la := range all {
		views = append(views, s.view(la))
	}
	return views
}

// Stop kills a tracked local agent by name and drops it from the roster.
func (s *Service) Stop(name string) error {
	s.mu.Lock()
	var found *agentProc
	for _, la := range s.agents {
		if la.name == name {
			found = la
			break
		}
	}
	s.mu.Unlock()
	if found == nil {
		return fmt.Errorf("local agent %s not found", name)
	}
	s.remove(found)
	killProc(found.cmd)
	return nil
}

// StopAll kills every tracked local agent (hub shutdown), so none are left
// orphaned once the hub — which holds the only handle to them — exits.
func (s *Service) StopAll() {
	s.mu.Lock()
	all := s.agents
	s.agents = nil
	s.mu.Unlock()
	for _, la := range all {
		killProc(la.cmd)
	}
}

// remove drops a specific tracked agent (called when its process exits).
func (s *Service) remove(la *agentProc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.agents {
		if x == la {
			s.agents = append(s.agents[:i], s.agents[i+1:]...)
			return
		}
	}
}

// view cross-references a child against the live pool (matched by node name) to
// report whether it has connected yet.
func (s *Service) view(la *agentProc) View {
	v := View{Name: la.name, PID: la.pid}
	if s.lookup != nil {
		v.Registered, v.Busy = s.lookup(la.name)
	}
	return v
}

// killProc terminates a child process (best-effort; no-op once it has exited).
func killProc(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

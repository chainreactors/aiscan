package manager

import (
	"context"

	"github.com/chainreactors/aiscan/pkg/deploy/local"
)

// hubURL returns the loopback hub address a local agent should dial (reuses the
// address configured for the outbound tunnel, derived from the web --addr).
// It reads tunnel-owned state, so it stays on the hub side and is injected into
// the local-agent service.
func (m *DeployManager) hubURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.localURL
}

// localLookup cross-references a node name against the live AgentPool (matched by
// IOA node name, falling back to the agent name) to report connection state.
func (m *DeployManager) localLookup(nodeName string) (registered, busy bool) {
	for _, a := range m.pool.AgentSnapshots() {
		if a.NodeName == nodeName {
			return true, a.Busy
		}
	}
	return false, false
}

// LaunchLocal spawns an `aiscan agent` on the hub host and tracks it.
func (m *DeployManager) LaunchLocal(ctx context.Context) (*local.View, error) {
	return m.local.Launch(ctx)
}

// ListLocal returns the tracked local agents, cross-referenced with the pool.
func (m *DeployManager) ListLocal() []local.View { return m.local.List() }

// StopLocal kills a tracked local agent by name and drops it from the roster.
func (m *DeployManager) StopLocal(name string) error { return m.local.Stop(name) }

// StopAllLocal kills every tracked local agent (hub shutdown).
func (m *DeployManager) StopAllLocal() { m.local.StopAll() }

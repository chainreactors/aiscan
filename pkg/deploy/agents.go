package deploy

// AgentSnapshot is the minimal live-agent state the deploy layer needs to
// cross-reference nodes: the IOA node name, the current agent id, and whether
// the agent is busy.
type AgentSnapshot struct {
	NodeName string // IOA node name (agent identity node name, or agent name fallback)
	AgentID  string
	Busy     bool
}

// AgentLister exposes a snapshot of the currently-registered agents. The hub's
// AgentPool satisfies this via an adapter, so the deploy layer never depends on
// the web package's concrete pool type.
type AgentLister interface {
	AgentSnapshots() []AgentSnapshot
}

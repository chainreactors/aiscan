package web

import "github.com/chainreactors/aiscan/pkg/deploy"

// NewPoolLister adapts an *AgentPool to deploy.AgentLister so the deploy layer
// can cross-reference live agents without depending on the web pool type.
func NewPoolLister(pool *AgentPool) deploy.AgentLister { return poolLister{pool} }

// poolLister adapts *AgentPool to deploy.AgentLister, applying the node-name
// fallback (identity node name, else agent name) once at the boundary so the
// deploy layer works in terms of node names alone.
type poolLister struct{ pool *AgentPool }

func (p poolLister) AgentSnapshots() []deploy.AgentSnapshot {
	if p.pool == nil {
		return nil
	}
	list := p.pool.List()
	out := make([]deploy.AgentSnapshot, 0, len(list))
	for _, a := range list {
		name := a.Identity.NodeName
		if name == "" {
			name = a.Name
		}
		out = append(out, deploy.AgentSnapshot{NodeName: name, AgentID: a.ID, Busy: a.Busy})
	}
	return out
}

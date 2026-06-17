package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSMessage is the single message type for all agent↔web communication.
type WSMessage struct {
	Type    string          `json:"type"`
	TaskID  string          `json:"task_id,omitempty"`
	Data    string          `json:"data,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// AgentInfo is the public view of a connected agent.
type AgentInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Commands  []string  `json:"commands,omitempty"`
	Busy      bool      `json:"busy"`
	ConnectAt time.Time `json:"connected_at"`
}

type taskResult struct {
	Output string
	Result json.RawMessage
	Err    string
}

type remoteAgent struct {
	id        string
	name      string
	commands  []string
	conn      *websocket.Conn
	sendCh    chan WSMessage
	connectAt time.Time

	mu    sync.Mutex
	tasks map[string]chan taskResult
	done  chan struct{}
}

func (a *remoteAgent) info() AgentInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	return AgentInfo{
		ID:        a.id,
		Name:      a.name,
		Commands:  a.commands,
		Busy:      len(a.tasks) > 0,
		ConnectAt: a.connectAt,
	}
}

// AgentPool manages connected remote aiscan agents via WebSocket.
type AgentPool struct {
	mu     sync.RWMutex
	agents map[string]*remoteAgent
	hub    *Hub
}

func NewAgentPool(hub *Hub) *AgentPool {
	return &AgentPool{
		agents: make(map[string]*remoteAgent),
		hub:    hub,
	}
}

func (p *AgentPool) register(a *remoteAgent) {
	p.mu.Lock()
	p.agents[a.id] = a
	p.mu.Unlock()
}

func (p *AgentPool) unregister(id string) {
	p.mu.Lock()
	a, ok := p.agents[id]
	delete(p.agents, id)
	p.mu.Unlock()
	if ok {
		a.mu.Lock()
		for _, ch := range a.tasks {
			close(ch)
		}
		a.tasks = nil
		a.mu.Unlock()
	}
}

func (p *AgentPool) get(id string) *remoteAgent {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.agents[id]
}

func (p *AgentPool) List() []AgentInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]AgentInfo, 0, len(p.agents))
	for _, a := range p.agents {
		out = append(out, a.info())
	}
	return out
}

func (p *AgentPool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.agents)
}

// Pick selects an idle agent, or any agent if none idle.
func (p *AgentPool) Pick() *remoteAgent {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var fallback *remoteAgent
	for _, a := range p.agents {
		a.mu.Lock()
		busy := len(a.tasks) > 0
		a.mu.Unlock()
		if !busy {
			return a
		}
		if fallback == nil {
			fallback = a
		}
	}
	return fallback
}

// DispatchCommand sends a command to an agent and returns a channel for the result.
func (p *AgentPool) DispatchCommand(agentID, taskID, command string) (<-chan taskResult, error) {
	a := p.get(agentID)
	if a == nil {
		return nil, fmt.Errorf("agent %s not connected", agentID)
	}
	ch := make(chan taskResult, 1)
	a.mu.Lock()
	a.tasks[taskID] = ch
	a.mu.Unlock()

	select {
	case a.sendCh <- WSMessage{Type: "exec", TaskID: taskID, Data: command}:
	default:
		a.mu.Lock()
		delete(a.tasks, taskID)
		a.mu.Unlock()
		close(ch)
		return nil, fmt.Errorf("agent %s send channel full", agentID)
	}
	return ch, nil
}

func (p *AgentPool) CancelTask(agentID, taskID string) {
	a := p.get(agentID)
	if a == nil {
		return
	}
	select {
	case a.sendCh <- WSMessage{Type: "cancel", TaskID: taskID}:
	default:
	}
}

// --- WebSocket handler ---

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// HandleWS upgrades to WebSocket and manages the agent lifecycle.
// This single endpoint replaces register + stream + output + complete.
func (p *AgentPool) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// First message must be register.
	var reg WSMessage
	if err := conn.ReadJSON(&reg); err != nil || reg.Type != "register" {
		conn.Close()
		return
	}
	var info struct {
		Name     string   `json:"name"`
		Commands []string `json:"commands"`
	}
	if reg.Payload != nil {
		_ = json.Unmarshal(reg.Payload, &info)
	}
	if info.Name == "" {
		info.Name = "agent"
	}

	agent := &remoteAgent{
		id:        generateID(),
		name:      info.Name,
		commands:  info.Commands,
		conn:      conn,
		sendCh:    make(chan WSMessage, 32),
		connectAt: time.Now(),
		tasks:     make(map[string]chan taskResult),
		done:      make(chan struct{}),
	}
	p.register(agent)
	defer func() {
		p.unregister(agent.id)
		conn.Close()
		close(agent.done)
	}()

	// Send connected ack.
	ack, _ := json.Marshal(map[string]string{"agent_id": agent.id, "name": agent.name})
	_ = conn.WriteJSON(WSMessage{Type: "connected", Payload: ack})

	// Write goroutine: sendCh → WebSocket.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case msg, ok := <-agent.sendCh:
				if !ok {
					return
				}
				if err := conn.WriteJSON(msg); err != nil {
					return
				}
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			case <-agent.done:
				return
			}
		}
	}()

	// Read loop: WebSocket → dispatch.
	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		p.handleAgentMessage(agent, msg)
	}
}

func (p *AgentPool) handleAgentMessage(a *remoteAgent, msg WSMessage) {
	switch msg.Type {
	case "output":
		if p.hub != nil && msg.TaskID != "" {
			raw, _ := json.Marshal(msg)
			p.hub.Broadcast(msg.TaskID, HubEvent{Type: "output", Data: raw})
		}

	case "complete":
		a.mu.Lock()
		ch, ok := a.tasks[msg.TaskID]
		if ok {
			delete(a.tasks, msg.TaskID)
		}
		a.mu.Unlock()
		if ok && ch != nil {
			res := taskResult{Output: msg.Data, Result: msg.Payload}
			ch <- res
			close(ch)
		}

	case "error":
		a.mu.Lock()
		ch, ok := a.tasks[msg.TaskID]
		if ok {
			delete(a.tasks, msg.TaskID)
		}
		a.mu.Unlock()
		if ok && ch != nil {
			ch <- taskResult{Err: msg.Data}
			close(ch)
		}

	default:
		// Agent events (agent.*, log.*, scanner.*) — forward raw payload to hub.
		if p.hub != nil && msg.TaskID != "" {
			data := msg.Payload
			if data == nil {
				data, _ = json.Marshal(msg)
			}
			p.hub.Broadcast(msg.TaskID, HubEvent{Type: msg.Type, Data: data})
		}
	}
}

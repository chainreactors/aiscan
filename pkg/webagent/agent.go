package webagent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/core/eventbus"
	"github.com/chainreactors/aiscan/core/output"
	"github.com/chainreactors/aiscan/core/runner"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/evaluator"
	"github.com/chainreactors/aiscan/pkg/agent/tmux"
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tui"
	"github.com/chainreactors/aiscan/pkg/webproto"
	"github.com/chainreactors/utils/pty"
	"github.com/gorilla/websocket"
)

func Run(ctx context.Context, option *cfg.Option, logger telemetry.Logger) error {
	if option.WebURL != "" {
		remoteOpt, err := cfg.FetchRemoteConfig(option.WebURL)
		if err != nil {
			logger.Warnf("fetch remote config from %s: %s (continuing with local config)", option.WebURL, err)
		} else {
			logger.Infof("fetched remote config from %s", option.WebURL)
			cfg.MergeRemoteOption(option, remoteOpt)
		}
	}

	rt, err := runner.NewAgentRuntime(ctx, option, logger, &runner.RuntimeConfig{
		NoOutput:         true,
		IOA:              remoteIOAConfig(option),
		ProviderOptional: true,
	})
	if err != nil {
		return err
	}
	defer rt.Close()

	connectionDone := make(chan struct{})
	go func() {
		defer close(connectionDone)
		_ = rt.App.WaitEngines(ctx)
		logger.Debugf("web agent connection to %s", option.WebURL)
		_ = RunConnectionRuntime(ctx, option.WebURL, rt.NodeName, rt)
	}()

	if rt.App.Provider == nil {
		logger.Warnf("no LLM provider configured; remote REPL and PTY are available, autonomous agent loop is disabled")
		<-ctx.Done()
		<-connectionDone
		return nil
	}

	task, err := webAgentTask(option)
	if err != nil {
		return err
	}
	if task == "" {
		logger.Infof("web agent connected; remote REPL and PTY are available")
		<-ctx.Done()
		<-connectionDone
		return nil
	}

	loopCfg := rt.Config.WithSystemPrompt(rt.SystemPrompt).WithStream(true)
	_, err = agent.NewAgent(loopCfg).Run(ctx, task)

	<-connectionDone
	return err
}

func RunConnection(ctx context.Context, serverURL, name string, reg *commands.CommandRegistry, bus *eventbus.Bus[agent.Event]) error {
	return runConnection(ctx, serverURL, name, reg, bus, nil)
}

func RunConnectionRuntime(ctx context.Context, serverURL, name string, rt *runner.AgentRuntime) error {
	if rt == nil || rt.App == nil {
		return fmt.Errorf("agent runtime is not configured")
	}
	return runConnection(ctx, serverURL, name, rt.App.Commands, rt.Bus, rt)
}

func runConnection(ctx context.Context, serverURL, name string, reg *commands.CommandRegistry, bus *eventbus.Bus[agent.Event], rt *runner.AgentRuntime) error {
	attempt := 0
	for {
		if ctx.Err() != nil {
			return nil //nolint:nilerr // intentional: suppress error on context cancellation
		}
		err := runConnectionOnce(ctx, serverURL, name, reg, bus, rt)
		if ctx.Err() != nil {
			return nil //nolint:nilerr // intentional: suppress error on context cancellation
		}
		if err != nil {
			delay := agent.RetryDelay(attempt)
			attempt++
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(delay):
			}
		} else {
			attempt = 0
		}
	}
}

// Agent-side WebSocket keepalive. The hub pings every 30s; the agent sends its
// own pings too, but the load-bearing piece is the read deadline that every
// inbound frame (a data message, the hub's ping, or a pong to our ping) pushes
// forward. Without it a half-open connection — hub restarted, or a NAT/firewall
// silently dropping an idle flow with no RST — leaves ReadJSON blocked forever,
// so runConnection's reconnect loop never fires and the node zombies out of the
// pool while its VM is still up. pongWait must comfortably exceed the hub's ping
// period so a single lost ping doesn't trip a reconnect. Both are vars so tests
// can shrink them instead of waiting out a real 70s deadline.
var (
	agentPingPeriod = 30 * time.Second
	agentPongWait   = 70 * time.Second
)

func runConnectionOnce(ctx context.Context, serverURL, name string, reg *commands.CommandRegistry, bus *eventbus.Bus[agent.Event], rt *runner.AgentRuntime) error {
	if reg == nil {
		return fmt.Errorf("command registry is nil")
	}
	wsURL := httpToWS(serverURL) + "/api/agent/ws"
	conn, wsResp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if wsResp != nil && wsResp.Body != nil {
		wsResp.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	// Hold a read deadline that inbound traffic — data frames, the hub's periodic
	// ping, or a pong to our own ping — keeps pushing forward. If the connection
	// half-opens (hub gone, or an idle flow dropped without a RST) frames stop
	// arriving, the deadline fires, ReadJSON returns an error, and runConnection
	// reconnects instead of the read blocking forever.
	resetReadDeadline := func() { _ = conn.SetReadDeadline(time.Now().Add(agentPongWait)) }
	resetReadDeadline()
	conn.SetPongHandler(func(string) error { resetReadDeadline(); return nil })
	defaultPing := conn.PingHandler()
	conn.SetPingHandler(func(appData string) error {
		resetReadDeadline()
		return defaultPing(appData) // preserve gorilla's default pong reply
	})

	sendCh := make(chan webproto.Message, 64)
	done := make(chan struct{})
	var wg sync.WaitGroup
	defer wg.Wait()
	defer close(done)

	send := func(m webproto.Message) {
		select {
		case sendCh <- m:
		case <-done:
		}
	}

	stats := newAgentStatsTracker()
	regPayload, _ := json.Marshal(agentRegisterPayload(name, reg, rt, stats.Snapshot()))
	if err := conn.WriteJSON(webproto.Message{Type: "register", Payload: regPayload}); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	var ack webproto.Message
	if err := conn.ReadJSON(&ack); err != nil || ack.Type != "connected" {
		return fmt.Errorf("expected connected ack")
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		ping := time.NewTicker(agentPingPeriod)
		defer ping.Stop()
		for {
			select {
			case msg, ok := <-sendCh:
				if !ok {
					return
				}
				_ = conn.WriteJSON(msg)
			case <-ping.C:
				// WriteControl is safe to call concurrently with the pong the read
				// side's ping handler emits. A failure means the peer is gone; the
				// read loop's deadline then surfaces it and triggers a reconnect.
				_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
			case <-ctx.Done():
				_ = conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			case <-done:
				return
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	var taskMu sync.Mutex
	tasks := make(map[string]context.CancelFunc)
	// taskSessions maps an active chat task to the agent session it belongs to,
	// so streaming agent events are delivered only to the conversation that
	// produced them instead of every concurrent task on this connection.
	taskSessions := make(map[string]string)
	if bus != nil {
		unsub := bus.Subscribe(func(e agent.Event) {
			if next, ok := stats.Observe(e); ok {
				statsPayload, _ := json.Marshal(next)
				send(webproto.Message{Type: "agent.stats", Payload: statsPayload})
			}
			rec := output.NewRecord(output.TypeAgent, e)
			payload, _ := json.Marshal(rec)
			data := agentEventSummary(e)
			if data == "" {
				data = string(payload)
			}
			taskMu.Lock()
			taskIDs := make([]string, 0, len(taskSessions))
			for taskID, sessionID := range taskSessions {
				if eventMatchesSession(e, sessionID) {
					taskIDs = append(taskIDs, taskID)
				}
			}
			taskMu.Unlock()
			for _, taskID := range taskIDs {
				send(webproto.Message{
					Type:    "agent." + string(e.Type),
					TaskID:  taskID,
					Data:    data,
					Payload: payload,
				})
			}
		})
		defer unsub()
	}

	ptyRouter := newPTYRouter(reg, rt)
	defer ptyRouter.Close()
	chatRuntime := newChatRuntimeManager(rt)
	defer chatRuntime.CloseStale()
	if mgr := registryPTYManager(reg); mgr != nil {
		unsub := subscribePTYSessions(ctx, mgr, ptyRouter, send)
		defer unsub()
	}

	for {
		var msg webproto.Message
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		resetReadDeadline()
		if ctx.Err() != nil {
			return nil
		}

		if strings.HasPrefix(msg.Type, "pty.") {
			frame, err := webproto.MessageToFrame(msg)
			if err != nil {
				send(webproto.Message{Type: "pty.error", StreamID: msg.StreamID, Data: err.Error()})
				continue
			}
			ptyRouter.Handle(ctx, frame, func(out pty.Frame) {
				send(webproto.FrameToMessage(out))
			})
			continue
		}

		switch msg.Type {
		case "exec":
			taskCtx, cancel := context.WithCancel(ctx)
			taskMu.Lock()
			tasks[msg.TaskID] = cancel
			// Exec tasks are not bound to an agent session; untagged telemetry
			// they emit on the bus is routed to them via the empty-session case.
			taskSessions[msg.TaskID] = ""
			taskMu.Unlock()
			go func(m webproto.Message, tCtx context.Context, tCancel context.CancelFunc) {
				defer tCancel()
				defer func() {
					taskMu.Lock()
					delete(tasks, m.TaskID)
					delete(taskSessions, m.TaskID)
					taskMu.Unlock()
				}()
				execCommand(tCtx, m.TaskID, m.Data, reg, send)
			}(msg, taskCtx, cancel)

		case "chat":
			sessionID := normalizeChatSessionID(parseChatPayload(msg).SessionID)
			taskCtx, cancel := context.WithCancel(ctx)
			taskMu.Lock()
			tasks[msg.TaskID] = cancel
			taskSessions[msg.TaskID] = sessionID
			taskMu.Unlock()
			go func(m webproto.Message, tCtx context.Context, tCancel context.CancelFunc) {
				defer tCancel()
				defer func() {
					taskMu.Lock()
					delete(tasks, m.TaskID)
					delete(taskSessions, m.TaskID)
					taskMu.Unlock()
				}()
				runChatPrompt(tCtx, m, rt, chatRuntime, send)
			}(msg, taskCtx, cancel)

		case "upload":
			go handleFileUpload(msg, send)

		case "config.update":
			if err := applyConfigUpdate(ctx, msg, rt, chatRuntime); err != nil {
				send(webproto.Message{Type: "config.error", Data: err.Error()})
				continue
			}
			send(webproto.Message{Type: "identity", Payload: mustJSON(chatRuntime.identity())})

		case "cancel":
			taskMu.Lock()
			if cancel, ok := tasks[msg.TaskID]; ok {
				cancel()
			}
			taskMu.Unlock()
		}
	}
}

func newPTYRouter(reg *commands.CommandRegistry, rt *runner.AgentRuntime) *pty.Router {
	mgr := registryPTYManager(reg)
	var baseMgr *pty.Manager
	if mgr != nil {
		baseMgr = mgr.Manager
	}
	openers := pty.DefaultOpeners(baseMgr, pty.DefaultSessionTimeout, pty.DefaultEnv())
	if rt != nil {
		openers["repl"] = runner.NewRemoteREPLOpener(rt, mgr)
	}
	// Replay the whole retained session buffer on attach instead of the router's
	// 64KB default. The always-on "main-repl" is long-lived, so re-opening its
	// terminal must restore the full conversation, not just the last screenful —
	// otherwise the head of the transcript appears truncated ("残缺"). Sized to
	// the buffer's own retention cap so we hand back everything we still hold.
	return pty.NewRouter(baseMgr, pty.WithOpeners(openers), pty.WithAttachBytes(pty.DefaultBufferCap))
}

func registryPTYManager(reg *commands.CommandRegistry) *tmux.Manager {
	if reg == nil {
		return nil
	}
	tool, ok := reg.GetTool("bash")
	if !ok {
		return nil
	}
	manager, ok := tool.(interface {
		Manager() *tmux.Manager
	})
	if !ok {
		return nil
	}
	return manager.Manager()
}

func subscribePTYSessions(ctx context.Context, mgr *tmux.Manager, router *pty.Router, send func(webproto.Message)) func() {
	if mgr == nil || router == nil || send == nil {
		return func() {}
	}
	activity := newPTYActivityTracker()
	notify := make(chan tmux.EventAction, 1)
	unsub := mgr.Subscribe(func(ev tmux.Event) {
		activity.Observe(ev)
		switch ev.Action {
		case tmux.EventSessionCreated, tmux.EventSessionUpdated, tmux.EventSessionOutput, tmux.EventSessionClosed:
			select {
			case notify <- ev.Action:
			default:
			}
		}
	})
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(350 * time.Millisecond)
		defer ticker.Stop()
		dirty := false
		for {
			select {
			case action := <-notify:
				if action == tmux.EventSessionOutput {
					dirty = true
					continue
				}
				dirty = false
				broadcastPTYSessions(mgr, router, activity, send)
			case <-ticker.C:
				if dirty {
					dirty = false
					broadcastPTYSessions(mgr, router, activity, send)
				}
			case <-ctx.Done():
				return
			case <-stop:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			unsub()
			close(stop)
		})
	}
}

func broadcastPTYSessions(mgr *tmux.Manager, router *pty.Router, activity *ptyActivityTracker, send func(webproto.Message)) {
	streamIDs := router.StreamIDs()
	if len(streamIDs) == 0 {
		return
	}
	sessions := ptySessionViews(mgr.List(), activity)
	for _, streamID := range streamIDs {
		payload, _ := json.Marshal(map[string]any{"sessions": sessions})
		send(webproto.Message{Type: "pty.sessions", StreamID: streamID, Payload: payload})
	}
}

type ptyActivity struct {
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`
	ActivitySeq    int64     `json:"activity_seq,omitempty"`
	OutputBytes    int64     `json:"output_bytes,omitempty"`
}

type ptyActivityTracker struct {
	mu       sync.Mutex
	sessions map[string]ptyActivity
}

type ptySessionView struct {
	tmux.Info
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`
	ActivitySeq    int64     `json:"activity_seq,omitempty"`
	OutputBytes    int64     `json:"output_bytes,omitempty"`
}

func newPTYActivityTracker() *ptyActivityTracker {
	return &ptyActivityTracker{sessions: make(map[string]ptyActivity)}
}

func (t *ptyActivityTracker) Observe(ev tmux.Event) {
	if t == nil || ev.Info.ID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	activity := t.sessions[ev.Info.ID]
	now := time.Now()
	if activity.LastActivityAt.IsZero() {
		activity.LastActivityAt = ev.Info.StartedAt
		if activity.LastActivityAt.IsZero() {
			activity.LastActivityAt = now
		}
	}
	switch ev.Action {
	case tmux.EventSessionOutput:
		activity.LastActivityAt = now
		activity.ActivitySeq++
		activity.OutputBytes += int64(ev.OutputBytes)
	case tmux.EventSessionCreated, tmux.EventSessionUpdated, tmux.EventSessionClosed:
		activity.LastActivityAt = now
		activity.ActivitySeq++
	}
	t.sessions[ev.Info.ID] = activity
}

func (t *ptyActivityTracker) Snapshot(id string) ptyActivity {
	if t == nil || id == "" {
		return ptyActivity{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessions[id]
}

func ptySessionViews(sessions []tmux.Info, activity *ptyActivityTracker) []ptySessionView {
	views := make([]ptySessionView, 0, len(sessions))
	for _, session := range sessions {
		snapshot := activity.Snapshot(session.ID)
		if snapshot.LastActivityAt.IsZero() {
			snapshot.LastActivityAt = session.EndedAt
		}
		if snapshot.LastActivityAt.IsZero() {
			snapshot.LastActivityAt = session.StartedAt
		}
		views = append(views, ptySessionView{
			Info:           session,
			LastActivityAt: snapshot.LastActivityAt,
			ActivitySeq:    snapshot.ActivitySeq,
			OutputBytes:    snapshot.OutputBytes,
		})
	}
	return views
}

func execCommand(ctx context.Context, taskID, cmdLine string, reg *commands.CommandRegistry, send func(webproto.Message)) {
	tokens, err := commands.SplitCommandLine(cmdLine)
	if err != nil {
		send(webproto.Message{Type: "error", TaskID: taskID, Data: err.Error()})
		return
	}
	if len(tokens) == 0 {
		send(webproto.Message{Type: "error", TaskID: taskID, Data: "empty command"})
		return
	}

	writer := &streamWriter{taskID: taskID, sendFn: send}

	if cmd, ok := reg.Get(tokens[0]); ok {
		if sc, ok := cmd.(interface {
			ExecuteStructured(ctx context.Context, args []string, stream io.Writer) (string, *output.Result, error)
		}); ok {
			out, result, err := sc.ExecuteStructured(ctx, tokens[1:], writer)
			writer.flush()
			if err != nil {
				send(webproto.Message{Type: "error", TaskID: taskID, Data: err.Error()})
				return
			}
			var payload json.RawMessage
			if result != nil {
				payload, _ = json.Marshal(result)
			}
			send(webproto.Message{Type: "complete", TaskID: taskID, Data: out, Payload: payload})
			return
		}
	}

	out, err := reg.ExecuteArgsStreaming(ctx, tokens, writer)
	writer.flush()
	if err != nil {
		send(webproto.Message{Type: "error", TaskID: taskID, Data: err.Error()})
		return
	}
	send(webproto.Message{Type: "complete", TaskID: taskID, Data: out})
}

type chatRuntimeManager struct {
	rt                *runner.AgentRuntime
	mu                sync.Mutex
	sessions          map[string]*agent.Agent
	stale             []any
	saveSessionActive bool
	saveSessionUnsub  func()
}

func newChatRuntimeManager(rt *runner.AgentRuntime) *chatRuntimeManager {
	m := &chatRuntimeManager{
		rt:       rt,
		sessions: make(map[string]*agent.Agent),
	}
	if rt != nil && rt.Option != nil {
		m.saveSessionActive = rt.Option.SaveSession
	}
	return m
}

func (m *chatRuntimeManager) agentFor(sessionID string) (*agent.Agent, error) {
	if m == nil || m.rt == nil || m.rt.App == nil {
		return nil, fmt.Errorf("agent runtime is not configured")
	}
	sessionID = normalizeChatSessionID(sessionID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if ag := m.sessions[sessionID]; ag != nil {
		return ag, nil
	}
	// Tag the agent with its session id so the events it emits on the shared
	// runtime bus can be routed back to this conversation only.
	ag := agent.NewAgent(m.rt.Config.WithSystemPrompt(m.rt.SystemPrompt).WithStream(true).WithSessionID(sessionID))
	m.sessions[sessionID] = ag
	return ag, nil
}

func (m *chatRuntimeManager) providerConfigured() bool {
	if m == nil || m.rt == nil || m.rt.App == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rt.App.Provider != nil
}

func (m *chatRuntimeManager) identity() webproto.AgentIdentity {
	if m == nil {
		return webproto.AgentIdentity{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return agentIdentity(m.rt)
}

func (m *chatRuntimeManager) evalProvider() (agent.Provider, string, *eventbus.Bus[agent.Event]) {
	if m == nil || m.rt == nil || m.rt.App == nil {
		return nil, "", nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	model := m.rt.App.ProviderConfig.Model
	if m.rt.Option != nil && m.rt.Option.EvalModel != "" {
		model = m.rt.Option.EvalModel
	}
	return m.rt.App.Provider, model, m.rt.Bus
}

func (m *chatRuntimeManager) runREPLLine(ctx context.Context, line string, ag *agent.Agent) (string, error) {
	if m == nil || m.rt == nil {
		return "", fmt.Errorf("agent runtime is not configured")
	}
	// Snapshot the runtime under the lock, then run the REPL line UNLOCKED. The
	// console executes an agent turn that emits bus events synchronously; the
	// SaveSession subscriber (setSaveSessionLocked) re-acquires m.mu inside that
	// Emit, so holding m.mu across execution both head-of-line-blocks every other
	// chat op and deadlocks outright when a slash command fires EventAgentEnd.
	// agent.Agent already serializes concurrent runs on the same session.
	m.mu.Lock()
	rt := m.rt
	m.mu.Unlock()
	return runChatREPLLine(ctx, line, rt, ag)
}

func (m *chatRuntimeManager) applyDistributeConfigContext(ctx context.Context, dc webproto.DistributeConfig) error {
	if m == nil || m.rt == nil || m.rt.App == nil {
		return fmt.Errorf("agent runtime is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	baseOpt := cfg.Option{}
	if m.rt.Option != nil {
		baseOpt = *m.rt.Option
	}
	currentKey := m.rt.App.ProviderConfig.APIKey
	logger := m.rt.Config.Logger
	if logger == nil {
		logger = telemetry.NopLogger()
	}
	m.mu.Unlock()

	nextOpt := cfg.ApplyDistributeConfig(baseOpt, dc)
	if strings.TrimSpace(nextOpt.APIKey) == "" {
		nextOpt.APIKey = currentKey
	}
	nextApp, err := runner.NewAgentRuntimeApp(ctx, &nextOpt, logger, &runner.AgentRuntimeAppConfig{
		ProviderOptional: true,
		Configure: func(appCfg *cfg.RuntimeConfig) {
			if dc.Search.TavilyKeys != "" {
				appCfg.Tools.TavilyKeys = dc.Search.TavilyKeys
			}
			if strings.TrimSpace(dc.Scan.Verify) != "" {
				appCfg.Scanner.VerifyMode = dc.Scan.Verify
			}
			if dc.Scan.VerifyTimeout > 0 {
				appCfg.Scanner.AITimeout = dc.Scan.VerifyTimeout
			}
		},
	})
	if err != nil {
		return fmt.Errorf("reload agent runtime: %w", err)
	}
	if err := nextApp.WaitEngines(ctx); err != nil {
		nextApp.Close()
		return fmt.Errorf("reload agent engines: %w", err)
	}
	if ioaCfg := remoteIOAConfig(&nextOpt); ioaCfg != nil {
		if err := nextApp.InitIOA(ctx, *ioaCfg); err != nil {
			logger.Warnf("reload agent IOA config: %s", err)
		}
	}
	runner.ConfigureAgentAppCommands(nextApp, m.rt)

	m.mu.Lock()
	defer m.mu.Unlock()
	oldApp := m.rt.App
	if oldApp != nil && oldApp.Commands != nil && nextApp.Commands != nil {
		oldApp.Commands.ReplaceWith(nextApp.Commands)
		nextApp.Commands = oldApp.Commands
	}
	m.rt.App = nextApp
	// Node identity is fixed at launch and must survive config pushes unchanged.
	// Re-resolving from scratch would mint a fresh random name whenever the config
	// carries none, orphaning the agent's pool/IOA registration (both matched by
	// node name). Adopt an explicit name if the config supplies one; otherwise keep
	// the current name, resolving only if we somehow have none yet.
	if nextOpt.IOANodeName != "" {
		m.rt.NodeName = nextOpt.IOANodeName
	} else if m.rt.NodeName == "" {
		m.rt.NodeName = runner.ResolveIOANodeName(&nextOpt)
	}
	m.rt.Option = &nextOpt
	m.rt.Config.Provider = nextApp.Provider
	m.rt.Config.Fallbacks = nextApp.ProviderFallbacks
	m.rt.Config.Tools = nextApp.Commands
	m.rt.Config.Model = nextApp.ProviderConfig.Model
	m.setSaveSessionLocked(nextOpt.SaveSession)
	m.sessions = make(map[string]*agent.Agent)
	if oldApp != nil && oldApp.Engines != nil {
		m.stale = append(m.stale, oldApp.Engines)
		oldApp.Engines = nil
	}
	return nil
}

func applyConfigUpdate(ctx context.Context, msg webproto.Message, rt *runner.AgentRuntime, sessions *chatRuntimeManager) error {
	if rt == nil || rt.App == nil {
		return fmt.Errorf("agent runtime is not configured")
	}
	var dc webproto.DistributeConfig
	if len(msg.Payload) == 0 {
		return fmt.Errorf("empty config update")
	}
	if err := json.Unmarshal(msg.Payload, &dc); err != nil {
		return fmt.Errorf("decode config update: %w", err)
	}
	return sessions.applyDistributeConfigContext(ctx, dc)
}

func (m *chatRuntimeManager) CloseStale() {
	if m == nil {
		return
	}
	m.mu.Lock()
	stale := append([]any(nil), m.stale...)
	m.stale = nil
	if m.saveSessionUnsub != nil {
		m.saveSessionUnsub()
		m.saveSessionUnsub = nil
	}
	m.mu.Unlock()
	for _, value := range stale {
		if closer, ok := value.(interface{ Close() }); ok {
			closer.Close()
		}
	}
}

func (m *chatRuntimeManager) setSaveSessionLocked(enabled bool) {
	if enabled {
		if m.saveSessionActive || m.rt == nil || m.rt.Bus == nil {
			return
		}
		sessDir := cfg.DataSubDir("sessions")
		m.saveSessionUnsub = m.rt.Bus.Subscribe(func(ev agent.Event) {
			if ev.Type != agent.EventAgentEnd || len(ev.Messages) == 0 {
				return
			}
			m.mu.Lock()
			model, provider := "", ""
			if m.rt != nil && m.rt.Option != nil {
				model = m.rt.Option.Model
				provider = m.rt.Option.Provider
			}
			m.mu.Unlock()
			_ = agent.SaveSession(sessDir, &agent.SessionData{
				Model:    model,
				Provider: provider,
				Messages: ev.Messages,
			})
		})
		m.saveSessionActive = true
		return
	}
	if m.saveSessionUnsub != nil {
		m.saveSessionUnsub()
		m.saveSessionUnsub = nil
		m.saveSessionActive = false
	}
}

// normalizeChatSessionID canonicalizes a chat session id so the value used to
// tag an agent matches the value tracked for event routing.
func normalizeChatSessionID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "default"
	}
	return id
}

// eventMatchesSession reports whether an agent event belongs to the given chat
// session. Sub-agent events carry the spawning agent's session as their parent,
// so those are matched too, keeping nested activity in the right conversation.
func eventMatchesSession(e agent.Event, sessionID string) bool {
	if e.SessionID == "" && e.ParentSessionID == "" {
		// Untagged telemetry (e.g. emitted by exec commands) goes to tasks that
		// are not bound to an agent session.
		return sessionID == ""
	}
	if sessionID == "" {
		return false
	}
	return e.SessionID == sessionID || e.ParentSessionID == sessionID
}

// errNoProvider is surfaced when a chat prompt needs the LLM but none is set up.
const errNoProvider = "LLM provider is not configured on this agent; configure aiscan.yaml and restart the agent, or prefix commands with !"

func runChatPrompt(ctx context.Context, msg webproto.Message, rt *runner.AgentRuntime, sessions *chatRuntimeManager, send func(webproto.Message)) {
	emitErr := func(data string) { send(webproto.Message{Type: "error", TaskID: msg.TaskID, Data: data}) }
	prompt := strings.TrimSpace(msg.Data)
	if prompt == "" {
		emitErr("empty prompt")
		return
	}
	if rt == nil || rt.App == nil {
		emitErr(errNoProvider)
		return
	}

	req := parseChatPayload(msg)
	isREPL := isREPLCommand(prompt)
	if !isREPL && !sessions.providerConfigured() {
		emitErr(errNoProvider)
		return
	}
	ag, err := sessions.agentFor(req.SessionID)
	if err != nil {
		emitErr(err.Error())
		return
	}

	if isREPL {
		out, err := sessions.runREPLLine(ctx, prompt, ag)
		if err != nil {
			emitErr(err.Error())
			return
		}
		send(webproto.Message{Type: "complete", TaskID: msg.TaskID, Data: out})
		return
	}

	var result *agent.Result
	if req.EvalCriteria != "" {
		result, err = runChatEval(ctx, ag, sessions, req, prompt)
	} else {
		result, err = ag.RunWith(ctx, prompt, persistOverride(req))
	}
	if err != nil {
		emitErr(err.Error())
		return
	}
	if result == nil {
		send(webproto.Message{Type: "complete", TaskID: msg.TaskID})
		return
	}
	send(webproto.Message{Type: "complete", TaskID: msg.TaskID, Data: trimChatOutput(result.Output)})
}

func parseChatPayload(msg webproto.Message) webproto.ChatPayload {
	var payload webproto.ChatPayload
	if len(msg.Payload) > 0 {
		_ = json.Unmarshal(msg.Payload, &payload)
	}
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	return payload
}

// persistOverride returns a per-run config mutator that enables persist mode and
// applies a turn cap when the chat request asked for it, or nil otherwise.
func persistOverride(req webproto.ChatPayload) func(agent.Config) agent.Config {
	if !req.Persist {
		return nil
	}
	return func(c agent.Config) agent.Config {
		c = c.WithPersist(true)
		if req.MaxTurns > 0 {
			c = c.WithMaxTurns(req.MaxTurns)
		} else if c.MaxTurns <= 0 {
			c = c.WithMaxTurns(agent.DefaultPersistMaxTurns)
		}
		return c
	}
}

// runChatEval drives an evaluator-judged completion loop for a chat turn: an
// independent LLM decides whether req.EvalCriteria is met, re-running the agent
// with feedback until it passes or the round cap is hit. This is the web
// equivalent of the CLI's -e/--eval flag. Eval verdict events are tagged with
// the chat session id so they route back to this conversation.
func runChatEval(ctx context.Context, ag *agent.Agent, sessions *chatRuntimeManager, req webproto.ChatPayload, prompt string) (*agent.Result, error) {
	provider, model, bus := sessions.evalProvider()
	if provider == nil {
		return nil, errors.New(errNoProvider)
	}
	result, _, err := evaluator.RunWithEval(ctx, ag, evaluator.EvalLoopConfig{
		Evaluator: evaluator.New(evaluator.Config{
			Provider: provider,
			Model:    model,
		}),
		MaxEvalRounds: req.EvalMaxRounds,
		Goal:          prompt,
		Criteria:      req.EvalCriteria,
		Bus:           bus,
		SessionID:     normalizeChatSessionID(req.SessionID),
	})
	return result, err
}

func handleFileUpload(msg webproto.Message, send func(webproto.Message)) {
	var payload webproto.FileUploadPayload
	if len(msg.Payload) > 0 {
		_ = json.Unmarshal(msg.Payload, &payload)
	}
	if payload.Filename == "" {
		payload.Filename = "upload"
	}

	data, err := base64.StdEncoding.DecodeString(msg.DataB64)
	if err != nil {
		send(webproto.Message{
			Type:    "complete",
			TaskID:  msg.TaskID,
			Payload: mustJSON(webproto.FileUploadResult{Filename: payload.Filename, Error: "decode failed: " + err.Error()}),
		})
		return
	}

	dir := filepath.Join(os.TempDir(), "aiscan-uploads")
	_ = os.MkdirAll(dir, 0o755)
	dest := filepath.Join(dir, payload.Filename)

	if err := os.WriteFile(dest, data, 0o644); err != nil {
		send(webproto.Message{
			Type:    "complete",
			TaskID:  msg.TaskID,
			Payload: mustJSON(webproto.FileUploadResult{Filename: payload.Filename, Error: "write failed: " + err.Error()}),
		})
		return
	}

	send(webproto.Message{
		Type:   "complete",
		TaskID: msg.TaskID,
		Data:   dest,
		Payload: mustJSON(webproto.FileUploadResult{
			Filename: payload.Filename,
			Path:     dest,
			Size:     int64(len(data)),
		}),
	})
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func isREPLCommand(prompt string) bool {
	return strings.HasPrefix(prompt, "/") || strings.HasPrefix(prompt, "!")
}

func runChatREPLLine(ctx context.Context, line string, rt *runner.AgentRuntime, ag *agent.Agent) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	option := rt.Option
	if option != nil {
		copy := *option
		copy.NoColor = true
		option = &copy
	}
	appInfo := tui.AppInfo{
		Provider:          rt.App.Provider,
		ProviderConfig:    rt.App.ProviderConfig,
		ProviderFallbacks: rt.App.ProviderFallbacks,
		Commands:          rt.App.Commands,
		Skills:            rt.App.Skills,
		OnProviderChange: func(provider agent.Provider, providerConfig agent.ProviderConfig) {
			rt.App.Provider = provider
			rt.App.ProviderConfig = providerConfig
			rt.Config.Provider = provider
			rt.Config.Model = providerConfig.Model
		},
	}
	console := tui.NewAgentConsoleWithWriters(ctx, option, appInfo, ag, &stdout, &stderr, rt.Bus)
	_, err := console.ExecuteLineAndWait(line)
	out := trimChatOutput(output.StripANSI(stdout.String()))
	errOut := trimChatOutput(output.StripANSI(stderr.String()))
	if err != nil {
		if errOut != "" {
			return "", fmt.Errorf("%s: %w", errOut, err)
		}
		return "", err
	}
	if out == "" {
		return errOut, nil
	}
	if errOut == "" {
		return out, nil
	}
	return trimChatOutput(out + "\n" + errOut), nil
}

func trimChatOutput(value string) string {
	return strings.TrimRight(value, " \t\r\n")
}

type agentStatsTracker struct {
	mu    sync.Mutex
	stats webproto.AgentStats
}

func newAgentStatsTracker() *agentStatsTracker {
	return &agentStatsTracker{}
}

func (t *agentStatsTracker) Snapshot() webproto.AgentStats {
	if t == nil {
		return webproto.AgentStats{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stats
}

func (t *agentStatsTracker) Observe(e agent.Event) (webproto.AgentStats, bool) {
	if t == nil {
		return webproto.AgentStats{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stats.LastEvent = string(e.Type)
	switch e.Type {
	case agent.EventTurnEnd:
		if e.Turn > t.stats.Turns {
			t.stats.Turns = e.Turn
		}
		if e.Usage != nil {
			t.stats.PromptTokens += e.Usage.PromptTokens
			t.stats.CompletionTokens += e.Usage.CompletionTokens
			t.stats.TotalTokens += e.Usage.TotalTokens
			t.stats.CacheReadTokens += e.Usage.CacheReadTokens
			t.stats.CacheWriteTokens += e.Usage.CacheWriteTokens
		}
	case agent.EventToolExecutionStart:
		t.stats.ToolCalls++
		t.stats.RunningTools++
		t.stats.CurrentTool = e.ToolName
		t.stats.CurrentDetail = toolActivityDetail(e.Arguments)
	case agent.EventToolExecutionEnd:
		if t.stats.RunningTools > 0 {
			t.stats.RunningTools--
		}
		if t.stats.RunningTools == 0 {
			t.stats.CurrentTool = ""
			t.stats.CurrentDetail = ""
		}
	default:
		return t.stats, false
	}
	return t.stats, true
}

func agentRegisterPayload(name string, reg *commands.CommandRegistry, rt *runner.AgentRuntime, stats webproto.AgentStats) webproto.RegisterPayload {
	payload := webproto.RegisterPayload{
		Name:     name,
		Commands: reg.Names(),
		Stats:    stats,
		Identity: agentIdentity(rt),
	}
	if payload.Identity.NodeName == "" {
		payload.Identity.NodeName = name
	}
	return payload
}

func agentIdentity(rt *runner.AgentRuntime) webproto.AgentIdentity {
	identity := webproto.AgentIdentity{
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		PID:          os.Getpid(),
		Capabilities: []string{"repl", "pty", "tmux", "ioa"},
		Meta:         map[string]any{"client": "aiscan", "transport": "web-agent"},
	}
	if host, err := os.Hostname(); err == nil {
		identity.Hostname = host
	}
	if wd, err := os.Getwd(); err == nil {
		identity.WorkingDir = wd
	}
	if current, err := user.Current(); err == nil && current != nil {
		identity.Username = current.Username
	}
	if rt == nil {
		return identity
	}
	identity.NodeName = rt.NodeName
	if rt.Option != nil {
		identity.Space = rt.Option.Space
		identity.IOAURL = publicIOAURL(rt.Option.IOAURL)
	}
	if rt.App != nil {
		if rt.App.IOAClient != nil {
			identity.NodeID = rt.App.IOAClient.NodeID()
		}
		identity.Provider = rt.App.ProviderConfig.Provider
		identity.Model = rt.App.ProviderConfig.Model
	}
	return identity
}

func publicIOAURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil {
		return raw
	}
	parsed.User = nil
	return parsed.String()
}

// toolActivityDetail extracts a short, human-readable target from a tool call's
// JSON arguments so the UI can show "katana · caict.ac.cn" instead of a bare
// tool name. Best-effort: returns "" when nothing meaningful is found.
func toolActivityDetail(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" || arguments == "{}" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(arguments), &m); err != nil {
		return truncateActivity(arguments)
	}
	// Probe common "target" argument names across aiscan's tools (scan/search/
	// katana/ioa_send/...) in rough priority order.
	for _, k := range []string{
		"url", "target", "targets", "host", "hosts", "ip", "domain",
		"query", "q", "keyword", "cmd", "command", "path", "file", "content", "text",
	} {
		if v, ok := m[k]; ok {
			if s := activityValue(v); s != "" {
				return truncateActivity(s)
			}
		}
	}
	return ""
}

func activityValue(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []any:
		parts := make([]string, 0, 3)
		for _, e := range t {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
				if len(parts) >= 3 {
					break
				}
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

func truncateActivity(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if len(r) <= 60 {
		return s
	}
	return string(r[:60]) + "…"
}

func agentEventSummary(e agent.Event) string {
	switch e.Type {
	case agent.EventToolExecutionStart:
		return e.ToolName
	case agent.EventToolExecutionEnd:
		if e.IsError {
			return e.ToolName + " error"
		}
		return e.ToolName + " done"
	case agent.EventTurnStart:
		return fmt.Sprintf("turn %d", e.Turn)
	case agent.EventTurnEnd:
		if e.Usage != nil {
			return fmt.Sprintf("turn %d tokens=%d", e.Turn, e.Usage.TotalTokens)
		}
		return fmt.Sprintf("turn %d", e.Turn)
	default:
		return ""
	}
}

const maxStreamBuf = 64 << 10

type streamWriter struct {
	mu     sync.Mutex
	taskID string
	sendFn func(webproto.Message)
	buf    []byte
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			if len(w.buf) >= maxStreamBuf {
				w.flushLocked()
			}
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		if strings.TrimSpace(line) == "" {
			continue
		}
		w.sendFn(webproto.Message{Type: "output", TaskID: w.taskID, Data: line})
	}
	return len(p), nil
}

func (w *streamWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
}

func (w *streamWriter) flushLocked() {
	if len(w.buf) == 0 {
		return
	}
	data := string(w.buf)
	w.buf = w.buf[:0]
	if strings.TrimSpace(data) != "" {
		w.sendFn(webproto.Message{Type: "output", TaskID: w.taskID, Data: data})
	}
}

// ScanSnapshot implements scan.ScanSnapshotSink. When a scan runs on this agent
// node, the collector calls it with throttled incremental results; we ship them
// to the hub as scan.stats messages (sendFn is a concurrency-safe channel send)
// so the web UI's live counters and findings update mid-scan. The final result
// still rides the "complete" message.
func (w *streamWriter) ScanSnapshot(result *output.Result) {
	if result == nil {
		return
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return
	}
	w.sendFn(webproto.Message{Type: "scan.stats", TaskID: w.taskID, Payload: payload})
}

func webAgentTask(option *cfg.Option) (string, error) {
	if option == nil {
		return "", nil
	}
	if strings.TrimSpace(option.Prompt) == "" && option.TaskFile == "" && len(option.Inputs) == 0 {
		return "", nil
	}
	return cfg.ResolveTask(option)
}

func remoteIOAConfig(option *cfg.Option) *cfg.IOAConfig {
	if option == nil || option.IOAURL == "" {
		return nil
	}
	return &cfg.IOAConfig{
		URL:           option.IOAURL,
		NodeID:        option.IOANodeID,
		NodeName:      option.IOANodeName,
		Space:         option.Space,
		RegisterTools: true,
		AutoRegister:  true,
		NodeMeta:      map[string]any{"client": "aiscan", "transport": "web-agent"},
	}
}

func httpToWS(rawURL string) string {
	u, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil {
		return rawURL
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	return u.String()
}

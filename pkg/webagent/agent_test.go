package webagent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/core/eventbus"
	"github.com/chainreactors/aiscan/core/output"
	"github.com/chainreactors/aiscan/core/runner"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/webproto"
	"github.com/gorilla/websocket"
)

type webConnectionTestCommand struct {
	bus *eventbus.Bus[agent.Event]
}

func (c webConnectionTestCommand) Name() string  { return "echo" }
func (c webConnectionTestCommand) Usage() string { return "echo" }

func (c webConnectionTestCommand) Execute(_ context.Context, args []string) error {
	if c.bus != nil {
		c.bus.Emit(agent.Event{Type: agent.EventTurnStart, Turn: 1})
	}
	fmt.Fprintf(commands.Output, "progress: %s\n", strings.Join(args, " "))
	return nil
}

func TestStreamWriterScanSnapshotSendsScanStats(t *testing.T) {
	var sent []webproto.Message
	writer := &streamWriter{
		taskID: "scan-1",
		sendFn: func(msg webproto.Message) {
			sent = append(sent, msg)
		},
	}

	writer.ScanSnapshot(&output.Result{Summary: output.Summary{Services: 1}})

	if len(sent) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(sent))
	}
	if sent[0].Type != "scan.stats" || sent[0].TaskID != "scan-1" {
		t.Fatalf("unexpected message: %+v", sent[0])
	}
	var result output.Result
	if err := json.Unmarshal(sent[0].Payload, &result); err != nil {
		t.Fatalf("decode scan.stats payload: %v", err)
	}
	if result.Summary.Services != 1 {
		t.Fatalf("services = %d, want 1", result.Summary.Services)
	}
}

func TestRunConnectionScopesTelemetryToActiveTask(t *testing.T) {
	var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	registered := make(chan struct{})
	var registeredOnce sync.Once
	messages := make(chan webproto.Message, 8)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		var reg webproto.Message
		if err := conn.ReadJSON(&reg); err != nil {
			t.Errorf("register read: %v", err)
			return
		}
		if reg.Type != "register" || !strings.Contains(string(reg.Payload), "echo") {
			t.Errorf("unexpected register: %+v", reg)
			return
		}
		ack, _ := json.Marshal(map[string]string{"agent_id": "agent-1"})
		if err := conn.WriteJSON(webproto.Message{Type: "connected", Payload: ack}); err != nil {
			t.Errorf("ack write: %v", err)
			return
		}
		registeredOnce.Do(func() { close(registered) })

		if err := conn.WriteJSON(webproto.Message{Type: "exec", TaskID: "task-1", Data: `echo "hello world"`}); err != nil {
			t.Errorf("exec write: %v", err)
			return
		}
		for {
			var msg webproto.Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			messages <- msg
			if msg.Type == "complete" {
				return
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus := eventbus.New[agent.Event]()
	reg := commands.NewRegistry()
	reg.Register(webConnectionTestCommand{bus: bus}, "test")

	done := make(chan error, 1)
	go func() {
		done <- RunConnection(ctx, srv.URL, "worker", reg, bus)
	}()

	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("web agent connection did not register")
	}

	seenOutput := false
	seenTelemetry := false
	seenComplete := false
	deadline := time.After(3 * time.Second)
	for !seenComplete {
		select {
		case msg := <-messages:
			if msg.TaskID != "task-1" {
				t.Fatalf("message missing task id: %+v", msg)
			}
			switch msg.Type {
			case "output":
				seenOutput = strings.Contains(msg.Data, "hello world")
			case "agent.turn_start":
				seenTelemetry = strings.Contains(msg.Data, "turn 1")
			case "complete":
				seenComplete = true
			}
		case <-deadline:
			t.Fatal("timeout waiting for web agent messages")
		}
	}

	if !seenOutput {
		t.Fatal("web agent connection did not stream command output")
	}
	if !seenTelemetry {
		t.Fatal("web agent connection did not scope telemetry to task")
	}

	cancel()
	<-done
}

// TestRunConnectionReconnectsOnHalfOpenConnection verifies the agent's read
// deadline surfaces a peer that has registered then gone silent (stops
// responding to pings without closing the socket). Without the deadline the
// read blocks forever and runConnection never reconnects — the exact failure
// that stranded a deployed node as an orphan while its VM stayed up.
func TestRunConnectionReconnectsOnHalfOpenConnection(t *testing.T) {
	// Shrink the keepalive windows so the deadline fires in millis, not ~70s.
	origWait, origPing := agentPongWait, agentPingPeriod
	agentPongWait, agentPingPeriod = 300*time.Millisecond, 80*time.Millisecond
	defer func() { agentPongWait, agentPingPeriod = origWait, origPing }()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	registers := make(chan struct{}, 8)
	stop := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var reg webproto.Message
		if err := conn.ReadJSON(&reg); err != nil || reg.Type != "register" {
			return
		}
		ack, _ := json.Marshal(map[string]string{"agent_id": "agent-1"})
		if err := conn.WriteJSON(webproto.Message{Type: "connected", Payload: ack}); err != nil {
			return
		}
		registers <- struct{}{}
		// Go silent: never read again (so gorilla never auto-pongs) and never
		// close on our own. The socket stays up at the TCP layer but dead at the ws
		// layer — the half-open case the agent's read deadline must catch. Because
		// we never close, the *only* route to a second registration is the agent's
		// deadline firing; teardown closes stop (before srv.Close, via LIFO defers)
		// to release every parked handler.
		<-stop
	}))
	// Order matters: close(stop) must run before srv.Close() (LIFO), or srv.Close
	// would block on a handler still parked on <-stop.
	defer srv.Close()
	defer close(stop)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reg := commands.NewRegistry()
	reg.Register(webConnectionTestCommand{}, "test")
	done := make(chan error, 1)
	go func() { done <- RunConnection(ctx, srv.URL, "worker", reg, nil) }()

	// The first registration is immediate; a second one only happens if the agent
	// detected the silent peer via its read deadline and reconnected.
	for i := 0; i < 2; i++ {
		select {
		case <-registers:
		case <-time.After(5 * time.Second):
			t.Fatalf("expected a reconnect but saw only %d registration(s)", i)
		}
	}

	cancel()
	<-done
}

func TestRunConnectionChatWithoutRuntimeReturnsClearError(t *testing.T) {
	var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	registered := make(chan struct{})
	var registeredOnce sync.Once
	messages := make(chan webproto.Message, 4)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		var reg webproto.Message
		if err := conn.ReadJSON(&reg); err != nil {
			t.Errorf("register read: %v", err)
			return
		}
		ack, _ := json.Marshal(map[string]string{"agent_id": "agent-1"})
		if err := conn.WriteJSON(webproto.Message{Type: "connected", Payload: ack}); err != nil {
			t.Errorf("ack write: %v", err)
			return
		}
		registeredOnce.Do(func() { close(registered) })

		if err := conn.WriteJSON(webproto.Message{Type: "chat", TaskID: "task-chat", Data: "hello"}); err != nil {
			t.Errorf("chat write: %v", err)
			return
		}
		for {
			var msg webproto.Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			messages <- msg
			if msg.Type == "error" {
				return
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reg := commands.NewRegistry()
	reg.Register(webConnectionTestCommand{}, "test")

	done := make(chan error, 1)
	go func() {
		done <- RunConnection(ctx, srv.URL, "worker", reg, nil)
	}()

	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("web agent connection did not register")
	}

	select {
	case msg := <-messages:
		if msg.Type != "error" || msg.TaskID != "task-chat" || !strings.Contains(msg.Data, "LLM provider is not configured") {
			t.Fatalf("unexpected message: %+v", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for chat error")
	}

	cancel()
	<-done
}

func TestRunConnectionPTYRoundTrip(t *testing.T) {
	var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	registered := make(chan struct{})
	var registeredOnce sync.Once
	result := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		var reg webproto.Message
		if err := conn.ReadJSON(&reg); err != nil {
			t.Errorf("register read: %v", err)
			return
		}
		ack, _ := json.Marshal(map[string]string{"agent_id": "agent-pty"})
		if err := conn.WriteJSON(webproto.Message{Type: "connected", Payload: ack}); err != nil {
			t.Errorf("ack write: %v", err)
			return
		}
		registeredOnce.Do(func() { close(registered) })

		if err := conn.WriteJSON(webproto.Message{Type: "pty.open", StreamID: "term-1"}); err != nil {
			t.Errorf("pty.open write: %v", err)
			return
		}

		opened := false
		inputSent := false
		for {
			var msg webproto.Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg.Type {
			case "pty.opened":
				opened = true
				lineEnding := "\n"
				if runtime.GOOS == "windows" {
					lineEnding = "\r\n"
				}
				payload, _ := json.Marshal(map[string]string{"data": "echo pty_web_ok" + lineEnding})
				if err := conn.WriteJSON(webproto.Message{Type: "pty.input", StreamID: "term-1", Payload: payload}); err != nil {
					t.Errorf("pty.input write: %v", err)
					return
				}
				inputSent = true
			case "pty.output":
				if opened && inputSent && strings.Contains(msg.Data, "pty_web_ok") {
					_ = conn.WriteJSON(webproto.Message{Type: "pty.kill", StreamID: "term-1"})
					result <- msg.Data
					return
				}
			case "pty.error":
				result <- "error: " + msg.Data
				return
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	reg := commands.NewRegistry()
	commands.BuildGroup("core", &commands.Deps{WorkDir: t.TempDir(), BashTimeout: 5}, reg)

	done := make(chan error, 1)
	go func() {
		done <- RunConnection(ctx, srv.URL, "worker", reg, nil)
	}()

	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("web agent connection did not register")
	}

	select {
	case out := <-result:
		if !strings.Contains(out, "pty_web_ok") {
			t.Fatalf("unexpected pty output: %q", out)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("timeout waiting for pty output")
	}

	cancel()
	<-done
}

func TestRunConnectionPushesPTYSessionsOnManagerEvents(t *testing.T) {
	var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	registered := make(chan struct{})
	var registeredOnce sync.Once
	sessionUpdates := make(chan webproto.Message, 8)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()

		var reg webproto.Message
		if err := conn.ReadJSON(&reg); err != nil {
			t.Errorf("register read: %v", err)
			return
		}
		ack, _ := json.Marshal(map[string]string{"agent_id": "agent-live"})
		if err := conn.WriteJSON(webproto.Message{Type: "connected", Payload: ack}); err != nil {
			t.Errorf("ack write: %v", err)
			return
		}
		registeredOnce.Do(func() { close(registered) })

		if err := conn.WriteJSON(webproto.Message{Type: "pty.list", StreamID: "term-live"}); err != nil {
			t.Errorf("pty.list write: %v", err)
			return
		}

		for {
			var msg webproto.Message
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			if msg.Type == "pty.sessions" && msg.StreamID == "term-live" {
				sessionUpdates <- msg
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	reg := commands.NewRegistry()
	commands.BuildGroup("core", &commands.Deps{WorkDir: t.TempDir(), BashTimeout: 5}, reg)
	mgr := registryPTYManager(reg)
	if mgr == nil {
		t.Fatal("bash command did not expose tmux manager")
	}

	done := make(chan error, 1)
	go func() {
		done <- RunConnection(ctx, srv.URL, "worker", reg, nil)
	}()

	select {
	case <-registered:
	case <-time.After(time.Second):
		t.Fatal("web agent connection did not register")
	}

	// Drain the explicit pty.list response so later reads prove event-driven pushes.
	readSessionUpdate(t, sessionUpdates, func(webproto.PTYPayload) bool { return true })

	release := make(chan struct{})
	info, err := mgr.CreateFunc(ctx, "live-session", 5*time.Second, func(ctx context.Context, w io.Writer) error {
		_, _ = w.Write([]byte("live\n"))
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatalf("CreateFunc: %v", err)
	}

	readSessionUpdate(t, sessionUpdates, func(payload webproto.PTYPayload) bool {
		return payloadHasSessionState(payload, info.ID, "running")
	})
	readSessionMessage(t, sessionUpdates, func(msg webproto.Message) bool {
		return payloadHasSessionActivity(msg.Payload, info.ID)
	})

	close(release)
	readSessionUpdate(t, sessionUpdates, func(payload webproto.PTYPayload) bool {
		return payloadHasSessionState(payload, info.ID, "completed")
	})

	cancel()
	<-done
}

func readSessionMessage(t *testing.T, updates <-chan webproto.Message, match func(webproto.Message) bool) webproto.Message {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-updates:
			if match(msg) {
				return msg
			}
		case <-deadline:
			t.Fatal("timeout waiting for pty.sessions message")
			return webproto.Message{}
		}
	}
}

func readSessionUpdate(t *testing.T, updates <-chan webproto.Message, match func(webproto.PTYPayload) bool) webproto.Message {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-updates:
			payload, err := webproto.DecodePTYPayload(msg.Payload)
			if err != nil {
				t.Fatalf("decode pty payload: %v", err)
			}
			if match(payload) {
				return msg
			}
		case <-deadline:
			t.Fatal("timeout waiting for pty.sessions update")
			return webproto.Message{}
		}
	}
}

func payloadHasSessionState(payload webproto.PTYPayload, sessionID, state string) bool {
	for _, session := range payload.Sessions {
		if session.ID == sessionID && string(session.State) == state {
			return true
		}
	}
	return false
}

func payloadHasSessionActivity(raw json.RawMessage, sessionID string) bool {
	var payload struct {
		Sessions []struct {
			ID          string `json:"id"`
			ActivitySeq int64  `json:"activity_seq"`
			OutputBytes int64  `json:"output_bytes"`
		} `json:"sessions"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return false
	}
	for _, session := range payload.Sessions {
		if session.ID == sessionID && session.ActivitySeq >= 2 && session.OutputBytes > 0 {
			return true
		}
	}
	return false
}

func TestEventMatchesSession(t *testing.T) {
	cases := []struct {
		name      string
		event     agent.Event
		sessionID string
		want      bool
	}{
		{"chat event to its own session", agent.Event{SessionID: "s1"}, "s1", true},
		{"chat event not to other session", agent.Event{SessionID: "s1"}, "s2", false},
		{"sub-agent event to parent session", agent.Event{SessionID: "child", ParentSessionID: "s1"}, "s1", true},
		{"tagged event not to exec task", agent.Event{SessionID: "s1"}, "", false},
		{"untagged event to exec task", agent.Event{}, "", true},
		{"untagged event not to chat session", agent.Event{}, "s1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eventMatchesSession(tc.event, tc.sessionID); got != tc.want {
				t.Fatalf("eventMatchesSession(%+v, %q) = %v, want %v", tc.event, tc.sessionID, got, tc.want)
			}
		})
	}
}

func TestApplyConfigUpdateRefreshesRuntimeProvider(t *testing.T) {
	rt := &runner.AgentRuntime{
		App: &runner.App{},
		Config: agent.Config{
			Model: "old-model",
		},
	}
	sessions := newChatRuntimeManager(rt)
	sessions.sessions["chat-1"] = agent.NewAgent(rt.Config.WithSessionID("chat-1"))

	var dc webproto.DistributeConfig
	dc.LLM.Provider = "openai"
	dc.LLM.BaseURL = "https://example.test/v1"
	dc.LLM.APIKey = "new-key"
	dc.LLM.Model = "new-model"
	dc.LLM.Proxy = "http://127.0.0.1:7890"
	dc.Cyberhub.URL = "https://cyberhub.example"
	dc.Cyberhub.Key = "hub-key"
	dc.Cyberhub.Mode = "override"
	dc.Cyberhub.Proxy = "socks5://127.0.0.1:1080"
	dc.Recon.FofaEmail = "ops@example.com"
	dc.Recon.FofaKey = "fofa-key"
	dc.Recon.HunterAPIKey = "hunter-key"
	dc.Recon.Proxy = "http://127.0.0.1:8080"
	limit := 25
	dc.Recon.Limit = &limit
	dc.Scan.Verify = "high"
	dc.Scan.VerifyTimeout = 77
	dc.Search.TavilyKeys = "tavily-key"
	dc.IOA.URL = "http://token@127.0.0.1:8080/ioa"
	dc.IOA.NodeName = "updated-node"
	dc.IOA.Space = "updated-space"
	dc.Agent.Tools = []string{"search"}
	dc.Agent.Timeout = 123
	dc.Agent.SaveSession = true

	if err := applyConfigUpdate(context.Background(), webproto.Message{
		Type:    "config.update",
		Payload: mustJSON(dc),
	}, rt, sessions); err != nil {
		t.Fatalf("applyConfigUpdate() error = %v", err)
	}
	if rt.App.Provider == nil {
		t.Fatal("provider was not initialized")
	}
	if rt.App.ProviderConfig.Model != "new-model" || rt.Config.Model != "new-model" {
		t.Fatalf("model not refreshed: app=%q config=%q", rt.App.ProviderConfig.Model, rt.Config.Model)
	}
	if rt.App.ProviderConfig.APIKey != "new-key" {
		t.Fatalf("api key not refreshed")
	}
	if rt.App.ProviderConfig.Proxy != "http://127.0.0.1:7890" {
		t.Fatalf("proxy not refreshed: %q", rt.App.ProviderConfig.Proxy)
	}
	if rt.Option == nil {
		t.Fatal("runtime option was not refreshed")
	}
	if rt.Option.CyberhubURL != "https://cyberhub.example" || rt.Option.CyberhubKey != "hub-key" || rt.Option.CyberhubMode != "override" {
		t.Fatalf("cyberhub options not refreshed: %+v", rt.Option.ScannerOptions)
	}
	if rt.Option.Proxy != "socks5://127.0.0.1:1080" {
		t.Fatalf("scanner proxy not refreshed: %q", rt.Option.Proxy)
	}
	if rt.Option.FofaEmail != "ops@example.com" || rt.Option.FofaKey != "fofa-key" || rt.Option.HunterAPIKey != "hunter-key" {
		t.Fatalf("recon options not refreshed: %+v", rt.Option.ReconOptions)
	}
	if rt.Option.ReconLimit == nil || *rt.Option.ReconLimit != 25 {
		t.Fatalf("recon limit not refreshed: %+v", rt.Option.ReconLimit)
	}
	if rt.Option.ScanConfig.Verify != "high" || rt.Option.ScanConfig.VerifyTimeout != 77 {
		t.Fatalf("scan config not refreshed: %+v", rt.Option.ScanConfig)
	}
	if rt.Option.IOAURL != "http://token@127.0.0.1:8080/ioa" || rt.Option.IOANodeName != "updated-node" || rt.Option.Space != "updated-space" {
		t.Fatalf("ioa options not refreshed: %+v", rt.Option.IOAOptions)
	}
	if !reflect.DeepEqual(rt.Option.Tools, []string{"search"}) || rt.Option.Timeout != 123 || !rt.Option.SaveSession {
		t.Fatalf("agent options not refreshed: tools=%v timeout=%d save=%v", rt.Option.Tools, rt.Option.Timeout, rt.Option.SaveSession)
	}
	if len(sessions.sessions) != 0 {
		t.Fatalf("chat sessions were not reset")
	}
}

// TestApplyConfigUpdatePreservesLaunchNodeName reproduces the "local agent stuck
// on 连接中" bug: an agent launched with an explicit --ioa-node-name must keep that
// identity when the hub pushes its shared global config (which carries no
// per-agent node name). Clobbering it would desync the pool/IOA node name from
// the launch handle, so viewForLocal can no longer match the tracked child.
func TestApplyConfigUpdatePreservesLaunchNodeName(t *testing.T) {
	opt := &cfg.Option{}
	opt.IOANodeName = "local-1"
	rt := &runner.AgentRuntime{
		App:      &runner.App{},
		NodeName: "local-1",
		Option:   opt,
		Config:   agent.Config{Model: "old-model"},
	}
	sessions := newChatRuntimeManager(rt)

	var dc webproto.DistributeConfig
	dc.LLM.Provider = "openai"
	dc.LLM.BaseURL = "https://example.test/v1"
	dc.LLM.APIKey = "new-key"
	dc.LLM.Model = "new-model"
	// dc.IOA.NodeName intentionally empty: the hub's global config has none.

	if err := applyConfigUpdate(context.Background(), webproto.Message{
		Type:    "config.update",
		Payload: mustJSON(dc),
	}, rt, sessions); err != nil {
		t.Fatalf("applyConfigUpdate() error = %v", err)
	}
	if rt.NodeName != "local-1" {
		t.Fatalf("runtime node name churned on config push: got %q, want local-1", rt.NodeName)
	}
	if rt.Option == nil || rt.Option.IOANodeName != "local-1" {
		t.Fatalf("option node name not preserved: %+v", rt.Option)
	}
}

// TestApplyConfigUpdateKeepsAutoResolvedNodeName covers agents started without
// --ioa-node-name: they get a random name at launch, and a config push carrying
// no name must keep that same name rather than mint a new random one (which would
// orphan their pool/IOA registration mid-session).
func TestApplyConfigUpdateKeepsAutoResolvedNodeName(t *testing.T) {
	rt := &runner.AgentRuntime{
		App:      &runner.App{},
		NodeName: "aiscan-deadbeef",
		Option:   &cfg.Option{}, // IOANodeName empty, like an auto-registered node
		Config:   agent.Config{Model: "old-model"},
	}
	sessions := newChatRuntimeManager(rt)

	var dc webproto.DistributeConfig
	dc.LLM.Provider = "openai"
	dc.LLM.BaseURL = "https://example.test/v1"
	dc.LLM.APIKey = "new-key"
	dc.LLM.Model = "new-model"

	if err := applyConfigUpdate(context.Background(), webproto.Message{
		Type:    "config.update",
		Payload: mustJSON(dc),
	}, rt, sessions); err != nil {
		t.Fatalf("applyConfigUpdate() error = %v", err)
	}
	if rt.NodeName != "aiscan-deadbeef" {
		t.Fatalf("auto-resolved node name churned on config push: got %q, want aiscan-deadbeef", rt.NodeName)
	}
}

func TestOptionFromDistributePreservesLaunchIOAWhenRemoteBlank(t *testing.T) {
	base := cfg.Option{}
	base.IOAURL = "http://token@127.0.0.1:3000/ioa"
	base.IOAToken = "token"
	base.IOANodeName = "local-1"
	base.Space = "default"

	var dc webproto.DistributeConfig
	dc.LLM.Provider = "openai"
	dc.LLM.Model = "gpt-test"

	got := cfg.ApplyDistributeConfig(base, dc)
	if got.IOAURL != base.IOAURL || got.IOAToken != base.IOAToken || got.IOANodeName != base.IOANodeName || got.Space != base.Space {
		t.Fatalf("blank remote IOA config should preserve launch IOA fields: %+v", got.IOAOptions)
	}
}

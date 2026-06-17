package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/gorilla/websocket"
)

// --- mock runtime ---

type mockRuntime struct {
	commands []string
}

func (m *mockRuntime) CommandNames() []string { return m.commands }

func (m *mockRuntime) ExecuteCommand(_ context.Context, cmdLine string, stream io.Writer) (string, json.RawMessage, error) {
	_, _ = stream.Write([]byte("progress: executing " + cmdLine + "\n"))
	result, _ := json.Marshal(map[string]string{"status": "done"})
	return "completed " + cmdLine, result, nil
}

// --- helpers ---

func dialAgent(t *testing.T, srv *httptest.Server, name string, commands []string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/agent/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	reg, _ := json.Marshal(map[string]any{"name": name, "commands": commands})
	conn.WriteJSON(WSMessage{Type: "register", Payload: reg})
	var ack WSMessage
	conn.ReadJSON(&ack)
	if ack.Type != "connected" {
		t.Fatalf("expected connected, got %s", ack.Type)
	}
	return conn
}

func setupTestServer(t *testing.T) (*httptest.Server, *AgentPool) {
	t.Helper()
	hub := NewHub()
	pool := NewAgentPool(hub)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/agent/ws", pool.HandleWS)
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, pool.List())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, pool
}

// --- tests ---

func TestWSRegisterAndList(t *testing.T) {
	srv, pool := setupTestServer(t)

	conn := dialAgent(t, srv, "test-agent", []string{"scan", "gogo"})
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	agents := pool.List()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "test-agent" {
		t.Fatalf("expected name test-agent, got %s", agents[0].Name)
	}
}

func TestWSDispatchAndComplete(t *testing.T) {
	srv, pool := setupTestServer(t)

	conn := dialAgent(t, srv, "worker", []string{"scan"})
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)
	agents := pool.List()
	if len(agents) == 0 {
		t.Fatal("no agents")
	}
	agentID := agents[0].ID

	// Subscribe to hub for progress events
	progressCh, unsub := pool.hub.Subscribe("task-1")
	defer unsub()

	resultCh, err := pool.DispatchCommand(agentID, "task-1", "scan -i 1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}

	// Agent reads exec command
	var cmd WSMessage
	conn.ReadJSON(&cmd)
	if cmd.Type != "exec" || cmd.TaskID != "task-1" || cmd.Data != "scan -i 1.2.3.4" {
		t.Fatalf("unexpected command: %+v", cmd)
	}

	// Agent sends output
	conn.WriteJSON(WSMessage{Type: "output", TaskID: "task-1", Data: "scanning port 80"})

	select {
	case evt := <-progressCh:
		if evt.Type != "output" {
			t.Fatalf("unexpected event type: %s", evt.Type)
		}
		if !strings.Contains(string(evt.Data), "scanning port 80") {
			t.Fatalf("unexpected progress data: %s", evt.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for progress")
	}

	// Agent sends complete
	result, _ := json.Marshal(map[string]int{"ports": 3})
	conn.WriteJSON(WSMessage{Type: "complete", TaskID: "task-1", Data: "done", Payload: result})

	select {
	case res := <-resultCh:
		if res.Err != "" {
			t.Fatalf("unexpected error: %s", res.Err)
		}
		if res.Output != "done" {
			t.Fatalf("expected output 'done', got %q", res.Output)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestWSFullRoundTrip(t *testing.T) {
	srv, pool := setupTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := &mockRuntime{commands: []string{"scan", "echo"}}

	done := make(chan error, 1)
	go func() {
		done <- RunCallback(ctx, CallbackConfig{
			ServerURL: srv.URL,
			Name:      "roundtrip-agent",
			Runtime:   rt,
		})
	}()

	// Wait for agent to register
	var agentID string
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		agents := pool.List()
		if len(agents) > 0 {
			agentID = agents[0].ID
			break
		}
	}
	if agentID == "" {
		t.Fatal("agent did not register")
	}

	resultCh, err := pool.DispatchCommand(agentID, "test-1", "scan -i 10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	select {
	case res := <-resultCh:
		if res.Err != "" {
			t.Fatalf("error: %s", res.Err)
		}
		if !strings.Contains(res.Output, "scan -i 10.0.0.1") {
			t.Fatalf("unexpected output: %q", res.Output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	cancel()
	<-done
}

func TestWSWithBridge(t *testing.T) {
	srv, pool := setupTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bridge := telemetry.NewBridge()

	rt := &mockRuntime{commands: []string{"echo"}}

	done := make(chan error, 1)
	go func() {
		done <- RunCallback(ctx, CallbackConfig{
			ServerURL: srv.URL,
			Name:      "bridge-agent",
			Runtime:   rt,
			Bridge:    bridge,
		})
	}()

	// Wait for agent
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if pool.Count() > 0 {
			break
		}
	}
	if pool.Count() == 0 {
		t.Fatal("agent did not register")
	}

	// Emit telemetry event through bridge
	bridge.Send(telemetry.Envelope{Source: "agent", Type: "turn_start", Data: "turn 1"})
	time.Sleep(200 * time.Millisecond)

	// The event was sent through WebSocket to the server.
	// Verify the connection is still alive.
	if pool.Count() != 1 {
		t.Fatal("agent disconnected after telemetry send")
	}

	cancel()
	<-done
}

func TestWSPick(t *testing.T) {
	_, pool := setupTestServer(t)

	if pool.Pick() != nil {
		t.Fatal("expected nil when no agents")
	}
}


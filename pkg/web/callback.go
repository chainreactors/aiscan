package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/gorilla/websocket"
)

// CallbackRuntime is the subset of runner that the callback client needs.
type CallbackRuntime interface {
	CommandNames() []string
	ExecuteCommand(ctx context.Context, cmdLine string, stream io.Writer) (string, json.RawMessage, error)
}

// CallbackConfig configures the agent callback client.
type CallbackConfig struct {
	ServerURL string
	Name      string
	Runtime   CallbackRuntime
	Bridge    *telemetry.Bridge
}

// RunCallback connects to the web server via WebSocket and enters a loop
// receiving commands and sending output/telemetry back.
func RunCallback(ctx context.Context, cfg CallbackConfig) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := runCallbackOnce(ctx, cfg)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
		}
	}
}

func runCallbackOnce(ctx context.Context, cfg CallbackConfig) error {
	wsURL := httpToWS(cfg.ServerURL) + "/api/agent/ws"
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	sendCh := make(chan WSMessage, 64)
	done := make(chan struct{})
	defer close(done)

	send := func(m WSMessage) {
		select {
		case sendCh <- m:
		case <-done:
		}
	}

	// Register.
	regPayload, _ := json.Marshal(map[string]any{
		"name":     cfg.Name,
		"commands": cfg.Runtime.CommandNames(),
	})
	if err := conn.WriteJSON(WSMessage{Type: "register", Payload: regPayload}); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	// Read connected ack.
	var ack WSMessage
	if err := conn.ReadJSON(&ack); err != nil || ack.Type != "connected" {
		return fmt.Errorf("expected connected ack")
	}

	// Write goroutine: sendCh → WebSocket (single writer, no concurrency).
	go func() {
		for {
			select {
			case msg, ok := <-sendCh:
				if !ok {
					return
				}
				_ = conn.WriteJSON(msg)
			case <-ctx.Done():
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			case <-done:
				return
			}
		}
	}()

	// Context cancellation: unblock ReadJSON by closing the connection.
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	// Bridge sink: forward telemetry to WebSocket via sendCh.
	if cfg.Bridge != nil {
		removeSink := cfg.Bridge.AddSink(func(e telemetry.Envelope) {
			send(WSMessage{
				Type: e.Source + "." + e.Type,
				Data: fmt.Sprint(e.Data),
			})
		})
		defer removeSink()
	}

	// Task management.
	var taskMu sync.Mutex
	taskCancels := make(map[string]context.CancelFunc)

	// Read loop.
	for {
		var msg WSMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}

		switch msg.Type {
		case "exec":
			taskCtx, cancel := context.WithCancel(ctx)
			taskMu.Lock()
			taskCancels[msg.TaskID] = cancel
			taskMu.Unlock()
			go func(m WSMessage, tCtx context.Context, tCancel context.CancelFunc) {
				defer tCancel()
				defer func() {
					taskMu.Lock()
					delete(taskCancels, m.TaskID)
					taskMu.Unlock()
				}()
				executeAndReport(tCtx, m.TaskID, m.Data, cfg.Runtime, send)
			}(msg, taskCtx, cancel)

		case "cancel":
			taskMu.Lock()
			if cancel, ok := taskCancels[msg.TaskID]; ok {
				cancel()
			}
			taskMu.Unlock()
		}
	}
}

func executeAndReport(ctx context.Context, taskID, command string, rt CallbackRuntime, send func(WSMessage)) {
	writer := &wsStreamWriter{taskID: taskID, sendFn: send}
	output, result, err := rt.ExecuteCommand(ctx, command, writer)

	if err != nil {
		send(WSMessage{Type: "error", TaskID: taskID, Data: err.Error()})
		return
	}
	send(WSMessage{Type: "complete", TaskID: taskID, Data: output, Payload: result})
}

type wsStreamWriter struct {
	taskID string
	sendFn func(WSMessage)
	buf    []byte
}

func (w *wsStreamWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		if strings.TrimSpace(line) == "" {
			continue
		}
		w.sendFn(WSMessage{Type: "output", TaskID: w.taskID, Data: line})
	}
	return len(p), nil
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

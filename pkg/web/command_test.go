package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/slashcmd"
	"github.com/chainreactors/aiscan/pkg/webproto"
)

func TestParseSlashCommand(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantCmd string
		wantArg string
		wantOK  bool
	}{
		{"scan with target", "/scan example.com", "scan", "example.com", true},
		{"scan with flags", "/scan example.com --mode full --deep", "scan", "example.com --mode full --deep", true},
		{"verb only", "/agents", "agents", "", true},
		{"lowercased verb", "/SCAN Example.com", "scan", "Example.com", true},
		{"extra spaces", "/scan    a.com   b.com", "scan", "a.com   b.com", true},
		{"tab separator", "/help\tx", "help", "x", true},
		{"plain message", "hello there", "", "", false},
		{"bare slash", "/", "", "", false},
		{"slash then spaces", "/   ", "", "", false},
		{"path-like, not a command", "/etc/passwd", "etc/passwd", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, arg, ok := parseSlashCommand(tc.in)
			if ok != tc.wantOK || cmd != tc.wantCmd || arg != tc.wantArg {
				t.Fatalf("parseSlashCommand(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.in, cmd, arg, ok, tc.wantCmd, tc.wantArg, tc.wantOK)
			}
		})
	}
}

// TestHubScopeCommandsCovered guards that every hub-scope command in the catalog
// is handled by runHubCommand's switch. Adding a hub command to slashcmd without
// wiring it here would silently route it to the agent instead.
func TestHubScopeCommandsCovered(t *testing.T) {
	handled := map[string]bool{"/scan": true, "/agents": true, "/help": true}
	for _, s := range slashcmd.HubWebMenu() {
		if !handled[s.Name] {
			t.Errorf("hub command %q is in the catalog but not handled by runHubCommand", s.Name)
		}
	}
}

func newMenuTestService(t *testing.T) *Service {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewService(ServiceConfig{Store: store})
}

// TestSessionMenuMergeAndFallback checks the "/" menu the hub serves: hub-scope
// commands merged with the agent's (here, the static fallback since no agent is
// bound), with run-control commands excluded.
func TestSessionMenuMergeAndFallback(t *testing.T) {
	svc := newMenuTestService(t)
	names := map[string]bool{}
	for _, s := range svc.SessionMenu("no-such-session") {
		names[s.Name] = true
	}
	for _, want := range []string{"/scan", "/agents", "/help", "/status", "/provider"} {
		if !names[want] {
			t.Errorf("SessionMenu missing %q", want)
		}
	}
	for _, absent := range []string{"/stop", "/eval", "/followup", "/loop"} {
		if names[absent] {
			t.Errorf("SessionMenu leaked run-control command %q", absent)
		}
	}
}

// TestClearCommandWipesTranscript verifies web /clear is a true "clear
// conversation": the session's persisted messages are deleted (so a reload stays
// empty), not merely the agent's model context. No agent is bound here, so the
// path is store-wipe + UI signal only.
func TestClearCommandWipesTranscript(t *testing.T) {
	svc := newMenuTestService(t)
	ctx := context.Background()
	sid := "sess-clear"
	for _, role := range []string{"user", "assistant", "user"} {
		err := svc.store.AddMessage(ctx, &ChatMessage{
			ID: generateID(), SessionID: sid, Role: role, Content: "x", CreatedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}
	if msgs, _ := svc.GetMessages(ctx, sid); len(msgs) != 3 {
		t.Fatalf("setup: got %d messages, want 3", len(msgs))
	}

	svc.handleClearCommand(sid, webproto.ChatPayload{})

	msgs, err := svc.GetMessages(ctx, sid)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("after /clear: got %d messages, want 0", len(msgs))
	}
}

// TestSessionCommandsRoute drives the real HTTP endpoint the frontend "/" menu
// fetches, proving the route is wired and returns a JSON slashcmd catalog.
func TestSessionCommandsRoute(t *testing.T) {
	svc := newMenuTestService(t)
	srv := httptest.NewServer(NewHandler(svc, nil, nil, nil, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/chat/sessions/anything/commands")
	if err != nil {
		t.Fatalf("GET /commands: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var specs []slashcmd.Spec
	if err := json.NewDecoder(resp.Body).Decode(&specs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	for _, want := range []string{"/scan", "/help", "/status"} {
		if !names[want] {
			t.Errorf("/commands response missing %q (got %d specs)", want, len(specs))
		}
	}
}

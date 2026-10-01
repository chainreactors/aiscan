package service

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/aop"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func legacyDatabase(t *testing.T, invalid bool) (string, *aop.Event) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	createStoredSession(t, store, "session")
	db := store.db
	if _, err := db.Exec(`UPDATE chat_sessions SET title = 'retained title'; ALTER TABLE chat_aop_events RENAME COLUMN event_proto TO event_json`); err != nil {
		t.Fatal(err)
	}
	event := &aop.Event{Id: "event", SessionId: "session", TurnId: "turn", Emitter: "node", Seq: 17,
		Payload: &aop.Event_ToolResult{ToolResult: &aop.ToolResult{CallId: "call", Name: "bash", DurationMs: 31, Terminate: true, Output: []*aop.Content{aop.Text("完整输出")}}}}
	raw, err := protojson.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO chat_aop_events VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "row", "session", event.Id, 9, "turn", "node", 17, string(raw), "original-time"); err != nil {
		t.Fatal(err)
	}
	if invalid {
		if _, err := db.Exec(`INSERT INTO chat_aop_events VALUES ('bad-row', 'session', 'bad-event', 10, '', '', 0, '{invalid', 'bad-time')`); err != nil {
			t.Fatal(err)
		}
	}
	return path, event
}

func TestSQLiteStoreStartupMigrationRetainsHistoryAndIndexes(t *testing.T) {
	path, event := legacyDatabase(t, false)
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := store.db
	var raw []byte
	var cursor, sequence int64
	if err := db.QueryRow(`SELECT cursor, sequence, event_proto FROM chat_aop_events`).Scan(&cursor, &sequence, &raw); err != nil {
		t.Fatal(err)
	}
	restored := new(aop.Event)
	if err := proto.Unmarshal(raw, restored); err != nil || cursor != 9 || sequence != 17 || !proto.Equal(restored, event) {
		t.Fatalf("history after migration = %v, cursor=%d sequence=%d, %v", restored, cursor, sequence, err)
	}
	var id, time, kind string
	if err := db.QueryRow(`SELECT id, created_at, typeof(event_proto) FROM chat_aop_events`).Scan(&id, &time, &kind); err != nil || id != "row" || time != "original-time" || kind != "blob" {
		t.Fatalf("stored metadata = %q, %q, %q, %v", id, time, kind, err)
	}
	var indexes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name LIKE 'idx_aop_events_%'`).Scan(&indexes); err != nil || indexes != 3 {
		t.Fatalf("indexes = %d, %v", indexes, err)
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM chat_sessions WHERE id = 'session'`).Scan(&title); err != nil || title != "retained title" {
		t.Fatalf("unrelated session data changed: %q, %v", title, err)
	}
	if _, err := db.Exec(`INSERT INTO chat_aop_events SELECT 'duplicate', session_id, event_id, cursor + 1, turn_id, emitter, sequence, event_proto, created_at FROM chat_aop_events`); err == nil {
		t.Fatal("event identity uniqueness was lost")
	}
	page, err := store.ListAOPEventsAfter(t.Context(), "session", 0, 10)
	if err != nil || len(page) != 1 || page[0].GetCursor() != "9" || !proto.Equal(page[0].Event, event) {
		t.Fatalf("replay after migration = %v, %v", page, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("second startup = %v", err)
	}
	defer reopened.Close()
	page, err = reopened.ListAOPEventsAfter(t.Context(), "session", 0, 10)
	if err != nil || len(page) != 1 || page[0].GetCursor() != "9" || !proto.Equal(page[0].Event, event) {
		t.Fatalf("replay after second startup = %v, %v", page, err)
	}
}

func TestMalformedEventRollsBackWholeMigration(t *testing.T) {
	path, _ := legacyDatabase(t, true)
	if _, err := NewSQLiteStore(path); err == nil || !strings.Contains(err.Error(), "bad-event") {
		t.Fatalf("migration = %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM chat_aop_events WHERE event_json IS NOT NULL`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("original records after rollback = %d, %v", count, err)
	}
	var leftovers int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'chat_aop_events_legacy_json'`).Scan(&leftovers); err != nil || leftovers != 0 {
		t.Fatalf("temporary tables after rollback = %d, %v", leftovers, err)
	}
}

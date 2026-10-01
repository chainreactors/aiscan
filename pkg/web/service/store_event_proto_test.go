package service

import (
	"path/filepath"
	"testing"

	"github.com/chainreactors/cyber/aop"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestSQLiteStoreMigratesEmptyLegacyEventsAtStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`ALTER TABLE chat_aop_events RENAME COLUMN event_proto TO event_json`); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := validateSchema(store.db, coreSchema); err != nil {
		t.Fatalf("startup did not migrate the legacy schema: %v", err)
	}
}

func TestStoredEventsRetainUnknownProtobufData(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "unknown.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	createStoredSession(t, store, "session")
	event := &aop.Event{Id: "unknown-extension", SessionId: "session", Seq: 1,
		Payload: &aop.Event_Extension{Extension: &anypb.Any{TypeUrl: "type.example/future.Event", Value: []byte{8, 42}}}}
	event.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	if _, _, err := store.AppendAOPEvent(t.Context(), "session", event); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListAOPEventsAfter(t.Context(), "session", 0, 10)
	if err != nil || len(page) != 1 || !proto.Equal(page[0].Event, event) {
		t.Fatalf("unknown payload was changed: %v, %v", page, err)
	}
	var kind string
	if err := store.db.QueryRow(`SELECT typeof(event_proto) FROM chat_aop_events`).Scan(&kind); err != nil || kind != "blob" {
		t.Fatalf("event storage = %q, %v", kind, err)
	}
}

package web_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/types"
	webext "github.com/chainreactors/cyber/exts/web"
	"github.com/chainreactors/cyber/pkg/testutil/hosttest"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	scanpb "github.com/chainreactors/cyber/pkg/web/scan"
	"github.com/chainreactors/cyber/pkg/web/service"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func openScanServer(t *testing.T, path string) (webpkg.Service, *extension.Set, error) {
	t.Helper()
	web := webext.New(webext.Config{Database: path, Scans: &service.ScanServiceConfig{}})
	set := hosttest.Set(t, web)
	if err := set.Load(t.Context()); err != nil {
		return nil, set, err
	}
	return web.Service(), set, nil
}

func legacyScans(t *testing.T) (string, []*scanpb.Scan, *aop.Event) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scans.db")
	_, set, err := openScanServer(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rawSession, err := protojson.Marshal(&types.SessionRecord{Session: &aop.Session{Id: "session"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO chat_sessions
		(id, node_id, status, archived, title, agent_name, session_json, created_at, updated_at)
		VALUES ('session', '', 'open', false, '', '', ?, '', '')`, string(rawSession)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE chat_aop_events RENAME COLUMN event_proto TO event_json`); err != nil {
		t.Fatal(err)
	}
	event := &aop.Event{Id: "event", SessionId: "session", TurnId: "turn", Emitter: "node", Seq: 17,
		Payload: &aop.Event_ToolResult{ToolResult: &aop.ToolResult{CallId: "call", Name: "bash", Output: []*aop.Content{aop.Text("完整输出")}}}}
	rawEvent, err := protojson.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO chat_aop_events VALUES ('row', 'session', 'event', 9, 'turn', 'node', 17, ?, 'original-time')`, string(rawEvent)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE scans; CREATE TABLE scans (
		id VARCHAR PRIMARY KEY, target VARCHAR NOT NULL, mode VARCHAR NOT NULL,
		verify BOOLEAN, sniper BOOLEAN NOT NULL, status VARCHAR NOT NULL,
		progress VARCHAR NOT NULL, error VARCHAR NOT NULL, scan_json TEXT NOT NULL,
		created_at VARCHAR NOT NULL, updated_at VARCHAR NOT NULL);
		CREATE INDEX idx_scans_created ON scans(created_at DESC)`); err != nil {
		t.Fatal(err)
	}
	var scans []*scanpb.Scan
	for i, options := range []*scanpb.ScanOptions{nil, {}, {Verify: proto.Bool(false)}, {Verify: proto.Bool(true), Sniper: true}} {
		scan := &scanpb.Scan{Id: string(rune('a' + i)), Target: "中文.example", Options: options, Status: scanpb.ScanStatus(73), Progress: "canonical"}
		if i != 0 {
			scan.CreatedAt = timestamppb.New(time.Date(2026, 9, 30, 7, 1, i, 123456789, time.UTC))
		}
		raw, err := protojson.Marshal(scan)
		if err != nil {
			t.Fatal(err)
		}
		// Stale relational projections must be replaced from the old read source.
		if _, err := db.Exec(`INSERT INTO scans VALUES (?, 'stale', 'stale', true, false, 'queued', 'stale', '', ?, 'stale', 'stale')`, scan.Id, string(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO session_scans VALUES ('session', ?)`, scan.Id); err != nil {
			t.Fatal(err)
		}
		scans = append(scans, scan)
	}
	return path, scans, event
}

func TestStartupMigrationPreservesCanonicalScansLinksAndIndexes(t *testing.T) {
	path, scans, event := legacyScans(t)
	svc, set, err := openScanServer(t, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range scans {
		got, err := svc.API().Scans.GetScan(t.Context(), &scanpb.GetScanRequest{ScanId: want.Id})
		if err != nil || !proto.Equal(got.GetScan(), want) {
			t.Fatalf("scan %s: %v, %v", want.Id, got, err)
		}
	}
	session, err := svc.API().Sessions.GetSession(t.Context(), &types.GetSessionRequest{SessionId: "session"})
	if err != nil {
		t.Fatal(err)
	}
	binding := session.GetSession().GetExtensions()["scan"]
	if len(binding.GetFields()["ids"].GetListValue().GetValues()) != len(scans) {
		t.Fatalf("session links: %v, %v", session, err)
	}
	if err := set.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc, set, err = openScanServer(t, path)
	if err != nil {
		t.Fatalf("second startup: %v", err)
	}
	for _, want := range scans {
		got, err := svc.API().Scans.GetScan(t.Context(), &scanpb.GetScanRequest{ScanId: want.Id})
		if err != nil || !proto.Equal(got.GetScan(), want) {
			t.Fatalf("scan %s after second startup: %v, %v", want.Id, got, err)
		}
	}
	if err := set.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var eventRaw []byte
	var cursor, sequence int64
	if err := db.QueryRow(`SELECT cursor, sequence, event_proto FROM chat_aop_events`).Scan(&cursor, &sequence, &eventRaw); err != nil {
		t.Fatal(err)
	}
	restored := new(aop.Event)
	if err := proto.Unmarshal(eventRaw, restored); err != nil || cursor != 9 || sequence != 17 || !proto.Equal(restored, event) {
		t.Fatalf("event after both migrations = %v, cursor=%d sequence=%d, %v", restored, cursor, sequence, err)
	}
	for _, scan := range scans {
		var linked string
		if err := db.QueryRow(`SELECT session_id FROM session_scans WHERE scan_id=?`, scan.Id).Scan(&linked); err != nil || linked != "session" {
			t.Fatalf("scan links: %q, %v", linked, err)
		}
	}
	if _, err := db.Exec(`DELETE FROM scans WHERE id=?`, scans[0].Id); err != nil {
		t.Fatal(err)
	}
	var linked int
	if err := db.QueryRow(`SELECT COUNT(*) FROM session_scans WHERE scan_id=?`, scans[0].Id).Scan(&linked); err != nil || linked != 0 {
		t.Fatalf("foreign-key cascade lost: %d, %v", linked, err)
	}
	var indexes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='idx_scans_created'`).Scan(&indexes); err != nil || indexes != 1 {
		t.Fatalf("indexes: %d, %v", indexes, err)
	}
}

func TestMigrationFailureRollsBackSchemaAndRows(t *testing.T) {
	for _, statement := range []string{
		`UPDATE scans SET scan_json='{invalid' WHERE id='d'`,
		`UPDATE scans SET scan_json='{"id":"different"}' WHERE id='d'`,
		`CREATE INDEX custom_json ON scans(scan_json)`,
		`CREATE TRIGGER custom_scan AFTER UPDATE ON scans BEGIN SELECT 1; END`,
		`CREATE TRIGGER custom_event AFTER INSERT ON chat_aop_events BEGIN SELECT 1; END`,
		`ALTER TABLE aop_request_ledger ADD COLUMN unexpected TEXT`,
	} {
		t.Run(statement, func(t *testing.T) {
			path, _, event := legacyScans(t)
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if _, _, err := openScanServer(t, path); err == nil {
				t.Fatal("accepted unsupported database")
			}
			var extra int
			if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('scans') WHERE name='has_options'`).Scan(&extra); err != nil || extra != 0 {
				t.Fatalf("schema partially converted: %d, %v", extra, err)
			}
			var target, raw string
			if err := db.QueryRow(`SELECT target, scan_json FROM scans WHERE id='a'`).Scan(&target, &raw); err != nil || target != "stale" {
				t.Fatalf("rows partially converted: %q, %v", target, err)
			}
			if err := db.QueryRow(`SELECT event_json FROM chat_aop_events WHERE event_id='event'`).Scan(&raw); err != nil {
				t.Fatalf("event migration was not rolled back: %v", err)
			}
			restored := new(aop.Event)
			if err := protojson.Unmarshal([]byte(raw), restored); err != nil || !proto.Equal(restored, event) {
				t.Fatalf("event changed after failed startup: %v, %v", restored, err)
			}
			var leftovers int
			if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'chat_aop_events_legacy_json'`).Scan(&leftovers); err != nil || leftovers != 0 {
				t.Fatalf("temporary tables after rollback = %d, %v", leftovers, err)
			}
		})
	}
}

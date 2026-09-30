package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/types"
	"github.com/chainreactors/cyber/internal/testutil/hosttest"
	webext "github.com/chainreactors/cyber/pkg/exts/web"
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

func legacyScans(t *testing.T) (string, []*scanpb.Scan) {
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
	return path, scans
}

func TestMigrationPreservesCanonicalScansLinksAndIndexes(t *testing.T) {
	path, scans := legacyScans(t)
	if _, _, err := openScanServer(t, path); err == nil {
		t.Fatal("server accepted legacy storage")
	} else if !strings.Contains(err.Error(), "migrate-scans") {
		t.Fatal(err)
	}
	count, err := migrateScans(t.Context(), path)
	if err != nil || count != int64(len(scans)) {
		t.Fatalf("migration: %d, %v", count, err)
	}
	svc, _, err := openScanServer(t, path)
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
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
	if _, err := migrateScans(t.Context(), path); err == nil {
		t.Fatal("converted twice")
	}
}

func TestMigrationFailureRollsBackSchemaAndRows(t *testing.T) {
	for _, statement := range []string{
		`UPDATE scans SET scan_json='{invalid' WHERE id='d'`,
		`UPDATE scans SET scan_json='{"id":"different"}' WHERE id='d'`,
		`CREATE INDEX custom_json ON scans(scan_json)`,
		`CREATE TRIGGER custom_scan AFTER UPDATE ON scans BEGIN SELECT 1; END`,
	} {
		t.Run(statement, func(t *testing.T) {
			path, _ := legacyScans(t)
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			if _, err := migrateScans(t.Context(), path); err == nil {
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
		})
	}
}

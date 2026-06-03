package web

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestScanRequestAnalysisOptions(t *testing.T) {
	verify, sniper, deep := ScanRequest{Verify: true, Deep: true}.AnalysisOptions()
	if !verify || sniper || !deep {
		t.Fatalf("new analysis options = verify:%v sniper:%v deep:%v", verify, sniper, deep)
	}

	verify, sniper, deep = ScanRequest{AI: true}.AnalysisOptions()
	if !verify || !sniper || deep {
		t.Fatalf("legacy AI options = verify:%v sniper:%v deep:%v", verify, sniper, deep)
	}
}

func TestScanArgsForSelectedAnalysisOptions(t *testing.T) {
	job := &ScanJob{
		Target: "127.0.0.1",
		Mode:   "full",
		Verify: true,
		Sniper: true,
		Deep:   true,
	}

	got := scanArgsForJob(job)
	want := []string{"-i", "127.0.0.1", "--mode", "full", "--verify=high", "--sniper", "--deep"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scan args = %#v, want %#v", got, want)
	}
}

func TestServiceStatusReportsLLMAvailability(t *testing.T) {
	service := NewService(ServiceConfig{})
	if service.Status().LLMAvailable {
		t.Fatal("LLMAvailable = true, want false without provider")
	}
}

func TestSQLiteStorePersistsAnalysisOptions(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "scans.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	now := time.Now()
	job := &ScanJob{
		ID:        "scan-1",
		Target:    "127.0.0.1",
		Mode:      "quick",
		Verify:    true,
		Deep:      true,
		Status:    StatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !got.Verify || got.Sniper || !got.AI || !got.Deep {
		t.Fatalf("stored options = verify:%v sniper:%v ai:%v deep:%v", got.Verify, got.Sniper, got.AI, got.Deep)
	}
}

func TestSQLiteStoreMapsLegacyAIToVerifyAndSniper(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "scans.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer store.Close()

	now := time.Now()
	job := &ScanJob{
		ID:        "scan-legacy",
		Target:    "127.0.0.1",
		Mode:      "quick",
		AI:        true,
		Status:    StatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := store.Get(context.Background(), job.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !got.Verify || !got.Sniper || !got.AI {
		t.Fatalf("legacy options = verify:%v sniper:%v ai:%v", got.Verify, got.Sniper, got.AI)
	}
}

func TestPartialStructuredBuilderAggregatesStreamLines(t *testing.T) {
	builder := newPartialStructuredBuilder("127.0.0.1", time.Now())
	builder.ObserveLine(`[service] 127.0.0.1:22 open tcp 12ms`)
	builder.ObserveLine(`[web] http://127.0.0.1:8765 200 550 3ms "Directory listing for /" [directory,pythonsimplehttp]`)
	builder.ObserveLine(`[deep:rejected] http://127.0.0.1:8765 not_confirmed "No issue" "static listing"`)

	result := builder.Result(time.Now())
	if result == nil {
		t.Fatal("partial result is nil")
	}
	if result.Summary.Services != 1 || result.Summary.Probes != 1 || result.Summary.Fingerprints != 2 || len(result.AI) != 1 {
		t.Fatalf("summary = %#v", result.Summary)
	}
	if len(result.Assets) != 2 {
		t.Fatalf("assets = %d, want 2: %#v", len(result.Assets), result.Assets)
	}
	var webAssetFound bool
	for _, asset := range result.Assets {
		if asset.Target == "http://127.0.0.1:8765" {
			webAssetFound = true
			if asset.Status != "not_confirmed" {
				t.Fatalf("web asset status = %q", asset.Status)
			}
		}
	}
	if !webAssetFound {
		t.Fatalf("web asset not found: %#v", result.Assets)
	}
}

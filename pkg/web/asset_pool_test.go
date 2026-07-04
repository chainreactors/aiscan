package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/core/output"
)

func newAssetTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "assets.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// A repeat sighting of the same target must dedup to one row, keep the
// first-seen source, and fill (not clobber) fields from the newer sighting.
func TestAssetUpsertDedup(t *testing.T) {
	ctx := context.Background()
	store := newAssetTestStore(t)

	if err := store.UpsertAsset(ctx, &PoolAsset{Target: "example.com", Source: AssetSourceAgent}); err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	if err := store.UpsertAsset(ctx, &PoolAsset{
		Target: "example.com", Source: AssetSourceScan, Services: 3, Label: "web", LastScanID: "scan-1",
	}); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}

	assets, err := store.ListAssets(ctx, "", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected 1 deduped asset, got %d", len(assets))
	}
	a := assets[0]
	if a.Source != AssetSourceAgent {
		t.Errorf("source should stay first-seen (agent), got %q", a.Source)
	}
	if a.Services != 3 {
		t.Errorf("services should merge to 3, got %d", a.Services)
	}
	if a.Label != "web" || a.LastScanID != "scan-1" {
		t.Errorf("label/scan-id should fill from scan sighting, got %q / %q", a.Label, a.LastScanID)
	}
	if a.FirstSeen.IsZero() || a.LastSeen.IsZero() {
		t.Errorf("timestamps should be set")
	}

	if err := store.DeleteAsset(ctx, "", a.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if assets, _ = store.ListAssets(ctx, "", 0); len(assets) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(assets))
	}
}

// Assets are isolated per project: the same target lives independently in two
// projects, and a scoped list/delete only touches its own project.
func TestAssetProjectScope(t *testing.T) {
	ctx := context.Background()
	store := newAssetTestStore(t)

	for _, proj := range []string{"alpha", "beta"} {
		if err := store.UpsertAsset(ctx, &PoolAsset{ProjectID: proj, Target: "example.com", Source: AssetSourceManual}); err != nil {
			t.Fatalf("upsert %s: %v", proj, err)
		}
	}

	alpha, _ := store.ListAssets(ctx, "alpha", 0)
	beta, _ := store.ListAssets(ctx, "beta", 0)
	if len(alpha) != 1 || len(beta) != 1 {
		t.Fatalf("each project should hold its own row, got alpha=%d beta=%d", len(alpha), len(beta))
	}
	if def, _ := store.ListAssets(ctx, "", 0); len(def) != 0 {
		t.Fatalf("default project should be empty, got %d", len(def))
	}

	if err := store.DeleteAsset(ctx, "alpha", alpha[0].ID); err != nil {
		t.Fatalf("delete alpha: %v", err)
	}
	if a, _ := store.ListAssets(ctx, "alpha", 0); len(a) != 0 {
		t.Fatalf("alpha should be empty after delete, got %d", len(a))
	}
	if b, _ := store.ListAssets(ctx, "beta", 0); len(b) != 1 {
		t.Fatalf("beta must be untouched by alpha delete, got %d", len(b))
	}
}

// A pre-project assets table (global UNIQUE(target), no project_id) is migrated
// in place on open: existing rows survive under the default project, and the new
// composite uniqueness lets the same target live in a second project.
func TestMigrateAssetsProjectScope(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")

	// Seed a legacy-schema assets table with a row, as an old hub would have.
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := raw.Exec(`
		CREATE TABLE assets (
			id TEXT PRIMARY KEY,
			target TEXT NOT NULL UNIQUE,
			label TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			services INTEGER NOT NULL DEFAULT 0,
			loots INTEGER NOT NULL DEFAULT 0,
			last_scan_id TEXT NOT NULL DEFAULT '',
			first_seen TEXT NOT NULL,
			last_seen TEXT NOT NULL
		);
		INSERT INTO assets (id, target, source, first_seen, last_seen)
			VALUES ('a1', 'legacy.example.com', 'scan', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z');
	`); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	_ = raw.Close()

	// Reopen through the store: migrate() must rebuild the table in place.
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore (migrate): %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	def, err := store.ListAssets(ctx, DefaultProjectID, 0)
	if err != nil {
		t.Fatalf("list default: %v", err)
	}
	if len(def) != 1 || def[0].Target != "legacy.example.com" {
		t.Fatalf("legacy row should survive under default project, got %+v", def)
	}
	if def[0].ProjectID != DefaultProjectID {
		t.Errorf("migrated row should carry default project id, got %q", def[0].ProjectID)
	}

	// Composite uniqueness: the same target now coexists in another project.
	if err := store.UpsertAsset(ctx, &PoolAsset{ProjectID: "other", Target: "legacy.example.com"}); err != nil {
		t.Fatalf("same target in another project should be allowed: %v", err)
	}
	if other, _ := store.ListAssets(ctx, "other", 0); len(other) != 1 {
		t.Fatalf("expected 1 row in 'other' project, got %d", len(other))
	}
}

// AddAssets validates each token and silently drops non-network junk (bash
// fragments, file paths) and duplicates.
func TestServiceAddAssetsSkipsJunk(t *testing.T) {
	ctx := context.Background()
	svc := &Service{store: newAssetTestStore(t)}

	added, err := svc.AddAssets(ctx, "", []string{
		"good.example.com",
		"1.2.3.4:8080",
		"rm -rf /",         // bash junk → skipped
		"/etc/passwd",      // path → skipped
		"good.example.com", // dup → skipped
	}, AssetSourceManual, "")
	if err != nil {
		t.Fatalf("AddAssets: %v", err)
	}
	if len(added) != 2 {
		t.Fatalf("expected 2 valid assets, got %d (%+v)", len(added), added)
	}
}

// The project + asset HTTP handlers wire end-to-end: the seeded default
// project lists, a created project scopes new assets, and the ?project= query
// filter isolates the list (the default project stays empty).
func TestProjectHTTPEndpoints(t *testing.T) {
	h := &handlerImpl{service: &Service{store: newAssetTestStore(t)}}

	var projs []Project
	doJSON(t, h.listProjects, "GET", "/api/projects", "", &projs)
	if len(projs) != 1 || projs[0].ID != DefaultProjectID {
		t.Fatalf("expected seeded default project, got %+v", projs)
	}

	var created Project
	doJSON(t, h.createProject, "POST", "/api/projects", `{"name":"Alpha Engagement"}`, &created)
	if created.ID == "" || created.Name != "Alpha Engagement" {
		t.Fatalf("create returned %+v", created)
	}

	doJSON(t, h.addAssets, "POST", "/api/assets", `{"targets":["1.2.3.4"],"project":"`+created.ID+`"}`, nil)

	var scoped, def []PoolAsset
	doJSON(t, h.listAssets, "GET", "/api/assets?project="+created.ID, "", &scoped)
	doJSON(t, h.listAssets, "GET", "/api/assets", "", &def)
	if len(scoped) != 1 || scoped[0].Target != "1.2.3.4" {
		t.Fatalf("scoped list = %+v", scoped)
	}
	if len(def) != 0 {
		t.Fatalf("default project list should be empty, got %+v", def)
	}
}

func TestServiceRejectsUnknownProject(t *testing.T) {
	ctx := context.Background()
	svc := &Service{store: newAssetTestStore(t)}

	if _, err := svc.AddAssets(ctx, "missing", []string{"example.com"}, AssetSourceManual, ""); err == nil || !strings.Contains(err.Error(), `project "missing" not found`) {
		t.Fatalf("AddAssets should reject unknown project, got %v", err)
	}
	if _, err := svc.SubmitScan(ctx, "example.com", "quick", false, false, false, "missing"); err == nil || !strings.Contains(err.Error(), `project "missing" not found`) {
		t.Fatalf("SubmitScan should reject unknown project, got %v", err)
	}
}

// Deleting a project cascades to its own asset pool while leaving other
// projects' assets untouched, and drops it from the project list.
func TestServiceDeleteProjectCascade(t *testing.T) {
	ctx := context.Background()
	store := newAssetTestStore(t)
	svc := &Service{store: store}

	if err := store.CreateProject(ctx, &Project{ID: "acme", Name: "Acme"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.AddAssets(ctx, "acme", []string{"1.1.1.1", "2.2.2.2"}, AssetSourceManual, ""); err != nil {
		t.Fatalf("add acme assets: %v", err)
	}
	if _, err := svc.AddAssets(ctx, DefaultProjectID, []string{"9.9.9.9"}, AssetSourceManual, ""); err != nil {
		t.Fatalf("add default asset: %v", err)
	}

	if err := svc.DeleteProject(ctx, "acme"); err != nil {
		t.Fatalf("DeleteProject(acme): %v", err)
	}

	projs, err := svc.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	for _, p := range projs {
		if p.ID == "acme" {
			t.Fatalf("acme should be gone from the list, got %+v", projs)
		}
	}
	acmeAssets, err := store.ListAssets(ctx, "acme", 0)
	if err != nil {
		t.Fatalf("list acme assets: %v", err)
	}
	if len(acmeAssets) != 0 {
		t.Fatalf("acme assets should cascade-delete, got %d", len(acmeAssets))
	}
	defAssets, err := store.ListAssets(ctx, DefaultProjectID, 0)
	if err != nil {
		t.Fatalf("list default assets: %v", err)
	}
	if len(defAssets) != 1 {
		t.Fatalf("default project's assets must survive, got %d", len(defAssets))
	}
}

// Drive the delete through a real mux so the "DELETE /api/projects/{id}" route
// string and its {id} path-value extraction are exercised, not just the handler
// func in isolation: a 200 removes the project, deleting default is a 400.
func TestDeleteProjectRoute(t *testing.T) {
	ctx := context.Background()
	h := &handlerImpl{service: &Service{store: newAssetTestStore(t)}}
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/projects/{id}", h.deleteProject)

	if err := h.service.store.CreateProject(ctx, &Project{ID: "acme", Name: "Acme"}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/projects/acme", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE acme => %d: %s", rec.Code, rec.Body.String())
	}
	if ok, _ := h.service.store.ProjectExists(ctx, "acme"); ok {
		t.Fatalf("acme should be gone after route delete")
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/projects/default", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE default should be 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// The default project is permanent (fallback scope) and unknown ids are rejected.
func TestServiceDeleteProjectGuards(t *testing.T) {
	ctx := context.Background()
	svc := &Service{store: newAssetTestStore(t)}

	if err := svc.DeleteProject(ctx, DefaultProjectID); err == nil || !strings.Contains(err.Error(), "default project cannot be deleted") {
		t.Fatalf("deleting default should be refused, got %v", err)
	}
	if err := svc.DeleteProject(ctx, ""); err == nil || !strings.Contains(err.Error(), "default project cannot be deleted") {
		t.Fatalf("deleting empty (normalizes to default) should be refused, got %v", err)
	}
	if err := svc.DeleteProject(ctx, "ghost"); err == nil || !strings.Contains(err.Error(), `project "ghost" not found`) {
		t.Fatalf("deleting unknown project should error, got %v", err)
	}
}

func doJSON(t *testing.T, fn http.HandlerFunc, method, target, body string, out any) {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	fn(rec, r)
	if rec.Code >= 400 {
		t.Fatalf("%s %s => %d: %s", method, target, rec.Code, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("decode %s: %v (body=%s)", target, err, rec.Body.String())
		}
	}
}

// A completed scan's structured assets fold into the pool, counting service
// items and tagging the source as scan.
func TestIngestScanAssets(t *testing.T) {
	ctx := context.Background()
	svc := &Service{store: newAssetTestStore(t)}
	result := &output.Result{
		Assets: []output.Asset{{
			Target: "10.0.0.1", Status: "up", Title: "host",
			Items: []output.AssetItem{
				{Kind: output.AssetItemService},
				{Kind: output.AssetItemService},
				{Kind: output.AssetItemPath},
			},
		}},
	}
	svc.ingestScanAssets(ctx, &ScanJob{ID: "job-1"}, result)

	assets, err := svc.store.ListAssets(ctx, "", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected 1 ingested asset, got %d", len(assets))
	}
	if assets[0].Services != 2 {
		t.Errorf("expected 2 service items counted, got %d", assets[0].Services)
	}
	if assets[0].Source != AssetSourceScan {
		t.Errorf("expected source scan, got %q", assets[0].Source)
	}
}

// Scan jobs are stored globally, but their selected asset project must survive
// the background runner's store.Get(jobID) reload so completed scan assets do
// not fall back into the default project.
func TestScanProjectPersistsForAssetIngestion(t *testing.T) {
	ctx := context.Background()
	store := newAssetTestStore(t)
	if err := store.CreateProject(ctx, &Project{ID: "alpha", Name: "Alpha", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	now := time.Now()
	job := &ScanJob{
		ID:        "scan-alpha",
		Target:    "example.com",
		Mode:      "quick",
		Project:   "alpha",
		Status:    StatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Create(ctx, job); err != nil {
		t.Fatalf("store scan: %v", err)
	}
	reloaded, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("reload scan: %v", err)
	}
	if reloaded.Project != "alpha" {
		t.Fatalf("reloaded scan project = %q, want alpha", reloaded.Project)
	}

	svc := &Service{store: store}
	svc.ingestScanAssets(ctx, reloaded, &output.Result{
		Assets: []output.Asset{{Target: "10.0.0.2", Status: "up", Title: "host"}},
	})

	alpha, err := store.ListAssets(ctx, "alpha", 0)
	if err != nil {
		t.Fatalf("list alpha: %v", err)
	}
	def, err := store.ListAssets(ctx, DefaultProjectID, 0)
	if err != nil {
		t.Fatalf("list default: %v", err)
	}
	if len(alpha) != 1 || alpha[0].Target != "10.0.0.2" {
		t.Fatalf("expected scanned asset in alpha, got %+v", alpha)
	}
	if len(def) != 0 {
		t.Fatalf("default project should stay empty, got %+v", def)
	}
}

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/core/extension"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

func TestManifestDescribesCoreOnlyHost(t *testing.T) {
	host := New(Config{Product: "cyber-harness", Profiles: []webpkg.Profile{{ID: "cyber-audit"}, {ID: "cyber-scan"}}, Database: filepath.Join(t.TempDir(), "web.db")})
	set, err := extension.New(host)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close(context.Background()) })

	manifest := requestManifest(t, host)
	if manifest.Product != "cyber-harness" {
		t.Fatalf("product = %q", manifest.Product)
	}
	if len(manifest.Capabilities) != 1 || manifest.Capabilities[0].ID != "core" {
		t.Fatalf("core-only capabilities = %+v", manifest.Capabilities)
	}
	if len(manifest.Profiles) != 2 || manifest.Profiles[0].ID != "cyber-audit" || manifest.Profiles[1].ID != "cyber-scan" {
		t.Fatalf("profiles = %+v", manifest.Profiles)
	}
	for _, route := range host.Routes() {
		if strings.Contains(route.Pattern, "/api/scans") {
			t.Fatalf("core-only host mounted scan route %q", route.Pattern)
		}
	}
}

func TestManifestIncludesMountedScanConsole(t *testing.T) {
	host := New(Config{
		Product:  "cyber-scan",
		Database: filepath.Join(t.TempDir(), "web.db"),
		Scans:    &webservice.ScanServiceConfig{},
	})
	set, err := extension.New(host)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close(context.Background()) })

	manifest := requestManifest(t, host)
	var found bool
	for _, capability := range manifest.Capabilities {
		if capability.ID == "scan" {
			found = true
		}
	}
	if !found {
		t.Fatalf("scan capability missing from %+v", manifest.Capabilities)
	}
}

func requestManifest(t *testing.T, host *Extension) webpkg.Manifest {
	t.Helper()
	for _, route := range host.Routes() {
		if route.Pattern != "GET /api/manifest" {
			continue
		}
		recorder := httptest.NewRecorder()
		route.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/manifest", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("manifest status = %d", recorder.Code)
		}
		var manifest webpkg.Manifest
		if err := json.Unmarshal(recorder.Body.Bytes(), &manifest); err != nil {
			t.Fatal(err)
		}
		return manifest
	}
	t.Fatal("manifest route was not registered")
	return webpkg.Manifest{}
}

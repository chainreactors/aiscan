package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	managementapi "github.com/chainreactors/cyber/pkg/web/api"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

type testCapability struct{ closed atomic.Bool }

func (c *testCapability) ID() string { return "test" }

func (c *testCapability) Manifest() webpkg.Capability {
	return webpkg.Capability{ID: "test", APIRoutes: []string{"GET /api/test-cap"}}
}

func (*testCapability) SchemaModules() []webservice.SchemaModule { return nil }

func (*testCapability) Routes(*managementapi.API) []webpkg.Route {
	return []webpkg.Route{{Pattern: "GET /api/test-cap", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}}
}

func (*testCapability) RegisterNamespaces(*aop.NamespaceMux) error { return nil }

func (c *testCapability) Close(context.Context) error {
	c.closed.Store(true)
	return nil
}

func TestCapabilityRoutesAndLifecycleAreOwnedByWebExtension(t *testing.T) {
	capability := &testCapability{}
	host := New(Config{Database: filepath.Join(t.TempDir(), "web.db"), Capabilities: []webservice.Capability{capability}})
	set, err := extension.New(host)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}

	var route webpkg.Route
	for _, candidate := range host.Routes() {
		if candidate.Pattern == "GET /api/test-cap" {
			route = candidate
			break
		}
	}
	if route.Handler == nil {
		t.Fatal("capability route was not mounted")
	}
	recorder := httptest.NewRecorder()
	route.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/test-cap", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("capability route status = %d", recorder.Code)
	}
	if err := set.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !capability.closed.Load() {
		t.Fatal("capability was not closed with its Web owner")
	}
}

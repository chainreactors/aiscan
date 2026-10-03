// Package web owns the typed HTTP route registry used by the Web host.
package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/pkg/profile"
	managementapi "github.com/chainreactors/cyber/pkg/web/api"
	webservice "github.com/chainreactors/cyber/pkg/web/service"

	"github.com/chainreactors/cyber/core/extension"
	coreregistry "github.com/chainreactors/cyber/core/registry"
	"github.com/chainreactors/cyber/core/resource"
	cfg "github.com/chainreactors/cyber/pkg/config"
	webpkg "github.com/chainreactors/cyber/pkg/web"
)

// Extension defines web.Route as a dynamic resource and contributes the routes
// backed by the management service. Later extensions may add their own routes.
//
// Batching, ordering, duplicate rules and revocation are the same problem the
// command and tool registries solve, so they use the same store rather than a
// third copy of it. The route pattern is the name.
// Config selects resources; no live business instance is accepted from a host.
type Config struct {
	// Product and Manifest describe the host to the browser. Product features
	// are contributed by profiles; the Web extension only publishes the
	// resulting capability set.
	Product        string
	Manifest       []webpkg.Capability
	Profiles       []webpkg.Profile
	Capabilities   []webservice.Capability
	Database       string
	AccessKey      string
	AllowedOrigins []string
	ConfigAPI      managementapi.ConfigOptions
	ConfigStore    webservice.ConfigStore
	InitialProfile func(context.Context) (profile.Profile, error)
	BuildProfile   func(context.Context, *webservice.PreparedConfig) (profile.Profile, error)
	// RuntimeLLM supplies effective settings for a Hub without a local profile.
	RuntimeLLM func() provider.ProviderConfig
	// Scans mounts the scan console and its storage module; nil disables both.
	Scans *webservice.ScanServiceConfig
}
type Extension struct {
	config   Config
	service  *webservice.Service
	database *webservice.SQLiteStore
	pool     *webservice.AgentPool
	initial  profile.Profile
	store    *coreregistry.Store[webpkg.Route]
	mu       sync.Mutex
	closing  bool
	requests map[*request]context.CancelFunc
	drained  chan struct{}
}

// A nonzero size keeps simultaneous request pointers distinct as map keys.
type request struct{ _ byte }

func New(config Config) *Extension {
	if strings.TrimSpace(config.Product) == "" {
		config.Product = "cyber-harness"
	}
	config.AllowedOrigins = append([]string(nil), config.AllowedOrigins...)
	config.Manifest = append([]webpkg.Capability(nil), config.Manifest...)
	config.Profiles = append([]webpkg.Profile(nil), config.Profiles...)
	config.Capabilities = append([]webservice.Capability(nil), config.Capabilities...)
	return &Extension{config: config, store: coreregistry.New[webpkg.Route](), requests: make(map[*request]context.CancelFunc)}
}

// Service borrows business operations after Load; lifecycle stays in Extension.
func (e *Extension) Service() webpkg.Service {
	if e == nil || e.service == nil {
		return nil
	}
	return e.service
}
func (e *Extension) Load(scope *extension.Scope) error {
	if e == nil {
		return fmt.Errorf("web extension is required")
	}
	if strings.TrimSpace(e.config.Database) == "" {
		return fmt.Errorf("web database path is required")
	}
	var err error
	var modules []webservice.SchemaModule
	for _, capability := range e.config.Capabilities {
		if capability == nil {
			return fmt.Errorf("nil web capability")
		}
		modules = append(modules, capability.SchemaModules()...)
	}
	if e.config.Scans != nil {
		modules = append(modules, webservice.ScanSchema)
	}
	e.database, err = webservice.NewSQLiteStore(e.config.Database, modules...)
	if err != nil {
		return err
	}
	if e.config.InitialProfile != nil {
		e.initial, err = e.config.InitialProfile(scope.Init())
		if err != nil {
			return err
		}
		if e.initial == nil || !e.initial.Active() {
			return fmt.Errorf("initial web profile is not active")
		}
	}
	e.service = webservice.NewService(webservice.ServiceConfig{
		Store: e.database, Profile: e.initial, AccessKey: e.config.AccessKey,
		ConfigAPI: e.config.ConfigAPI, ConfigStore: e.config.ConfigStore, BuildProfile: e.config.BuildProfile,
		Scans:        e.config.Scans,
		RuntimeLLM:   e.config.RuntimeLLM,
		Capabilities: e.config.Capabilities,
	})
	e.initial = nil // Service owns initial and replacement profiles from here.
	e.pool = webservice.NewAgentPool(e.service.Hub(), e.database, e.config.AllowedOrigins...)
	e.service.SetAgentPool(e.pool)
	if err := extension.Provide[webpkg.Service](scope, e.service); err != nil {
		return err
	}
	if err := extension.Provide[webpkg.Auth](scope, e.service.Auth()); err != nil {
		return err
	}
	if err := extension.Define[webpkg.Route](scope, e); err != nil {
		return err
	}
	if err := extension.Add(scope, webpkg.ManagementRoutes(e.service)...); err != nil {
		return err
	}
	for _, capability := range e.config.Capabilities {
		if err := extension.Add(scope, capability.Routes(e.service.API())...); err != nil {
			return err
		}
	}
	manifest := webpkg.Manifest{Product: e.config.Product, Version: cfg.Version, Profiles: e.config.Profiles}
	manifest.Capabilities = append(manifest.Capabilities, webpkg.Capability{
		ID: "core", Title: "Cyber Harness", APIRoutes: []string{"SessionService", "AgentService", "ConfigService", "SystemService", "ArtifactService"},
		UIContribs: []string{"sessions", "chat", "agents", "settings"},
	})
	if e.config.Scans != nil {
		manifest.Capabilities = append(manifest.Capabilities, webpkg.Capability{
			ID: "scan", Title: "Scan", APIRoutes: []string{"ScanService"},
			AOPTypes: []string{"cyber.scan.ProtocolMessage"}, UIContribs: []string{"scan-results", "assets", "findings"},
		})
	}
	manifest.Capabilities = append(manifest.Capabilities, e.config.Manifest...)
	for _, capability := range e.config.Capabilities {
		manifest.Capabilities = append(manifest.Capabilities, capability.Manifest())
	}
	if _, err := e.store.Add(coreregistry.Value[webpkg.Route]{Name: "GET /api/manifest", Value: webpkg.ManifestRoute(manifest)}); err != nil {
		return err
	}
	return e.store.Activate(scope.Init())
}

// admit keeps snapshots of revoked routes from admitting work after shutdown.
func (e *Extension) admit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		if e.closing {
			e.mu.Unlock()
			http.Error(w, "web service is closing", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		key := &request{}
		e.requests[key] = cancel
		e.mu.Unlock()
		defer func() {
			cancel()
			e.mu.Lock()
			delete(e.requests, key)
			if e.closing && len(e.requests) == 0 && e.drained != nil {
				close(e.drained)
				e.drained = nil
			}
			e.mu.Unlock()
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (e *Extension) Add(values ...webpkg.Route) (resource.Handle, error) {
	if e == nil || e.store == nil || len(values) == 0 {
		return nil, resource.ErrInvalid
	}
	batch := make([]coreregistry.Value[webpkg.Route], 0, len(values))
	for _, route := range values {
		if strings.TrimSpace(route.Pattern) == "" || route.Handler == nil {
			return nil, fmt.Errorf("HTTP route requires pattern and handler")
		}
		route.Handler = e.admit(route.Handler)
		batch = append(batch, coreregistry.Value[webpkg.Route]{Name: route.Pattern, Value: route})
	}
	return e.store.Add(batch...)
}

func (e *Extension) Close(ctx context.Context) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	e.closing = true
	var cancels []context.CancelFunc
	for _, cancel := range e.requests {
		cancels = append(cancels, cancel)
	}
	if len(e.requests) > 0 && e.drained == nil {
		e.drained = make(chan struct{})
	}
	drained := e.drained
	e.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	if err := e.store.Close(ctx); err != nil {
		return err
	}
	if e.pool != nil {
		if err := e.pool.Close(ctx); err != nil {
			return err
		}
	}
	if drained != nil {
		select {
		case <-drained:
		case <-ctx.Done():
			return errors.Join(extension.ErrCloseIncomplete, ctx.Err())
		}
	}
	var result error
	if e.service != nil {
		result = e.service.Close(ctx)
	}
	if e.initial != nil {
		result = errors.Join(result, e.initial.Close(ctx))
	}
	if errors.Is(result, extension.ErrCloseIncomplete) || ctx.Err() != nil {
		return errors.Join(result, extension.ErrCloseIncomplete, ctx.Err())
	}
	e.initial = nil
	if e.database != nil {
		result = errors.Join(result, e.database.Close())
		e.database = nil
	}
	return result
}

// Routes returns the current immutable route snapshot in contribution order.
// It is empty until the graph activates, so a half-built profile cannot be
// served.
func (e *Extension) Routes() []webpkg.Route {
	if e == nil || e.store == nil {
		return nil
	}
	entries := e.store.Entries()
	routes := make([]webpkg.Route, 0, len(entries))
	for _, entry := range entries {
		routes = append(routes, entry.Value)
	}
	return routes
}

var (
	_ extension.Extension          = (*Extension)(nil)
	_ resource.Point[webpkg.Route] = (*Extension)(nil)
)

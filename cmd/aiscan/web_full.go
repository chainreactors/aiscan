//go:build full

package main

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/core/runner"
	"github.com/chainreactors/aiscan/pkg/deploy"
	"github.com/chainreactors/aiscan/pkg/deploy/manager"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/web"
	"github.com/chainreactors/aiscan/pkg/webproto"
	webstatic "github.com/chainreactors/aiscan/web"
	"github.com/chainreactors/ioa/protocols"
	ioaserver "github.com/chainreactors/ioa/server"
	"gopkg.in/yaml.v3"
)

func init() {
	webServeFunc = runWeb
}

func runWeb(ctx context.Context, option *cfg.Option, opts webCommand, logger telemetry.Logger) error {
	store, err := web.NewSQLiteStore(opts.DB)
	if err != nil {
		return fmt.Errorf("open database: %s", err)
	}
	defer store.Close()

	application, err := initWebApp(ctx, option, logger, false)
	if err != nil {
		return fmt.Errorf("init aiscan: %s", err)
	}

	if application.Provider != nil {
		logger.Infof("LLM provider ready, AI features enabled")
	} else {
		logger.Warnf("no LLM provider configured, AI features disabled (set api_key in aiscan.yaml or env)")
	}

	configFile := option.ConfigFile
	appOption := *option
	service := web.NewService(web.ServiceConfig{
		Store:          store,
		App:            application,
		ConfigStore:    &webConfigStore{explicit: configFile},
		AppFactory:     func(ctx context.Context) (*runner.App, error) { return initWebApp(ctx, &appOption, logger, true) },
		RuntimeContext: ctx,
		MaxConcurrent:  opts.MaxScans,
		ScanTimeout:    time.Duration(opts.ScanTimeout) * time.Second,
	})
	defer service.Close()

	var pool *web.AgentPool
	if option.Debug {
		pool = web.NewAgentPool(service.Hub(), "*")
	} else {
		pool = web.NewAgentPool(service.Hub())
	}
	pool.SetRecordStore(store)
	service.SetAgentPool(pool)

	staticSub, err := fs.Sub(webstatic.FS, "static")
	if err != nil {
		return fmt.Errorf("load static assets: %s", err)
	}

	accessKey := opts.IOAToken
	if accessKey == "" {
		accessKey = protocols.NewToken()
	}
	ioaSvc := ioaserver.NewService(ioaserver.NewMemoryStore(), accessKey)
	ioaHandler := ioaserver.AuthMiddleware(ioaSvc)(ioaserver.NewHandler(ioaSvc))

	// Cloud auto-deploy: credentials/records persisted next to the config file;
	// the hub's IOA access key is embedded into the IOA URL handed to new nodes.
	deployStatePath := resolveDeployPath(configFile)
	logger.Infof("deploy state: %s", deployStatePath)
	deployStore := deploy.NewFileStore(deployStatePath)
	deployMgr := manager.NewDeployManager(deployStore, web.NewPoolLister(pool), accessKey, opts.AgentBinary)
	// Outbound SSH reverse tunnel: lets a hub without a public IP still be reached
	// by deployed nodes via an auto-provisioned relay. Exposes the hub's own
	// loopback addr and, once the tunnel was enabled, reconnects to the relay on
	// boot (the relay's public IP:port is the deploy PublicURL).
	deployMgr.ConfigureTunnel(hubLocalURL(opts.Addr))
	go deployMgr.AutoStartTunnel(ctx)
	go func() {
		<-ctx.Done()
		deployMgr.ShutdownTunnel()
		deployMgr.StopAllLocal()
	}()
	go deployMgr.StartReaper(ctx, logger.Infof)

	// The cloud/deploy control plane spends money, holds credentials, and spawns
	// processes. When it is reachable off-loopback without an admin token it is
	// wide open; warn loudly rather than silently. (Kept back-compat/open so an
	// existing public hub isn't locked out mid-run — set --admin-token to gate it.)
	if opts.AdminToken == "" && !isLoopbackListen(opts.Addr) {
		logger.Warnf("SECURITY: cloud/deploy API is UNAUTHENTICATED on non-loopback %s — set --admin-token or AISCAN_ADMIN_TOKEN to gate /api/cloud and /api/deploy", opts.Addr)
	}

	handler := web.NewHandler(service, pool, deployMgr, opts.AdminToken, ioaHandler, newSPAFileServer(staticSub))

	srv := &http.Server{
		Addr:    opts.Addr,
		Handler: handler,
	}

	go func() {
		<-ctx.Done()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
	}()

	logger.Infof("aiscan web server listening on http://%s", opts.Addr)
	logger.Infof("IOA server embedded at http://%s/ioa (token=%s)", opts.Addr, accessKey)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func newSPAFileServer(fsys fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(fsys))
	return func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" {
			if f, err := fsys.Open(name); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		r = r.Clone(r.Context())
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	}
}

func initWebApp(ctx context.Context, baseOption *cfg.Option, logger telemetry.Logger, isReload bool) (*runner.App, error) {
	option := cfg.Option{}
	if baseOption != nil {
		if isReload {
			// On hot-reload the saved aiscan.yaml is authoritative for the editable
			// sections; only carry over infrastructural flags (config path, data dir)
			// so a value typed in the web UI isn't shadowed by a startup CLI flag.
			option.MiscOptions = baseOption.MiscOptions
		} else {
			option = *baseOption
		}
	}
	cfgPath, err := cfg.ResolveRuntimeConfig(&option)
	if err != nil {
		return nil, err
	}
	if cfgPath != "" {
		logger.Infof("loaded config: %s", cfgPath)
	}

	appCfg := cfg.AppConfig(&option, cfg.RuntimeFeatures{
		ProviderEnabled:  true,
		ProviderOptional: true,
		ToolsEnabled:     true,
		AIEnabled:        true,
	}, logger)
	appCfg.Scanner.EnableAllAISkills = false
	appCfg.Scanner.VerifyMode = "off"

	app, err := runner.NewApp(ctx, appCfg)
	if err != nil {
		return nil, err
	}
	if !isReload {
		// First boot blocks until engines are ready; a hot-reload returns
		// immediately and lets engines warm up in the background.
		if err := app.WaitEngines(ctx); err != nil {
			app.Close()
			return nil, err
		}
	}
	return app, nil
}

// ---------------------------------------------------------------------------
// Config file store for web UI settings page
// ---------------------------------------------------------------------------

type webConfigStore struct {
	explicit string
	mu       sync.Mutex
}

func (s *webConfigStore) GetDistributeConfig(ctx context.Context) (string, bool, webproto.DistributeConfig, error) {
	if err := ctx.Err(); err != nil {
		return "", false, webproto.DistributeConfig{}, err
	}
	p, loaded := s.resolveConfigPath()
	if !loaded {
		return p, false, webproto.DistributeConfig{}, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return p, false, webproto.DistributeConfig{}, err
	}
	var dc webproto.DistributeConfig
	_ = yaml.Unmarshal(data, &dc)
	return p, true, dc, nil
}

func (s *webConfigStore) SaveDistributeConfig(ctx context.Context, incoming webproto.DistributeConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	p, loaded := s.resolveConfigPath()
	var current webproto.DistributeConfig
	if loaded {
		if data, err := os.ReadFile(p); err == nil {
			_ = yaml.Unmarshal(data, &current)
		}
	}

	// Preserve existing secrets when incoming value is empty.
	preserveSecret(&incoming.LLM.APIKey, current.LLM.APIKey)
	preserveSecret(&incoming.Cyberhub.Key, current.Cyberhub.Key)
	preserveSecret(&incoming.Recon.FofaKey, current.Recon.FofaKey)
	preserveSecret(&incoming.Recon.HunterToken, current.Recon.HunterToken)
	preserveSecret(&incoming.Recon.HunterAPIKey, current.Recon.HunterAPIKey)
	preserveSecret(&incoming.Search.TavilyKeys, current.Search.TavilyKeys)
	preserveSecret(&incoming.IOA.Token, current.IOA.Token)

	next, _ := yaml.Marshal(&incoming)
	if dir := filepath.Dir(p); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(p, next, 0600)
}

func preserveSecret(incoming *string, existing string) {
	if strings.TrimSpace(*incoming) == "" {
		*incoming = existing
	}
}

func (s *webConfigStore) resolveConfigPath() (string, bool) {
	p := findWebConfigFile(s.explicit)
	if p != "" {
		return p, true
	}
	if s.explicit != "" {
		return s.explicit, false
	}
	return "aiscan.yaml", false
}

func findWebConfigFile(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if _, err := os.Stat("aiscan.yaml"); err == nil {
		return "aiscan.yaml"
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "aiscan.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Cloud deploy state store (credentials + deployment records)
// ---------------------------------------------------------------------------

// isLoopbackListen reports whether addr binds only to loopback, so an open
// (token-less) control plane is not actually reachable from the network. A
// wildcard/empty host, a non-loopback IP, or an unparseable address is treated
// as exposed (returns false) so the security warning errs toward firing.
func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return false
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}

// hubLocalURL derives the loopback URL the outbound tunnel should expose from
// the server listen address. A wildcard/empty host becomes 127.0.0.1; an
// unparseable address yields "" (tunnel disabled).
func hubLocalURL(addr string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || port == "" {
		return ""
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// resolveDeployPath picks where the deploy state (cloud creds + records) lives.
// It must be STABLE across restarts: the file is the hub's only record of which
// cloud instances it owns, so a path that moves with the working directory would
// strand running instances as untrackable orphans on every restart. It anchors
// to the config file's directory, else a fixed per-user dir — never a bare
// relative path.
func resolveDeployPath(configFile string) string {
	if configFile != "" {
		return filepath.Join(filepath.Dir(configFile), "aiscan-deploy.yaml")
	}
	if p := findWebConfigFile(""); p != "" {
		return filepath.Join(filepath.Dir(p), "aiscan-deploy.yaml")
	}
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "aiscan", "aiscan-deploy.yaml")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".aiscan", "aiscan-deploy.yaml")
	}
	if abs, err := filepath.Abs("aiscan-deploy.yaml"); err == nil {
		return abs
	}
	return "aiscan-deploy.yaml"
}


//go:build full

package main

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/telemetry"
	types "github.com/chainreactors/cyber/core/types"
	ioaclient "github.com/chainreactors/cyber/exts/ioa/client"
	ioaserver "github.com/chainreactors/cyber/exts/ioa/server"
	webext "github.com/chainreactors/cyber/exts/web"
	cfg "github.com/chainreactors/cyber/pkg/config"
	node "github.com/chainreactors/cyber/pkg/node"
	profile "github.com/chainreactors/cyber/pkg/profile"

	webpkg "github.com/chainreactors/cyber/pkg/web"
	webhost "github.com/chainreactors/cyber/pkg/web/host"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	ioaservice "github.com/chainreactors/cyber/tools/ioa/server"
	webstatic "github.com/chainreactors/cyber/web"
	"github.com/chainreactors/ioa/protocols"
)

func serveWeb(ctx context.Context, option, explicitOption *cfg.Option, opts webCommand, logger telemetry.Logger) (resultErr error) {
	accessKey := opts.Token
	if accessKey == "" {
		accessKey = protocols.NewToken()
	}
	webConfig := webext.Config{
		Product:  productName,
		Profiles: []webpkg.Profile{{ID: "cyber-scan", Title: "Cyber Scan"}, {ID: "cyber-audit", Title: "Cyber Audit"}},
		Database: opts.DB,
		InitialProfile: func(ctx context.Context) (profile.Profile, error) {
			p, err := initWebProfile(ctx, option, logger)
			if err != nil {
				return p, err
			}
			providers, err := p.Providers()
			if err != nil {
				return p, err
			}
			if model, _ := providers.Current(); model == nil {
				logger.Warnf("%s", telemetry.StartupLine("skip", "llm", "AI disabled: set api_key in cyber.yaml or env"))
			}
			return p, nil
		},
		ConfigAPI:   configAPI(),
		AccessKey:   accessKey,
		ConfigStore: &webConfigStore{explicit: option.ConfigFile, runtime: option, overrides: explicitOption},
		BuildProfile: func(ctx context.Context, prepared *webservice.PreparedConfig) (profile.Profile, error) {
			if prepared.Runtime == nil {
				return nil, fmt.Errorf("config candidate has no resolved runtime options")
			}
			candidateOption := prepared.Runtime
			candidateProfile, err := initWebProfile(ctx, candidateOption, logger)
			if err != nil {
				return candidateProfile, err
			}
			return candidateProfile, nil
		},
		Scans: &webservice.ScanServiceConfig{
			MaxConcurrent: opts.MaxScans,
			ScanTimeout:   time.Duration(opts.ScanTimeout) * time.Second,
		},
	}
	if option.Debug {
		webConfig.AllowedOrigins = []string{"*"}
	}

	staticSub, err := fs.Sub(webstatic.FS, "static")
	if err != nil {
		return fmt.Errorf("load static assets: %s", err)
	}

	config := webhost.Config{
		Addr: opts.Addr, Static: staticSub, Management: webConfig, Logger: logger,
		Extensions: []extension.Extension{ioaserver.NewBrowser(ioaservice.Config{AccessKey: accessKey})},
	}
	if !opts.NoAgent {
		config.StartNode = func(ctx context.Context, addr string) error {
			agentOption, err := embeddedAgentOption(option, accessKey, addr)
			if err != nil {
				return err
			}
			return node.RunWebSocketWithCapabilities(ctx, newAIScanProfile, &agentOption, logger, "scan")
		}
	}
	return webhost.Serve(ctx, config)
}

func embeddedAgentOption(base *cfg.Option, accessKey, listenAddr string) (cfg.Option, error) {
	var option cfg.Option
	if base != nil {
		option = *base
	}
	if err := applyIdentity(&option); err != nil {
		return cfg.Option{}, err
	}
	serverURL := &url.URL{Scheme: "http", Host: listenAddr}
	serverURL.User = url.User(accessKey)
	option.ServerURL = serverURL.String()
	// This legacy product host explicitly mounts IOA beside Web. Independent
	// Web nodes cannot infer an IOA endpoint from their enrollment address.
	client, err := ioaclient.ReadOptions(&option)
	if err != nil {
		return cfg.Option{}, err
	}
	if _, explicit := option.Extensions[ioaclient.ConfigKey]["url"]; !explicit && client.URL == "" {
		option.Extensions = cfg.CloneValues(option.Extensions)
		if option.Extensions[ioaclient.ConfigKey] == nil {
			option.Extensions[ioaclient.ConfigKey] = map[string]any{}
		}
		endpoint := *serverURL
		endpoint.Path = "/ioa"
		option.Extensions[ioaclient.ConfigKey]["url"] = endpoint.String()
		option.Resolved = nil
	}
	if option.NodeID == "" && option.NodeName == "" {
		option.NodeName = "local"
	}
	if err := cfg.ResolveAgentServerURLs(&option); err != nil {
		return cfg.Option{}, fmt.Errorf("configure embedded agent: %w", err)
	}
	return option, nil
}

func initWebProfile(ctx context.Context, baseOption *cfg.Option, logger telemetry.Logger) (*aiscanProfile, error) {
	option := cfg.Option{}
	if baseOption != nil {
		option = *baseOption
	}
	if option.Resolved == nil {
		resolved, err := defaultSections().ResolveValues(option.Extensions, nil, nil)
		if err != nil {
			return nil, err
		}
		option.Resolved = resolved
		option.Extensions = resolved.Values()
	}
	config, err := configFromOption(&option, profile.ProviderOptional, nil, logger)
	if err != nil {
		return nil, err
	}
	config.Base.SkipEngines, config.IOA = true, nil
	p, err := buildAIScanProfile(config)
	if err != nil {
		return nil, err
	}
	if err := p.Load(ctx); err != nil {
		return p, err
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Config file store for web UI settings page
// ---------------------------------------------------------------------------

type webConfigStore struct {
	explicit  string
	runtime   *cfg.Option
	overrides *cfg.Option
	once      sync.Once
	store     *webhost.FileConfigStore
}

func (s *webConfigStore) shared() *webhost.FileConfigStore {
	s.once.Do(func() {
		s.store = &webhost.FileConfigStore{Explicit: s.explicit, Runtime: s.runtime, Overrides: s.overrides,
			Codec: webhost.ConfigCodec{Sections: defaultSections(), ParseDocument: parseConfigDocument,
				Document: func(value *types.DistributeConfig) map[string]any { return configDocument(value, nil) },
				Validate: validateConfig, PreserveValues: preserveURLCredentials,
			},
		}
	})
	return s.store
}
func (s *webConfigStore) GetDistributeConfig(ctx context.Context) (string, bool, *types.DistributeConfig, error) {
	return s.shared().GetDistributeConfig(ctx)
}
func (s *webConfigStore) PrepareDistributeConfig(ctx context.Context, value *types.DistributeConfig) (*webservice.PreparedConfig, error) {
	return s.shared().PrepareDistributeConfig(ctx, value)
}
func (s *webConfigStore) CommitDistributeConfig(ctx context.Context, value *webservice.PreparedConfig) error {
	return s.shared().CommitDistributeConfig(ctx, value)
}
func (s *webConfigStore) DiscardDistributeConfig(value *webservice.PreparedConfig) {
	s.shared().DiscardDistributeConfig(value)
}
func preserveLLMProfileSecrets(next, current *types.LLMConfig) {
	webhost.PreserveLLMProfileSecrets(next, current)
}

// ---------------------------------------------------------------------------
// Listen-address helpers
// ---------------------------------------------------------------------------

// hubLocalURL derives the loopback URL a local agent child should dial from the
// server listen address. A wildcard/empty host becomes 127.0.0.1; an
// unparseable address yields "".
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

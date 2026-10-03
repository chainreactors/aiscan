// Package provider adapts the LLM provider State to Extension lifecycle. It is
// unrelated to declaration aggregation; CLI and Config register through typed
// resource Points directly.
package provider

import (
	"context"
	"fmt"
	"sync"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/telemetry"
)

type Extension struct {
	mu       sync.Mutex
	release  func()
	state    *provider.State
	config   provider.StartupConfig
	logger   telemetry.Logger
	lifetime context.Context
	cancel   context.CancelFunc
}

func New(config provider.StartupConfig) *Extension {
	config.Fallbacks = append([]provider.ProviderConfig(nil), config.Fallbacks...)
	return &Extension{config: config}
}

// Load owns and publishes the provider state for this installation.
func (e *Extension) Load(scope *extension.Scope) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lifetime, e.cancel = context.WithCancel(scope.Lifetime())
	e.state = &provider.State{}
	logger, err := extension.Use[telemetry.Logger](scope)
	if err != nil {
		return err
	}
	e.logger = logger
	release, err := provider.Initialize(scope.Init(), e.state, e.config, logger)
	e.release = release
	if err != nil {
		return err
	}
	if err := extension.Provide[*provider.State](scope, e.state); err != nil {
		return err
	}
	return extension.Provide[provider.Controller](scope, e)
}

// Reload is the provider extension's live configuration boundary. Profiles
// may delegate to it, but session/runtime code must not initialize providers.
func (e *Extension) Reload(ctx context.Context, config provider.ProviderConfig, commit ...func() error) error {
	if e == nil {
		return fmt.Errorf("provider extension is unavailable")
	}
	e.mu.Lock()
	state, logger, lifetime := e.state, e.logger, e.lifetime
	e.mu.Unlock()
	if state == nil || lifetime == nil || lifetime.Err() != nil {
		return fmt.Errorf("provider extension is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	updateCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lifetime, cancel)
	defer stop()
	defer cancel()
	return state.Update(updateCtx, config, logger, commit...)
}
func (e *Extension) Close(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
	}
	if e.release != nil {
		e.release()
		e.release = nil
	}
	return nil
}

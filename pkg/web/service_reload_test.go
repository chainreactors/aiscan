package web

import (
	"context"
	"testing"

	"github.com/chainreactors/aiscan/core/runner"
	"github.com/chainreactors/aiscan/pkg/webproto"
)

type reloadCtxKey struct{}

type reloadConfigStore struct {
	cfg webproto.DistributeConfig
}

func (s *reloadConfigStore) GetDistributeConfig(context.Context) (string, bool, webproto.DistributeConfig, error) {
	return "aiscan.yaml", true, s.cfg, nil
}

func (s *reloadConfigStore) SaveDistributeConfig(_ context.Context, cfg webproto.DistributeConfig) error {
	s.cfg = cfg
	return nil
}

func TestSaveConfigUsesRuntimeContextForReload(t *testing.T) {
	runtimeCtx := context.WithValue(context.Background(), reloadCtxKey{}, "runtime")
	requestCtx := context.WithValue(context.Background(), reloadCtxKey{}, "request")
	store := &reloadConfigStore{}

	called := false
	svc := NewService(ServiceConfig{
		ConfigStore:    store,
		RuntimeContext: runtimeCtx,
		AppFactory: func(ctx context.Context) (*runner.App, error) {
			called = true
			if got := ctx.Value(reloadCtxKey{}); got != "runtime" {
				t.Fatalf("reload ctx value = %v, want runtime", got)
			}
			return &runner.App{}, nil
		},
	})

	var cfg webproto.DistributeConfig
	cfg.LLM.Model = "model-from-request"
	if _, err := svc.SaveConfig(requestCtx, cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if !called {
		t.Fatal("reload was not called")
	}
}

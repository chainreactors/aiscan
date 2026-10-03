package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/telemetry"
	providerext "github.com/chainreactors/cyber/exts/provider"
	profile "github.com/chainreactors/cyber/pkg/profile"
)

type providerCommitProfile struct {
	*recordingProfile
	controller provider.Controller
}

func (p *providerCommitProfile) CommitProvider(ctx context.Context, config provider.ProviderConfig, commit func() error) error {
	return p.controller.Reload(ctx, config, commit)
}

func TestWebProviderSavePreservesInstallationAndWorkAcrossCommitFailures(t *testing.T) {
	for _, failure := range []string{"none", "probe", "commit"} {
		t.Run(failure, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if failure == "probe" {
					http.Error(w, `{"error":{"message":"quota"}}`, http.StatusPaymentRequired)
					return
				}
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			}))
			defer server.Close()
			base, _, closed := newRecordingProfile(t)
			var state *provider.State
			var controller provider.Controller
			set, err := extension.New(
				extension.Func{LoadFunc: func(scope *extension.Scope) error {
					return extension.Provide[telemetry.Logger](scope, telemetry.NopLogger())
				}},
				providerext.New(provider.StartupConfig{Mode: provider.StartupDisabled}),
				extension.Func{LoadFunc: func(scope *extension.Scope) error {
					var err error
					if state, err = extension.Use[*provider.State](scope); err != nil {
						return err
					}
					controller, err = extension.Use[provider.Controller](scope)
					return err
				}},
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := set.Load(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer set.Close(context.Background())
			state.Set(nil, provider.ProviderConfig{Provider: "openai", Model: "old-model"})
			originalHealth := state.Health()
			owner := &providerCommitProfile{recordingProfile: base, controller: controller}
			store := &transactionalConfigStore{cfg: configForModel("old-model")}
			if failure == "commit" {
				store.commitErr = errors.New("disk full")
			}
			var builds atomic.Int32
			svc := NewService(ServiceConfig{Profile: owner, ConfigStore: store, BuildProfile: func(context.Context, *PreparedConfig) (profile.Profile, error) {
				builds.Add(1)
				return nil, errors.New("provider edit incorrectly rebuilt installation")
			}})
			defer svc.Close(context.Background())
			workCtx, admitted := svc.beginWork()
			if !admitted {
				t.Fatal("work was not admitted")
			}
			defer svc.work.Done()
			candidate := configForModel("new-model")
			candidate.Llm.Providers[0].BaseUrl = server.URL + "/v1"
			candidate.Llm.Providers[0].ApiKey = "fixture"
			_, err = svc.SaveConfig(t.Context(), candidate)
			if builds.Load() != 0 || svc.profile != owner || closed() || workCtx.Err() != nil {
				t.Fatal("provider save replaced installation or canceled admitted work")
			}
			_, config := state.Current()
			if failure == "none" {
				if err != nil || config.Model != "new-model" || activeModel(store.cfg) != "new-model" {
					t.Fatalf("successful save = %v, provider=%v", err, config)
				}
			} else if err == nil || config.Model != "old-model" || activeModel(store.cfg) != "old-model" || state.Health() != originalHealth || store.discarded != 1 {
				t.Fatalf("failed save damaged previous state: err=%v provider=%v discarded=%d", err, config, store.discarded)
			}
		})
	}
}

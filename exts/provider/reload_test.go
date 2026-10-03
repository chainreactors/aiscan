package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/telemetry"
)

func TestCloseCancelsProviderProbeWithoutWaitingForNetwork(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	owner := New(provider.StartupConfig{Mode: provider.StartupDisabled})
	set, err := extension.New(extension.Provided[telemetry.Logger](telemetry.NopLogger()), owner)
	if err != nil {
		t.Fatal(err)
	}
	defer set.Close(context.Background())
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- owner.Reload(t.Context(), provider.ProviderConfig{Provider: "openai", BaseURL: server.URL + "/v1", APIKey: "fixture", Model: "next"})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := set.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("owner shutdown did not cancel reload: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("provider reload survived its owner")
	}
	if current, config := owner.state.Current(); current != nil || config != (provider.ProviderConfig{}) {
		t.Fatal("late probe resurrected a closed provider")
	}
	if err := owner.Reload(t.Context(), provider.ProviderConfig{}); err == nil {
		t.Fatal("closed extension accepted configuration")
	}
}

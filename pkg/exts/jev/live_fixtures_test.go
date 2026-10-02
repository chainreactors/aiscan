//go:build full

package jev

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	coretool "github.com/chainreactors/cyber/core/tool"
)

type liveNativeInstallation struct {
	e      *Extension
	cfg    agent.Config
	meter  *benchmarkProvider
	client *jevapi.Client
}

func installLiveNative(t *testing.T, providerConfig *provider.ProviderConfig, config Config, key, system string, maxTurns int, closeTimeout time.Duration, nativeTools []coretool.Tool) liveNativeInstallation {
	t.Helper()
	if err := os.MkdirAll(config.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	llm, err := provider.NewProvider(providerConfig)
	if err != nil {
		t.Fatal(err)
	}
	meter := &benchmarkProvider{Provider: llm, tracePath: filepath.Join(config.Directory, "llm.jsonl")}
	registry, tools := corehooks.New(), coretool.NewToolRegistry()
	client := jevapi.New(key, "", 10*time.Second)
	t.Cleanup(client.Close)
	e := New(config)
	set, err := extension.New(extension.Provided[*corehooks.Registry](registry), tools,
		extension.Func{LoadFunc: func(scope *extension.Scope) error { return extension.Add[coretool.Tool](scope, nativeTools...) }},
		extension.Provided[*jevapi.Client](client), e)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		if err := set.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return liveNativeInstallation{e: e, meter: meter, client: client, cfg: agent.Config{
		Loop: agent.StandardLoop{}, Provider: meter, Tools: tools, Hooks: registry,
		Model: providerConfig.Model, MaxTokens: 4096, MaxTurns: maxTurns, MaxRetries: -1, SystemPrompt: system,
	}}
}

func executedJEVActions(result *agent.Result) int {
	if result == nil {
		return 0
	}
	actions := 0
	for _, message := range result.Messages {
		if message.Name == "jev" {
			actions += strings.Count(provider.MessageText(message), "Executed [")
		}
	}
	return actions
}

func writeLiveReport(t *testing.T, path string, report any) {
	t.Helper()
	data, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		err = os.WriteFile(path, data, 0600)
	}
	if err != nil {
		t.Error(err)
	}
}

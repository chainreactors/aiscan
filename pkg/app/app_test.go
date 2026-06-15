package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/spray"

	_ "github.com/chainreactors/aiscan/pkg/tools/search"
)

func mustGogoEngine(t *testing.T) *gogo.Engine {
	t.Helper()
	engine, err := gogo.NewEngine(nil)
	if err != nil {
		t.Fatalf("gogo.NewEngine() error = %v", err)
	}
	return engine
}

func mustSprayEngine(t *testing.T) *spray.Engine {
	t.Helper()
	engine, err := spray.NewEngine(nil)
	if err != nil {
		t.Fatalf("spray.NewEngine() error = %v", err)
	}
	return engine
}

func TestInitCommandRegistryRegistersSearchAlways(t *testing.T) {
	logger := telemetry.NopLogger()
	reg := initCommandRegistry(nil, ScannerConfig{}, ToolConfig{}, nil, agent.ProviderConfig{}, nil, logger)

	if !reg.Has("search") {
		t.Fatal("search command should always be registered")
	}
}

func TestInitCommandRegistryRegistersScannerCommands(t *testing.T) {
	logger := telemetry.NopLogger()
	engines := &engine.Set{
		Gogo:  mustGogoEngine(t),
		Spray: mustSprayEngine(t),
	}

	reg := initCommandRegistry(engines, ScannerConfig{}, ToolConfig{}, nil, agent.ProviderConfig{}, nil, logger)

	for _, name := range []string{"scan", "gogo", "spray"} {
		if !reg.Has(name) {
			t.Fatalf("%s command should be registered when scanner engines are available", name)
		}
	}
}

func TestInitCommandRegistryRegistersCoreTools(t *testing.T) {
	logger := telemetry.NopLogger()
	reg := initCommandRegistry(nil, ScannerConfig{}, ToolConfig{BashTimeout: 30}, nil, agent.ProviderConfig{}, nil, logger)

	tools := reg.Tools()
	expected := map[string]bool{"read": true, "write": true, "glob": true, "bash": true, "fetch": true}
	for _, tool := range tools {
		if !expected[tool.Name()] {
			t.Fatalf("unexpected agent tool: %s", tool.Name())
		}
	}
	if len(tools) != len(expected) {
		names := make([]string, len(tools))
		for i, tool := range tools {
			names[i] = tool.Name()
		}
		t.Fatalf("expected %d agent tools, got %d: %v", len(expected), len(tools), names)
	}
}

func TestInitCommandRegistryRegistersWebSearchWithProvider(t *testing.T) {
	logger := telemetry.NopLogger()
	reg := initCommandRegistry(nil, ScannerConfig{}, ToolConfig{BashTimeout: 30}, fakeProvider{}, agent.ProviderConfig{Model: "test"}, nil, logger)

	if _, ok := reg.GetTool("web_search"); !ok {
		t.Fatal("web_search tool should be registered when an LLM provider is configured")
	}
}

func TestCommandRegistryOnlyExposesNativeAgentTools(t *testing.T) {
	reg := command.NewRegistry()
	command.BuildAll(&command.Deps{
		WorkDir:     "/tmp",
		BashTimeout: 30,
	}, reg)

	for _, tool := range reg.Tools() {
		switch tool.Name() {
		case "read", "write", "glob", "bash", "fetch":
		default:
			t.Fatalf("pseudo-command %q should NOT be registered as an agent tool", tool.Name())
		}
	}
}

func TestNewKeepsProviderConfigWhenProviderDisabled(t *testing.T) {
	providerCfg := agent.ProviderConfig{
		Provider: "deepseek",
		APIKey:   "test-key",
		BaseURL:  "https://api.deepseek.com/v1",
		Model:    "deepseek-v4-pro",
	}
	app, err := New(t.Context(), Config{
		Provider: ProviderConfig{Config: providerCfg},
		Logger:   telemetry.NopLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	if app.Provider != nil {
		t.Fatal("provider should not be initialized when disabled")
	}
	if app.ProviderConfig.Provider != providerCfg.Provider ||
		app.ProviderConfig.APIKey != providerCfg.APIKey ||
		app.ProviderConfig.BaseURL != providerCfg.BaseURL ||
		app.ProviderConfig.Model != providerCfg.Model {
		t.Fatalf("provider config not preserved: %#v", app.ProviderConfig)
	}
}

func TestAppCloseClosesPseudoCommands(t *testing.T) {
	reg := command.NewRegistry()
	closed := false
	reg.Register(&closeRecordingCommand{closed: &closed}, "tools")

	app := &App{Commands: reg}
	app.Close()

	if !closed {
		t.Fatal("pseudo-command Close() was not called")
	}
}

func TestInitIOAFailureDoesNotExposePartialState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "ioa unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	reg := command.NewRegistry()
	app := &App{Commands: reg}

	err := app.InitIOA(context.Background(), IOAConfig{
		URL:           srv.URL,
		NodeName:      "tester",
		RegisterTools: true,
		AutoRegister:  true,
	})
	if err == nil {
		t.Fatal("InitIOA() error = nil, want failure")
	}
	if app.IOAClient != nil {
		t.Fatal("IOAClient should not be committed after init failure")
	}
	if app.IOAStreamClient != nil {
		t.Fatal("IOAStreamClient should not be committed after init failure")
	}
	for _, name := range []string{"ioa_space", "ioa_send", "ioa_read"} {
		if reg.Has(name) {
			t.Fatalf("%s should not be registered after init failure", name)
		}
	}
}

func TestInitIOASuccessCommitsClientAndCommands(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/nodes" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"node-1","name":"tester"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	reg := command.NewRegistry()
	app := &App{Commands: reg}

	err := app.InitIOA(context.Background(), IOAConfig{
		URL:           srv.URL,
		NodeName:      "tester",
		RegisterTools: true,
		AutoRegister:  true,
	})
	if err != nil {
		t.Fatalf("InitIOA() error = %v", err)
	}
	if app.IOAClient == nil {
		t.Fatal("IOAClient should be committed after successful init")
	}
	for _, name := range []string{"ioa_space", "ioa_send", "ioa_read"} {
		if !reg.Has(name) {
			t.Fatalf("%s should be registered after successful init", name)
		}
	}
}

type closeRecordingCommand struct {
	closed *bool
}

func (c *closeRecordingCommand) Name() string { return "closer" }

func (c *closeRecordingCommand) Usage() string { return "" }

func (c *closeRecordingCommand) Execute(_ context.Context, _ []string, _ io.Writer) error {
	return nil
}

func (c *closeRecordingCommand) Close() {
	*c.closed = true
}

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }

func (fakeProvider) ChatCompletion(context.Context, *agent.ChatCompletionRequest) (*agent.ChatCompletionResponse, error) {
	return &agent.ChatCompletionResponse{}, nil
}

func (fakeProvider) WebSearch(context.Context, string, int) (*agent.WebSearchResponse, error) {
	return &agent.WebSearchResponse{}, nil
}

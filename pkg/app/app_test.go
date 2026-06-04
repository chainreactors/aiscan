package app

import (
	"context"
	"io"
	"testing"

	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/spray"

	_ "github.com/chainreactors/aiscan/pkg/tools/search"
)

func TestInitCommandRegistryRegistersSearchAlways(t *testing.T) {
	logger := telemetry.NopLogger()
	reg := initCommandRegistry(nil, ScannerConfig{}, ToolConfig{}, nil, "", nil, logger)

	if !reg.Has("search") {
		t.Fatal("search command should always be registered")
	}
}

func TestInitCommandRegistryRegistersScannerCommands(t *testing.T) {
	logger := telemetry.NopLogger()
	engines := &engine.Set{
		Gogo:  gogo.NewEngine(nil),
		Spray: spray.NewEngine(nil),
	}

	reg := initCommandRegistry(engines, ScannerConfig{}, ToolConfig{}, nil, "", nil, logger)

	for _, name := range []string{"scan", "gogo", "spray"} {
		if !reg.Has(name) {
			t.Fatalf("%s command should be registered when scanner engines are available", name)
		}
	}
}

func TestInitCommandRegistryRegistersCoreTools(t *testing.T) {
	logger := telemetry.NopLogger()
	reg := initCommandRegistry(nil, ScannerConfig{}, ToolConfig{BashTimeout: 30}, nil, "", nil, logger)

	tools := reg.Tools()
	expected := map[string]bool{"read": true, "write": true, "glob": true, "bash": true}
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

func TestCommandRegistryOnlyExposesCoreTrueTools(t *testing.T) {
	reg := command.NewRegistry()
	command.BuildAll(&command.Deps{
		WorkDir:     "/tmp",
		BashTimeout: 30,
	}, reg)

	for _, tool := range reg.Tools() {
		switch tool.Name() {
		case "read", "write", "glob", "bash":
		default:
			t.Fatalf("pseudo-command %q should NOT be registered as an agent tool", tool.Name())
		}
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

//go:build browser

package app

import (
	"testing"

	"github.com/chainreactors/aiscan/pkg/command"

	_ "github.com/chainreactors/aiscan/pkg/tools/playwright"
)

func TestBrowserBuildRegistersPlaywrightPseudoCommand(t *testing.T) {
	reg := command.NewRegistry()
	command.BuildAll(&command.Deps{
		WorkDir:     t.TempDir(),
		BashTimeout: 30,
	}, reg)

	if !reg.Has("playwright") {
		t.Fatalf("browser build should register playwright pseudo-command; got %v", reg.Names())
	}
}

//go:build full

package tools

import (
	"testing"

	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"

	_ "github.com/chainreactors/aiscan/pkg/tools/katana"
	_ "github.com/chainreactors/aiscan/pkg/tools/passive"
)

func TestRegisterAllRegistersKatanaInFullBuild(t *testing.T) {
	engineSet := &engine.Set{
		Gogo:  mustGogoEngine(t),
		Spray: mustSprayEngine(t),
	}
	reg := buildRegistry(engineSet)

	if !reg.Has("katana") {
		t.Fatal("expected katana to be registered in full build")
	}
}

func TestRegisterAllRegistersPassiveWithUncover(t *testing.T) {
	engineSet := &engine.Set{}
	engineSet.SetupUncover(engine.ReconOptions{
		FofaEmail: "test@example.com",
		FofaKey:   "deadbeef",
	}, nil)
	reg := buildRegistry(engineSet)

	if !reg.Has("passive") {
		t.Fatal("expected passive to be registered when engineSet.Uncover is non-nil")
	}
}

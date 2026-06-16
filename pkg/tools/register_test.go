package tools

import (
	"testing"

	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/core/resources"
	_ "github.com/chainreactors/aiscan/pkg/tools/search"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	fingerslib "github.com/chainreactors/fingers/fingers"
	sdkfingers "github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/spray"
)

func buildRegistry(engineSet *engine.Set) *commands.CommandRegistry {
	reg := commands.NewRegistry()
	deps := &commands.Deps{
		EngineSet: engineSet,
		Resources: engineSet.Resources,
	}
	commands.BuildAll(deps, reg)
	return reg
}

func TestRegisterAllTreatsNeutronAsOptional(t *testing.T) {
	gogoEng, _ := gogo.NewEngine(nil)
	sprayEng, _ := spray.NewEngine(nil)
	engineSet := &engine.Set{
		Gogo:  gogoEng,
		Spray: sprayEng,
	}
	reg := buildRegistry(engineSet)

	for _, name := range []string{"scan", "gogo", "spray"} {
		if !reg.Has(name) {
			t.Fatalf("expected %q to be registered", name)
		}
	}
	if reg.Has("neutron") {
		t.Fatal("neutron should not be registered without templates")
	}
}

func TestRegisterAllRegistersSearchWithResources(t *testing.T) {
	engineSet := &engine.Set{
		Resources: &resources.Set{
			FingersConfig: sdkfingers.NewConfig().WithFingers(fingerslib.Fingers{{Name: "nginx", Protocol: "http"}}),
		},
	}
	reg := buildRegistry(engineSet)

	if !reg.Has("search") {
		t.Fatal("expected search to be registered")
	}
}

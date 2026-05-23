package tools

import (
	"testing"

	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/resources"
	_ "github.com/chainreactors/aiscan/pkg/tools/cyberhub"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	fingerslib "github.com/chainreactors/fingers/fingers"
	sdkfingers "github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/spray"
)

func buildRegistry(engineSet *engine.Set) *command.CommandRegistry {
	reg := command.NewRegistry()
	deps := &command.Deps{
		EngineSet: engineSet,
		Resources: engineSet.Resources,
	}
	command.BuildAll(deps, reg)
	return reg
}

func TestRegisterAllTreatsNeutronAsOptional(t *testing.T) {
	engineSet := &engine.Set{
		Gogo:  gogo.NewEngine(nil),
		Spray: spray.NewEngine(nil),
	}
	reg := buildRegistry(engineSet)

	for _, name := range []string{"katana", "scan", "gogo", "spray"} {
		if !reg.Has(name) {
			t.Fatalf("expected %q to be registered", name)
		}
	}
	if reg.Has("neutron") {
		t.Fatal("neutron should not be registered without templates")
	}
	if reg.Has("passive") {
		t.Fatal("passive should not be registered without recon engines")
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

func TestRegisterAllRegistersCyberhubWhenResourcesAvailable(t *testing.T) {
	engineSet := &engine.Set{
		Resources: &resources.Set{
			FingersConfig: sdkfingers.NewConfig().WithFingers(fingerslib.Fingers{{Name: "nginx", Protocol: "http"}}),
		},
	}
	reg := buildRegistry(engineSet)

	if !reg.Has("cyberhub") {
		t.Fatal("expected cyberhub to be registered")
	}
}

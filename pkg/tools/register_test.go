package tools

import (
	"testing"

	"github.com/chainreactors/aiscan/pkg/resources"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	fingerslib "github.com/chainreactors/fingers/fingers"
	sdkfingers "github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/spray"
)

func TestRegisterAllTreatsNeutronAsOptional(t *testing.T) {
	reg := NewScannerRegistry()
	engineSet := &engine.Set{
		Gogo:  gogo.NewEngine(nil),
		Spray: spray.NewEngine(nil),
	}

	if err := RegisterAll(reg, engineSet); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}
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
	reg := NewScannerRegistry()
	engineSet := &engine.Set{}
	engineSet.SetupIna(engine.ReconOptions{
		FofaEmail: "test@example.com",
		FofaKey:   "deadbeef",
	}, nil)
	if err := RegisterAll(reg, engineSet); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}
	if !reg.Has("passive") {
		t.Fatal("expected passive to be registered when engineSet.Ina is non-nil")
	}
}

func TestRegisterAllRegistersCyberhubWhenResourcesAvailable(t *testing.T) {
	reg := NewScannerRegistry()
	engineSet := &engine.Set{
		Resources: &resources.Set{
			FingersConfig: sdkfingers.NewConfig().WithFingers(fingerslib.Fingers{{Name: "nginx", Protocol: "http"}}),
		},
	}

	if err := RegisterAll(reg, engineSet); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}
	if !reg.Has("cyberhub") {
		t.Fatal("expected cyberhub to be registered")
	}
}

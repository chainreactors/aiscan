package tools

import (
	"testing"

	"github.com/chainreactors/aiscan/pkg/resources"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	anigo "github.com/chainreactors/ani-go"
	fingerslib "github.com/chainreactors/fingers/fingers"
	inago "github.com/chainreactors/ina-go"
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
	if reg.Has("ina") {
		t.Fatal("ina should not be registered without recon credentials")
	}
	if reg.Has("ani") {
		t.Fatal("ani should not be registered when engineSet.Ani is nil")
	}
}

func TestRegisterAllRegistersInaWhenConfigured(t *testing.T) {
	reg := NewScannerRegistry()
	engineSet := &engine.Set{
		Ina: inago.NewEngine(inago.NewConfig().WithFofa("test@example.com", "deadbeef")),
	}
	if err := RegisterAll(reg, engineSet); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}
	if !reg.Has("ina") {
		t.Fatal("expected ina to be registered when engineSet.Ina is non-nil")
	}
}

func TestRegisterAllRegistersAniWhenConfigured(t *testing.T) {
	reg := NewScannerRegistry()
	engineSet := &engine.Set{
		Ani: anigo.NewEngine(nil),
	}
	if err := RegisterAll(reg, engineSet); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}
	if !reg.Has("ani") {
		t.Fatal("expected ani to be registered when engineSet.Ani is non-nil")
	}
}

// 默认 Config (无任何凭证) 只注册 aqc_unauth + tyc_unauth, cred-gated 源 (tyc/qcc/aqc) 跳过。
func TestAniSourcesDefaultsToUnauthOnly(t *testing.T) {
	e := anigo.NewEngine(nil)
	sources := map[string]bool{}
	for _, s := range e.Sources() {
		sources[s] = true
	}
	for _, want := range []string{"aqc_unauth", "tyc_unauth"} {
		if !sources[want] {
			t.Errorf("expected %q in sources, got %v", want, sources)
		}
	}
	for _, banned := range []string{"tyc", "qcc", "aqc"} {
		if sources[banned] {
			t.Errorf("expected %q NOT in sources (no creds), got %v", banned, sources)
		}
	}
}

// 提供凭证后, 对应源应该自动注册。
func TestAniSourcesRegistersOnCreds(t *testing.T) {
	cfg := anigo.NewConfig().
		WithTycToken("fake-jwt").
		WithQccCookie("QCCSESSID=fake").
		WithAqcCookie("BAIDUID=fake")
	e := anigo.NewEngine(cfg)
	sources := map[string]bool{}
	for _, s := range e.Sources() {
		sources[s] = true
	}
	for _, want := range []string{"aqc_unauth", "tyc_unauth", "tyc", "qcc", "aqc"} {
		if !sources[want] {
			t.Errorf("expected %q in sources with creds, got %v", want, sources)
		}
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

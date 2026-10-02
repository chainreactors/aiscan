package jev

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
)

func TestNativeArgumentsRemainOpaque(t *testing.T) {
	call := &aop.ToolCall{Name: "native", Arguments: &aop.EncodedValue{Data: []byte(`{"command":"opaque $VALUE | \"data\"","id":9007199254740993}`)}}
	if encoded := canonical(call); encoded != `["native",{"command":"opaque $VALUE | \"data\"","id":9007199254740993}]` {
		t.Fatalf("native argument semantics changed: %s", encoded)
	}
}

func TestLibraryPublicationFailurePreservesMemory(t *testing.T) {
	for _, saveFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "change rejected", true: "save rejected"}[saveFailure], func(t *testing.T) {
			e := New(Config{Directory: t.TempDir()})
			claim := Claim{When: "Current workflow", Question: "Can it progress?", Options: map[string]string{"go": "Progress", Defer: "Missing facts"}}
			e.library.Claims["current"] = claimRecord{Claim: claim}
			before := digest(e.snapshot())
			if saveFailure {
				if err := os.Mkdir(filepath.Join(e.config.Directory, "library.json"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := e.updateLibrary(func(lib *library) (bool, error) {
				c := lib.Claims["current"]
				c.Options["go"], c.Consumed = "Changed", true
				lib.Claims["current"] = c
				if !saveFailure {
					return false, errors.New("reject candidate library")
				}
				return true, nil
			})
			if err == nil || changed || digest(e.snapshot()) != before {
				t.Fatalf("failed publication changed memory: changed=%t error=%v", changed, err)
			}
			if files, _ := filepath.Glob(filepath.Join(e.config.Directory, ".library-*")); len(files) != 0 {
				t.Fatal("failed publication retained a temporary library")
			}
		})
	}
}

func TestLibraryDerivesCompiledFromPublishedScenes(t *testing.T) {
	e := New(Config{Directory: t.TempDir()})
	claim := Claim{When: "Current workflow", Question: "Can it progress?", Options: map[string]string{"go": "Progress", Defer: "Missing facts"}}
	cid := "c" + digest(claim)[:16]
	r := observationReflex(t, `js:({state:{},candidates:{}})`)
	rid := "r" + digest(r)[:16]
	group := digest(map[string]Claim{cid: claim})
	if _, err := e.updateLibrary(func(lib *library) (bool, error) {
		lib.Claims[cid] = claimRecord{Claim: claim}
		lib.Reflexes[rid] = reflexRecord{Reflex: r, Claims: []string{cid}}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(e.config.Directory, "library.json"))
	var saved library
	if err != nil || json.Unmarshal(data, &saved) != nil || !saved.Compiled[group] || !e.snapshot().Compiled[group] || e.library.Compiled != nil {
		t.Fatalf("derived compatibility field lost: saved=%+v error=%v", saved, err)
	}
	if !e.retireReflex(rid, errors.New("retire test scene")) || len(e.snapshot().Compiled) != 0 {
		t.Fatal("retired scene still marks its declarations compiled")
	}
}

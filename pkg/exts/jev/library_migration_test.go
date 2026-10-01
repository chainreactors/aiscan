package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestLibraryMigrationRetiresExprWithoutLosingClaimsOrJavaScript(t *testing.T) {
	var claims []Claim
	if err := json.Unmarshal([]byte(fixtureClaim), &claims); err != nil {
		t.Fatal(err)
	}
	claimID := "c" + digest(claims[0])[:16]
	old := Reflex{When: "Finite advancement", Decide: "Advance the next step", Observe: `{state:{},candidates:{}}`}
	current := Reflex{When: "A separate current scene", Decide: "Use recorded evidence", Observe: constantObserve(`{}`, nil)}
	oldID, currentID := "r"+digest(old)[:16], "r"+digest(current)[:16]
	directory := t.TempDir()
	legacy := library{Version: 2,
		Claims: map[string]claimRecord{claimID: {Claim: claims[0], Task: "original", Consumed: true}},
		Reflexes: map[string]reflexRecord{
			oldID: {Reflex: old, Claims: []string{claimID}}, currentID: {Reflex: current},
		},
		Compiled: map[string]bool{digest(map[string]Claim{claimID: claims[0]}): true},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "library.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	e := New(Config{Directory: directory})
	if err := e.loadLibrary(); err != nil {
		t.Fatal(err)
	}
	lib := e.snapshot()
	if lib.Version != libraryVersion || len(lib.Reflexes) != 1 || lib.Reflexes[currentID].Observe != current.Observe || len(lib.Compiled) != 0 || !reflect.DeepEqual(lib.Claims[claimID], legacy.Claims[claimID]) {
		t.Fatalf("migration lost a declaration or reused Expr: %+v", lib)
	}
	backups, err := filepath.Glob(filepath.Join(directory, "library-v2-*.json"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups=%v error=%v", backups, err)
	}
	if saved, err := os.ReadFile(backups[0]); err != nil || !bytes.Equal(saved, raw) {
		t.Fatalf("original library was not preserved: %v", err)
	}
	if err := e.loadLibrary(); err != nil {
		t.Fatal(err)
	}
	if repeated, _ := filepath.Glob(filepath.Join(directory, "library-v2-*.json")); len(repeated) != 1 {
		t.Fatal("restart repeated the migration")
	}

	// A consumed Claim remains a compilation source, never a replayable action.
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer { return declarationAnswers(req, true) })
	e, cfg, _ := testInstallation(t, Config{Mode: "auto", Directory: directory}, client,
		coretool.Command{Name: "advance", Run: func(context.Context, *coretool.Execution) (any, error) {
			t.Fatal("startup or recompilation executed a tool")
			return nil, nil
		}})
	generations := 0
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		generations++
		if provider.MessageText(req.Messages[0]) != compilePrompt {
			t.Fatal("migration regenerated an existing Claim")
		}
		return reply(provider.TextMessage("assistant", fixtureReflex)), nil
	})
	if err := e.compile(t.Context(), declaration{cfg: cfg}, claimID); err != nil {
		t.Fatal(err)
	}
	if generations != 1 || len(e.snapshot().Reflexes) != 2 || !e.snapshot().Claims[claimID].Consumed {
		t.Fatal("retired declaration was not recompiled independently of consumption")
	}
}

func TestCurrentLibraryRejectsExprWithoutRewritingData(t *testing.T) {
	old := Reflex{When: "Finite scene", Decide: "Choose current actions", Observe: `{state:{},candidates:{}}`}
	if err := old.validate(); err == nil {
		t.Fatal("runtime accepted Expr")
	}
	directory := t.TempDir()
	lib := library{Version: libraryVersion, Claims: map[string]claimRecord{},
		Reflexes: map[string]reflexRecord{"r" + digest(old)[:16]: {Reflex: old}}, Compiled: map[string]bool{}}
	raw, err := json.Marshal(lib)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "library.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := New(Config{Directory: directory}).loadLibrary(); err == nil {
		t.Fatal("current library silently migrated an invalid scene")
	}
	if saved, err := os.ReadFile(path); err != nil || !bytes.Equal(saved, raw) {
		t.Fatal("invalid library was rewritten")
	}
}

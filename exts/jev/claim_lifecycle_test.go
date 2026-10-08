package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestClaimLibraryRejectsUnknownAndInvalidFormatsWithoutRewriting(t *testing.T) {
	for _, data := range []string{
		`{"format":"claim/99","claims":{},"reflexes":{}}`,
		`{"claims":null,"reflexes":{}}`,
		`{"unrelated":"file"}`,
		`{"format":"claim/2"`,
		`{"format":"claim/2","claims":{},"reflexes":{},"compiled":{}}`,
		`{"format":"claim/2","claims":{},"reflexes":{},"unknown":true}`,
		`{"format":"claim/2","claims":{"c":{"text":"old","task":"x"}},"reflexes":{}}`,
		`{"format":"claim/2","claims":{"c":{"type":"noul","context":"valid","when":"legacy"}},"reflexes":{}}`,
	} {
		e := New(Config{Directory: t.TempDir()})
		path := filepath.Join(e.config.Directory, "library.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := e.loadLibrary(); err == nil {
			t.Fatal("old or ambiguous format accepted")
		}
		got, _ := os.ReadFile(path)
		if string(got) != data || len(e.snapshot().Claims) != 0 {
			t.Fatal("rejected library mutated")
		}
	}
}

func TestClaimLibraryUpgradeArchivesOnceAndRemainsUsable(t *testing.T) {
	for _, format := range []string{"", "claim/1"} {
		for _, mode := range []string{"off", "auto"} {
			t.Run(format+"/"+mode, func(t *testing.T) {
				directory := t.TempDir()
				original := `{"claims":{"old":{"text":"Historical judgment","task":"previous","consumed":true}},"reflexes":{"old":{"observe":"old executable source"}},"compiled":{"old":true}}`
				if format != "" {
					original = `{"format":"` + format + `",` + original[1:]
				}
				path := filepath.Join(directory, "library.json")
				if err := os.WriteFile(path, []byte(original), 0600); err != nil {
					t.Fatal(err)
				}
				client := fakeJEV(t, func(inferenceRequest) map[string]inferenceAnswer { return map[string]inferenceAnswer{} })
				e, _, _ := testInstallation(t, Config{Directory: directory, Mode: mode}, client)
				current := e.snapshot()
				if current.Format != libraryFormat || len(current.Claims) != 0 || len(current.Reflexes) != 0 || len(current.Candidates) != 0 {
					t.Fatal("historical definitions became active without current validation")
				}
				archives, err := filepath.Glob(filepath.Join(directory, "library-backup-*.json"))
				if err != nil || len(archives) != 1 {
					t.Fatalf("archives=%v error=%v", archives, err)
				}
				got, err := os.ReadFile(archives[0])
				if err != nil || string(got) != original {
					t.Fatal("historical library bytes were not preserved", err)
				}
				claim := Claim{Type: jevapi.ClaimNoul, Context: "Current observed page is ready."}
				id := "c" + digest(claim)[:16]
				if _, err := e.updateLibrary(func(lib *library) (bool, error) { lib.Claims[id] = claimRecord{Claim: claim}; return true, nil }); err != nil {
					t.Fatal(err)
				}
				reloaded := New(Config{Directory: directory})
				if err := reloaded.loadLibrary(); err != nil {
					t.Fatal(err)
				}
				if reloaded.snapshot().Claims[id].Context != claim.Context {
					t.Fatal("current Claim was not persisted")
				}
				archives, err = filepath.Glob(filepath.Join(directory, "library-backup-*.json"))
				if err != nil || len(archives) != 1 {
					t.Fatal("a normal reload archived the library again", err)
				}
			})
		}
	}
}

func TestOrdinaryReflexPublishesAndCompilesItsOwnReplacement(t *testing.T) {
	client := fakeJEV(t, func(req inferenceRequest) map[string]inferenceAnswer {
		for _, kind := range []string{"input", "binding", "completion"} {
			if _, ok := req.Questions[kind]; ok {
				return map[string]inferenceAnswer{kind: answer("accept")}
			}
		}
		if runtimeRequest(req) {
			return runtimeAnswers(req, "run")
		}
		return declarationAnswers(req, true)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	c := Claim{Type: jevapi.ClaimNoul, Context: "Publish the current requested semantic judgment and report its content ID."}
	ids, err := e.publishClaims(t.Context(), []Claim{c}, "recorded")
	cid := ""
	if err == nil {
		cid = ids[0]
	}
	if err != nil {
		t.Fatal(err)
	}
	parentSource := `js:function(context,args){if(!args)return {defer:"missing arguments",parameters:"claim,previous"};const published=execute({name:"bash",arguments:{command:command("jev",["claim",JSON.stringify(args.claim)])},read:false,step:"publish",occurrence:0});const compiled=execute({name:"bash",arguments:{command:command("jev",["compile",published.data.claim_id,args.previous])},read:false,step:"compile",occurrence:0});return {report:compiled.data};}`
	childSource := `js:function(context,args){if(!args)return {defer:"missing arguments",parameters:"claim"};const published=execute({name:"bash",arguments:{command:command("jev",["claim",JSON.stringify(args.claim)])},read:false,step:"publish",occurrence:0});return {report:published.data};}`
	parent := Reflex{APIVersion: 2, When: "Publish a requested judgment and compile its replacement", Decide: "Publish current arguments, compile with host evidence, and report the returned IDs.", Observe: parentSource, Steps: map[string]StepDefinition{
		"publish": {Contract: "jev-library", Count: 1}, "compile": {Contract: "jev-library", Count: 1},
	}, Parameters: json.RawMessage(`{"type":"object","required":["claim","previous"],"properties":{"claim":{"type":"object"},"previous":{"type":"string"}},"additionalProperties":false}`), arguments: map[string]any{"claim": c, "previous": "recorded-parent"}}
	// Recorded native results are replayed during qualification. Compilation
	// cannot dispatch these effects, and no bootstrap source bypass is installed.
	rows := []map[string]any{{"role": "user", "text": "Publish the requested judgment and compile a replacement for recorded-parent."}}
	for i, args := range [][]string{{"claim", jsonText(c)}, {"compile", cid, "recorded-parent"}} {
		callID := []string{"publish", "compile"}[i]
		result := []string{jsonText(map[string]string{"claim_id": cid}), `{"reflex_ids":["recorded-replacement"]}`}[i]
		rows = append(rows, map[string]any{"role": "assistant", "calls": []map[string]any{{"id": callID, "name": "bash", "arguments": map[string]any{"command": map[string]any{"name": "jev", "argv": args}}}}}, map[string]any{"role": "tool", "call_id": callID, "text": result})
	}
	state := json.RawMessage(jsonText(map[string]any{"messages": rows}))
	caps, err := e.capabilities(cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.qualify(t.Context(), &parent, caps, state); err != nil {
		t.Fatal(err)
	}
	plan := &compilation{claims: map[string]Claim{cid: c}, ids: []string{cid}, capabilities: caps}
	if err := e.publishReflex(plan, &parent); err != nil {
		t.Fatal(err)
	}
	parentID := "r" + digest(parent)[:16]
	rounds := 0
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		switch req.Purpose {
		case "parameters":
			return reply(provider.TextMessage("assistant", jsonText(map[string]any{"claim": c, "previous": parentID}))), nil
		case "compilation":
			rounds++
			if rounds > 1 {
				return nil, errors.New("replacement failed ordinary validation")
			}
			return reply(provider.TextMessage("assistant", jsonText(map[string]any{"api_version": 2, "steps": map[string]StepDefinition{"publish": {Contract: "jev-library", Count: 1}}, "parameters_schema": json.RawMessage(`{"type":"object","required":["claim"],"properties":{"claim":{"type":"object"}},"additionalProperties":false}`), "observe": childSource, "arguments": map[string]any{"claim": c}}))), nil
		default:
			return nil, errors.New("unexpected model invocation")
		}
	})
	cfg.SessionID, cfg.TurnID = "ordinary-learning", "current"
	cfg.Messages = []*aop.Message{provider.TextMessage("user", "Publish this judgment: "+jsonText(c)+" and compile a replacement for "+parentID)}
	ctx := agent.ContextWithToolAgentConfig(t.Context(), cfg)
	receipt, err := e.beforeModel(ctx, hooks.ContextEvent{SessionID: cfg.SessionID, TurnID: cfg.TurnID, Messages: cfg.Messages})
	if err != nil || !strings.Contains(jsonText(receipt), "REPORT:") {
		t.Fatalf("ordinary self-compilation failed: %v %s", err, jsonText(receipt))
	}
	lib := e.snapshot()
	if rounds != 1 || len(lib.Reflexes) != 1 || len(lib.Claims) != 1 {
		t.Fatalf("unexpected self-compilation: rounds=%d library=%+v", rounds, lib)
	}
	if _, exists := lib.Reflexes[parentID]; exists {
		t.Fatal("ordinary Reflex did not replace itself")
	}
	for _, r := range lib.Reflexes {
		if r.Observe != childSource || !e.qualified(r) || r.Proof.Replayed != 1 {
			t.Fatal("replacement bypassed ordinary recorded replay and qualification")
		}
	}
	// Both completed effects remain available at the same host boundary, once.
	interaction := e.interaction(hooks.ContextEvent{SessionID: cfg.SessionID, TurnID: cfg.TurnID, Messages: cfg.Messages})
	if len(interaction) != 5 {
		t.Fatalf("completed evidence was lost or duplicated: %d messages", len(interaction))
	}
}

func TestClaimCommandIsOrdinaryDeduplicatedPublication(t *testing.T) {
	e := New(Config{Directory: t.TempDir()})
	c := Claim{Type: jevapi.ClaimChoice, Context: "Choose current progress.", Options: []string{"inspect", "defer"}}
	var output bytes.Buffer
	ex := &coretool.Execution{Args: []string{"claim", jsonText(c)}, Stdout: &output}
	if _, err := e.runLibraryCommand(t.Context(), ex); err != nil {
		t.Fatal(err)
	}
	var first struct {
		ID string `json:"claim_id"`
	}
	if json.Unmarshal(output.Bytes(), &first) != nil || first.ID == "" {
		t.Fatal("missing content ID")
	}
	before := digest(e.snapshot())
	output.Reset()
	if _, err := e.runLibraryCommand(t.Context(), ex); err != nil || before != digest(e.snapshot()) {
		t.Fatal("duplicate changed the library", err)
	}
	loaded := New(Config{Directory: e.config.Directory})
	if err := loaded.loadLibrary(); err != nil {
		t.Fatal(err)
	}
	view := loaded.snapshot()
	record := view.Claims[first.ID]
	record.Options[0] = "mutated"
	if loaded.snapshot().Claims[first.ID].Options[0] != "inspect" {
		t.Fatal("mutable snapshot")
	}
	contract := libraryContract()
	for _, tc := range []struct {
		args   []string
		access coretool.NativeAccess
	}{
		{[]string{"jev", "status"}, coretool.NativeRead},
		{[]string{"jev", "claim", jsonText(c)}, coretool.NativeEffect},
		{[]string{"jev", "compile", first.ID}, coretool.NativeEffect},
	} {
		access, err := contract.Classify(coretool.NativeCall{Name: "bash", Argv: tc.args})
		if access != tc.access || err != nil {
			t.Fatal("native operation classification", err)
		}
	}
}

func TestEmptyLibraryCompilesAtFirstReadyBoundary(t *testing.T) {
	checks := 0
	client := fakeJEV(t, func(req inferenceRequest) map[string]inferenceAnswer {
		if _, ok := req.Questions["compile"]; ok {
			checks++
		}
		return declarationAnswers(req, true)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if provider.MessageText(req.Messages[0]) == claimPrompt {
			return reply(provider.TextMessage("assistant", `{"claims":[{"type":"noul","context":"Report the current user's literal text when requested."}]}`)), nil
		}
		return reply(provider.TextMessage("assistant", `{"api_version":2,"steps":{},"observe":"js:function(context){return {report:context.user};}"}`)), nil
	})
	job := declaration{cfg: cfg, task: "first", state: json.RawMessage(`{"messages":[{"role":"user","text":"Report my literal text"}]}`), focus: []string{"Report current literal text"}}
	if err := e.declare(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if checks == 0 || len(e.snapshot().Claims) != 1 || len(e.snapshot().Reflexes) != 1 {
		t.Fatalf("first evidence-ready boundary did not bootstrap: checks=%d library=%+v", checks, e.snapshot())
	}
}

func TestOrdinaryReflexReplacementPreservesClaimAndRollsBack(t *testing.T) {
	e := testLaboratory(t)
	c := Claim{Type: jevapi.ClaimNoul, Context: "Add the requested items and report current evidence."}
	ids, err := e.publishClaims(t.Context(), []Claim{c}, "source")
	cid := ""
	if err == nil {
		cid = ids[0]
	}
	if err != nil {
		t.Fatal(err)
	}
	caps := observationCapabilities("bash")
	previous := qualifiedLaboratory(t, e, caps)
	plan := &compilation{claims: map[string]Claim{cid: c}, ids: []string{cid}, capabilities: caps}
	if err = e.publishReflex(plan, &previous); err != nil {
		t.Fatal(err)
	}
	oldID := "r" + digest(previous)[:16]
	draft := previous
	draft.Observe += " "
	draft.Proof = nil
	plan.job.repair = oldID
	if err = e.publishReflex(plan, &draft); err == nil || len(e.snapshot().Reflexes) != 1 {
		t.Fatal("unqualified replacement removed original")
	}
	if err = qualifyIndependent(e, t.Context(), &draft, caps); err != nil {
		t.Fatal(err)
	}
	before := digest(e.snapshot())
	// Exercise durable publication failure through the same replacement path.
	dir := e.config.Directory
	e.config.Directory = filepath.Join(dir, "missing")
	if err = e.publishReflex(plan, &draft); err == nil || before != digest(e.snapshot()) {
		t.Fatal("failed publication changed active source")
	}
	e.config.Directory = dir
	if err = e.publishReflex(plan, &draft); err != nil {
		t.Fatal(err)
	}
	if _, exists := e.snapshot().Reflexes[oldID]; exists {
		t.Fatal("previous source was not replaced")
	}
	newID := "r" + digest(draft)[:16]
	if !e.retireReflex(t.Context(), newID, errors.New("retire ordinary source")) || e.snapshot().Claims[cid].Context != c.Context {
		t.Fatal("Reflex lifecycle consumed its Claim")
	}
	// No compilation command may accept caller-authored native evidence.
	var output bytes.Buffer
	ctx := agent.ContextWithToolAgentConfig(t.Context(), agent.Config{})
	if _, err = e.runLibraryCommand(ctx, &coretool.Execution{Args: []string{"compile", cid}, Stdout: &output}); err == nil {
		t.Fatal("compile accepted missing host evidence")
	}
}

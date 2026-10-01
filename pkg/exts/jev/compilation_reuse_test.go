package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestExistingClaimReconsidersCompileWithoutRegenerating(t *testing.T) {
	var id string
	groups, compiles := 0, 0
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if _, ok := req.Questions["claim0"]; ok {
			var state map[string]json.RawMessage
			_ = json.Unmarshal(req.State, &state)
			if !strings.Contains(string(state["capabilities"]), "advance") {
				t.Error("discovery cannot see the registered capability")
			}
			return map[string]jevapi.Answer{"claim0": answer(id)}
		}
		var state map[string]json.RawMessage
		_ = json.Unmarshal(req.State, &state)
		if state["reflex"] != nil {
			return declarationAnswers(req, true)
		}
		groups++
		if !strings.Contains(string(req.State), "current-goal") {
			t.Error("compile judgment lost the current interaction")
		}
		return declarationAnswers(req, groups > 1)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "advance", Run: func(context.Context, *coretool.Execution) (any, error) { return "ok", nil },
	})
	var claims []Claim
	_ = json.Unmarshal([]byte(fixtureClaim), &claims)
	id = "c" + digest(claims[0])[:16]
	e.library.Claims[id] = claimRecord{Claim: claims[0], Task: "original-task", Consumed: true}
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if provider.MessageText(req.Messages[0]) != compilePrompt {
			t.Fatal("matching a Claim regenerated it")
		}
		compiles++
		return reply(provider.TextMessage("assistant", fixtureReflex)), nil
	})
	state, _ := json.Marshal(map[string]string{"goal": "current-goal"})
	job := declaration{cfg: cfg, task: "current-task", final: true, state: state, focus: []string{"Select the current operation"}}
	for i := 0; i < 3; i++ {
		if err := e.declare(t.Context(), job); err != nil {
			t.Fatal(err)
		}
		if i == 0 && len(e.snapshot().Reflexes) != 0 {
			t.Fatal("ignored JEV's compile defer")
		}
	}
	lib := e.snapshot()
	if groups != 2 || compiles != 1 || len(lib.Claims) != 1 || len(lib.Reflexes) != 1 || !lib.Claims[id].Consumed || lib.Claims[id].Task != "original-task" {
		t.Fatalf("groups=%d compiles=%d library=%+v", groups, compiles, lib)
	}
}

func TestCompileFailureDoesNotMarkGroupComplete(t *testing.T) {
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer { return declarationAnswers(req, true) })
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "advance", Run: func(context.Context, *coretool.Execution) (any, error) { return "ok", nil },
	})
	var claims []Claim
	_ = json.Unmarshal([]byte(fixtureClaim), &claims)
	id := "c" + digest(claims[0])[:16]
	e.library.Claims[id] = claimRecord{Claim: claims[0]}
	// A legacy attempt marker without a published scene must not survive load.
	e.library.Compiled[digest(map[string]Claim{id: claims[0]})] = true
	if err := e.saveLibrary(); err != nil {
		t.Fatal(err)
	}
	if err := e.loadLibrary(); err != nil || len(e.snapshot().Compiled) != 0 {
		t.Fatalf("legacy failed generation remained complete: %v", err)
	}
	calls := 0
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("generation unavailable")
		}
		if calls == 2 {
			return reply(provider.TextMessage("assistant", "null")), nil
		}
		return reply(provider.TextMessage("assistant", fixtureReflex)), nil
	})
	for i := 0; i < 4; i++ {
		err := e.compile(t.Context(), declaration{cfg: cfg}, id)
		if (err != nil) != (i == 0) {
			t.Fatalf("attempt=%d error=%v", i, err)
		}
		lib := e.snapshot()
		if i < 2 && (len(lib.Compiled) != 0 || len(lib.Reflexes) != 0) {
			t.Fatal("unsuccessful generation permanently marked the group complete")
		}
	}
	if calls != 3 || len(e.snapshot().Compiled) != 1 || len(e.snapshot().Reflexes) != 1 {
		t.Fatalf("generation calls=%d library=%+v", calls, e.snapshot())
	}
	restored := New(Config{Directory: e.config.Directory})
	if err := restored.loadLibrary(); err != nil || len(restored.snapshot().Compiled) != 1 || len(restored.snapshot().Reflexes) != 1 {
		t.Fatalf("durable completion failed: %v", err)
	}
}

func TestIdleAutoPreservesOrdinaryModelPrompt(t *testing.T) {
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		out := map[string]jevapi.Answer{}
		for id := range req.Questions {
			out[id] = answer(Defer)
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	calls := 0
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		calls++
		if len(req.Messages) != 2 || provider.MessageText(req.Messages[0]) != cfg.SystemPrompt || provider.MessageText(req.Messages[1]) != "Answer directly" {
			t.Error("idle acceleration expanded or rewrote the model context")
		}
		return reply(provider.TextMessage("assistant", "done")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Answer directly")); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if calls != 1 {
		t.Fatalf("model calls=%d", calls)
	}
	received := receipt([]string{"Executed a native operation"}, "evidence.jsonl", "REPORT")
	if len(received) != 1 || !strings.Contains(provider.MessageText(received[0]), Prompt) || len(receipt(nil, "", "")) != 0 {
		t.Fatal("controller guidance must accompany actual handoff evidence")
	}
}

func TestSceneReviewRejectsTaskSpecificDraftBeforePublication(t *testing.T) {
	reviews, generations := 0, 0
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		var state struct {
			Reflex *Reflex `json:"reflex"`
		}
		_ = json.Unmarshal(req.State, &state)
		if state.Reflex != nil {
			if _, diagnostic := req.Questions["defect"]; diagnostic {
				return map[string]jevapi.Answer{"defect": answer("scope")}
			}
			reviews++
			if strings.Contains(state.Reflex.Observe, "RememberedTarget") {
				return map[string]jevapi.Answer{"compile": answer(Defer)}
			}
			return declarationAnswers(req, true)
		}
		return declarationAnswers(req, true)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto", DeclarationEffort: "none"}, client, coretool.Command{Name: "advance", Run: func(context.Context, *coretool.Execution) (any, error) { return "unused", nil }})
	var claims []Claim
	_ = json.Unmarshal([]byte(fixtureClaim), &claims)
	id := "c" + digest(claims[0])[:16]
	e.library.Claims[id] = claimRecord{Claim: claims[0]}
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		generations++
		if req.ReasoningEffort != "none" {
			t.Error("background generation lost its optional inference setting")
		}
		if generations == 1 {
			draft := strings.Replace(fixtureReflex, "js:(() => {", `js:(() => { const remembered = "RememberedTarget";`, 1)
			return reply(provider.TextMessage("assistant", draft)), nil
		}
		if !strings.Contains(provider.MessageText(req.Messages[1]), "scene review rejected (scope)") || !strings.Contains(provider.MessageText(req.Messages[1]), "Task-specific targets") || len(e.snapshot().Reflexes) != 0 {
			t.Error("invalid draft was published or correction lacks its diagnostic")
		}
		return reply(provider.TextMessage("assistant", fixtureReflex)), nil
	})
	if err := e.compile(t.Context(), declaration{cfg: cfg}, id); err != nil {
		t.Fatal(err)
	}
	if reviews != 2 || generations != 2 || len(e.snapshot().Reflexes) != 1 {
		t.Fatalf("reviews=%d generations=%d library=%+v", reviews, generations, e.snapshot())
	}
}

func TestCompileCorrectsMalformedOutputWithinDraftBudget(t *testing.T) {
	for _, mode := range []string{"corrected", "still malformed", "provider failure"} {
		t.Run(mode, func(t *testing.T) {
			client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer { return declarationAnswers(req, true) })
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
			var claims []Claim
			_ = json.Unmarshal([]byte(fixtureClaim), &claims)
			id := "c" + digest(claims[0])[:16]
			e.library.Claims[id] = claimRecord{Claim: claims[0]}
			generations := 0
			cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				generations++
				if mode == "provider failure" {
					return nil, errors.New("provider unavailable")
				}
				if generations > 1 && (!strings.Contains(provider.MessageText(req.Messages[1]), "Compilation failed:") || !strings.Contains(provider.MessageText(req.Messages[1]), "Do not encode it in JSON.") || len(e.snapshot().Reflexes) != 0) {
					t.Error("correction lost format feedback or malformed program was published")
				}
				if mode == "corrected" && generations > 1 {
					return reply(provider.TextMessage("assistant", fixtureReflex)), nil
				}
				return reply(provider.TextMessage("assistant", `{"when":"Current capability","decide":"Choose native operations","code":"js:({})"}`)), nil
			})
			err := e.compile(t.Context(), declaration{cfg: cfg}, id)
			want := 3
			if mode == "corrected" {
				want = 2
				if err != nil || len(e.snapshot().Reflexes) != 1 {
					t.Fatalf("corrected output was not published: %v", err)
				}
			} else {
				if mode == "provider failure" {
					want = 1
				}
				if err == nil || len(e.snapshot().Reflexes) != 0 {
					t.Fatal("failed output was accepted")
				}
			}
			if generations != want {
				t.Fatalf("generations=%d want=%d", generations, want)
			}
		})
	}
}

func TestRawObserveReusesAdmittedJudgmentsAndBindsActualResults(t *testing.T) {
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		out := declarationAnswers(req, true)
		if _, exists := req.Questions["ownership"]; exists {
			out["ownership"] = answer("partial")
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	var claims []Claim
	_ = json.Unmarshal([]byte(fixtureClaim), &claims)
	id := "c" + digest(claims[0])[:16]
	e.library.Claims[id] = claimRecord{Claim: claims[0]}
	generated := 0
	code := `js:(() => { const latest = history.length ? history[history.length-1] : null; const items = latest ? latest.data.items : [];
return {state:{items:items},candidates:choices(items.map(item => bind(latest.name,{command:'advance ' + quote(item.id)},false)))}; })()`
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		generated++
		return reply(provider.TextMessage("assistant", code)), nil
	})
	state := json.RawMessage(`{"messages":[{"role":"user","text":"Select from current alternatives"},{"role":"assistant","calls":[{"id":"actual","name":"bash","arguments":{"command":"inspect"}}]},{"role":"tool","call_id":"actual","text":"{\"items\":[{\"id\":\"one\"},{\"id\":\"two\"}]}"}]}`)
	if err := e.compile(t.Context(), declaration{cfg: cfg, state: state}, id); err != nil {
		t.Fatal(err)
	}
	if generated != 1 || len(e.snapshot().Reflexes) != 1 {
		t.Fatalf("generation/publication changed: generated=%d scenes=%d", generated, len(e.snapshot().Reflexes))
	}
	capabilities, _ := e.capabilities(cfg)
	for _, r := range e.snapshot().Reflexes {
		if !strings.Contains(r.When, claims[0].When) || !strings.Contains(r.Decide, claims[0].Question) || r.Observe != code {
			t.Fatal("raw compiler output lost admitted semantics or executable source")
		}
		_, bindings, err := r.observe(t.Context(), state, capabilities)
		if err != nil || len(bindings) != 2 || !strings.Contains(string(bindings["c1"].Arguments), "two") {
			t.Fatalf("published program did not bind actual native results: bindings=%v error=%v", bindings, err)
		}
	}
}

func TestBoundaryCoverageRejectsDraftDespiteGlobalAcceptance(t *testing.T) {
	const incomplete = `js:({state:{},candidates:choices([])})`
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		out := declarationAnswers(req, true)
		if _, exists := req.Questions["ownership"]; exists {
			out["ownership"] = answer("partial")
		}
		var state struct{ Reflex *Reflex }
		_ = json.Unmarshal(req.State, &state)
		if state.Reflex != nil && state.Reflex.Observe == incomplete {
			if _, exists := req.Questions["coverage0"]; !exists {
				t.Error("known next operation lacked its boundary coverage judgment")
			}
			out["coverage0"] = answer(Defer)
		}
		return out
	})
	dispatched := 0
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "advance", Run: func(context.Context, *coretool.Execution) (any, error) {
		dispatched++
		return "unused", nil
	}})
	var claims []Claim
	_ = json.Unmarshal([]byte(fixtureClaim), &claims)
	id := "c" + digest(claims[0])[:16]
	e.library.Claims[id] = claimRecord{Claim: claims[0]}
	generated := 0
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		generated++
		if generated == 1 {
			return reply(provider.TextMessage("assistant", incomplete)), nil
		}
		if !strings.Contains(provider.MessageText(req.Messages[1]), "missing next progress") || len(e.snapshot().Reflexes) != 0 {
			t.Error("uncovered program published or specific boundary feedback lost")
		}
		return reply(provider.TextMessage("assistant", fixtureReflex)), nil
	})
	state := json.RawMessage(`{"messages":[{"role":"user","text":"Advance four steps"},{"role":"assistant","calls":[{"id":"known","name":"bash","arguments":{"command":"advance 0"}}]},{"role":"tool","call_id":"known","text":"step=1"}]}`)
	if err := e.compile(t.Context(), declaration{cfg: cfg, state: state}, id); err != nil {
		t.Fatal(err)
	}
	if generated != 2 || dispatched != 0 || len(e.snapshot().Reflexes) != 1 {
		t.Fatalf("generation=%d native dispatch=%d scenes=%d", generated, dispatched, len(e.snapshot().Reflexes))
	}
}

func TestMatchedSceneRepairsMissingEntryFromOrdinaryEvidence(t *testing.T) {
	var matched, repairs, generated int
	var repairID string
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		out := declarationAnswers(req, true)
		for id := range req.Questions {
			if strings.HasPrefix(id, "claim") {
				out[id] = answer(repairID)
				matched++
			}
		}
		var state struct {
			Repair  string          `json:"repair"`
			Handoff json.RawMessage `json:"handoff"`
		}
		_ = json.Unmarshal(req.State, &state)
		if state.Repair != "" {
			repairs++
			if state.Repair != repairID || !strings.Contains(string(state.Handoff), "entry missing") {
				t.Error("repair lost its matched scene or original entry boundary")
			}
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "advance", Run: func(context.Context, *coretool.Execution) (any, error) { return "unused", nil }})
	var claims []Claim
	_ = json.Unmarshal([]byte(fixtureClaim), &claims)
	cid := "c" + digest(claims[0])[:16]
	e.library.Claims[cid] = claimRecord{Claim: claims[0]}
	old := Reflex{When: "Advancement with an existing handle", Decide: "Use current state", Observe: `js:({state:{},candidates:{}})`}
	repairID = "r" + digest(old)[:16]
	e.library.Reflexes[repairID] = reflexRecord{Reflex: old, Claims: []string{cid}}
	e.library.Compiled[digest(map[string]Claim{cid: claims[0]})] = true
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		generated++
		if provider.MessageText(req.Messages[0]) != compilePrompt || !strings.Contains(provider.MessageText(req.Messages[1]), "recorded handoff BEFORE") {
			t.Error("ordinary supplementation started new discovery instead of scene repair")
		}
		return reply(provider.TextMessage("assistant", fixtureReflex)), nil
	})
	handoff, _ := json.Marshal(map[string]any{"observations": map[string]string{repairID: "entry missing"}})
	job := declaration{cfg: cfg, task: "entry-gap", operational: true, final: true, focus: []string{`["bash",{"command":"advance 0"}]`},
		state:   json.RawMessage(`{"messages":[{"role":"user","text":"Advance four steps"},{"role":"assistant","calls":[{"id":"next","name":"bash","arguments":{"command":"advance 0"}}]}]}`),
		handoff: handoff}
	// A newer coalesced boundary has no selected Reflex either. It must not
	// erase the semantic match discovered while reviewing ordinary output.
	e.queued[job.task] = job
	if err := e.declare(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if matched != 1 || repairs != 1 || generated != 1 || len(e.snapshot().Reflexes) != 1 {
		t.Fatalf("matches=%d repairs=%d generations=%d scenes=%d", matched, repairs, generated, len(e.snapshot().Reflexes))
	}
	if _, exists := e.snapshot().Reflexes[repairID]; exists {
		t.Fatal("deficient entry scene was not replaced")
	}
}

func TestJEVDefersRepairBeforeAnyLLMGeneration(t *testing.T) {
	judgments, generated := 0, 0
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		judgments++
		if !strings.Contains(fmt.Sprint(req.Questions["compile"].Instructions), "recorded handoff BEFORE") || !strings.Contains(string(req.State), "redundant verification") {
			t.Error("repair judgment lost its specific pre-supplementation evidence")
		}
		out := declarationAnswers(req, true)
		out["compile"] = answer(Defer)
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	c := Claim{When: "Current native workflow", Question: "Which operation advances it?", Options: map[string]string{"operate": "Use current bindings", Defer: "Missing facts"}}
	cid := "c" + digest(c)[:16]
	r := Reflex{When: c.When, Decide: "Report actual evidence", Observe: `js:({state:{},candidates:{}})`}
	rid := "r" + digest(r)[:16]
	e.library.Claims[cid] = claimRecord{Claim: c}
	e.library.Reflexes[rid] = reflexRecord{Reflex: r, Claims: []string{cid}}
	before := digest(e.snapshot())
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		generated++
		return reply(provider.TextMessage("assistant", "null")), nil
	})
	job := declaration{cfg: cfg, repair: rid, state: json.RawMessage(`{"messages":[{"role":"user","text":"Get current result"}]}`), handoff: json.RawMessage(`{"observations":{"result":"complete; later read was redundant verification"},"candidates":{}}`)}
	if err := e.compile(t.Context(), job, cid); err != nil {
		t.Fatal(err)
	}
	if judgments != 1 || generated != 0 || digest(e.snapshot()) != before {
		t.Fatalf("JEV repair defer was bypassed: judgments=%d generated=%d", judgments, generated)
	}
}

func TestRepairProposalStillRequiresAdmissionAndNeverDispatchesTools(t *testing.T) {
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		out := declarationAnswers(req, true)
		var state struct {
			Reflex *Reflex `json:"reflex"`
			Repair string  `json:"repair"`
		}
		_ = json.Unmarshal(req.State, &state)
		if state.Repair != "" {
			if q, exists := req.Questions["compile"]; !exists || !strings.Contains(fmt.Sprint(q.Instructions), "recorded handoff BEFORE") {
				t.Error("repair necessity was not judged against the actual gap")
			}
		} else if _, diagnostic := req.Questions["defect"]; diagnostic {
			out["defect"] = answer("progress")
		} else if state.Reflex != nil {
			out["compile"] = answer(Defer)
		}
		return out
	})
	dispatched := 0
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "advance", Run: func(context.Context, *coretool.Execution) (any, error) {
		dispatched++
		return "unused", nil
	}})
	var claims []Claim
	_ = json.Unmarshal([]byte(fixtureClaim), &claims)
	cid := "c" + digest(claims[0])[:16]
	e.library.Claims[cid] = claimRecord{Claim: claims[0]}
	old := Reflex{When: "Current capability", Decide: "Use actual state", Observe: `js:({state:{},candidates:{}})`}
	rid := "r" + digest(old)[:16]
	e.library.Reflexes[rid] = reflexRecord{Reflex: old, Claims: []string{cid}}
	before := digest(e.snapshot())
	generated := 0
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		generated++
		if generated > 1 && !strings.Contains(provider.MessageText(req.Messages[1]), "scene review rejected (progress)") {
			t.Error("repair correction lost admission diagnostic")
		}
		return reply(provider.TextMessage("assistant", fixtureReflex)), nil
	})
	job := declaration{cfg: cfg, repair: rid, final: true, state: json.RawMessage(`{"messages":[{"role":"user","text":"Advance the current workflow"}]}`)}
	if err := e.compile(t.Context(), job, cid); err == nil || generated != 3 || dispatched != 0 || digest(e.snapshot()) != before {
		t.Fatalf("rejected repair changed execution/library: error=%v drafts=%d dispatched=%d", err, generated, dispatched)
	}
}

func TestWholeOwnershipRequiresEntryWhilePartialReadsRemainUseful(t *testing.T) {
	for _, ownership := range []string{"whole", "partial"} {
		t.Run(ownership, func(t *testing.T) {
			client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
				out := declarationAnswers(req, true)
				if _, exists := req.Questions["ownership"]; exists {
					out["ownership"] = answer(ownership)
				}
				return out
			})
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client,
				coretool.Command{Name: "start", Run: func(context.Context, *coretool.Execution) (any, error) { return "unused", nil }},
				coretool.Command{Name: "read", Run: func(context.Context, *coretool.Execution) (any, error) { return "unused", nil }})
			claim := Claim{When: "The user requests the resource capability", Question: "Which resource capability applies?", Options: map[string]string{"operate": "Use the known resource capability", Defer: "Missing capability"}}
			cid := "c" + digest(claim)[:16]
			e.library.Claims[cid] = claimRecord{Claim: claim}
			const partial = `js:(() => { const h = history.length ? history[history.length-1] : null; const handle = h && h.data && h.data.handle;
return {state:{handle:handle || null},candidates:choices(handle ? [bind('bash',{command:'read ' + quote(handle)},true)] : [])}; })()`
			const complete = `js:(() => { const h = history.length ? history[history.length-1] : null; const handle = h && h.data && h.data.handle;
return {state:{handle:handle || null},candidates:choices(handle ? [bind('bash',{command:'read ' + quote(handle)},true)] : [bind('bash',{command:'start'},false)])}; })()`
			generations := 0
			cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				generations++
				if generations == 1 {
					return reply(provider.TextMessage("assistant", partial)), nil
				}
				if !strings.Contains(provider.MessageText(req.Messages[1]), "user-only entry") || len(e.snapshot().Reflexes) != 0 {
					t.Error("entry defect lacked correction feedback or was published")
				}
				return reply(provider.TextMessage("assistant", complete)), nil
			})
			state := json.RawMessage(`{"messages":[{"role":"user","text":"Read the resource"},{"role":"assistant","calls":[{"id":"entry","name":"bash","arguments":{"command":"start"}}]},{"role":"tool","call_id":"entry","text":"{\"handle\":\"current\"}"}]}`)
			if err := e.compile(t.Context(), declaration{cfg: cfg, state: state}, cid); err != nil {
				t.Fatal(err)
			}
			want := 1
			if ownership == "whole" {
				want = 2
			}
			if generations != want || len(e.snapshot().Reflexes) != 1 {
				t.Fatalf("generations=%d expected=%d scenes=%d", generations, want, len(e.snapshot().Reflexes))
			}
		})
	}
}

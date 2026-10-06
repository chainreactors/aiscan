package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chainreactors/cyber/internal/jevwire"
	"reflect"
	"strings"
	"testing"
)

func TestObservationReplayPreservesSamplingAndInputIdentity(t *testing.T) {
	r := observationReflex(t, `js:({state:{omitted:omitted_evidence},candidates:{}})`)
	messages := []map[string]any{{"role": "user", "text": "Current task"}}
	for i := range 25 {
		messages = append(messages, map[string]any{"role": "tool", "call_id": fmt.Sprint(i), "text": "Actual result"})
	}
	for _, omitted := range []int{0, 3} {
		state, _ := json.Marshal(map[string]any{"messages": messages, "omitted_evidence": omitted})
		replay, err := newObservationReplay(&r, state, observationCapabilities())
		if err != nil {
			t.Fatal(err)
		}
		wantRecent := append([]int{1}, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26)
		if got := replay.boundaries(true); !reflect.DeepEqual(got, wantRecent) {
			t.Fatalf("verification sample changed: %v", got)
		}
		if err := replay.verify(t.Context()); err != nil {
			t.Fatal(err)
		}
		before := len(replay.cache)
		witnesses, err := replay.witnesses(t.Context())
		if err != nil || len(witnesses) != 16 || witnesses[0]["boundary"] != 1 || witnesses[15]["boundary"] != 16 {
			t.Fatalf("witness sample changed: witnesses=%v error=%v", witnesses, err)
		}
		wantAdditional := 10
		if omitted != 0 {
			wantAdditional = 16
		}
		if len(replay.cache)-before != wantAdditional {
			t.Fatalf("distinct observation inputs were confused: before=%d after=%d", before, len(replay.cache))
		}
		if !strings.Contains(string(witnesses[0]["state"].(json.RawMessage)), `"omitted":0`) {
			t.Fatal("witness inherited verification-only omitted evidence")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, _, err := replay.evaluate(ctx, replay.input(1, true), true); err == nil {
			t.Fatal("cached observation bypassed cancellation")
		}
	}
}

func TestCompilerAllowsHonestPartialSceneWithoutEntryBinding(t *testing.T) {
	r := Reflex{When: "Current native resource workflow", Decide: "Inspect known resources, defer until a handle is available", Observe: normalizeFixture(`js:(() => {
const recent = history.length ? history[history.length-1] : null;
const handle = recent && recent.data && recent.data.handle;
return {state:{handle:handle || null},candidates:choices(handle ? [bind(tools[0].name,{handle:handle},true)] : [])};
})()`)}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"messages":[{"role":"user","text":"Inspect a resource"},{"role":"assistant","calls":[{"id":"open","name":"native","arguments":{}}]},{"role":"tool","call_id":"open","text":"{\"handle\":\"current\"}"}]}`)
	if err := verifyObserve(t.Context(), &r, input, map[string]any{"tools": []any{map[string]any{"name": "native"}}, "commands": []any{}}); err != nil {
		t.Fatalf("useful partial scene cannot reach semantic review: %v", err)
	}
}

func TestFreshnessReviewRetainsConcreteRejectionWithEitherGlobalVerdict(t *testing.T) {
	for _, verdict := range []string{"compile", Defer} {
		t.Run(verdict, func(t *testing.T) {
			requests, checked := 0, false
			client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
				requests++
				out := declarationAnswers(req, true)
				if _, exists := req.Questions["coverage_freshness"]; exists {
					checked = true
					out["compile"] = answer(verdict)
					out["coverage_freshness"] = answer(Defer)
				}
				return out
			})
			e, _, _ := testInstallation(t, Config{Mode: "auto"}, client)
			r := observationReflex(t, `js:function(context,args){const r=execute({name:'native',arguments:{},read:false});return {report:r.data};}`)
			caps := observationCapabilities("native")
			state := json.RawMessage(`{"messages":[{"role":"user","text":"Inspect fresh state"}]}`)
			witnesses, err := observationWitnesses(t.Context(), &r, state, caps)
			if err != nil {
				t.Fatal(err)
			}
			err = e.reviewReflex(t.Context(), &r, &compilation{capabilities: caps}, witnesses)
			if !checked || requests != 1 || err == nil {
				t.Fatalf("concrete rejection lost or redundant diagnosis requested: checked=%t requests=%d err=%v", checked, requests, err)
			}
			diagnostic := compilerDiagnostic(err)
			if diagnostic.Code != "native_access_invalid" || diagnostic.Stage != "semantic" || !strings.Contains(diagnostic.Action, "helper") {
				t.Fatalf("concrete structured repair lost: %+v", diagnostic)
			}
		})
	}
}

func TestReviewRetainsBoundaryRejectionWithGlobalDefer(t *testing.T) {
	requests := 0
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		requests++
		out := declarationAnswers(req, true)
		out["compile"], out["coverage0"] = answer(Defer), answer(Defer)
		return out
	})
	e, _, _ := testInstallation(t, Config{Mode: "auto"}, client)
	r := observationReflex(t, `js:function(){return{defer:'missing next operation'};}`)
	boundary := map[string]any{"boundary": 2, "state": map[string]any{"current": "actual"}, "candidates": map[string]binding{}, "next_calls": []any{map[string]any{"name": "native", "arguments": map[string]any{"operation": "required"}}}}
	err := e.reviewReflex(t.Context(), &r, &compilation{}, []map[string]any{boundary})
	if err == nil {
		t.Fatal("rejected boundary accepted")
	}
	diagnostic := compilerDiagnostic(err)
	if requests != 1 || diagnostic.Code != "semantic_validation_failed" || !strings.Contains(jsonText(diagnostic.Expected), "required") {
		t.Fatalf("concrete boundary lost: requests=%d diagnostic=%+v", requests, diagnostic)
	}
}

func TestResultReviewDistinguishesCompletionFromReadClassification(t *testing.T) {
	requests := 0
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		requests++
		out := declarationAnswers(req, true)
		out["compile"], out["coverage_result"] = answer(Defer), answer(Defer)
		out["coverage_freshness"] = answer("compile")
		return out
	})
	e, _, _ := testInstallation(t, Config{Mode: "auto"}, client)
	r := observationReflex(t, `js:function(){const r=execute({name:'native',arguments:{},read:true});return{report:'done'};}`)
	witness := map[string]any{"boundary": 1, "report": "done", "candidates": map[string]binding{"read": {Name: "native", Arguments: json.RawMessage(`{}`), Read: true}}}
	err := e.reviewReflex(t.Context(), &r, &compilation{capabilities: observationCapabilities("native")}, []map[string]any{witness})
	diagnostic := compilerDiagnostic(err)
	if requests != 1 || diagnostic.Code != "semantic_validation_failed" || diagnostic.Stage != "completion" || !strings.Contains(jsonText(diagnostic.Actual), "done") {
		t.Fatalf("completion defect was misclassified or its output evidence lost: requests=%d diagnostic=%+v", requests, diagnostic)
	}
}

func TestReviewSharesBindingsWithoutLosingActualBoundaryEvidence(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"program": strings.Repeat("native program ", 1000), "version": json.Number("9007199254740993")})
	candidate := binding{Name: "ordinary", Arguments: args, Read: true}
	var witnesses []map[string]any
	for i := 0; i < 8; i++ {
		witnesses = append(witnesses, map[string]any{"boundary": i, "latest": fmt.Sprintf("actual result %d", i), "candidates": map[string]binding{"current": candidate}, "next_calls": []string{fmt.Sprintf("next %d", i)}})
	}
	raw, _ := json.Marshal(witnesses)
	rows, bindings := compactWitnesses(witnesses)
	compact, _ := json.Marshal(map[string]any{"evaluations": rows, "bindings": bindings})
	if len(raw) <= 64<<10 || len(compact) >= 32<<10 || len(rows) != 8 || len(bindings) != 1 {
		t.Fatalf("duplicate readers exceed review budget: raw=%d compact=%d rows=%d bindings=%d", len(raw), len(compact), len(rows), len(bindings))
	}
	for i, row := range rows {
		ref := row["candidates"].(map[string]string)["current"]
		if canonicalBinding := bindings[ref]; canonicalBinding.Name != candidate.Name || string(canonicalBinding.Arguments) != string(args) || !canonicalBinding.Read || row["latest"] != witnesses[i]["latest"] || row["next_calls"] == nil {
			t.Fatal("compaction changed an actual binding or its boundary evidence")
		}
		if _, ok := witnesses[i]["candidates"].(map[string]binding); !ok {
			t.Fatal("compaction mutated original evidence")
		}
	}
}

func TestCompilerChecksEarlierBoundariesAndNativeRequiredArguments(t *testing.T) {
	caps := observationCapabilities("native")
	caps["tools"].([]any)[0].(map[string]any)["input_schema"] = map[string]any{"type": "object", "required": []any{"selector"}, "properties": map[string]any{"selector": map[string]any{"type": "string", "minLength": 1}}}
	input := json.RawMessage(`{"messages":[{"role":"user","text":"Select something"},{"role":"assistant","calls":[{"id":"read","name":"native","arguments":{"selector":"current"}}]},{"role":"tool","call_id":"read","text":"{\"complete\":true}"}]}`)
	for _, code := range []string{
		`js:function(context,args){return {report:history[0].data};}`,
		`js:function(context,args){execute({name:'native',arguments:{selector:context.user.split('Select ')[2]},read:false});return {report:'done'};}`,
	} {
		r := observationReflex(t, code)
		if err := verifyObserve(t.Context(), &r, input, caps); err == nil {
			t.Fatal("invalid earlier boundary or native arguments were admitted")
		}
	}
}

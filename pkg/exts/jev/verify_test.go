package jev

import (
	"context"
	"encoding/json"
	"fmt"
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
		if string(witnesses[0]["state"].(json.RawMessage)) != `{"omitted":0}` {
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
	r := Reflex{When: "Current native resource workflow", Decide: "Inspect known resources, defer until a handle is available", Observe: `js:(() => {
const recent = history.length ? history[history.length-1] : null;
const handle = recent && recent.data && recent.data.handle;
return {state:{handle:handle || null},candidates:choices(handle ? [bind(tools[0].name,{handle:handle},true)] : [])};
})()`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"messages":[{"role":"user","text":"Inspect a resource"},{"role":"assistant","calls":[{"id":"open","name":"native","arguments":{}}]},{"role":"tool","call_id":"open","text":"{\"handle\":\"current\"}"}]}`)
	if err := verifyObserve(t.Context(), &r, input, map[string]any{"tools": []any{map[string]any{"name": "native"}}, "commands": []any{}}); err != nil {
		t.Fatalf("useful partial scene cannot reach semantic review: %v", err)
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

func TestCompilationRejectsCopiedOpaqueIdentifiersAndEscapedResources(t *testing.T) {
	capabilities := map[string]any{"tools": []any{map[string]any{"name": "native"}}, "commands": []any{}}
	input := json.RawMessage(`{"messages":[{"role":"user","text":"Use http://127.0.0.1:32997/current"},{"role":"assistant","calls":[{"id":"read","name":"native","arguments":{}}]},{"role":"tool","call_id":"read","text":"{\"id\":\"node-91e567acf604ae29\"}"}]}`)
	for _, code := range []string{
		`js:({state:{},candidates:choices([bind('native',{id:'node-91e567acf604ae29'},false)])})`,
		`js:({state:{pattern:/http:\/\/127\.0\.0\.1:32997\/current/},candidates:choices([])})`,
	} {
		r := Reflex{When: "Current capability", Decide: "Use actual alternatives", Observe: code}
		if err := r.validate(); err != nil {
			t.Fatal(err)
		}
		if err := verifyObserve(t.Context(), &r, input, capabilities); err == nil {
			t.Fatal("copied runtime identifier/resource passed compilation checks")
		}
	}
}

func TestCompilerRejectsGoalSelectionFromUnrequestedQuotedExample(t *testing.T) {
	input := json.RawMessage(`{"messages":[{"role":"user","text":"Select second from the current alternatives"},{"role":"assistant","calls":[{"id":"read","name":"native","arguments":{}}]},{"role":"tool","call_id":"read","text":"{\"items\":[{\"id\":\"first\"},{\"id\":\"second\"}]}"}]}`)
	capabilities := map[string]any{"tools": []any{map[string]any{"name": "native"}}, "commands": []any{}}
	for _, filter := range []bool{false, true} {
		code := `js:(() => { const latest = history.length ? history[history.length-1] : null; let items = latest ? latest.data.items : [];`
		if filter {
			code += `const target = (user.match(/select\s+(\S+)/i) || [])[1]; items = items.filter(item => item.id === target);`
		}
		code += `return {state:{items:items},candidates:choices(items.map(item => bind(tools[0].name,{id:item.id},false)))}; })()`
		r := Reflex{When: "Choose from current native alternatives", Decide: "JEV chooses the intended current binding", Observe: code}
		if err := r.validate(); err != nil {
			t.Fatal(err)
		}
		err := verifyObserve(t.Context(), &r, input, capabilities)
		if (err != nil) != filter {
			t.Fatalf("goal filter=%t verification error=%v", filter, err)
		}
	}
}

func TestCompilerChecksEarlierBoundariesAndGoalWording(t *testing.T) {
	capabilities := map[string]any{"tools": []any{map[string]any{"name": "native"}}, "commands": []any{}}
	input := json.RawMessage(`{"messages":[{"role":"user","text":"Select something"},{"role":"assistant","calls":[{"id":"read","name":"native","arguments":{}}]},{"role":"tool","call_id":"read","text":"{\"complete\":true}"}]}`)
	for _, code := range []string{
		`js:(() => { const result=history[0].data; return {state:result,candidates:choices([])}; })()`,
		`js:({state:{},candidates:choices([bind("native",{selector:user.split("Select ")[1]},false)])})`,
	} {
		r := Reflex{When: "Current scene", Decide: "Select current bindings", Observe: code}
		if err := r.validate(); err != nil {
			t.Fatal(err)
		}
		if err := verifyObserve(t.Context(), &r, input, capabilities); err == nil {
			t.Fatalf("unsafe earlier/goal-dependent branch passed publication checks: %s", code)
		}
	}
}

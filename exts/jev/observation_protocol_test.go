package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/dop251/goja"
	"mvdan.cc/sh/v3/shell"
)

func TestObserveJoinsNativeResultsAndEnumeratesOpaqueBindings(t *testing.T) {
	r := Reflex{When: "An operation can use the current result", Decide: "Choose under the current user constraint", Observe: `js:(() => {
const recent = history.length === 0 ? null : history[history.length - 1];
const items = recent?.data?.items ?? [];
return {state: {user: user, result: recent}, candidates: choices(items.map(item => bind(recent.name, {command: item.id}, false)))};
})()`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"messages":[{"role":"user","text":"New goal"},{"role":"assistant","calls":[{"id":"actual","name":"opaque_operation","arguments":{"command":"not a shell"}}]},{"role":"tool","name":"annotation-not-tool","call_id":"unrelated","text":"invented"},{"role":"tool","name":"jev-step","call_id":"actual","text":"Arbitrary envelope\n{\"items\":[{\"id\":\"one\"},{\"id\":\"two\"}]}"},{"role":"user","name":"jev","text":"not the real user"}]}`)
	state, choices, err := r.observe(t.Context(), input, map[string]any{"tools": []any{map[string]any{"name": "opaque_operation"}}, "commands": []any{}})
	if err != nil || len(choices) != 2 || !strings.Contains(string(state), "New goal") || strings.Contains(string(state), "invented") || strings.Contains(string(state), "not the real user") {
		t.Fatalf("state=%s choices=%v error=%v", state, choices, err)
	}
	for id, item := range choices {
		if item.Name != "opaque_operation" || !strings.Contains(string(item.Arguments), "command") {
			t.Fatalf("invalid joined binding %s: %+v", id, item)
		}
	}
}

func TestBackgroundUsesLatestBoundaryInsteadOfQueuedOldOutputs(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var calls atomic.Int64
	var received string
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		} else {
			received = string(req.State)
		}
		out := map[string]jevapi.Answer{}
		for key := range req.Questions {
			out[key] = answer(Defer)
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		t.Error("deferred background discovery generated a scene")
		return nil, nil
	})
	event := func(output string) hooks.ContextEvent {
		return hooks.ContextEvent{SessionID: "coalesce", TurnID: "turn", Messages: []*aop.Message{provider.TextMessage("user", "Current goal"), provider.TextMessage("assistant", output)}}
	}
	e.enqueue(cfg, event("First boundary"))
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	e.enqueue(cfg, event("Obsolete boundary"))
	e.enqueue(cfg, event("Latest actual evidence"))
	e.mu.Lock()
	pending := e.pending
	e.mu.Unlock()
	if pending != 2 || len(e.queue) != 1 {
		t.Fatalf("obsolete work was duplicated: pending=%d queued=%d", pending, len(e.queue))
	}
	once.Do(func() { close(release) })
	settle(t, e)
	if calls.Load() != 2 || !strings.Contains(received, "Latest actual evidence") || strings.Contains(received, "Obsolete boundary") {
		t.Fatalf("calls=%d final discovery=%s", calls.Load(), received)
	}
}

func TestJavaScriptObserveBindsOnlyCurrentNativeData(t *testing.T) {
	r := Reflex{When: "Operate on current data", Decide: "Choose a requested item", Observe: `js:(() => {
const recent = history.length ? history[history.length-1] : null;
const rows = recent ? recent.data.items : [];
return {state:{user:user, items:rows}, candidates:choices(rows.map(item => bind(tools[0].name, {value:item.id}, false)))};
})()`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"messages":[{"role":"user","text":"Select second"},{"role":"assistant","calls":[{"id":"read","name":"native","arguments":{}}]},{"role":"tool","call_id":"read","text":"Result\n{\"items\":[{\"id\":\"one\"},{\"id\":\"two\"}]}"}]}`)
	state, bindings, err := r.observe(t.Context(), input, map[string]any{"tools": []any{map[string]any{"name": "native"}}, "commands": []any{}})
	if err != nil || len(bindings) != 2 || !strings.Contains(string(state), "Select second") || string(bindings["c1"].Arguments) != `{"value":"two"}` {
		t.Fatalf("state=%s bindings=%v error=%v", state, bindings, err)
	}
}

func TestObserveSerializesReaderWithoutExecutingIt(t *testing.T) {
	// A reader's globals exist only at native execution. Serialization must be
	// pure and preserve both its source and runtime resource parameters.
	r := Reflex{When: "A resource needs inspection", Decide: "Read current state", Observe: `js:(() => {
const reader = function(resource) {
  return JSON.stringify({resource:resource, text:nativeState.text, items:nativeState.items});
};
const script = '(' + reader.toString() + ')(' + JSON.stringify(user) + ')';
return {state:{},candidates:choices([bind(tools[0].name,{program:script},true)])};
})()`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	resource := "当前资源 'quoted' \"double\"\nnext line"
	input, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "text": resource}}})
	_, candidates, err := r.observe(t.Context(), input, map[string]any{"tools": []any{map[string]any{"name": "ordinary_reader"}}, "commands": []any{}})
	if err != nil || len(candidates) != 1 || !candidates["c0"].Read {
		t.Fatalf("pure serialization failed: candidates=%v error=%v", candidates, err)
	}
	var args struct{ Program string }
	if err := json.Unmarshal(candidates["c0"].Arguments, &args); err != nil {
		t.Fatal(err)
	}
	native := goja.New()
	if _, err := native.RunString(`const nativeState = {text:"actual receipt",items:[{identifier:"fresh-address"}]};`); err != nil {
		t.Fatal(err)
	}
	result, err := native.RunString(args.Program)
	if err != nil {
		t.Fatalf("serialized native reader is invalid: %v", err)
	}
	var data struct {
		Resource, Text string
		Items          []struct{ Identifier string }
	}
	if err := json.Unmarshal([]byte(result.String()), &data); err != nil || data.Resource != resource || data.Text != "actual receipt" || len(data.Items) != 1 || data.Items[0].Identifier != "fresh-address" {
		t.Fatalf("native reader lost runtime facts: result=%s error=%v", result, err)
	}
}

func TestProgramProducerReturnsGroundedBindingsWithoutRemapping(t *testing.T) {
	r := Reflex{When: "A native resource workflow is requested", Decide: "Select current alternatives", Observe: `js:(() => {
const recent = history.length ? history[history.length-1] : null;
const reader = function(resource, actor) {
  const items = nativeState.items;
  return {state:{resource:resource,text:nativeState.text,items:items},
    candidates:choices(items.map(item => bind(actor,{command:'act ' + quote(item.identifier),resource:resource},false)))};
};
return {state:{},candidates:choices([bind(tools[0].name,{program:program(reader,[user,tools[1].name])},true)])};
})()`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	resource := "opaque resource 'quoted'\nsecond line"
	identifier := "current 'item' $literal `literal`\nnext"
	capabilities := map[string]any{"tools": []any{map[string]any{"name": "ordinary_reader"}, map[string]any{"name": "ordinary_actor"}}, "commands": []any{}}
	messages := []any{map[string]any{"role": "user", "text": resource}}
	input, _ := json.Marshal(map[string]any{"messages": messages})
	_, bindings, err := r.observe(t.Context(), input, capabilities)
	if err != nil || len(bindings) != 1 || !bindings["c0"].Read {
		t.Fatalf("producer serialization failed: bindings=%v error=%v", bindings, err)
	}
	var args struct{ Program string }
	_ = json.Unmarshal(bindings["c0"].Arguments, &args)
	state, _ := json.Marshal(map[string]any{"text": "actual native content", "items": []any{map[string]any{"label": "Current item", "identifier": identifier}}})
	native := goja.New()
	if _, err := native.RunString("const nativeState = " + string(state)); err != nil {
		t.Fatal(err)
	}
	value, err := native.RunString(args.Program)
	if err != nil {
		t.Fatalf("serialized producer failed in native context: %v", err)
	}
	output, err := json.Marshal(value.Export())
	if err != nil {
		t.Fatal(err)
	}
	messages = append(messages,
		map[string]any{"role": "assistant", "calls": []any{map[string]any{"id": "read", "name": "ordinary_reader", "arguments": map[string]any{"program": args.Program}}}},
		map[string]any{"role": "tool", "call_id": "read", "text": "ordinary envelope\n" + string(output)})
	input, _ = json.Marshal(map[string]any{"messages": messages})
	facts, bindings, err := r.observe(t.Context(), input, capabilities)
	if err != nil || len(bindings) != 1 || bindings["c0"].Name != "ordinary_actor" || bindings["c0"].Read || !strings.Contains(string(facts), "actual native content") {
		t.Fatalf("actual producer output was lost: facts=%s bindings=%v error=%v", facts, bindings, err)
	}
	var action struct{ Command, Resource string }
	_ = json.Unmarshal(bindings["c0"].Arguments, &action)
	parts, err := shell.Fields(action.Command, func(string) string { return "" })
	if err != nil || len(parts) != 2 || parts[1] != identifier || action.Resource != resource {
		t.Fatalf("native argument encoding changed actual values: parts=%v args=%+v error=%v", parts, action, err)
	}
}

func TestNativeObservationProtocolIsValidatedAndOnlyReusesFreshSuccess(t *testing.T) {
	r := Reflex{When: "Current resource workflow", Decide: "Choose current bindings", Observe: `js:({state:{needs_read:true},candidates:choices([bind('reader',{},true)])})`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	capabilities := map[string]any{"tools": []any{map[string]any{"name": "reader"}, map[string]any{"name": "actor"}}, "commands": []any{}}
	for _, mode := range []string{"valid", "array", "empty array", "business alternatives", "unknown tool", "missing read", "error result", "later effect"} {
		t.Run(mode, func(t *testing.T) {
			payload := `{"state":{"text":"actual content","version":9007199254740993},"candidates":{"live":{"name":"actor","arguments":{"id":"fresh","version":9007199254740993},"read":false}}}`
			if mode == "array" {
				payload = `{"state":{"text":"actual content","version":9007199254740993},"candidates":[{"name":"actor","arguments":{"id":"fresh","version":9007199254740993},"read":false}]}`
			}
			if mode == "empty array" {
				payload = `{"state":{"text":"actual final content"},"candidates":[]}`
			}
			if mode == "business alternatives" {
				payload = `{"state":{"text":"business result"},"candidates":[{"name":"a product","id":"fresh"}]}`
			}
			if mode == "unknown tool" {
				payload = strings.ReplaceAll(payload, `"name":"actor"`, `"name":"invented"`)
			}
			if mode == "missing read" {
				payload = strings.ReplaceAll(payload, `,"read":false`, "")
			}
			messages := []any{
				map[string]any{"role": "user", "text": "Use the current alternative"},
				map[string]any{"role": "assistant", "calls": []any{map[string]any{"id": "read", "name": "reader", "arguments": map[string]any{}}}},
				map[string]any{"role": "tool", "call_id": "read", "text": payload, "is_error": mode == "error result"},
			}
			if mode == "later effect" {
				messages = append(messages,
					map[string]any{"role": "assistant", "calls": []any{map[string]any{"id": "effect", "name": "actor", "arguments": map[string]any{"id": "fresh"}}}},
					map[string]any{"role": "tool", "call_id": "effect", "text": "action acknowledgement"})
			}
			input, _ := json.Marshal(map[string]any{"messages": messages})
			facts, candidates, err := r.observe(t.Context(), input, capabilities)
			switch mode {
			case "unknown tool":
				if err == nil {
					t.Fatal("native protocol bypassed binding validation")
				}
			case "missing read", "business alternatives", "error result", "later effect":
				if err != nil || len(candidates) != 1 || candidates["c0"].Name != "reader" {
					t.Fatalf("failed or stale protocol was reused: candidates=%v error=%v", candidates, err)
				}
			case "empty array":
				if err != nil || len(candidates) != 0 || !strings.Contains(string(facts), "actual final content") {
					t.Fatalf("final content without alternatives was lost: facts=%s candidates=%v error=%v", facts, candidates, err)
				}
			default:
				id := "live"
				if mode == "array" {
					id = "c0"
				}
				if err != nil || len(candidates) != 1 || candidates[id].Name != "actor" || !strings.Contains(string(facts), "9007199254740993") || !strings.Contains(string(candidates[id].Arguments), "9007199254740993") {
					t.Fatalf("actual alternatives or numeric values were remapped: facts=%s candidates=%v error=%v", facts, candidates, err)
				}
			}
		})
	}
}

func TestPureObservationUsesSameArrayBindingProtocol(t *testing.T) {
	r := Reflex{When: "Current resource", Decide: "Select actual alternatives", Observe: `js:({state:{text:"current content"},candidates:[bind("reader",{resource:user},true)]})`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"messages":[{"role":"user","text":"current resource"}]}`)
	facts, candidates, err := r.observe(t.Context(), input, map[string]any{"tools": []any{map[string]any{"name": "reader"}}, "commands": []any{}})
	if err != nil || len(candidates) != 1 || candidates["c0"].Name != "reader" || !candidates["c0"].Read || !strings.Contains(string(facts), "current content") || !strings.Contains(string(candidates["c0"].Arguments), "current resource") {
		t.Fatalf("pure output diverged from native protocol: facts=%s candidates=%v error=%v", facts, candidates, err)
	}
}

func TestDecodeReflex(t *testing.T) {
	source := `(() => ({state:{},candidates:choices([bind("ordinary",{},true)])}))()`
	raw := `js:(() => { return {state:{text:"你好",pattern:/["\\]/},candidates:choices([])}; })()`
	cases := []struct {
		name, input, want string
		reject            bool
		candidates        int
	}{
		{"raw", "js:" + source, "js:" + source, false, 1},
		{"js fence", "```js\n" + source + "\n```", "js:" + source, false, 1},
		{"javascript fence", "```javascript\njs:" + source + "\n```", "js:" + source, false, 1},
		{"prefixed js fence", "js:\n```js\n" + source + "\n```", "js:" + source, false, 1},
		{"prefixed javascript fence", "js:\n```javascript\n" + source + "\n```", "js:" + source, false, 1},
		{"unicode and regex", raw, raw, false, 0},
		{"null", "null", "", false, 0},
		{"leading prose", "Here is the code:\n```js\n" + source + "\n```", "", true, 0},
		{"trailing prose", "```js\n" + source + "\n```\nExecute this", "", true, 0},
		{"prefixed trailing prose", "js:\n```js\n" + source + "\n```\nExecute this", "", true, 0},
		{"wrong language", "js:\n```python\n" + source + "\n```", "", true, 0},
		{"incomplete fence", "js:\n```js\n" + source, "", true, 0},
		{"multiple fences", "js:\n```js\n" + source + "\n```\n```js\n" + source + "\n```", "", true, 0},
		{"extra after null", `null {}`, "", true, 0},
		{"unknown metadata", `{"when":"scene","decide":"choose","extra":true}`, "", true, 0},
		{"multiple JSON objects", `{"when":"scene","decide":"choose"} {}`, "", true, 0},
		{"metadata before source", `{"when":"scene","decide":"choose","observe":"old"} js:({})`, "", true, 0},
		{"JSON scene", `{"when":"scene","decide":"choose","observe":"js:({})"}`, "", true, 0},
		{"metadata and raw code", `{"when":"scene","decide":"choose"}` + raw, "", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r *Reflex
			err := decodeReflex(tc.input, &r)
			if tc.reject {
				if err == nil {
					t.Fatal("malformed compilation accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if r != nil {
					t.Fatal("null compilation produced a scene")
				}
				return
			}
			if r == nil || r.Observe != tc.want {
				t.Fatalf("source changed: %+v", r)
			}
			r.When, r.Decide = "Current native resource", "Choose actual native calls"
			if err := r.validate(); err != nil {
				t.Fatal(err)
			}
			_, candidates, err := r.observe(t.Context(), json.RawMessage(`{"messages":[]}`), map[string]any{"tools": []any{map[string]any{"name": "ordinary"}}, "commands": []any{}})
			if err != nil || len(candidates) != tc.candidates {
				t.Fatalf("source did not execute: candidates=%v error=%v", candidates, err)
			}
		})
	}
}

func TestGeneratedInspectionRunsBeforeEffectCanReport(t *testing.T) {
	var rid string
	var reportAvailable bool
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		criteria := req.Questions[rid].Criteria.(map[string]any)
		_, reportAvailable = criteria[report]
		if reportAvailable {
			return runtimeAnswers(req, report)
		}
		return runtimeAnswers(req, rid+"/inspect")
	})
	e, _, _ := testInstallation(t, Config{Mode: "auto"}, client)
	r := Reflex{When: "A native workflow is requested", Decide: "Inspect after effects and report actual evidence", Observe: `js:({state:{},candidates:{}})`}
	rid = "r" + digest(r)[:16]
	e.tasks["run"] = taskRecord{Key: "task", NeedsRead: true}
	key := rid + "/inspect"
	candidate := binding{Name: "bash", Arguments: json.RawMessage(`{"command":"ordinary current-state"}`), Read: true}
	observations := map[string]json.RawMessage{rid: json.RawMessage(`{"phase":"inspection needed"}`)}
	observed := &observation{context: json.RawMessage(`{"messages":[]}`), facts: observations, candidates: map[string]binding{key: candidate}}
	selected, result, err := e.decide(t.Context(), observed, nil, &r, "run", "task")
	if err != nil || selected == nil || result != key || reportAvailable {
		t.Fatalf("unexecuted inspection was bypassed: choice=%s report=%t error=%v", result, reportAvailable, err)
	}
	record := e.tasks["run"]
	record.NeedsRead = false
	e.tasks["run"] = record
	selected, result, err = e.decide(t.Context(), observed, nil, &r, "run", "task")
	if err != nil || selected != nil || result != report || !reportAvailable {
		t.Fatalf("completed read cannot report: choice=%s report=%t error=%v", result, reportAvailable, err)
	}
	// An effect that already returned complete evidence need not invent an
	// inspection when its generated scene offers none.
	record.NeedsRead = true
	e.tasks["run"] = record
	observed.candidates = nil
	_, result, err = e.decide(t.Context(), observed, nil, &r, "run", "task")
	if err != nil || result != report || !reportAvailable {
		t.Fatalf("self-contained effect result was blocked: choice=%s error=%v", result, err)
	}
}

func TestRetiredSceneRemainsEligibleAfterRestart(t *testing.T) {
	e := New(Config{Directory: t.TempDir()})
	c := Claim{When: "Current native task", Question: "Which item applies?", Options: map[string]string{"item": "Choose item", Defer: "Missing facts"}}
	claimID := "c" + digest(c)[:16]
	r := Reflex{When: c.When, Decide: "Choose current item", Observe: `js:({state:{},candidates:{}})`}
	id := "r" + digest(r)[:16]
	e.library.Claims[claimID] = claimRecord{Claim: c}
	e.library.Reflexes[id] = reflexRecord{Reflex: r, Claims: []string{claimID}}
	if !e.retireReflex(id, fmt.Errorf("invalid generated binding")) {
		t.Fatal("failed scene still owns declarations")
	}
	reloaded := New(e.config)
	if err := reloaded.loadLibrary(); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.library.Claims) != 1 || len(reloaded.library.Reflexes) != 0 || len(reloaded.snapshot().Compiled) != 0 {
		t.Fatalf("retirement lost declarations or blocked regeneration: %+v", reloaded.library)
	}
}

func TestToolSupplementationAfterReportTriggersSceneRepair(t *testing.T) {
	var requests atomic.Int64
	var proposals atomic.Int64
	var repairing string
	var handedOff, supplemented string
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		requests.Add(1)
		var state struct {
			Repair  string          `json:"repair"`
			Handoff json.RawMessage `json:"handoff"`
			Context json.RawMessage `json:"context"`
		}
		_ = json.Unmarshal(req.State, &state)
		if q, exists := req.Questions["compile"]; !exists || !strings.Contains(fmt.Sprint(q.Instructions), "recorded handoff BEFORE") {
			t.Error("JEV did not judge repair necessity against the original gap")
		}
		repairing = state.Repair
		handedOff, supplemented = string(state.Handoff), string(state.Context)
		out := map[string]jevapi.Answer{}
		for name := range req.Questions {
			out[name] = answer("include")
			if name == "ownership" {
				out[name] = answer("partial")
			}
			if name == "compile" {
				out[name] = answer("compile")
			}
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	claim := Claim{When: "Current native workflow", Question: "Which operation applies?", Options: map[string]string{"operate": "Use known operations", Defer: "Missing facts"}}
	cid := "c" + digest(claim)[:16]
	r := Reflex{When: claim.When, Decide: "Report recorded result", Observe: `js:({state:{},candidates:{}})`}
	rid := "r" + digest(r)[:16]
	e.library.Claims[cid] = claimRecord{Claim: claim}
	e.library.Reflexes[rid] = reflexRecord{Reflex: r, Claims: []string{cid}}
	ev := hooks.ContextEvent{SessionID: "supplement", TurnID: "turn", Messages: []*aop.Message{provider.TextMessage("user", "Get the actual result"), provider.TextMessage("assistant", "Final answer")}}
	run, task := taskIdentity(ev)
	boundary := json.RawMessage(`{"context":{"messages":[{"role":"user","text":"Get the actual result"}]},"observations":{"pending":"actual result missing at handoff"},"candidates":{}}`)
	e.tasks[run] = taskRecord{Key: task, Reported: rid, Handoff: boundary}
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		proposals.Add(1)
		return reply(provider.TextMessage("assistant", "null")), nil
	})
	e.enqueue(cfg, ev)
	settle(t, e)
	if requests.Load() != 0 {
		t.Fatal("known final answer triggered discovery")
	}
	ev.Messages[1] = &aop.Message{Role: "assistant", Content: []*aop.Content{action("ordinary read remaining-result")}}
	e.enqueue(cfg, ev)
	settle(t, e)
	if requests.Load() != 0 {
		t.Fatal("unfinished supplementation compiled a scene")
	}
	// After the model fills the gap, JEV may resume and REPORT. That later
	// completion must not erase the earlier scene defect or suppress repair.
	e.mu.Lock()
	record := e.tasks[run]
	record.Reported = rid
	e.tasks[run] = record
	e.mu.Unlock()
	ev.Messages = append(ev.Messages, provider.TextMessage("assistant", "Actual remaining result obtained"))
	e.enqueue(cfg, ev)
	settle(t, e)
	if repairing != rid || requests.Load() != 1 || proposals.Load() != 1 || len(e.snapshot().Reflexes) != 1 {
		t.Fatalf("supplementation did not propose bounded repair or null changed library: repair=%q requests=%d proposals=%d", repairing, requests.Load(), proposals.Load())
	}
	if handedOff != string(boundary) || strings.Contains(handedOff, "ordinary read remaining-result") || !strings.Contains(supplemented, "ordinary read remaining-result") {
		t.Fatal("repair confused pre-supplementation controller evidence with later model work")
	}
}

func TestLaterControllerHandoffPreservesUnresolvedRepairEvidence(t *testing.T) {
	for _, choice := range []string{report, Defer} {
		t.Run(choice, func(t *testing.T) {
			client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
				if runtimeRequest(req) {
					return runtimeAnswers(req, choice)
				}
				return declarationAnswers(req, false)
			})
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
			cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				return reply(provider.TextMessage("assistant", "done")), nil
			})
			r := Reflex{When: "Current workflow", Decide: "Report actual completed work", Observe: `js:({state:{current:"later completed result"},candidates:{}})`}
			if err := r.validate(); err != nil {
				t.Fatal(err)
			}
			rid := "r" + digest(r)[:16]
			e.library.Reflexes[rid] = reflexRecord{Reflex: r}
			ev := hooks.ContextEvent{SessionID: "resumed", TurnID: "turn", Messages: []*aop.Message{provider.TextMessage("user", "Complete the workflow")}}
			run, task := taskIdentity(ev)
			gap := json.RawMessage(`{"observations":{"missing":"required operation was absent"},"candidates":{}}`)
			e.tasks[run] = taskRecord{Key: task, Repair: rid, Handoff: gap}
			if _, err := e.beforeModel(agent.ContextWithToolAgentConfig(t.Context(), cfg), ev); err != nil {
				t.Fatal(err)
			}
			e.mu.Lock()
			record := e.tasks[run]
			e.mu.Unlock()
			if record.Repair != rid || string(record.Handoff) != string(gap) || (choice == report && record.Reported != rid) {
				t.Fatalf("later %s erased original repair: %+v", choice, record)
			}
		})
	}
}

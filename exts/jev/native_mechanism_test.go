package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chainreactors/cyber/internal/jevwire"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestReflexV2ReplayLiteralShellEncoding(t *testing.T) {
	call := func(command any, timeout int) binding {
		return binding{Name: "bash", Arguments: json.RawMessage(jsonText(map[string]any{"command": command, "timeout": timeout}))}
	}
	recorded := call(`experiment append "actor with spaces"`, 30)
	generated := call(map[string]any{"name": "experiment", "argv": []string{"append", "actor with spaces"}}, 30)
	if recorded.replayKey() != generated.replayKey() {
		t.Fatal("equivalent literal argv did not replay")
	}
	for _, other := range []binding{
		call(`experiment append "another actor"`, 30),
		call(`experiment append "actor with spaces"`, 31),
		call(`experiment append "actor with spaces" && experiment summary "actor with spaces"`, 30),
		call(`experiment append "$ACTOR"`, 30),
	} {
		if other.replayKey() == generated.replayKey() {
			t.Fatal("changed target/options or nonliteral script matched evidence")
		}
	}
}

func TestReflexV2FrozenReusesWithoutLearning(t *testing.T) {
	effects := 0
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer { return runtimeAnswers(req, "run") })
	e, cfg, _ := testInstallation(t, Config{Mode: "auto", Learning: "frozen"}, client, coretool.Command{Name: "lab", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
		if ex.Args[0] == "add" {
			effects++
		}
		fmt.Fprint(ex.Stdout, jsonText(map[string]any{"status": 200, "complete": true, "count": effects, "receipt": "current-receipt"}))
		return nil, nil
	}})
	if err := testVerification(e).Register(laboratorySuite()); err != nil {
		t.Fatal(err)
	}
	caps, err := e.capabilities(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := qualifiedLaboratory(t, e, caps)
	e.library.Reflexes["r"+digest(r)[:16]] = reflexRecord{Reflex: r}
	before := digest(e.snapshot())
	parameters, composition := 0, 0
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		switch req.Purpose {
		case "parameters":
			parameters++
			return reply(provider.TextMessage("assistant", `{"actor":"current","query":true,"count":2}`)), nil
		case "composition":
			composition++
			if len(req.Tools) > 0 {
				t.Error("composition can execute tools")
			}
			return reply(provider.TextMessage("assistant", "current-receipt")), nil
		default:
			return nil, fmt.Errorf("frozen runtime requested LLM reasoning: %s", req.Purpose)
		}
	})
	cfg.SessionID = "frozen-runtime"
	result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Add exactly two entries for current and report its receipt."))
	if err != nil || result == nil || result.Output != "current-receipt" || parameters != 1 || composition != 1 || effects != 2 {
		t.Fatalf("result=%v err=%v parameters=%d composition=%d effects=%d", result, err, parameters, composition, effects)
	}
	if err := e.compile(t.Context(), declaration{}, "unused"); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if digest(e.snapshot()) != before {
		t.Fatal("frozen runtime changed library")
	}
}

func TestReflexV2CompilerRetainsCoverageCandidateOnFailure(t *testing.T) {
	e := testLaboratory(t)
	r := laboratoryReflex()
	r.LegacySuite = ""
	artifact := jsonText(map[string]any{"api_version": r.APIVersion, "observe": r.Observe, "steps": r.Steps, "parameters_schema": r.Parameters, "arguments": r.arguments})
	requests := 0
	cfg := agent.Config{Model: "test", Provider: testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		requests++
		if requests > 1 {
			return nil, errors.New("compiler unavailable after draft")
		}
		return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: "draft", Name: "validate_reflex", Arguments: &aop.EncodedValue{Data: []byte(jsonText(map[string]any{"artifact": json.RawMessage(artifact)})), MediaType: aop.JSONMediaType}}}}}}), nil
	})}
	claim := Claim{Type: jevapi.ClaimNoul, Context: "Add entries for current actor."}
	id := "c" + digest(claim)[:16]
	e.library.Claims[id] = claimRecord{Claim: claim}
	plan := &compilation{job: declaration{cfg: cfg}, claims: map[string]Claim{id: claim}, ids: []string{id}, capabilities: observationCapabilities("bash"), state: json.RawMessage(`{"messages":[{"role":"user","text":"Add entries for current actor"}]}`), input: map[string]any{}}
	if _, err := e.generateReflex(t.Context(), plan); err == nil {
		t.Fatal("compiler failure disappeared")
	}
	lib := e.snapshot()
	if len(lib.Reflexes) != 0 || len(lib.Candidates) != 1 {
		t.Fatalf("lost candidate or published unverified source: %+v", lib)
	}
	for _, candidate := range lib.Candidates {
		if candidate.Observe != r.Observe || candidate.Proof != nil || !strings.Contains(candidate.Blocker, "recorded trajectory") {
			t.Fatal("candidate lost source/blocker")
		}
	}
}

func TestReflexV2MechanismRequiresRecordedEvidence(t *testing.T) {
	e := testLaboratory(t)
	r := laboratoryReflex()
	r.LegacySuite = ""
	if err := e.qualify(t.Context(), &r, observationCapabilities("bash")); err == nil || !strings.Contains(err.Error(), "no recorded trajectory") {
		t.Fatalf("missing evidence accepted: %v", err)
	}
	if r.Proof != nil {
		t.Fatal("unreplayed artifact has proof")
	}
	raw := json.RawMessage(`{"messages":[{"role":"user","text":"add items"}],"omitted_evidence":1}`)
	if err := e.qualify(t.Context(), &r, observationCapabilities("bash"), raw); err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Fatalf("truncated evidence accepted: %v", err)
	}
}

func TestReflexV2ReplayRequiresReportAfterAllResults(t *testing.T) {
	e := testLaboratory(t)
	r := Reflex{APIVersion: 2, When: "Inspect current state", Decide: "Read and report its receipt", Observe: `js:function(context){execute({name:'bash',arguments:{command:'lab status current'},read:true});return{defer:'waiting for entry history to grow'};}`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"messages":[{"role":"user","text":"Read the current receipt"},{"role":"assistant","calls":[{"id":"read","name":"bash","arguments":{"command":"lab status current"}}]},{"role":"tool","call_id":"read","text":"{\"receipt\":\"current-receipt\"}"}]}`)
	err := e.qualify(t.Context(), &r, observationCapabilities("bash"), state)
	diagnostic := compilerDiagnostic(err)
	if err == nil || r.Proof != nil || diagnostic.Code != "completion_missing" || diagnostic.Replayed != 1 || diagnostic.Recorded != 1 {
		t.Fatalf("fully replayed handoff was accepted as completion: err=%v proof=%+v diagnostic=%+v", err, r.Proof, diagnostic)
	}
	r.Observe = `js:function(context){const r=execute({name:'bash',arguments:{command:'lab status current'},read:true});return{report:{evidence:r.call_id,path:['data','receipt']}};}`
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	if err := e.qualify(t.Context(), &r, observationCapabilities("bash"), state); err != nil || r.Proof == nil {
		t.Fatalf("grounded fresh return value cannot qualify: err=%v proof=%+v", err, r.Proof)
	}
	if !e.qualified(reflexRecord{Reflex: r}) {
		t.Fatal("current completed proof cannot run")
	}
	r.Proof.Checks = r.Proof.Checks[:len(r.Proof.Checks)-1]
	if e.qualified(reflexRecord{Reflex: r}) {
		t.Fatal("legacy proof without entry report check can still take over")
	}
}

func TestReflexV2UnsupportedRecordedOperationWaitsForNativeEvidence(t *testing.T) {
	e := testLaboratory(t)
	r := Reflex{APIVersion: 2, When: "Inspect current state", Decide: "Read current state", Observe: `js:function(){return{report:'done'};}`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"messages":[{"role":"user","text":"Inspect current state"},{"role":"assistant","calls":[{"id":"compound","name":"bash","arguments":{"command":"lab status current; lab status other"}}]},{"role":"tool","call_id":"compound","text":"opaque combined result"}]}`)
	err := e.qualify(t.Context(), &r, observationCapabilities("bash"), state)
	diagnostic := compilerDiagnostic(err)
	if err == nil || r.Proof != nil || diagnostic.Code != "recorded_capability_unavailable" || diagnostic.Status != "waiting" || !strings.Contains(jsonText(diagnostic.Expected), "lab status other") {
		t.Fatalf("unsupported evidence was treated as an endless code repair: err=%v diagnostic=%+v", err, diagnostic)
	}
}

func TestReflexV2RuntimeSemanticJudgmentsDefer(t *testing.T) {
	for _, kind := range []string{"input", "binding", "completion"} {
		t.Run(kind, func(t *testing.T) {
			e := testLaboratory(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(jevwire.Response{Answers: map[string]jevwire.Answer{kind: {Type: "choice", Choice: Defer}}})
			}))
			defer server.Close()
			e.client = jevapi.New("test-only", "test", time.Second)
			e.client.Endpoint = server.URL
			defer e.client.Close()
			if err := e.judgeRuntime(t.Context(), kind, json.RawMessage(`{"messages":[]}`), map[string]any{}); err == nil {
				t.Fatal("deferred semantic judgment authorized work")
			}
		})
	}
}

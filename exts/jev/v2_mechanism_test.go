package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

const laboratorySource = `js:function(context,args){
 if(!args)return {defer:"missing parameters",parameters:"actor, query, count"};
 for(let i=0;i<args.count;i++){
  const result=execute({name:"bash",arguments:{command:command("lab",["add",args.actor])},read:false,step:"add",occurrence:i});
  if(result.is_error || result.data.status===503){
   if(!args.query)return {defer:"effect_unknown: no query capability"};
   for(let n=0;n<4;n++){
    const state=execute({name:"bash",arguments:{command:command("lab",["status",args.actor])},read:true});
    if(state.data.status===200 && state.data.complete){break;}
    if(n===3)return {defer:"still pending"};
   }
  }
 }
 const state=execute({name:"bash",arguments:{command:command("lab",["status",args.actor])},read:true});
 if(!state.data.complete)return {defer:"pending"};
 return {report:{count:{evidence:state.call_id,path:["data","count"]},receipt:{evidence:state.call_id,path:["data","receipt"]}}};
}`

func laboratoryReflex() Reflex {
	return Reflex{APIVersion: 2, When: "add requested items", Decide: "use actual results", Observe: laboratorySource, LegacySuite: "laboratory", Steps: map[string]StepDefinition{"add": {Contract: "lab", CountArgument: "count"}}, Parameters: json.RawMessage(`{"type":"object","required":["actor","query","count"],"properties":{"actor":{"type":"string"},"query":{"type":"boolean"},"count":{"type":"integer","minimum":1,"maximum":2}},"additionalProperties":false}`), arguments: map[string]any{"actor": "initial", "query": true, "count": 2}}
}
func laboratorySuite() VerificationSuite {
	contract := NativeContract{ID: "lab", Version: "1", Classify: func(c NativeCall) (Access, error) {
		if c.Name != "bash" || len(c.Argv) != 3 || c.Argv[0] != "lab" {
			return UnsupportedAccess, nil
		}
		if c.Argv[1] == "add" {
			return EffectAccess, nil
		}
		if c.Argv[1] == "status" {
			return ReadAccess, nil
		}
		return UnsupportedAccess, nil
	}, Outcome: func(_ NativeCall, r map[string]any) string {
		if data, ok := r["data"].(map[string]any); ok && fmt.Sprint(data["status"]) == "200" {
			return "applied"
		}
		return "unknown"
	}, Resolve: func(effect, read NativeCall, r map[string]any) bool {
		data, ok := r["data"].(map[string]any)
		return ok && len(effect.Argv) == 3 && len(read.Argv) == 3 && effect.Argv[2] == read.Argv[2] && read.Argv[1] == "status" && data["complete"] == true
	}}
	return VerificationSuite{ID: "laboratory", Version: "1", Contracts: map[string]NativeContract{"lab": contract}, CheckInput: func(_ map[string]any, args map[string]any) error {
		if args == nil {
			return errors.New("missing current input")
		}
		return nil
	}, CheckCall: func(call VerificationCall) error {
		if len(call.Call.Argv) != 3 || call.Call.Argv[2] != call.Arguments["actor"] {
			return errors.New("native target does not match the current actor")
		}
		if call.Call.Step == "add" && call.Call.Occurrence > 0 && len(call.Evidence) == 0 {
			return errors.New("later occurrence has no prior native evidence")
		}
		return nil
	}, CheckReport: func(report VerificationReport) error {
		p, ok := report.Report.(map[string]any)
		if !ok {
			return errors.New("invalid report")
		}
		if fmt.Sprint(p["count"]) != fmt.Sprint(report.Arguments["count"]) {
			return errors.New("report does not satisfy the current requested count")
		}
		for _, r := range report.Evidence {
			d, ok := r["data"].(map[string]any)
			if ok && d["complete"] == true && p["receipt"] == d["receipt"] && fmt.Sprint(p["count"]) == fmt.Sprint(d["count"]) {
				return nil
			}
		}
		return errors.New("report has no actual completion witness")
	}, Cases: func(_ map[string]any) []VerificationCase {
		cases := []VerificationCase{}
		for seed := 0; seed < 20; seed++ {
			for _, condition := range []string{"normal", "503", "tool_error", "no_query"} {
				actor := fmt.Sprintf("用户 %d '引号' \\ \"路径\"\n下一行", seed)
				count, polls := 0, 0
				receipt := fmt.Sprintf("server-only-%s-%d", condition, seed)
				query := condition != "no_query"
				args := map[string]any{"actor": actor, "query": query, "count": 2}
				c := VerificationCase{ID: fmt.Sprintf("%s/%d", condition, seed), Input: map[string]any{"user": "add two current items"}, Arguments: args}
				c.Execute = func(call NativeCall) (map[string]any, error) {
					if len(call.Argv) != 3 || call.Argv[2] != actor {
						return nil, errors.New("wrong current actor or encoding")
					}
					if call.Argv[1] == "add" {
						count++
						status := 200
						toolError := false
						if count == 1 && condition != "normal" {
							status = 503
							toolError = condition == "tool_error"
						}
						return map[string]any{"is_error": toolError, "data": map[string]any{"status": status}}, nil
					}
					if !query {
						return nil, errors.New("queried unavailable capability")
					}
					polls++
					complete := condition == "normal" || polls >= 3
					data := map[string]any{"status": 200, "complete": complete, "count": count}
					if complete {
						data["receipt"] = receipt
					}
					return map[string]any{"is_error": false, "data": data}, nil
				}
				c.Check = func(run VerificationRun) error {
					if run.Error != nil {
						return run.Error
					}
					if !query {
						if count != 1 || polls != 0 || run.Output[Defer] == nil {
							return errors.New("unknown outcome was retried, queried or fabricated")
						}
						return nil
					}
					p, ok := run.Output[report].(map[string]any)
					if !ok || count != 2 || p["receipt"] != receipt || fmt.Sprint(p["count"]) != "2" {
						return fmt.Errorf("wrong business result: effects=%d output=%v", count, run.Output)
					}
					return nil
				}
				cases = append(cases, c)
			}
		}
		return cases
	}}
}
func testLaboratory(t *testing.T) *Extension {
	t.Helper()
	e := New(Config{Directory: t.TempDir()})
	if err := testVerification(e).Register(laboratorySuite()); err != nil {
		t.Fatal(err)
	}
	return e
}
func TestReflexV2IndependentQualification(t *testing.T) {
	e := testLaboratory(t)
	r := laboratoryReflex()
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	if err := qualifyIndependent(e, t.Context(), &r, observationCapabilities("bash")); err != nil {
		t.Fatal(err)
	}
	if !e.qualified(reflexRecord{Reflex: r}) || len(r.Proof.Checks) == 0 {
		t.Fatal("missing qualification")
	}
	mutants := map[string]string{
		"wrong actor":          strings.ReplaceAll(r.Observe, "args.actor", "'initial'"),
		"effect as read":       strings.ReplaceAll(r.Observe, "read:false", "read:true"),
		"poll as effect":       strings.ReplaceAll(r.Observe, "read:true", "read:false"),
		"skip operation":       strings.ReplaceAll(r.Observe, "i<args.count", "i<0"),
		"fabricated report":    strings.ReplaceAll(r.Observe, `{evidence:state.call_id,path:["data","receipt"]}`, `"invented"`),
		"wrong command":        strings.ReplaceAll(r.Observe, `command("lab"`, `command("missing"`),
		"duplicate occurrence": strings.ReplaceAll(r.Observe, "occurrence:i", "occurrence:0"),
	}
	t.Run("missing effect manifest", func(t *testing.T) {
		m := laboratoryReflex()
		m.Steps = nil
		_ = m.validate()
		if err := qualifyIndependent(e, t.Context(), &m, observationCapabilities("bash")); err == nil {
			t.Fatal("effects without declared identities were qualified")
		}
	})
	for name, source := range mutants {
		t.Run(name, func(t *testing.T) {
			m := laboratoryReflex()
			m.Observe = source
			_ = m.validate()
			if err := qualifyIndependent(e, t.Context(), &m, observationCapabilities("bash")); err == nil {
				t.Fatal("mutant independently qualified")
			}
		})
	}
	r.Observe = strings.ReplaceAll(r.Observe, "return {report:", "return {report:") + " "
	if e.qualified(reflexRecord{Reflex: r}) {
		t.Fatal("changed source retained stale proof")
	}
}
func TestReflexV2EffectIdentityConflictAndConcurrency(t *testing.T) {
	call := NativeCall{Name: "native", Arguments: json.RawMessage(`{"x":1}`), Step: "add"}
	ledger := newEffectLedger()
	if _, cached, err := ledger.reserve("task", call); err != nil || cached {
		t.Fatal(err)
	}
	ledger.complete("task", call, map[string]any{"is_error": false, "data": "first"}, nil)
	if _, cached, err := ledger.reserve("task", call); err != nil || !cached {
		t.Fatal("same logical effect replayed")
	}
	second := call
	second.Occurrence = 1
	if _, cached, err := ledger.reserve("task", second); err != nil || cached {
		t.Fatal("legitimate repetition swallowed")
	}
	conflict := call
	conflict.Arguments = json.RawMessage(`{"x":2}`)
	if _, _, err := ledger.reserve("task", conflict); err == nil {
		t.Fatal("changed payload accepted")
	}
	ledger = newEffectLedger()
	var mu sync.Mutex
	attempts := 0
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, cached, err := ledger.reserve("task", call); err == nil && !cached {
				mu.Lock()
				attempts++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if attempts != 1 {
		t.Fatalf("concurrent dispatches=%d", attempts)
	}
}
func TestReflexV2InputRevisionAndCancellation(t *testing.T) {
	a := hooks.ContextEvent{SessionID: "s", TurnID: "t", Messages: []*aop.Message{provider.TextMessage("user", "add two")}}
	b := a
	b.Messages = append(append([]*aop.Message(nil), a.Messages...), provider.TextMessage("user", "use the existing handle"))
	_, first := taskIdentity(a)
	_, second := taskIdentity(b)
	if first != second || inputRevision(a) == inputRevision(b) {
		t.Fatal("input revision reset task identity")
	}
	b.TurnID = "next"
	_, next := taskIdentity(b)
	if next == first {
		t.Fatal("new task reused identity")
	}
}
func TestReflexV2ClaimTriggerAndCandidate(t *testing.T) {
	var claimID string
	compileDecision := false
	generations := 0
	client := fakeJEV(t, func(req inferenceRequest) map[string]inferenceAnswer {
		out := declarationAnswers(req, true)
		if _, ok := req.Questions["claim0"]; ok {
			if claimID != "" {
				out["claim0"] = answer(claimID)
			} else {
				out["claim0"] = answer("new")
			}
		}
		if _, ok := req.Questions["compile"]; ok {
			if !compileDecision {
				out["compile"] = answer(Defer)
			}
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if provider.MessageText(req.Messages[0]) == claimPrompt {
			return reply(provider.TextMessage("assistant", `{"claims":[{"type":"noul","context":"When a queued order has an uncertain submission result, inspect its current status without resubmitting."}]}`)), nil
		}
		generations++
		if generations > 1 {
			t.Fatalf("unexpected candidate retry: %s", provider.MessageText(req.Messages[len(req.Messages)-1]))
		}
		return reply(provider.TextMessage("assistant", `{"api_version":2,"steps":{"pending":{"contract":"unconfigured","count":1}},"observe":"js:function(){return {defer:'missing supported contract'};}"}`)), nil
	})
	job := declaration{cfg: cfg, task: "first", state: json.RawMessage(`{"messages":[{"role":"user","text":"inspect order"}]}`), focus: []string{"inspect order"}, final: true}
	if err := e.declare(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	for id := range e.snapshot().Claims {
		claimID = id
	}
	if claimID == "" || generations != 0 {
		t.Fatal("new natural-language Claim auto-compiled")
	}
	job.task = "next"
	if err := e.declare(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if generations != 0 {
		t.Fatal("JEV defer started a compiler")
	}
	compileDecision = true
	if err := e.declare(t.Context(), job); err != nil {
		t.Fatal(err)
	}
	if generations != 1 || len(e.snapshot().Candidates) != 1 || len(e.snapshot().Reflexes) != 0 || len(e.snapshot().Claims) != 1 {
		t.Fatal("candidate was published or Claim consumed")
	}
}

// Keep cancellation bounded by the caller rather than the deleted 100ms timer.
func TestReflexV2NoComputeBudget(t *testing.T) {
	r := Reflex{When: "pure", Decide: "pure", Observe: `js:function(){while(true){}}`}
	_ = r.validate()
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	_, err := runReflexJS(ctx, &r, map[string]any{}, nil, nil, nil)
	if !errors.Is(interruptedCause(err), context.DeadlineExceeded) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

var _ coretool.Executor = (*compilerAgent)(nil)

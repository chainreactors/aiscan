package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chainreactors/cyber/internal/jevwire"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
)

func TestReflexV2AgentExecutorAndHandoff(t *testing.T) {
	for seed := 0; seed < 20; seed++ {
		for _, condition := range []string{"normal", "503", "tool_error", "no_query", "guardrail", "missing_input"} {
			t.Run(fmt.Sprintf("%s/%d", condition, seed), func(t *testing.T) {
				actor := fmt.Sprintf("当前用户 %d 'quoted' \\ \"值\"", seed)
				effects, polls := 0, 0
				receipt := fmt.Sprintf("actual-%s-%d", condition, seed)
				client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
					if runtimeRequest(req) {
						return runtimeAnswers(req, "run")
					}
					return declarationAnswers(req, false)
				})
				command := coretool.Command{Name: "lab", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
					if len(ex.Args) != 2 || ex.Args[1] != actor {
						t.Errorf("wrong argument: %q", ex.Args)
					}
					if ex.Args[0] == "add" {
						effects++
						status := 200
						if effects == 1 && condition != "normal" {
							status = 503
						}
						fmt.Fprint(ex.Stdout, jsonText(map[string]any{"status": status}))
						if condition == "tool_error" && effects == 1 {
							return nil, errors.New("response lost")
						}
						return nil, nil
					}
					polls++
					complete := condition == "normal" || polls >= 3
					data := map[string]any{"status": 200, "complete": complete, "count": effects}
					if complete {
						data["receipt"] = receipt
					}
					fmt.Fprint(ex.Stdout, jsonText(data))
					return nil, nil
				}}
				e, cfg, registry := testInstallation(t, Config{Mode: "auto"}, client, command)
				if err := testVerification(e).Register(laboratorySuite()); err != nil {
					t.Fatal(err)
				}
				r := laboratoryReflex()
				if err := r.validate(); err != nil {
					t.Fatal(err)
				}
				caps, err := e.capabilities(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err = qualifyIndependent(e, t.Context(), &r, caps); err != nil {
					t.Fatal(err)
				}
				rid := "r" + digest(r)[:16]
				e.library.Reflexes[rid] = reflexRecord{Reflex: r, Contracts: map[string]string{}}
				var events []*aop.Event
				sub := e.stream.Observe(func(ev *aop.Event) { events = append(events, ev) })
				defer sub.Close(t.Context())
				if condition == "guardrail" {
					toolhooks.Before.On(registry, "deny-v2", func(context.Context, toolhooks.CallEvent) (toolhooks.Admission, error) {
						return toolhooks.Admission{Deny: errors.New("blocked")}, nil
					})
				}
				finals := 0
				cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
					if provider.MessageText(req.Messages[0]) == claimPrompt {
						return reply(provider.TextMessage("assistant", `{"claims":[]}`)), nil
					}
					if strings.HasPrefix(provider.MessageText(req.Messages[0]), "Supply only the CURRENT") {
						if condition == "missing_input" {
							return reply(provider.TextMessage("assistant", "null")), nil
						}
						return reply(provider.TextMessage("assistant", jsonText(map[string]any{"actor": actor, "count": 2, "query": condition != "no_query"}))), nil
					}
					finals++
					text := provider.MessageText(req.Messages[len(req.Messages)-1])
					if !strings.Contains(text, "Reflex handoff:") {
						t.Error("main model did not receive handoff")
					}
					if condition == "normal" || condition == "503" || condition == "tool_error" {
						if !strings.Contains(text, receipt) || !strings.Contains(text, "REPORT:") {
							t.Error("actual completion lost")
						}
					}
					return reply(provider.TextMessage("assistant", "finished")), nil
				})
				cfg.SessionID = fmt.Sprintf("v2-%s-%d", condition, seed)
				result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Add exactly two current items and report the actual receipt."))
				if err != nil || result.Output != "finished" || finals != 1 {
					t.Fatalf("run result=%v error=%v finals=%d", result, err, finals)
				}
				settle(t, e)
				wantEffects := 2
				if condition == "no_query" {
					wantEffects = 1
				}
				if condition == "guardrail" || condition == "missing_input" {
					wantEffects = 0
				}
				if effects != wantEffects {
					t.Fatalf("effects=%d expected=%d", effects, wantEffects)
				}
				found := false
				for _, ev := range events {
					var runtime RuntimeEvent
					if ev.GetExtension() != nil && ev.GetExtension().UnmarshalTo(&runtime) == nil && runtime.GetHandoff() != nil {
						h := runtime.GetHandoff()
						if h.Code == "" || h.Detail == "" {
							t.Error("structured handoff lost reason")
						}
						found = true
					}
				}
				if !found {
					t.Fatal("handoff event missing")
				}
			})
		}
	}
}

func TestReflexV2RuntimeJudgmentsReceiveEachCurrentResult(t *testing.T) {
	bindings, completions := 0, 0
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		if runtimeRequest(req) {
			return runtimeAnswers(req, "run")
		}
		if _, ok := req.Questions["binding"]; ok {
			bindings++
			if bindings == 2 && !strings.Contains(string(req.State), "fresh-native-handle") {
				t.Error("next binding judgment did not receive the just-created handle")
			}
		}
		if _, ok := req.Questions["completion"]; ok {
			completions++
			var payload struct {
				Context json.RawMessage `json:"context"`
			}
			_ = json.Unmarshal(req.State, &payload)
			if !strings.Contains(string(payload.Context), "fresh-native-receipt") {
				t.Error("completion judgment used stale entry context")
			}
		}
		return declarationAnswers(req, true)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto", Learning: "frozen"}, client, coretool.Command{Name: "resource", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
		if ex.Args[0] == "create" {
			fmt.Fprint(ex.Stdout, `{"handle":"fresh-native-handle"}`)
		} else {
			fmt.Fprint(ex.Stdout, `{"receipt":"fresh-native-receipt"}`)
		}
		return nil, nil
	}})
	if err := e.contracts.Register(coretool.NativeContract{ID: "resource", Version: "1", Outcome: func(coretool.NativeCall, map[string]any) string { return "applied" }, Classify: func(c coretool.NativeCall) (coretool.NativeAccess, error) {
		if len(c.Argv) > 1 && c.Argv[0] == "resource" {
			if c.Argv[1] == "create" {
				return coretool.NativeEffect, nil
			}
			return coretool.NativeRead, nil
		}
		return coretool.NativeUnsupported, nil
	}}); err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"messages":[{"role":"user","text":"Create resource and inspect its receipt"},{"role":"assistant","calls":[{"id":"create","name":"bash","arguments":{"command":"resource create"}}]},{"role":"tool","call_id":"create","text":"{\"handle\":\"fresh-native-handle\"}"},{"role":"assistant","calls":[{"id":"inspect","name":"bash","arguments":{"command":"resource inspect fresh-native-handle"}}]},{"role":"tool","call_id":"inspect","text":"{\"receipt\":\"fresh-native-receipt\"}"}]}`)
	caps, err := e.capabilities(cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	r := Reflex{APIVersion: 2, When: "Create and inspect a resource", Decide: "Use its current handle", Steps: map[string]StepDefinition{"create": {Contract: "resource", Count: 1}}, Observe: `js:function(){const created=execute({name:"bash",arguments:{command:command("resource",["create"])},read:false,step:"create",occurrence:0});const read=execute({name:"bash",arguments:{command:command("resource",["inspect",created.data.handle])},read:true});return{report:{evidence:read.call_id,path:["data","receipt"]}};}`}
	if err := e.qualify(t.Context(), &r, caps, state); err != nil {
		t.Fatal(err)
	}
	e.library.Reflexes["resource"] = reflexRecord{Reflex: r}
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if req.Purpose != "composition" || len(req.Tools) != 0 {
			t.Fatalf("runtime unexpectedly used execution reasoning: %s", provider.MessageText(req.Messages[len(req.Messages)-1]))
		}
		return reply(provider.TextMessage("assistant", "fresh-native-receipt")), nil
	})
	_, err = agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Create resource and inspect its receipt"))
	if err != nil || bindings != 2 || completions != 1 {
		t.Fatalf("bindings=%d completions=%d err=%v", bindings, completions, err)
	}
}

func TestReflexV2CompilerAgentToolFeedback(t *testing.T) {
	e := testLaboratory(t)
	e.client = fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer { return declarationAnswers(req, true) })
	claims := map[string]Claim{"a": {Type: jevapi.ClaimNoul, Context: "Add requested items exactly once per occurrence."}, "b": {Type: jevapi.ClaimNoul, Context: "After an uncertain submission, query current completion or defer."}}
	r := laboratoryReflex()
	r.arguments = laboratorySuite().Cases(nil)[0].Arguments
	artifact := func(source string) string {
		return jsonText(map[string]any{"api_version": 2, "steps": r.Steps, "parameters_schema": r.Parameters, "observe": source, "arguments": r.arguments})
	}
	bad := artifact(strings.ReplaceAll(r.Observe, "occurrence:i", "occurrence:0"))
	good := artifact(r.Observe)
	requests := 0
	cfg := agent.Config{Model: "test", Provider: testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		requests++
		if len(req.Tools) != 2 || req.Tools[0].Name != "validate_reflex" || req.Tools[1].Name != "inspect_evidence" {
			t.Fatal("compiler acquired foreground capabilities")
		}
		if requests == 2 {
			text := coretool.ResultText(provider.MessageToolResult(req.Messages[len(req.Messages)-1]))
			if !strings.Contains(text, "diagnostic") {
				t.Error("agent did not see executable counterexample")
			}
		}
		if requests < 3 {
			source := bad
			if requests == 2 {
				source = good
			}
			args := jsonText(map[string]any{"artifact": json.RawMessage(source)})
			return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: aop.EnvelopeID(), Name: "validate_reflex", Arguments: &aop.EncodedValue{Data: []byte(args), MediaType: aop.JSONMediaType}}}}}}), nil
		}
		return reply(provider.TextMessage("assistant", "validated artifact submitted")), nil
	})}
	if err := qualifyIndependent(e, t.Context(), &r, observationCapabilities("bash")); err != nil {
		t.Fatal(err)
	}
	trajectory, _ := testTrajectories.Load(e)
	plan := &compilation{state: json.RawMessage(trajectory.([]byte)), job: declaration{cfg: cfg}, claims: claims, ids: []string{"a", "b"}, capabilities: observationCapabilities("bash"), input: map[string]any{}}
	compiler := e.newCompilerAgent(plan)
	var generated *Reflex
	if err := compiler.generate(t.Context(), plan.input, &generated); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || compiler.submissions != 2 || generated == nil || !e.qualified(reflexRecord{Reflex: *generated}) {
		t.Fatal("compiler Agent did not revise, validate and retain code")
	}
	if _, err := compiler.ExecuteTool(t.Context(), "bash", `{"command":"anything"}`); err != nil {
		t.Fatal(err)
	}
}

func TestReflexV2GroupingFailureAndBackgroundIsolation(t *testing.T) {
	compiling, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var e *Extension
	var selected []string
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		out := declarationAnswers(req, true)
		for id, q := range req.Questions {
			if strings.HasPrefix(id, "c") && id != "compile" {
				if strings.Contains(string(req.State), "unrelated") && strings.Contains(fmt.Sprint(q.Instructions), id) {
					out[id] = answer(Defer)
				}
			}
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	a := Claim{Type: jevapi.ClaimNoul, Context: "Create a queued operation."}
	b := Claim{Type: jevapi.ClaimNoul, Context: "Inspect uncertain completion of the queued operation."}
	unrelated := Claim{Type: jevapi.ClaimNoul, Context: "Translate prose."}
	ids := []string{}
	for _, c := range []Claim{a, b, unrelated} {
		id := "c" + digest(c)[:16]
		ids = append(ids, id)
		e.library.Claims[id] = claimRecord{Claim: c, Task: "old"}
	}
	client2 := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		out := declarationAnswers(req, true)
		for id := range req.Questions {
			if strings.HasPrefix(id, "claim") {
				out[id] = answer(Defer)
			}
		}
		out[ids[2]] = answer(Defer)
		return out
	})
	e.client = client2
	cfg.Provider = testProvider(func(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if strings.HasPrefix(provider.MessageText(req.Messages[0]), compilePrompt) {
			raw := provider.MessageText(req.Messages[1])
			if strings.Contains(raw, unrelated.Context) {
				t.Error("unrelated Claim entered compiler scope")
			}
			close(compiling)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return nil, errors.New("compiler provider unavailable")
		}
		return reply(provider.TextMessage("assistant", "ordinary task completed")), nil
	})
	job := declaration{cfg: cfg, task: "new", state: json.RawMessage(`{"messages":[{"role":"user","text":"create queued operation"}]}`), focus: []string{"create"}}
	plan, err := e.prepareCompilation(t.Context(), job, ids[0])
	if err != nil || plan == nil {
		t.Fatal(err)
	}
	for id := range plan.claims {
		selected = append(selected, id)
	}
	if len(selected) != 2 {
		t.Fatalf("group=%v", selected)
	}
	done := make(chan error, 1)
	go func() { done <- e.compile(t.Context(), job, ids[0]) }()
	select {
	case <-compiling:
	case <-time.After(time.Second):
		t.Fatal("compiler not started")
	}
	if err := e.compile(t.Context(), job, ids[0]); err != nil {
		t.Fatal("duplicate pending compilation did not defer")
	}
	// Ordinary execution remains independent while the compiler provider waits.
	result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Continue ordinary work"))
	if err != nil || result.Output != "ordinary task completed" {
		t.Fatal("compiler blocked foreground")
	}
	once.Do(func() { close(release) })
	if err := <-done; err == nil {
		t.Fatal("compiler failure hidden")
	}
	// Settle first-task learning before its inference server is closed. The
	// foreground result must return independently of compilation, as above.
	if err := e.WaitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(e.snapshot().Claims) != 3 || len(e.snapshot().Reflexes) != 0 {
		t.Fatal("failure consumed Claims or published source")
	}
	if e.beginCompilation(digest([]any{ids[1], nativeContracts(plan.capabilities)})) {
		t.Fatal("another group member bypassed failure cooldown")
	}
}

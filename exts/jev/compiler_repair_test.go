package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chainreactors/cyber/internal/jevwire"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func compilerReadPlan(t *testing.T, review func(jevwire.Request) map[string]jevwire.Answer) (*Extension, *compilation, string) {
	t.Helper()
	if review == nil {
		review = func(req jevwire.Request) map[string]jevwire.Answer { return declarationAnswers(req, true) }
	}
	executions := 0
	client := fakeJEV(t, review)
	e, cfg, _ := testInstallation(t, Config{Mode: "off"}, client, coretool.Command{Name: "lab", Run: func(context.Context, *coretool.Execution) (any, error) {
		executions++
		return nil, errors.New("compiler dispatched a foreground tool")
	}})
	e.client = client
	t.Cleanup(func() {
		if executions != 0 {
			t.Error("compiler executed real tools")
		}
	})
	if err := testVerification(e).Register(laboratorySuite()); err != nil {
		t.Fatal(err)
	}
	actor := "当前 'quoted' \\ path"
	state := json.RawMessage(jsonText(map[string]any{"messages": []any{
		map[string]any{"role": "user", "text": "Read the current native receipt for " + actor},
		map[string]any{"role": "assistant", "calls": []any{map[string]any{"id": "actual-read", "name": "bash", "arguments": map[string]any{"command": map[string]any{"name": "lab", "argv": []string{"status", actor}}}}}},
		map[string]any{"role": "tool", "name": "bash", "call_id": "actual-read", "text": `{"complete":true,"count":2,"receipt":"current-native-receipt"}`},
	}}))
	caps, err := e.capabilities(cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	input, err := compilerInput(state, caps)
	if err != nil {
		t.Fatal(err)
	}
	claim := Claim{Type: jevapi.ClaimNoul, Context: "Read the current receipt using the requested actor."}
	id := "c" + digest(claim)[:16]
	e.library.Claims[id] = claimRecord{Claim: claim}
	return e, &compilation{job: declaration{cfg: cfg}, claims: map[string]Claim{id: claim}, ids: []string{id}, capabilities: caps, state: state, input: map[string]any{"input": input}}, actor
}

func compilerReadArtifact(actor string) map[string]any {
	return map[string]any{"api_version": 2, "steps": map[string]any{}, "parameters_schema": json.RawMessage(`{"type":"object","required":["actor"],"properties":{"actor":{"type":"string"}},"additionalProperties":false}`), "arguments": map[string]any{"actor": actor}, "observe": `js:function(context,args){if(!args||!args.actor)return{defer:"missing current arguments",parameters:"actor"};const r=execute({name:"bash",arguments:{command:command("lab",["status",args.actor])},read:true});return{report:{evidence:r.call_id,path:["data","receipt"]}};}`}
}

func compilerTool(name string, args any) *aop.Message {
	return &aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: aop.EnvelopeID(), Name: name, Arguments: &aop.EncodedValue{MediaType: aop.JSONMediaType, Data: []byte(jsonText(args))}}}}}}
}

func TestReflexV2CompilerRepairsPastOldLimits(t *testing.T) {
	for _, mode := range []string{"tool", "final_text"} {
		t.Run(mode, func(t *testing.T) {
			e, plan, actor := compilerReadPlan(t, nil)
			requests, sawDiagnostic, sawExactEvidence := 0, false, false
			plan.job.cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				requests++
				if !strings.Contains(provider.MessageText(req.Messages[0]), "name: reflex-compiler") {
					t.Fatal("runtime compiler skill not loaded")
				}
				for _, m := range req.Messages {
					text := provider.MessageText(m)
					if r := provider.MessageToolResult(m); r != nil {
						text = coretool.ResultText(r)
					}
					sawDiagnostic = sawDiagnostic || strings.Contains(text, "native_call_mismatch") && strings.Contains(text, "expected") && strings.Contains(text, "actual")
					sawExactEvidence = sawExactEvidence || strings.Contains(text, "decoded_argv") && strings.Contains(text, "current-native-receipt")
				}
				if requests == 1 {
					return reply(compilerTool("inspect_evidence", map[string]any{})), nil
				}
				value := actor
				if requests <= 13 {
					value += fmt.Sprintf("-wrong-%d", requests)
				}
				artifact := compilerReadArtifact(value)
				if mode == "tool" {
					return reply(compilerTool("validate_reflex", map[string]any{"artifact": artifact})), nil
				}
				return reply(provider.TextMessage("assistant", jsonText(artifact))), nil
			})
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			r, err := e.generateReflex(ctx, plan)
			if err != nil || r == nil || !e.qualified(reflexRecord{Reflex: *r}) || requests != 14 || !sawDiagnostic || !sawExactEvidence {
				t.Fatalf("repair failed: requests=%d diagnostic=%v evidence=%v artifact=%v err=%v", requests, sawDiagnostic, sawExactEvidence, r != nil, err)
			}
			if len(e.snapshot().Reflexes) != 0 {
				t.Fatal("generation bypassed publication boundary")
			}
			if err := e.publishReflex(plan, r); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReflexV2CompilerSemanticRejectionReturnsToAgent(t *testing.T) {
	reviews := 0
	e, plan, actor := compilerReadPlan(t, func(req jevwire.Request) map[string]jevwire.Answer {
		out := declarationAnswers(req, true)
		if _, ok := req.Questions["coverage_freshness"]; ok {
			reviews++
			if reviews == 1 {
				out["coverage_freshness"] = answer(Defer)
			}
		}
		return out
	})
	requests := 0
	plan.job.cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		requests++
		if requests == 2 {
			last := provider.MessageToolResult(req.Messages[len(req.Messages)-1])
			if last == nil || !strings.Contains(coretool.ResultText(last), "native_access_invalid") {
				t.Fatal("independent semantic rejection never reached compiler")
			}
		}
		return reply(compilerTool("validate_reflex", map[string]any{"artifact": compilerReadArtifact(actor)})), nil
	})
	r, err := e.generateReflex(t.Context(), plan)
	if err != nil || r == nil || requests != 2 || reviews != 2 {
		t.Fatalf("requests=%d reviews=%d err=%v", requests, reviews, err)
	}
}

func TestReflexV2CompilerCompactsAndContinuesRepair(t *testing.T) {
	e, plan, actor := compilerReadPlan(t, nil)
	plan.job.cfg.ContextWindow = 24000
	plan.job.cfg.Compaction.ReserveTokens = 8000
	plan.job.cfg.Compaction.KeepRecentTokens = 3000
	drafts, summaries := 0, 0
	plan.job.cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if provider.MessageText(req.Messages[0]) == compilerCompactionSystem {
			summaries++
			return reply(provider.TextMessage("assistant", "Repair checkpoint: previous drafts used the wrong actor. Reload inspect_evidence for exact current arguments. No artifact is qualified yet.")), nil
		}
		drafts++
		artifact := compilerReadArtifact(actor)
		if drafts <= 20 {
			artifact["arguments"] = map[string]any{"actor": "wrong actor"}
		}
		message := compilerTool("validate_reflex", map[string]any{"artifact": artifact})
		message.Content = append([]*aop.Content{{Value: &aop.Content_Text{Text: &aop.TextContent{Text: strings.Repeat("Investigating the exact mismatch and retaining repair history. ", 200)}}}}, message.Content...)
		return reply(message), nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	r, err := e.generateReflex(ctx, plan)
	if err != nil || r == nil || drafts != 21 || summaries == 0 || !e.qualified(reflexRecord{Reflex: *r}) {
		t.Fatalf("context compaction interrupted repair: drafts=%d summaries=%d qualified=%t err=%v", drafts, summaries, r != nil, err)
	}
}

func TestReflexV2CompilerRepairsFullyReplayedHandoff(t *testing.T) {
	e, plan, actor := compilerReadPlan(t, nil)
	requests := 0
	plan.job.cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		requests++
		artifact := compilerReadArtifact(actor)
		switch requests {
		case 1:
			artifact["observe"] = `js:function(context,args){if(!args||!args.actor)return{defer:'missing arguments',parameters:'actor'};execute({name:'bash',arguments:{command:command('lab',['status',args.actor])},read:true});return{defer:'waiting for old entry history'};}`
		case 2:
			last := provider.MessageToolResult(req.Messages[len(req.Messages)-1])
			if last == nil || !strings.Contains(coretool.ResultText(last), "completion_missing") {
				t.Fatal("fully replayed handoff never returned to the compiler for repair")
			}
		default:
			t.Fatal("accepted report did not complete compilation")
		}
		return reply(compilerTool("validate_reflex", map[string]any{"artifact": artifact})), nil
	})
	r, err := e.generateReflex(t.Context(), plan)
	if err != nil || r == nil || requests != 2 || !e.qualified(reflexRecord{Reflex: *r}) {
		t.Fatalf("handoff repair did not converge: requests=%d err=%v", requests, err)
	}
}

func TestReflexV2CompilerCancellationPreservesRejectedDraft(t *testing.T) {
	e, plan, actor := compilerReadPlan(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	requests := 0
	plan.job.cfg.Provider = testProvider(func(_ context.Context, _ *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		requests++
		if requests == 2 {
			cancel()
			return nil, ctx.Err()
		}
		return reply(compilerTool("validate_reflex", map[string]any{"artifact": compilerReadArtifact(actor + "-wrong")})), nil
	})
	r, err := e.generateReflex(ctx, plan)
	if !errors.Is(err, context.Canceled) || r != nil {
		t.Fatalf("artifact=%v err=%v", r, err)
	}
	lib := e.snapshot()
	if len(lib.Reflexes) != 0 || len(lib.Candidates) != 1 {
		t.Fatal("cancellation published source or discarded repair progress")
	}
	for _, r := range lib.Candidates {
		if r.Proof != nil || !strings.Contains(r.Blocker, "differs") {
			t.Fatal("candidate lost precise failure")
		}
	}
}

func TestReflexV2CompilerFormatFailuresAreRepairable(t *testing.T) {
	for _, mode := range []string{"malformed_tool", "null_tool", "malformed_final"} {
		t.Run(mode, func(t *testing.T) {
			e, plan, actor := compilerReadPlan(t, nil)
			requests := 0
			plan.job.cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				requests++
				if requests == 1 {
					switch mode {
					case "malformed_tool":
						return reply(compilerTool("validate_reflex", map[string]any{"artifact": map[string]any{"observe": true}})), nil
					case "null_tool":
						return reply(compilerTool("validate_reflex", map[string]any{"artifact": nil})), nil
					default:
						return reply(provider.TextMessage("assistant", "{invalid json")), nil
					}
				}
				last := req.Messages[len(req.Messages)-1]
				feedback := provider.MessageText(last)
				if result := provider.MessageToolResult(last); result != nil {
					feedback = coretool.ResultText(result)
				}
				if !strings.Contains(feedback, "diagnostic") || !strings.Contains(feedback, "action") {
					t.Fatal("format error lost structured repair feedback")
				}
				return reply(compilerTool("validate_reflex", map[string]any{"artifact": compilerReadArtifact(actor)})), nil
			})
			r, err := e.generateReflex(t.Context(), plan)
			if err != nil || r == nil || requests != 2 {
				t.Fatalf("requests=%d artifact=%v err=%v", requests, r != nil, err)
			}
		})
	}
}

func TestReflexV2CompilerMissingEvidenceWaitsWithoutPublishing(t *testing.T) {
	for _, stage := range []string{"mechanism", "semantic"} {
		t.Run(stage, func(t *testing.T) {
			e, plan, actor := compilerReadPlan(t, func(req jevwire.Request) map[string]jevwire.Answer {
				out := declarationAnswers(req, true)
				out["compile"], out["defect"] = answer(Defer), answer(Defer)
				return out
			})
			if stage == "mechanism" {
				plan.state = nil
			}
			requests := 0
			plan.job.cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				requests++
				if requests > 1 {
					t.Fatal("missing evidence caused an empty repair loop")
				}
				return reply(compilerTool("validate_reflex", map[string]any{"artifact": compilerReadArtifact(actor)})), nil
			})
			r, err := e.generateReflex(t.Context(), plan)
			lib := e.snapshot()
			if err != nil || r != nil || len(lib.Reflexes) != 0 || len(lib.Candidates) != 1 {
				t.Fatalf("qualified=%d candidates=%d artifact=%v err=%v", len(lib.Reflexes), len(lib.Candidates), r != nil, err)
			}
		})
	}
}

func TestReflexV2CompilerTimeoutIsOptional(t *testing.T) {
	if c := defaults(Config{}); c.CompilationTimeout != "0" || c.validate() != nil {
		t.Fatal("compilation remains subject to an implicit deadline")
	}
	for _, value := range []string{"0", "30m"} {
		if err := (Config{CompilationTimeout: value}).validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"-1s", "wrong"} {
		if err := (Config{CompilationTimeout: value}).validate(); err == nil {
			t.Fatal("invalid compilation duration accepted")
		}
	}
}

func TestReflexV2CompilerEvidenceUpdateMatchesRuntimeHistory(t *testing.T) {
	e, plan, actor := compilerReadPlan(t, nil)
	latest := plan.state
	plan.job.task = "repair-current-evidence"
	plan.state = json.RawMessage(`{"messages":[{"role":"user","text":"Read current receipt"}]}`)
	e.queued[plan.job.task] = declaration{task: plan.job.task, state: latest}
	compiler := e.newCompilerAgent(plan)
	result, err := compiler.ExecuteTool(t.Context(), "validate_reflex", jsonText(map[string]any{"artifact": compilerReadArtifact(actor + "-wrong")}))
	feedback := coretool.ResultText(result)
	if err != nil || !strings.Contains(feedback, "current_evidence") || !strings.Contains(feedback, "current-native-receipt") {
		t.Fatalf("new actual evidence was hidden: err=%v feedback=%s", err, feedback)
	}
	runtimeInput, err := observeInput(latest, plan.capabilities)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := compiler.ExecuteTool(t.Context(), "inspect_evidence", `{}`)
	if err != nil || !strings.Contains(coretool.ResultText(inspection), "decoded_argv") {
		t.Fatal("inspection lost normalized native argv")
	}
	r := observationReflex(t, `js:function(context){return{report:context.history[0].decoded_argv[2]};}`)
	out, err := runReflexJS(t.Context(), &r, runtimeInput, nil, nil, nil)
	if err != nil || out[report] != actor {
		t.Fatalf("runtime and compiler evidence ABI diverged: output=%v err=%v", out, err)
	}
}

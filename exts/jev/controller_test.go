package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestPendingEffectStaysWithReflexUntilReport(t *testing.T) {
	var executed, reads, decisions, declarations atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if !runtimeRequest(req) {
			declarations.Add(1)
			return runtimeAnswers(req, Defer)
		}
		n := decisions.Add(1)
		var state struct {
			Candidates map[string]string `json:"candidates"`
			Reads      map[string]bool   `json:"reads"`
		}
		if err := json.Unmarshal(req.State, &state); err != nil {
			t.Error(err)
		}
		if n == 1 && candidateBySuffix(state.Candidates, "async/go") == "" {
			t.Error("generation judgment cannot see current argument bindings")
		}
		if n == 1 && len(state.Reads) != 0 {
			t.Error("effect was mislabeled as an inspection")
		}
		if n > 1 && n < 6 {
			valid := len(state.Reads) == 1
			for key := range state.Candidates {
				valid = valid && strings.HasSuffix(key, "/async/status") && state.Reads[key]
			}
			if !valid || len(state.Candidates) != 1 {
				t.Error("generation judgment lost the bound inspection classification")
			}
		}
		for id, q := range req.Questions {
			if !strings.HasPrefix(id, "r") {
				continue
			}
			for key, description := range q.Criteria.(map[string]any) {
				if binding, ok := state.Candidates[key]; ok && !strings.Contains(description.(string), binding) {
					t.Error("finite alternative omitted its executable meaning")
				}
			}
		}
		if n > 1 && candidateBySuffix(state.Candidates, "async/go") != "" {
			t.Error("already dispatched binding remained in shared state")
		}
		if _, entry := req.Questions["entry"]; entry != (n == 1) {
			t.Errorf("scene entry re-evaluated during an active Reflex: decision=%d entry=%t", n, entry)
		}
		switch n {
		case 1:
			return runtimeAnswers(req, "async/go")
		case 2, 3, 4, 5:
			for id, q := range req.Questions {
				if id != "entry" {
					for key := range q.Criteria.(map[string]any) {
						if strings.HasSuffix(key, "/async/go") {
							t.Error("already dispatched action remained selectable")
						}
					}
				}
			}
			return runtimeAnswers(req, "async/status")
		default:
			return runtimeAnswers(req, report)
		}
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "async", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			executed.Add(1)
			_, err := fmt.Fprint(ex.Stdout, "effect dispatched")
			return nil, err
		},
	}, coretool.Command{Name: "status", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
		if executed.Load() != 1 {
			t.Error("read before dispatch")
		}
		receipt := "pending"
		if reads.Add(1) == 4 {
			receipt = "completed"
		}
		_, err := fmt.Fprint(ex.Stdout, receipt)
		return nil, err
	}})
	installObserve(e, `js:(() => {
const dispatched = messages.some(m => m.text === "effect dispatched");
const completed = messages.some(m => m.text === "completed");
return {state: {receipt: completed ? "completed" : dispatched ? "pending" : "not started"}, candidates: completed ? {} : dispatched ? {"async/status": bind("bash", {command: "status"}, true)} : {"async/go": bind("bash", {command: "async"}, false)}};
})()`)
	var modelCalls atomic.Int64
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		modelCalls.Add(1)
		last := provider.MessageText(req.Messages[len(req.Messages)-1])
		if !strings.Contains(last, "REPORT:") || !strings.Contains(last, `"receipt":"completed"`) {
			t.Errorf("premature model handoff: %s", last)
		}
		return reply(provider.TextMessage("assistant", "completed")), nil
	})
	result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Complete the asynchronous operation"))
	if err != nil || result.Output != "completed" || executed.Load() != 1 || reads.Load() != 4 || modelCalls.Load() != 1 {
		t.Fatalf("result=%v err=%v executions=%d reads=%d model=%d", result, err, executed.Load(), reads.Load(), modelCalls.Load())
	}
	settle(t, e)
	if declarations.Load() != 0 {
		t.Fatalf("a reported scene's final prose triggered discovery: %d", declarations.Load())
	}
}

func TestNativeEvidenceSurvivesModelHandoff(t *testing.T) {
	job := aop.EnvelopeID()
	var prepared, supplied, finished, model atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if !runtimeRequest(req) {
			return runtimeAnswers(req, Defer)
		}
		var state struct {
			Observations map[string]json.RawMessage `json:"observations"`
		}
		_ = json.Unmarshal(req.State, &state)
		for _, raw := range state.Observations {
			var observed struct {
				Job, Value, User string
				Complete         bool
			}
			_ = json.Unmarshal(raw, &observed)
			if observed.User != "Complete the authorized job with the missing input" {
				t.Error("a controller receipt replaced the actual user request")
			}
			if observed.Complete {
				return runtimeAnswers(req, report)
			}
			if observed.Job == "" {
				return runtimeAnswers(req, "work/prepare")
			}
			if observed.Value != "" {
				return runtimeAnswers(req, "work/finish")
			}
		}
		return runtimeAnswers(req, Defer)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client,
		coretool.Command{Name: "prepare", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			prepared.Add(1)
			_, err := fmt.Fprintf(ex.Stdout, `{"job":%q}`, job)
			return nil, err
		}},
		coretool.Command{Name: "supply", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			supplied.Add(1)
			_, err := fmt.Fprint(ex.Stdout, `{"value":"current-input"}`)
			return nil, err
		}},
		coretool.Command{Name: "finish", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			finished.Add(1)
			if len(ex.Args) != 2 || ex.Args[0] != job || ex.Args[1] != "current-input" {
				return nil, fmt.Errorf("wrong native binding: %v", ex.Args)
			}
			_, err := fmt.Fprint(ex.Stdout, `{"complete":true}`)
			return nil, err
		}})
	installObserve(e, `js:(() => {
const prepare = history.find(result => result.arguments.command === 'prepare');
const supply = history.find(result => result.arguments.command === 'supply');
const finish = history.find(result => result.arguments.command.startsWith('finish '));
const job = prepare ? prepare.data.job : '';
const value = supply ? supply.data.value : '';
const complete = !!(finish && finish.data.complete);
return {state: {job: job, value: value, complete: complete, user: user},
candidates: complete ? {} : job === '' ? {'work/prepare': bind('bash', {command:'prepare'}, false)} :
value !== '' ? {'work/finish': bind('bash', {command:'finish ' + quote(job) + ' ' + quote(value)}, false)} : {}};
})()`)
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if model.Add(1) == 1 {
			return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{action("supply")}}), nil
		}
		if finished.Load() != 1 || !strings.Contains(provider.MessageText(req.Messages[len(req.Messages)-1]), "REPORT:") {
			t.Error("native evidence was lost across the model's tool batch")
		}
		return reply(provider.TextMessage("assistant", "complete")), nil
	})
	result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Complete the authorized job with the missing input"))
	if err != nil || result.Output != "complete" || prepared.Load() != 1 || supplied.Load() != 1 || finished.Load() != 1 || model.Load() != 2 {
		t.Fatalf("result=%v error=%v prepare=%d supply=%d finish=%d model=%d", result, err, prepared.Load(), supplied.Load(), finished.Load(), model.Load())
	}
	settle(t, e)
	if len(e.tasks) != 0 {
		t.Fatal("completed run retained private native evidence")
	}
}

func TestPendingObservationStopsAtDecisionBudget(t *testing.T) {
	var decisions, reads, modelCalls atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if runtimeRequest(req) {
			decisions.Add(1)
			return runtimeAnswers(req, "pending/read")
		}
		return runtimeAnswers(req, Defer)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "pending", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			reads.Add(1)
			_, err := fmt.Fprint(ex.Stdout, "pending")
			return nil, err
		},
	})
	installObserve(e, `js:({state: {receipt: "pending"}, candidates: {"pending/read": bind("bash", {command: "pending"}, true)}})`)
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		modelCalls.Add(1)
		return reply(provider.TextMessage("assistant", "The effect is still pending.")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Report when the pending operation completes")); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if decisions.Load() != maxDecisions || reads.Load() != maxDecisions || modelCalls.Load() != 1 {
		t.Fatalf("decisions=%d reads=%d model=%d", decisions.Load(), reads.Load(), modelCalls.Load())
	}
}

func TestCommandErrorYieldsToModelWithoutReplay(t *testing.T) {
	var attempts atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if attempts.Load() >= 2 {
			return runtimeAnswers(req, report)
		}
		return runtimeAnswers(req, "fresh/go")
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "fresh", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			if attempts.Add(1) == 1 {
				return nil, coretool.ErrStaleChoice
			}
			_, err := fmt.Fprint(ex.Stdout, "fresh-result")
			return nil, err
		},
	})
	installReflex(e, "fresh")
	var modelCalls atomic.Int64
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		modelCalls.Add(1)
		if text := provider.MessageText(req.Messages[len(req.Messages)-1]); !strings.Contains(text, "Attempted (tool error;") || !strings.Contains(text, "outcome requires model review") {
			t.Errorf("failed tool call was not handed back: %s", text)
		}
		return reply(provider.TextMessage("assistant", "done")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Perform the operation")); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if attempts.Load() != 1 || modelCalls.Load() != 1 {
		t.Fatalf("attempts=%d model=%d", attempts.Load(), modelCalls.Load())
	}
}

func TestCompoundCommandWithEffectsCannotRecoverAsStale(t *testing.T) {
	var effects atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		return runtimeAnswers(req, "mixed/go")
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
		Name: "mixed", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			if len(ex.Args) != 0 {
				return nil, coretool.ErrStaleChoice
			}
			effects.Add(1)
			return nil, nil
		},
	})
	installObserve(e, constantObserve(`{}`, map[string]string{"mixed/go": "mixed; mixed stale"}))
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		return reply(provider.TextMessage("assistant", "Partial effects need review")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Perform the known operation")); err != nil {
		t.Fatal(err)
	}
	if effects.Load() != 1 {
		t.Fatalf("partial effects replayed %d times", effects.Load())
	}
}

func TestModelSuppliesMissingInputThenReflexResumes(t *testing.T) {
	for _, gap := range []string{"parameter", "strategy", Defer} {
		t.Run(gap, func(t *testing.T) {
			var ready atomic.Bool
			var position, modelCalls atomic.Int64
			client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
				if !ready.Load() {
					answers := runtimeAnswers(req, "workflow/go")
					if _, ok := req.Questions["generation"]; ok {
						answers["generation"] = answer(gap)
					}
					return answers // The closest action cannot override a generation gap.
				}
				if position.Load() == 3 {
					return runtimeAnswers(req, report)
				}
				return runtimeAnswers(req, "workflow/go")
			})
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client,
				coretool.Command{Name: "provide", Run: func(context.Context, *coretool.Execution) (any, error) {
					ready.Store(true)
					return nil, nil
				}},
				coretool.Command{Name: "workflow", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
					if !ready.Load() {
						t.Error("acted before missing input was supplied")
					}
					_, err := fmt.Fprintf(ex.Stdout, "step=%d", position.Add(1))
					return nil, err
				}})
			installReflex(e, "workflow")
			cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				if modelCalls.Add(1) == 1 {
					return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{action("provide")}}), nil
				}
				if position.Load() != 3 || !strings.Contains(provider.MessageText(req.Messages[len(req.Messages)-1]), "REPORT:") {
					t.Error("model retained control of the remaining workflow")
				}
				return reply(provider.TextMessage("assistant", "done")), nil
			})
			result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Complete the workflow"))
			if err != nil || result.Output != "done" || modelCalls.Load() != 2 {
				t.Fatalf("result=%v err=%v model=%d", result, err, modelCalls.Load())
			}
			settle(t, e)
		})
	}
}

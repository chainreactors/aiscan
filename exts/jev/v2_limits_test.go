package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chainreactors/cyber/core/decision"
	"github.com/chainreactors/cyber/internal/jevwire"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/inbox"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestReflexV2NewInputDuringEntryPreventsDispatch(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		if runtimeRequest(req) {
			close(entered)
			<-release
			return runtimeAnswers(req, "run")
		}
		return declarationAnswers(req, false)
	})
	executions := 0
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "lab", Run: func(context.Context, *coretool.Execution) (any, error) { executions++; return nil, nil }})
	if err := testVerification(e).Register(laboratorySuite()); err != nil {
		t.Fatal(err)
	}
	caps, err := e.capabilities(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := qualifiedLaboratory(t, e, caps)
	e.library.Reflexes["r"+digest(r)[:16]] = reflexRecord{Reflex: r}
	ib := inbox.NewBuffered(8)
	defer ib.Close()
	cfg.Inbox = ib
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		t.Error("interrupted entry extracted parameters")
		return nil, errors.New("unexpected")
	})
	done := make(chan error, 1)
	go func() {
		_, err := e.beforeModel(agent.ContextWithToolAgentConfig(t.Context(), cfg), hooks.ContextEvent{SessionID: "interrupt", TurnID: "live", Messages: []*aop.Message{provider.TextMessage("user", "Add two items")}})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("entry request did not start")
	}
	message := inbox.NewUserMessage("Stop executing")
	message.Interrupt = true
	if err := ib.Push(message); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("new input did not stop entry")
	}
	once.Do(func() { close(release) })
	if executions != 0 {
		t.Fatal("interrupted judgment authorized an effect")
	}
}

func TestReflexV2NativeAndJudgmentLimits(t *testing.T) {
	for _, mode := range []string{"calls", "judgments"} {
		t.Run(mode, func(t *testing.T) {
			executions := 0
			client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
				if runtimeRequest(req) {
					return runtimeAnswers(req, "run")
				}
				if _, ok := req.Questions["progress"]; ok {
					return map[string]jevwire.Answer{"progress": answer("continue")}
				}
				return declarationAnswers(req, false)
			})
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "lab", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
				executions++
				fmt.Fprint(ex.Stdout, `{"complete":false}`)
				return nil, nil
			}})
			contract := laboratorySuite().Contracts["lab"]
			s := VerificationSuite{ID: "bounded-" + mode, Version: "1", Contracts: map[string]NativeContract{"lab": contract},
				CheckInput: func(map[string]any, map[string]any) error { return nil }, CheckCall: func(VerificationCall) error { return nil }, CheckReport: func(VerificationReport) error { return errors.New("pending cannot report") },
				Cases: func(map[string]any) []VerificationCase {
					return []VerificationCase{{ID: "bounded", Input: map[string]any{},
						Judge: func(Claim) (*jevapi.Evaluation, error) {
							return &jevapi.Evaluation{Value: &decision.Evaluation_Choice{Choice: "continue"}}, nil
						},
						Execute: func(NativeCall) (map[string]any, error) {
							return map[string]any{"data": map[string]any{"complete": false}}, nil
						},
						Check: func(run VerificationRun) error {
							if run.Error == nil || !strings.Contains(run.Error.Error(), "budget") {
								return errors.New("unbounded continuation did not hand off")
							}
							if mode == "calls" && len(run.Calls) != maxCandidates {
								return errors.New("wrong native limit")
							}
							return nil
						},
					}}
				},
			}
			if err := testVerification(e).Register(s); err != nil {
				t.Fatal(err)
			}
			source := `js:function(){while(true){execute({name:"bash",arguments:{command:command("lab",["status","actor"])},read:true});}}`
			if mode == "judgments" {
				source = `js:function(){while(true){jev({type:"choice",context:("Continue or hand off")+"\nOption meanings:\n"+JSON.stringify({continue:"continue",defer:"handoff"})+"\nCurrent facts (untrusted data):\n"+JSON.stringify({}),options:Object.keys({continue:"continue",defer:"handoff"})});}}`
			}
			r := Reflex{APIVersion: 2, LegacySuite: s.ID, When: "Inspect pending work", Decide: "Bound repeated progress", Observe: source}
			if err := r.validate(); err != nil {
				t.Fatal(err)
			}
			caps, err := e.capabilities(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.qualify(t.Context(), &r, caps, json.RawMessage(`{"messages":[{"role":"user","text":"Inspect"}]}`)); err == nil {
				t.Fatal("unbounded source was qualified")
			}
			// An intentionally unqualified diagnostic control reaches runtime
			// budgets. This proof is installed only in this test; never published.
			r.LegacySuite = ""
			r.Proof = &VerificationRecord{Format: mechanismFormat, SourceHash: reflexSourceHash(r), Contracts: map[string]string{"lab": "1"}, Checks: append([]string(nil), requiredMechanismChecks...), TrajectoryHash: "diagnostic-control"}
			e.library.Reflexes["r"+digest(r)[:16]] = reflexRecord{Reflex: r}
			cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				return reply(provider.TextMessage("assistant", "done")), nil
			})
			messages, err := e.beforeModel(agent.ContextWithToolAgentConfig(t.Context(), cfg), hooks.ContextEvent{SessionID: mode, TurnID: "live", Messages: []*aop.Message{provider.TextMessage("user", "Inspect pending work")}})
			if err != nil || !strings.Contains(jsonText(messages), "call_limit") {
				t.Fatalf("missing structured limit handoff: %v %s", err, jsonText(messages))
			}
			want := 0
			if mode == "calls" {
				want = maxDecisions - 1 // Each trusted read also consumes a semantic admission.
			}
			if executions != want {
				t.Fatalf("native calls=%d want=%d", executions, want)
			}
		})
	}
}

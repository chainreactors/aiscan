package jev

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	coretool "github.com/chainreactors/cyber/core/tool"
	guardext "github.com/chainreactors/cyber/pkg/exts/guardrail"
	"google.golang.org/protobuf/proto"
)

func TestOffNeedsNoCapabilitiesAndAddsNothing(t *testing.T) {
	e := New(Config{Mode: "off"})
	set, err := extension.New(e)
	if err != nil {
		t.Fatal(err)
	}
	if err = set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if e.cancel != nil || len(e.subs) != 0 || e.client != nil {
		t.Fatal("off installed behavior")
	}
	if err = set.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAutoLoadsWithoutObserverProtocol(t *testing.T) {
	e := New(Config{Mode: "auto", Directory: t.TempDir()})
	client := fakeJEV(t, func(jevapi.Request) map[string]jevapi.Answer {
		t.Error("inactive extension called JEV")
		return nil
	})
	// A native executor does not need a command registry or an Observe protocol.
	set, err := extension.New(extension.Provided[*corehooks.Registry](corehooks.New()),
		extension.Provided[*jevapi.Client](client), e)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if e.cancel == nil || len(e.subs) == 0 {
		t.Fatal("generic executor failed to acquire controller hooks")
	}
	if err := set.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestTakeoverUsesGuardrailAndYieldsAfterDenial(t *testing.T) {
	var executions atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		return runtimeAnswers(req, "protected/go")
	})
	e, cfg, registry := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "protected", Run: func(context.Context, *coretool.Execution) (any, error) { executions.Add(1); return "executed", nil }})
	installReflex(e, "protected")
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if len(req.Messages) != 3 || req.Messages[2].Name != "jev" {
			t.Fatal("denial receipt missing")
		}
		if text := provider.MessageText(req.Messages[2]); !strings.Contains(text, "Attempted (tool error;") || strings.Contains(text, "Executed ") {
			t.Error("blocked call reported as an executed effect")
		}
		return reply(provider.TextMessage("assistant", "Action was denied.")), nil
	})
	guardClient := fakeJEV(t, func(jevapi.Request) map[string]jevapi.Answer {
		return map[string]jevapi.Answer{"action": answer("block")}
	})
	guards, err := extension.New(
		extension.Provided[*corehooks.Registry](registry),
		extension.Provided[*events.Stream](events.New()),
		extension.Provided[*jevapi.Client](guardClient),
		guardext.New(guardext.Config{Provider: "jev"}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := guards.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer guards.Close(t.Context())
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Perform the protected step if permitted.")); err != nil {
		t.Fatal(err)
	}

	if executions.Load() != 0 {
		t.Fatalf("denial bypass: executions=%d requests=%d", executions.Load(), client.Usage().Detail["requests"])
	}
	settle(t, e)
}

func TestLongContextProjectionPreservesConstraintsWithoutChangingHistory(t *testing.T) {
	messages := []*aop.Message{provider.TextMessage("system", "Only inspect target A"), provider.TextMessage("user", "Do not submit the form")}
	for i := 0; i < 30; i++ {
		messages = append(messages, provider.TextMessage("tool", strings.Repeat("evidence", 1000)))
	}
	copy := cloneMessages(messages)
	data, ok := contextState(messages)
	if !ok || len(data) > 24<<10 || !strings.Contains(string(data), "Do not submit") || strings.Contains(string(data), `"omitted_evidence":0`) {
		t.Fatalf("bad projection %d %v", len(data), ok)
	}
	for i := range messages {
		if !proto.Equal(copy[i], messages[i]) {
			t.Fatal("history rewritten")
		}
	}
	if _, ok = contextState([]*aop.Message{provider.TextMessage("user", strings.Repeat("constraint", 4000))}); ok {
		t.Fatal("oversized task constraints were silently truncated")
	}
}

// Every fresh task can bypass all four intermediate model decisions. No task
// -specific state survives, and a changing rendered system prompt is harmless.
func TestReflexShortCircuitsAcrossFreshTasks(t *testing.T) {
	for _, mode := range []string{"off", "auto"} {
		t.Run(mode, func(t *testing.T) {
			var position, observations atomic.Int64
			command := coretool.Command{Name: "advance", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
				if len(ex.Args) != 1 || ex.Args[0] != strconv.Itoa(int(position.Load())) {
					return nil, coretool.ErrStaleChoice
				}
				_, err := fmt.Fprintf(ex.Stdout, "step=%d", position.Add(1))
				return nil, err
			}}
			client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer { return runtimeAnswers(req, "advance/go") })
			e, cfg, _ := testInstallation(t, Config{Mode: mode}, client, command)
			installReflex(e, "advance")
			cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				if position.Load() == 4 {
					if mode == "auto" && !strings.Contains(provider.MessageText(req.Messages[len(req.Messages)-1]), `"step":4`) {
						t.Error("final observation was lost")
					}
					return reply(provider.TextMessage("assistant", "done")), nil
				}
				return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{action(fmt.Sprintf("advance %d", position.Load()))}}), nil
			})
			for n := 0; n < 3; n++ {
				position.Store(0)
				cfg.SystemPrompt = fmt.Sprintf("Complete the task. Current Time: %d", n)
				cfg.SessionID = fmt.Sprintf("task-%d", n)
				r, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Advance through four steps."))
				if err != nil || r.Output != "done" {
					t.Fatalf("result=%v error=%v", r, err)
				}
				want := 5
				if mode == "auto" {
					want = 1
				}
				if r.Turns != want {
					t.Fatalf("turns=%d want=%d", r.Turns, want)
				}
			}
			if mode == "off" && (observations.Load() != 0 || client.Usage().Detail["requests"] != 0) {
				t.Fatal("off performed work")
			}

		})
	}
}

func TestAllCapabilitiesShareOneCurrentDecision(t *testing.T) {
	var calls atomic.Int64
	makeCommand := func(name string) coretool.Command {
		return coretool.Command{Name: name, Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
			if name != "second" {
				t.Error("wrong capability chosen")
			}
			calls.Add(1)
			return nil, nil
		}}
	}
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if runtimeRequest(req) {
			for id, q := range req.Questions {
				if strings.HasPrefix(id, "r") {
					choices := q.Criteria.(map[string]any)
					hasFirst, hasSecond := false, false
					for key := range choices {
						hasFirst = hasFirst || strings.HasSuffix(key, "/first/go")
						hasSecond = hasSecond || strings.HasSuffix(key, "/second/go")
					}
					if choices[report] == nil || choices[Defer] == nil || (calls.Load() == 0 && (!hasFirst || !hasSecond)) {
						t.Error("missing live capability")
					}
				}
			}
		}
		return runtimeAnswers(req, "second/go")
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, makeCommand("first"), makeCommand("second"))
	installReflex(e, "first", "second")
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		return reply(provider.TextMessage("assistant", "done")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Use second")); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("wrong number of calls")
	}
}

func TestDeferAndInvalidAnswerLeaveHistoryUntouched(t *testing.T) {
	for _, choice := range []string{Defer, "unbound"} {
		t.Run(choice, func(t *testing.T) {
			client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
				return runtimeAnswers(req, choice)
			})
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "step", Run: func(context.Context, *coretool.Execution) (any, error) { t.Error("unexpected action"); return nil, nil }})
			installReflex(e, "step")
			cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				if len(req.Messages) != 2 || req.Messages[1].Name != "" || provider.MessageText(req.Messages[1]) != "Do the task" {
					t.Error("fallback changed history")
				}
				return reply(provider.TextMessage("assistant", "done")), nil
			})
			if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Do the task")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

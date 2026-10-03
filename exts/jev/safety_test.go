package jev

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/inbox"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestContextRewriterLeavesDecisionWithModel(t *testing.T) {
	for _, useHook := range []bool{false, true} {
		t.Run(fmt.Sprint(useHook), func(t *testing.T) {
			client := fakeJEV(t, func(jevapi.Request) map[string]jevapi.Answer {
				t.Error("controller must not act on a different request projection")
				return nil
			})
			_, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
			if useHook {
				hooks.Context.On(cfg.Hooks, "rewrite", func(_ context.Context, ev hooks.ContextEvent) (hooks.ContextResult, error) {
					return hooks.ContextResult{Messages: []*aop.Message{provider.TextMessage("user", "Updated task")}}, nil
				})
			} else {
				cfg.TransformContext = func([]*aop.Message) []*aop.Message {
					return []*aop.Message{provider.TextMessage("user", "Updated task")}
				}
			}
			cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				if provider.MessageText(req.Messages[len(req.Messages)-1]) != "Updated task" {
					t.Error("ordinary context projection was bypassed")
				}
				return reply(provider.TextMessage("assistant", "Done")), nil
			})
			if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Old task")); err != nil {
				t.Fatal(err)
			}
			if client.Usage().Detail["requests"] != 0 {
				t.Fatal("rewritten context produced takeover")
			}
		})
	}
}

func TestMediaConstraintsCannotSilentlyBecomeTextOnly(t *testing.T) {
	m := provider.TextMessage("user", "Use the target shown in this image")
	m.Content = append(m.Content, &aop.Content{Value: &aop.Content_Media{Media: &aop.MediaContent{Kind: "image"}}})
	if _, ok := contextState([]*aop.Message{m}); ok {
		t.Fatal("controller accepted task without its media constraints")
	}
}

func TestInputDuringDecisionStopsDispatch(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var executions atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if !runtimeRequest(req) {
			return runtimeAnswers(req, Defer)
		}
		if strings.Contains(string(req.State), "Stop executing") {
			return runtimeAnswers(req, Defer)
		}
		close(entered)
		<-release
		return runtimeAnswers(req, "step/go")
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "step", Run: func(context.Context, *coretool.Execution) (any, error) { executions.Add(1); return nil, nil }})
	installReflex(e, "step")
	ib := inbox.NewBuffered(8)
	cfg.Inbox = ib
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		return reply(provider.TextMessage("assistant", "Stopped as requested.")), nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Perform the finite step."))
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("controller did not enter")
	}
	msg := inbox.NewUserMessage("Stop executing steps")
	msg.Interrupt = true
	if err := ib.Push(msg); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("input failed to interrupt controller")
	}
	if executions.Load() != 0 {
		t.Fatal("dispatched after new input")
	}
}

func TestNoProgressYieldsWithinBound(t *testing.T) {
	var executions atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		return runtimeAnswers(req, "stuck/go")
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "stuck", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
		executions.Add(1)
		_, err := fmt.Fprint(ex.Stdout, "unchanged")
		return nil, err
	}}, coretool.Command{Name: "unrelated", Run: func(context.Context, *coretool.Execution) (any, error) { return nil, nil }})
	installReflex(e, "stuck")
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		return reply(provider.TextMessage("assistant", "No progress; another strategy is required.")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Try the known step.")); err != nil {
		t.Fatal(err)
	}
	if executions.Load() != 1 {
		t.Fatalf("no-progress count=%d", executions.Load())
	}
}

func TestObservationFailureDoesNotHideCompetingCapability(t *testing.T) {
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if runtimeRequest(req) {
			t.Error("incomplete observation reached runtime decision")
		}
		return runtimeAnswers(req, Defer)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client,
		coretool.Command{Name: "available", Run: func(context.Context, *coretool.Execution) (any, error) {
			t.Error("partial observation dispatched action")
			return nil, nil
		}},
		coretool.Command{Name: "failed", Run: func(context.Context, *coretool.Execution) (any, error) { return nil, nil }})
	installReflex(e, "available")
	installObserve(e, `js:({state: JSON.parse("invalid JSON"), candidates: {}})`)
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		return reply(provider.TextMessage("assistant", "ordinary fallback")), nil
	})
	result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Inspect both capabilities"))
	if err != nil || result.Output != "ordinary fallback" {
		t.Fatalf("fallback: %v %v", result, err)
	}
}

package jev

import (
	"context"
	"fmt"
	"github.com/chainreactors/cyber/internal/jevwire"
	"testing"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestContextRewriterLeavesDecisionWithModel(t *testing.T) {
	for _, useHook := range []bool{false, true} {
		t.Run(fmt.Sprint(useHook), func(t *testing.T) {
			client := fakeJEV(t, func(jevwire.Request) map[string]jevwire.Answer {
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

func TestEffectIdentityPreservesAllNativeJSONShapes(t *testing.T) {
	seen := map[string]bool{}
	for _, arguments := range []string{`[1]`, `[2]`, `null`, `{}`, `{"id":9007199254740993}`, `{"id":9007199254740992}`, `broken`, `{"id":1} trailing`} {
		key := canonical(&aop.ToolCall{Name: "native", Arguments: &aop.EncodedValue{Data: []byte(arguments)}})
		if key == "" || seen[key] {
			t.Fatalf("different native calls share an effect identity: %s", arguments)
		}
		seen[key] = true
	}
	call := func(arguments string) *aop.ToolCall {
		return &aop.ToolCall{Name: "native", Arguments: &aop.EncodedValue{Data: []byte(arguments)}}
	}
	if canonical(call(`{"b":2,"a":1}`)) != canonical(call(`{"a":1,"b":2}`)) {
		t.Fatal("object field order changed native effect identity")
	}
}

func TestMediaConstraintsCannotSilentlyBecomeTextOnly(t *testing.T) {
	m := provider.TextMessage("user", "Use the target shown in this image")
	m.Content = append(m.Content, &aop.Content{Value: &aop.Content_Media{Media: &aop.MediaContent{Kind: "image"}}})
	if _, ok := contextState([]*aop.Message{m}, 32<<10); ok {
		t.Fatal("controller accepted task without its media constraints")
	}
}

func TestObservationFailureDoesNotHideCompetingCapability(t *testing.T) {
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		if runtimeRequest(req) && req.Questions["entry"].Type == "" {
			t.Error("unselected broken program reached runtime decision")
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

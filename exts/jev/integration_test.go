package jev

import (
	"context"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	coretool "github.com/chainreactors/cyber/core/tool"
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
	client := fakeJEV(t, func(inferenceRequest) map[string]inferenceAnswer {
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

func TestLongContextProjectionPreservesConstraintsWithoutChangingHistory(t *testing.T) {
	messages := []*aop.Message{provider.TextMessage("system", "Only inspect target A"), provider.TextMessage("user", "Do not submit the form")}
	for i := 0; i < 30; i++ {
		messages = append(messages, provider.TextMessage("tool", strings.Repeat("evidence", 1000)))
	}
	copy := cloneMessages(messages)
	data, ok := contextState(messages, 32<<10)
	if !ok || len(data) > 32<<10 || !strings.Contains(string(data), "Do not submit") || strings.Contains(string(data), `"omitted_evidence":0`) {
		t.Fatalf("bad projection %d %v", len(data), ok)
	}
	for i := range messages {
		if !proto.Equal(copy[i], messages[i]) {
			t.Fatal("history rewritten")
		}
	}
	if _, ok = contextState([]*aop.Message{provider.TextMessage("user", strings.Repeat("constraint", 4000))}, 32<<10); ok {
		t.Fatal("oversized task constraints were silently truncated")
	}
}

// Every fresh task can bypass all four intermediate model decisions. No task
// -specific state survives, and a changing rendered system prompt is harmless.

func TestDeferAndInvalidAnswerLeaveHistoryUntouched(t *testing.T) {
	for _, choice := range []string{Defer, "unbound"} {
		t.Run(choice, func(t *testing.T) {
			client := fakeJEV(t, func(req inferenceRequest) map[string]inferenceAnswer {
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

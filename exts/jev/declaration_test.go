package jev

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/chainreactors/cyber/internal/jevwire"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestClaimOnlyFeedsCompilationAndNeverRunsWithoutReflex(t *testing.T) {
	var judgments atomic.Int64
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		if runtimeRequest(req) {
			judgments.Add(1)
			return runtimeAnswers(req, "advance")
		}
		return declarationAnswers(req, false)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "step", Run: func(context.Context, *coretool.Execution) (any, error) { return "ok", nil }})
	var calls atomic.Int64
	cfg.Provider = testProvider(func(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if provider.MessageText(req.Messages[0]) == claimPrompt {
			return reply(provider.TextMessage("assistant", fixtureClaim)), nil
		}
		n := calls.Add(1)
		if n == 1 {
			if err := e.WaitIdle(ctx); err != nil {
				return nil, err
			}
		}
		if n < 3 {
			return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{action("step")}}), nil
		}
		return reply(provider.TextMessage("assistant", "done")), nil
	})
	cfg.SessionID = "first"
	result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Advance the task."))
	if err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	receipts := 0
	for _, m := range result.Messages {
		if m.Name == "jev" {
			receipts++
			if !strings.Contains(provider.MessageText(m), "requires review") {
				t.Error("judgment presented as fact")
			}
		}
	}
	if receipts != 0 || judgments.Load() != 0 {
		t.Fatalf("receipts=%d judgments=%d", receipts, judgments.Load())
	}
	cfg.SessionID = "second"
	if _, err = agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Advance the task.")); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if judgments.Load() != 0 || len(e.snapshot().Claims) != 1 {
		t.Fatal("same declaration was recreated or reused")
	}
}

func TestGeneratedDeclarationsRejectUnknownFieldsAndCapabilities(t *testing.T) {
	for _, output := range []string{
		`{"when":"x","decide":"y","sources":["invented"]}`,
		`{"when":"x","decide":"y","observe":"{state: {}, candidates: {}}","script":"execute()"}`,
		`{"when":"x","decide":"y","observe":"{state: {}, candidates: {go: {name: 'invented', arguments: {}}}}"}`,
		`{"when":"x","decide":"y","observe":"{state: {}, candidates: {go: {name: 'bash', arguments: nil}}}"}`,
		`{"when":"x","decide":"y","observe":"ExecuteTool('bash', '{}')"}`,
	} {
		t.Run(output, func(t *testing.T) {
			client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer { return declarationAnswers(req, true) })
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
			requests := 0
			cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				requests++
				if requests > 1 {
					return nil, errors.New("invalid-format fixture has no further drafts")
				}
				return reply(provider.TextMessage("assistant", output)), nil
			})
			var c []Claim
			_ = json.Unmarshal([]byte(fixtureClaim), &c)
			id := "c" + digest(c[0])[:16]
			e.mu.Lock()
			e.library.Claims[id] = claimRecord{Claim: c[0]}
			e.mu.Unlock()
			if err := e.compile(t.Context(), declaration{cfg: cfg}, id); err == nil {
				t.Fatal("invalid Reflex accepted")
			}
			if len(e.snapshot().Reflexes) != 0 {
				t.Fatal("invalid Reflex published")
			}
		})
	}
}

func TestCloseCancelsBackgroundModelCall(t *testing.T) {
	entered := make(chan struct{})
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer { return declarationAnswers(req, false) })
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	cfg.Provider = testProvider(func(ctx context.Context, _ *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	e.enqueue(cfg, hooks.ContextEvent{SessionID: "s", TurnID: "t", Messages: []*aop.Message{provider.TextMessage("assistant", "Finite decision")}})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("background call did not start")
	}
	e.enqueue(cfg, hooks.ContextEvent{SessionID: "queued", TurnID: "t", Messages: []*aop.Message{provider.TextMessage("assistant", "Queued boundary")}})
	e.enqueue(cfg, hooks.ContextEvent{SessionID: "queued", TurnID: "t", Messages: []*aop.Message{provider.TextMessage("assistant", "Latest queued boundary")}})
	e.mu.Lock()
	pending := e.pending
	e.mu.Unlock()
	if pending != 2 {
		t.Fatalf("queued snapshots were not merged: pending=%d", pending)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := e.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if e.pending != 0 || len(e.queue) != 0 || len(e.queued) != 0 {
		t.Fatal("close did not settle and drain admitted work")
	}
}

func TestExistingSceneSkipsPageActionDeclarations(t *testing.T) {
	var discovered atomic.Int64
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		out := map[string]jevwire.Answer{}
		for id, q := range req.Questions {
			if !strings.HasPrefix(id, "claim") {
				t.Errorf("unexpected compilation request %s", id)
			}
			out[id] = answer(Defer)
			var options map[string]json.RawMessage
			_ = json.Unmarshal(q.Criteria, &options)
			for key := range options {
				if strings.HasPrefix(key, "r") {
					out[id] = answer(key)
					discovered.Add(1)
				}
			}
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	_ = testVerification(e).Register(laboratorySuite())
	r := laboratoryReflex()
	_ = r.validate()
	if err := qualifyIndependent(e, t.Context(), &r, observationCapabilities("bash")); err != nil {
		t.Fatal(err)
	}
	e.library.Reflexes["r"+digest(r)[:16]] = reflexRecord{Reflex: r}
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		t.Error("existing scene caused another model generation")
		return reply(provider.TextMessage("assistant", "[]")), nil
	})
	for _, focus := range []string{"click the current control", "fill a field", "wait for an async update"} {
		if err := e.declare(t.Context(), declaration{cfg: cfg, task: "task", state: json.RawMessage(`{}`), focus: []string{focus}}); err != nil {
			t.Fatal(err)
		}
	}
	if discovered.Load() != 3 || len(e.snapshot().Claims) != 0 || len(e.snapshot().Reflexes) != 1 {
		t.Fatal("page actions changed the scene library")
	}
}

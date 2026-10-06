package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/chainreactors/cyber/internal/jevwire"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
)

func TestIdleAutoPreservesOrdinaryModelPrompt(t *testing.T) {
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		out := map[string]jevwire.Answer{}
		for id := range req.Questions {
			out[id] = answer(Defer)
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	calls := 0
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		calls++
		if len(req.Messages) != 2 || provider.MessageText(req.Messages[0]) != cfg.SystemPrompt || provider.MessageText(req.Messages[1]) != "Answer directly" {
			t.Error("idle acceleration expanded or rewrote the model context")
		}
		return reply(provider.TextMessage("assistant", "done")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Answer directly")); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if calls != 1 {
		t.Fatalf("model calls=%d", calls)
	}
	received := receipt([]string{"Executed a native operation"}, "evidence.jsonl", "REPORT")
	if len(received) != 1 || !strings.Contains(provider.MessageText(received[0]), Prompt) || len(receipt(nil, "", "")) != 0 {
		t.Fatal("controller guidance must accompany actual handoff evidence")
	}
}

func TestJEVDefersRepairBeforeAnyLLMGeneration(t *testing.T) {
	judgments, generated := 0, 0
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		judgments++
		if !strings.Contains(fmt.Sprint(req.Questions["compile"].Instructions), "recorded handoff BEFORE") || !strings.Contains(string(req.State), "redundant verification") {
			t.Error("repair judgment lost its specific pre-supplementation evidence")
		}
		out := declarationAnswers(req, true)
		out["compile"] = answer(Defer)
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	c := choiceClaim("Current native workflow"+". "+"Which operation advances it?", map[string]string{"operate": "Use current bindings", Defer: "Missing facts"})
	cid := "c" + digest(c)[:16]
	r := Reflex{When: c.Context, Decide: "Report actual evidence", Observe: normalizeFixture(`js:({state:{},candidates:{}})`)}
	rid := "r" + digest(r)[:16]
	e.library.Claims[cid] = claimRecord{Claim: c}
	e.library.Reflexes[rid] = reflexRecord{Reflex: r, Claims: []string{cid}}
	before := digest(e.snapshot())
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		generated++
		return reply(provider.TextMessage("assistant", "null")), nil
	})
	job := declaration{cfg: cfg, repair: rid, state: json.RawMessage(`{"messages":[{"role":"user","text":"Get current result"}]}`), handoff: json.RawMessage(`{"observations":{"result":"complete; later read was redundant verification"},"candidates":{}}`)}
	if err := e.compile(t.Context(), job, cid); err != nil {
		t.Fatal(err)
	}
	if judgments != 1 || generated != 0 || digest(e.snapshot()) != before {
		t.Fatalf("JEV repair defer was bypassed: judgments=%d generated=%d", judgments, generated)
	}
}

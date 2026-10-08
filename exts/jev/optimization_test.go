package jev

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/aop"
)

func TestReceiptCompactionPreservesOutcomesAndExactArguments(t *testing.T) {
	call := &aop.ToolCall{Name: "native", Arguments: &aop.EncodedValue{Data: []byte(`{"id":9007199254740993,"source":"function bind(){};function choices(){};private_reader_code"}`)}}
	compact := receiptBinding(call)
	if !strings.Contains(compact, "9007199254740993") || strings.Contains(compact, "private_reader_code") {
		t.Fatalf("receipt changed arguments or repeated reader source: %s", compact)
	}
	read := "Inspected " + compact + "\n{\"state\":\"fresh\"}"
	effect, failure := "Executed native effect", "Attempted (tool error; outcome requires review) native"
	text := provider.MessageText(receipt([]string{read, read, effect, effect, failure, failure}, "evidence.jsonl", "REPORT")[0])
	if strings.Count(text, read) != 1 || strings.Count(text, effect) != 2 || strings.Count(text, failure) != 2 {
		t.Fatal("receipt suppressed effects/errors or repeated identical read evidence")
	}
	result := resultSummary(`{"state":{"receipt":"current","id":9007199254740993},"candidates":[{"name":"native","arguments":{"source":"private_reader_code"},"read":true}]}`)
	if !strings.Contains(result, "current") || !strings.Contains(result, "9007199254740993") || !strings.Contains(result, "candidates") || !strings.Contains(result, "private_reader_code") {
		t.Fatalf("receipt altered native result data: %s", result)
	}
	if got := resultSummary("tool failed: missing permission"); !strings.Contains(got, "missing permission") {
		t.Fatal("failure evidence lost")
	}
}

func TestCompilationCooldownCoversOtherClaimAndSession(t *testing.T) {
	groups := 0
	client := fakeJEV(t, func(req inferenceRequest) map[string]inferenceAnswer { groups++; return declarationAnswers(req, true) })
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	var claims []Claim
	_ = json.Unmarshal([]byte(fixtureClaim), &claims)
	other := claims[0]
	other.Context = "Should the current operation continue?"
	ids := []string{"c" + digest(claims[0])[:16], "c" + digest(other)[:16]}
	e.library.Claims[ids[0]], e.library.Claims[ids[1]] = claimRecord{Claim: claims[0]}, claimRecord{Claim: other}
	generation := 0
	cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		generation++
		if generation == 1 {
			return nil, errors.New("provider unavailable")
		}
		return reply(provider.TextMessage("assistant", "null")), nil
	})
	if e.compile(t.Context(), declaration{cfg: cfg, session: "first"}, ids[0]) == nil {
		t.Fatal("expected provider failure")
	}
	for _, id := range ids {
		if err := e.compile(t.Context(), declaration{cfg: cfg, session: "next", task: "next-task"}, id); err != nil {
			t.Fatal(err)
		}
	}
	if generation != 1 || groups != 1 {
		t.Fatalf("cooldown restarted grouping/generation: groups=%d generations=%d", groups, generation)
	}
	e.mu.Lock()
	for key, attempt := range e.compiling {
		attempt.retryAt = time.Time{}
		e.compiling[key] = attempt
	}
	e.mu.Unlock()
	if err := e.compile(t.Context(), declaration{cfg: cfg, session: "later"}, ids[1]); err != nil {
		t.Fatal(err)
	}
	if generation != 2 {
		t.Fatal("expired cooldown permanently suppressed compilation")
	}
}

func TestReflexEnvelopeRejectsMissingOrAmbiguousObserve(t *testing.T) {
	for _, source := range []string{`{}`, `{"readers":{"inspect":"\"observe\""}}`, `{"observe":2}`, `{"observe":"null"}`, `{"observe":null,"readers":{"inspect":"function(){}"}}`, `{"observe":"js:({})","extra":true}`, `{"observe":null} {}`} {
		var r *Reflex
		if decodeReflex(source, &r) == nil {
			t.Fatalf("accepted malformed artifact %s", source)
		}
	}
	for _, source := range []string{`null`, `{"observe":null}`} {
		var r *Reflex
		if err := decodeReflex(source, &r); err != nil || r != nil {
			t.Fatalf("null=%s err=%v", source, err)
		}
	}
}

func TestWaitIdleTimeoutDoesNotCancelAdmittedWork(t *testing.T) {
	e := New(Config{})
	e.idle = make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := e.WaitIdle(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error=%v", err)
	}
	select {
	case <-e.idle:
		t.Fatal("timed-out accounting wait closed pending work")
	default:
	}
	close(e.idle)
	if err := e.WaitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
}

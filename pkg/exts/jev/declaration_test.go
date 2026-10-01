package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

const fixtureClaim = `[{"when":"A task requires finite step advancement","question":"Can the task advance now?","options":{"advance":"A known step can advance the task","defer":"Missing information or a completed task"}}]`

var fixtureReflex = stepObserve("advance", true)

func declarationAnswers(req jevapi.Request, compile bool) map[string]jevapi.Answer {
	out := map[string]jevapi.Answer{}
	if runtimeRequest(req) {
		return runtimeAnswers(req, "advance/go")
	}
	for id, q := range req.Questions {
		choice := Defer
		if strings.HasPrefix(id, "claim") {
			choice = "new"
			for key := range q.Criteria.(map[string]any) {
				if strings.HasPrefix(key, "c") {
					choice = key
					break
				}
			}
		} else if id == "ownership" {
			choice = "whole"
		} else if strings.HasPrefix(id, "compile") || strings.HasPrefix(id, "coverage") {
			if compile {
				choice = "compile"
			}
		} else if strings.HasPrefix(id, "c") {
			choice = "include"
		}
		out[id] = answer(choice)
	}
	return out
}
func settle(t *testing.T, e *Extension) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := e.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyLibraryLearnsCompletedRunAndTakesOverNextTask(t *testing.T) {
	compiling, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var position, foreground, claims, compiles atomic.Int64
	var e *Extension
	command := coretool.Command{Name: "advance", Run: func(_ context.Context, ex *coretool.Execution) (any, error) {
		old := position.Load()
		if len(ex.Args) != 1 || ex.Args[0] != fmt.Sprint(old) {
			return nil, coretool.ErrStaleChoice
		}
		position.Add(1)
		_, err := fmt.Fprintf(ex.Stdout, "step=%d", position.Load())
		return nil, err
	}}
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer { return declarationAnswers(req, true) })
	var cfg agent.Config
	e, cfg, _ = testInstallation(t, Config{Mode: "auto"}, client, command)
	cfg.Provider = testProvider(func(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		switch provider.MessageText(req.Messages[0]) {
		case claimPrompt:
			claims.Add(1)
			return reply(provider.TextMessage("assistant", fixtureClaim)), nil
		case compilePrompt:
			compiles.Add(1)
			if !strings.Contains(provider.MessageText(req.Messages[1]), "step=4") {
				t.Error("compilation started before the ordinary trajectory completed")
			}
			close(compiling)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return reply(provider.TextMessage("assistant", fixtureReflex)), nil
		}
		foreground.Add(1)
		if position.Load() < 4 {
			if len(e.snapshot().Reflexes) != 0 {
				t.Error("unfinished compilation was published")
			}
			return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{action("advance " + fmt.Sprint(position.Load()))}}), nil
		}
		if position.Load() != 4 {
			t.Errorf("Reflex did not bypass intermediate thinking: step=%d", position.Load())
		}
		var evidence strings.Builder
		for _, message := range req.Messages {
			evidence.WriteString(provider.MessageText(message))
			if result := provider.MessageToolResult(message); result != nil {
				evidence.WriteString(coretool.ResultText(result))
			}
		}
		if !strings.Contains(evidence.String(), `"step":4`) && !strings.Contains(evidence.String(), "step=4") {
			t.Error("missing final observed state")
		}
		return reply(provider.TextMessage("assistant", "done")), nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := agent.NewAgent(cfg).Run(ctx, agent.TextInput("Advance four steps and report the result."))
	if err != nil || result.Output != "done" {
		t.Fatalf("%v %v", result, err)
	}
	if foreground.Load() != 5 || len(e.snapshot().Reflexes) != 0 {
		t.Fatal("initial ordinary task did not complete independently of compilation")
	}
	select {
	case <-compiling:
	case <-ctx.Done():
		t.Fatal("completed trajectory did not trigger background compilation")
	}
	once.Do(func() { close(release) })
	settle(t, e)
	position.Store(0)
	cfg.SessionID = "next-task"
	result, err = agent.NewAgent(cfg).Run(ctx, agent.TextInput("Advance four steps and report the result."))
	if err != nil || result.Output != "done" || result.Turns != 1 || position.Load() != 4 {
		t.Fatalf("next task was not fully taken over: result=%v error=%v step=%d", result, err, position.Load())
	}
	settle(t, e)
	if claims.Load() != 1 || compiles.Load() != 1 || foreground.Load() != 6 {
		t.Fatalf("claim=%d compile=%d foreground=%d", claims.Load(), compiles.Load(), foreground.Load())
	}
	lib := e.snapshot()
	if len(lib.Claims) != 1 || len(lib.Reflexes) != 1 {
		t.Fatalf("library=%+v", lib)
	}
	data, _ := os.ReadFile(filepath.Join(e.config.Directory, "library.json"))
	for _, bad := range []string{"chosen", "training", "phase", "selector"} {
		if strings.Contains(string(data), bad) {
			t.Fatalf("unexpected state %s", bad)
		}
	}
	restored := New(Config{Directory: e.config.Directory})
	if err := restored.loadLibrary(); err != nil || len(restored.snapshot().Reflexes) != 1 {
		t.Fatalf("restore: %v", err)
	}
}

func TestClaimConsumedOnceAndNeverReusedByAnotherTask(t *testing.T) {
	var judgments atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
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
	if receipts != 1 || judgments.Load() != 1 {
		t.Fatalf("receipts=%d judgments=%d", receipts, judgments.Load())
	}
	for _, c := range e.snapshot().Claims {
		if !c.Consumed {
			t.Fatal("consumption was not durable")
		}
	}
	cfg.SessionID = "second"
	if _, err = agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Advance the task.")); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if judgments.Load() != 1 || len(e.snapshot().Claims) != 1 {
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
			client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer { return declarationAnswers(req, true) })
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
			cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
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

func TestOutputBatchDeclaresRelatedClaimsAndCompilesOnce(t *testing.T) {
	var batched, compiled atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if _, ok := req.Questions["claim0"]; ok {
			out := map[string]jevapi.Answer{}
			for id := range req.Questions {
				out[id] = answer(Defer)
			}
			if len(req.Questions) == 3 {
				batched.Add(1)
				for id := range req.Questions {
					out[id] = answer("new")
				}
			}
			return out
		}
		return declarationAnswers(req, true)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "advance", Run: func(context.Context, *coretool.Execution) (any, error) { return "ok", nil }})
	calls := 0
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		switch provider.MessageText(req.Messages[0]) {
		case claimPrompt:
			var claims []Claim
			_ = json.Unmarshal([]byte(fixtureClaim), &claims)
			second := claims[0]
			second.Question = "Should the task yield for missing information?"
			claims = append(claims, second)
			data, _ := json.Marshal(claims)
			return reply(provider.TextMessage("assistant", string(data))), nil
		case compilePrompt:
			compiled.Add(1)
			var input struct {
				Scope []map[string]string `json:"scope"`
			}
			_ = json.Unmarshal([]byte(provider.MessageText(req.Messages[1])), &input)
			if len(input.Scope) != 2 || input.Scope[0]["question"] == "" || input.Scope[1]["question"] == "" {
				t.Error("compile did not receive related declarations together")
			}
			return reply(provider.TextMessage("assistant", fixtureReflex)), nil
		}
		calls++
		if calls == 1 {
			return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{aop.Text("Perform both known steps"), action("advance a"), action("advance b")}}), nil
		}
		return reply(provider.TextMessage("assistant", "done")), nil
	})
	if _, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Perform two checks")); err != nil {
		t.Fatal(err)
	}
	settle(t, e)
	if batched.Load() != 1 || compiled.Load() != 1 || len(e.snapshot().Claims) != 2 || len(e.snapshot().Reflexes) != 1 {
		t.Fatalf("batch=%d compile=%d library=%+v", batched.Load(), compiled.Load(), e.snapshot())
	}
	for _, c := range e.snapshot().Claims {
		if c.Consumed {
			t.Fatal("ended source task consumed a late declaration")
		}
	}
}

func TestCloseCancelsBackgroundModelCall(t *testing.T) {
	entered := make(chan struct{})
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer { return declarationAnswers(req, false) })
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
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := e.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestExistingSceneSkipsPageActionDeclarations(t *testing.T) {
	var discovered atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		out := map[string]jevapi.Answer{}
		for id, q := range req.Questions {
			if !strings.HasPrefix(id, "claim") {
				t.Errorf("unexpected compilation request %s", id)
			}
			out[id] = answer(Defer)
			for key := range q.Criteria.(map[string]any) {
				if strings.HasPrefix(key, "r") {
					out[id] = answer(key)
					discovered.Add(1)
				}
			}
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client)
	installReflex(e, "browser")
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

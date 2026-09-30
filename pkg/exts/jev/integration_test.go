package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/egress"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/telemetry"
	coretool "github.com/chainreactors/cyber/core/tool"
	guardext "github.com/chainreactors/cyber/pkg/exts/guardrail"
	terminalext "github.com/chainreactors/cyber/pkg/exts/terminal"
	"google.golang.org/protobuf/proto"
)

type testProvider func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error)

func (testProvider) Name() string { return "test" }
func (f testProvider) ChatCompletion(ctx context.Context, r *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	return f(ctx, r)
}
func reply(m *aop.Message) *provider.ChatCompletionResponse {
	return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: m, FinishReason: "stop"}}, Usage: &aop.TokenUsage{InputTokens: 1000, OutputTokens: 100, TotalTokens: 1100, Detail: map[string]uint64{"reasoning": 60}}}
}
func action(command string) *aop.Content {
	args, _ := json.Marshal(map[string]string{"command": command})
	return &aop.Content{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: aop.EnvelopeID(), Name: "bash", Arguments: &aop.EncodedValue{Data: args, MediaType: aop.JSONMediaType}}}}
}

// Use the actual extension host, command and tool boundaries, shell adapter and
// Agent loop. Only model inference is replaced for deterministic mechanism tests.
func testInstallation(t *testing.T, config Config, client *jevapi.Client, commands ...coretool.Command) (*Extension, agent.Config, *corehooks.Registry) {
	t.Helper()
	contribution := extension.Func{LoadFunc: func(scope *extension.Scope) error {
		if len(commands) == 0 {
			return nil
		}
		return extension.Add(scope, commands...)
	}}
	e, cfg, _ := testInstallationWithExtensions(t, config, client, contribution)
	return e, cfg, cfg.Hooks
}

func testInstallationWithExtensions(t *testing.T, config Config, client *jevapi.Client, entries ...extension.Extension) (*Extension, agent.Config, *coretool.CommandRegistry) {
	t.Helper()
	if config.Directory == "" {
		config.Directory = t.TempDir()
	}
	registry := corehooks.New()
	cmds, tools := coretool.NewCommandRegistry(), coretool.NewToolRegistry()
	e := New(config)
	values := []extension.Extension{
		extension.Provided[*corehooks.Registry](registry),
		extension.Provided[*events.Stream](events.New()),
		extension.Provided[telemetry.Logger](telemetry.NopLogger()),
		extension.Provided[egress.Endpoint](egress.Disabled()),
		cmds, tools, terminalext.New(terminalext.Config{Directory: t.TempDir(), Timeout: 10}),
	}
	if client != nil {
		values = append(values, extension.Provided[*jevapi.Client](client))
	}
	values = append(values, entries...)
	values = append(values, e)
	set, err := extension.New(values...)
	if err != nil {
		t.Fatal(err)
	}
	if err = set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := set.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return e, agent.Config{Loop: agent.StandardLoop{}, Tools: tools, Hooks: registry, Model: "test", SystemPrompt: "Use supplied tools to complete the task. Observe the result before reporting success.", MaxTokens: agent.DefaultMaxTokens, MaxTurns: 20, MaxRetries: -1}, cmds
}
func awaitLearning(t *testing.T, e *Extension) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	if err := e.WaitLearning(ctx); err != nil {
		t.Fatal(err)
	}
}
func fakeJEV(t *testing.T, choose func(jevapi.Request) map[string]jevapi.Answer) *jevapi.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jevapi.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": choose(req), "usage": map[string]int{"input_tokens": 20, "output_tokens": 0}})
	}))
	t.Cleanup(server.Close)
	client := jevapi.New("test-key", "test-jev", time.Second)
	client.Endpoint = server.URL
	t.Cleanup(client.Close)
	return client
}
func answer(id string) jevapi.Answer { return jevapi.Answer{Type: "choice", Choice: id} }
func testPrices() map[string]map[string]float64 {
	return map[string]map[string]float64{"test": {"input": 2, "output": 8, "cache_read": 1}, "test-jev": {"input": 1, "output": 1, "cache_read": 1}}
}

func TestEmptyLibraryAutomaticallyLearnsValidatesAndAccelerates(t *testing.T) {
	var position, observations, modelCalls, compilations atomic.Int64
	command := coretool.Command{Name: "advance", Contract: "advance-v1", Usage: "advance <current-step>", Run: func(ctx context.Context, ex *coretool.Execution) (any, error) {
		if len(ex.Args) != 1 || ex.Args[0] != strconv.Itoa(int(position.Load())) {
			return nil, coretool.ErrStaleChoice
		}
		_, err := fmt.Fprintf(ex.Stdout, "step=%d", position.Add(1))
		return nil, err
	}, Choices: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
		observations.Add(1)
		step := position.Load()
		choices := map[string]*aop.Content{}
		if step < 4 {
			choices[fmt.Sprintf("step-%d", step)] = action(fmt.Sprintf("advance %d", step))
		}
		return json.RawMessage(fmt.Sprintf(`{"step":%d}`, step)), choices, nil
	}}
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if len(req.Questions) != 2 {
			t.Errorf("entry and speculative selection must share one request: %d", len(req.Questions))
		}
		out := map[string]jevapi.Answer{"entry": answer(Defer)}
		for id, q := range req.Questions {
			if id == "entry" {
				continue
			}
			out[id] = answer(Defer)
			for choice := range q.Criteria.(map[string]any) {
				if choice != Defer {
					out["entry"], out[id] = answer(id), answer(choice)
					break
				}
			}
		}
		return out
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto", Prices: testPrices()}, client, command)
	cfg.Provider = testProvider(func(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		first := provider.MessageText(req.Messages[0])
		if first == compilerPrompt {
			compilations.Add(1)
			if strings.Contains(provider.MessageText(req.Messages[1]), `"source":"context"`) {
				return reply(provider.TextMessage("assistant", `[]`)), nil
			}
			return reply(provider.TextMessage("assistant", `[{"enter":"The task requests advancing through the available finite steps; a next action exists.","decide":"Select the current next step. Defer after the last step."}]`)), nil
		}
		select {
		case <-time.After(12 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		// Replay derives the decision from its historical prefix, never from the
		// live fixture that may already have completed a different task.
		step := 0
		for _, m := range req.Messages {
			if result := provider.MessageToolResult(m); result != nil {
				_, _ = fmt.Sscanf(coretool.ResultText(result), "step=%d", &step)
			}
			if m.Name == "jev" {
				for n := 1; n <= 4; n++ {
					if strings.Contains(provider.MessageText(m), fmt.Sprintf("step=%d", n)) {
						step = n
					}
				}
			}
		}
		if req.SessionID != "" {
			modelCalls.Add(1)
		}
		if step >= 4 {
			return reply(provider.TextMessage("assistant", "All four steps observed.")), nil
		}
		return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{action(fmt.Sprintf("advance %d", step))}}), nil
	})
	if len(e.Rules()) != 0 {
		t.Fatal("test must start with an empty library")
	}
	for task := 0; task < 14; task++ {
		position.Store(0)
		cfg.SessionID = fmt.Sprintf("training-%d", task)
		result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Advance through all four available steps and report the observed result."))
		if err != nil {
			t.Fatal(err)
		}
		if position.Load() != 4 {
			t.Fatalf("ordinary task outcome failed: task=%d position=%d stop=%v", task, position.Load(), result.Stop)
		}
		awaitLearning(t, e)
	}
	active := 0
	for _, r := range e.Rules() {
		if r.Phase != "active" {
			continue
		}
		active++
		if !passes(r.Checks) {
			t.Fatal("activation bypassed validation")
		}
		for _, c := range r.Checks {
			if contains(r.TrainingTasks, c.Task) {
				t.Fatal("training task leaked into validation")
			}
		}
	}
	if active == 0 || compilations.Load() == 0 {
		t.Fatalf("automatic path never activated: %+v", e.Rules())
	}
	beforeCalls, beforeObs := modelCalls.Load(), observations.Load()
	position.Store(0)
	cfg.SessionID = "holdout"
	result, err := agent.NewAgent(cfg).Run(t.Context(), agent.TextInput("Advance through all four available steps and report the observed result."))
	if err != nil {
		t.Fatal(err)
	}
	awaitLearning(t, e)
	if position.Load() != 4 || modelCalls.Load()-beforeCalls >= 5 {
		t.Fatalf("not accelerated: position=%d L2=%d", position.Load(), modelCalls.Load()-beforeCalls)
	}
	if observations.Load()-beforeObs < 5 {
		t.Fatal("candidates were not refreshed after each action")
	}
	for _, m := range result.Messages {
		if m.Name == "jev" && (m.Role != "user" || m.Id == "") {
			t.Fatal("invalid receipt")
		}
		if m.Name == "jev" && !strings.Contains(provider.MessageText(m), `Executed ["bash",`) {
			t.Fatal("takeover receipt omitted the dispatched call; model cannot identify completed effects")
		}
		if m.Role == "tool" {
			callID := provider.MessageToolResult(m).CallId
			found := false
			for _, prev := range result.Messages {
				for _, c := range provider.MessageToolCalls(prev) {
					found = found || c.Id == callID
				}
			}
			if !found {
				t.Fatal("orphan tool result leaked into model history")
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(e.config.Directory, "execution-*.jsonl"))
	if len(files) == 0 {
		t.Fatal("missing separate execution evidence")
	}
	t.Logf("mechanism fixture only: warm L2 calls=%d vs 5; real speedup requires live A/B/C", modelCalls.Load()-beforeCalls)
}

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

func seedActive(e *Extension, cfg agent.Config, source string, labels map[string]string) *Reflex {
	cfg.SystemPrompt += "\n\n" + Prompt
	r := &Reflex{Source: source, Contract: source + "-v1", Enter: "Eligible finite step", Decide: "Select current action", Labels: labels, Environment: environment(cfg, e.clientIdentity()), Phase: "active"}
	r.ID = ruleID(r)
	e.mu.Lock()
	e.rules[r.ID] = r
	e.mu.Unlock()
	return r
}

func TestTakeoverUsesGuardrailAndYieldsAfterDenial(t *testing.T) {
	var executions atomic.Int64
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		out := map[string]jevapi.Answer{}
		for id := range req.Questions {
			if id != "entry" {
				out["entry"], out[id] = answer(id), answer("go")
			}
		}
		return out
	})
	e, cfg, registry := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{Name: "protected", Contract: "protected-v1", Run: func(context.Context, *coretool.Execution) (any, error) { executions.Add(1); return "executed", nil }, Choices: func(context.Context, []*aop.Message) (json.RawMessage, map[string]*aop.Content, error) {
		return json.RawMessage(`{}`), map[string]*aop.Content{"go": action("protected")}, nil
	}})
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if len(req.Messages) != 3 || req.Messages[2].Name != "jev" {
			t.Fatal("denial receipt missing")
		}
		if text := provider.MessageText(req.Messages[2]); !strings.Contains(text, "Attempted (tool error;") || strings.Contains(text, "Executed ") {
			t.Error("blocked call reported as an executed effect")
		}
		return reply(provider.TextMessage("assistant", "Action was denied.")), nil
	})
	seedActive(e, cfg, "protected", nil)
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

	if executions.Load() != 0 || client.Usage().Detail["requests"] != 1 {
		t.Fatalf("denial bypass: executions=%d requests=%d", executions.Load(), client.Usage().Detail["requests"])
	}
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

func TestImportClearsEvidenceAndEnvironment(t *testing.T) {
	e := New(Config{Directory: t.TempDir()})
	r := &Reflex{Source: "test", Contract: "test-v1", Enter: "condition", Decide: "decision", Phase: "active", Environment: "deployment-a", Uses: 10, TrainingTasks: []string{"private-task"}, Checks: []check{{Task: "private-validation"}}}
	r.ID = ruleID(r)
	e.rules[r.ID] = r
	data, err := e.Export()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-") || strings.Contains(string(data), "deployment-a") {
		t.Fatal("export leaked evidence")
	}
	other := New(Config{Directory: t.TempDir()})
	if err = other.Import(data); err != nil {
		t.Fatal(err)
	}
	for _, r := range other.Rules() {
		if r.Phase != "validating" || len(r.Checks) != 0 || r.Environment != "" || r.Uses != 0 {
			t.Fatal("import trusted old activation")
		}
	}
	if _, err = os.Stat(filepath.Join(other.config.Directory, "reflex-"+other.Rules()[0].ID+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedAnswerOnlyAndUnknownBindingFailsClosed(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint(invalid), func(t *testing.T) {
			client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
				choice := "yes"
				if invalid {
					choice = "unbound"
				}
				return map[string]jevapi.Answer{"entry": answer("one"), "one": answer(choice), "two": {Type: "broken", Choice: "nonexistent"}}
			})
			e := New(Config{Directory: t.TempDir()})
			e.client = client
			rules := []*Reflex{{ID: "one", Enter: "one", Decide: "one"}, {ID: "two", Enter: "two", Decide: "two"}}
			samples := map[string]sample{"one": {State: json.RawMessage(`{}`), Choices: map[string]*aop.Content{"yes": aop.Text("known conclusion")}}, "two": {State: json.RawMessage(`{}`)}}
			id, choice, _, err := e.decide(t.Context(), rules, samples, "test")
			if invalid {
				if err == nil {
					t.Fatal("unbound choice accepted")
				}
			} else if err != nil || id != "one" || choice != "yes" {
				t.Fatalf("unused speculative answer rejected: %s %s %v", id, choice, err)
			}
			if client.Usage().Detail["requests"] != 1 {
				t.Fatal("decision split into multiple requests")
			}
		})
	}
}

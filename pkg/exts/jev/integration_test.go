package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// Unit execution tests install a scene to isolate the executor; automatic
// declaration/compilation is covered separately from an empty library.
func installReflex(e *Extension, sources ...string) {
	code := constantObserve(`{}`, map[string]string{})
	calls := map[string]string{}
	for _, source := range sources {
		calls[source+"/go"] = source
	}
	code = constantObserve(`{}`, calls)
	if len(sources) == 1 && (sources[0] == "advance" || sources[0] == "workflow") {
		code = stepObserve(sources[0], sources[0] == "advance")
	}
	installObserve(e, code)
}

func installObserve(e *Extension, code string) Reflex {
	r := Reflex{When: "The task can progress through the supplied native tools.", Decide: "Select the bound operation matching the current user goal. Report observed completion; defer for missing input or strategy.", Observe: code}
	if err := r.validate(); err != nil {
		panic(err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.library.Reflexes["r"+digest(r)[:16]] = reflexRecord{Reflex: r}
	return r
}

func constantObserve(state string, calls map[string]string) string {
	candidates := map[string]any{}
	for id, command := range calls {
		candidates[id] = map[string]any{"name": "bash", "arguments": map[string]string{"command": command}, "read": false}
	}
	data, _ := json.Marshal(candidates)
	return "js:({state: JSON.parse(" + strconv.Quote(state) + "), candidates: JSON.parse(" + strconv.Quote(string(data)) + ")})"
}

func stepObserve(command string, withArgument bool) string {
	binding := strconv.Quote(command)
	if withArgument {
		binding += ` + " " + String(step)`
	}
	return `js:(() => {
const results = messages.filter(m => m.call_id != null && !m.is_error && /step=([0-9]+)/.test(m.text || ""));
const step = results.length === 0 ? 0 : Number(results[results.length - 1].text.match(/step=([0-9]+)/)[1]);
return {state: {step: step}, candidates: step < 4 ? {` + strconv.Quote(command+"/go") + `: bind("bash", {command: ` + binding + `}, false)} : {}};
})()`
}
func runtimeRequest(req jevapi.Request) bool {
	if _, ok := req.Questions["entry"]; ok {
		return true
	}
	for id := range req.Questions {
		if strings.HasPrefix(id, "r") {
			return true
		}
	}
	return false
}

func candidateBySuffix(candidates map[string]string, suffix string) string {
	for key, call := range candidates {
		if strings.HasSuffix(key, "/"+suffix) {
			return call
		}
	}
	return ""
}
func runtimeAnswers(req jevapi.Request, choice string) map[string]jevapi.Answer {
	var state struct {
		Candidates map[string]string `json:"candidates"`
	}
	_ = json.Unmarshal(req.State, &state)
	for key := range state.Candidates {
		if strings.HasSuffix(key, "/"+choice) {
			choice = key
			break
		}
	}
	out := map[string]jevapi.Answer{}
	for id := range req.Questions {
		out[id] = answer(Defer)
	}
	if !runtimeRequest(req) {
		return out
	}
	if _, ok := req.Questions["generation"]; ok {
		out["generation"] = answer("ready")
	}
	for id := range req.Questions {
		if strings.HasPrefix(id, "r") {
			if _, ok := req.Questions["entry"]; ok {
				out["entry"] = answer(id)
			}
			out[id] = answer(choice)
			return out
		}
	}
	for id := range req.Questions {
		if id != "entry" {
			out["entry"], out[id] = answer(id), answer(choice)
			break
		}
	}
	return out
}

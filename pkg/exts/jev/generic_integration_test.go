package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	coretool "github.com/chainreactors/cyber/core/tool"
)

type nativeFixtureTool struct {
	definition *aop.ToolDefinition
	run        func(context.Context, string) (*coretool.Result, error)
}

func (t nativeFixtureTool) Name() string                    { return t.definition.Name }
func (t nativeFixtureTool) Description() string             { return t.definition.Description }
func (t nativeFixtureTool) Definition() *aop.ToolDefinition { return t.definition }
func (t nativeFixtureTool) Execute(ctx context.Context, arguments string) (*coretool.Result, error) {
	return t.run(ctx, arguments)
}

// The host supplies only ordinary native tools. A compiler response creates the
// observer at runtime from an empty library; tool implementations know nothing
// about JEV, candidate spaces or the eventual scene.
func TestAutomaticObserveAcrossNativeToolsWithoutCommandRegistry(t *testing.T) {
	names := []string{"catalog_" + digest(aop.EnvelopeID())[:8], "activate_" + digest(aop.EnvelopeID())[:8], "receipt_" + digest(aop.EnvelopeID())[:8]}
	var mu sync.Mutex
	var target, version, resource, job, proof string
	var lists, effects, polls int
	var foreground, claims, compiles atomic.Int64
	var e *Extension
	encode := func(value any) *coretool.Result {
		data, _ := json.Marshal(value)
		return coretool.TextResult(string(data))
	}
	list := nativeFixtureTool{definition: coretool.Def(names[0], "List current resource identifiers and labels", struct{}{}), run: func(ctx context.Context, _ string) (*coretool.Result, error) {
		// Make background publication deterministic, without preinstalling a
		// scene or giving the background worker permission to execute tools.
		if err := e.WaitIdle(ctx); err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		lists++
		if lists != 1 {
			return nil, fmt.Errorf("unnecessary catalog replay")
		}
		return encode(map[string]any{"phase": "ready", "version": version, "items": []map[string]string{{"id": resource, "label": target}, {"id": "unrelated-" + resource, "label": "Other"}}}), nil
	}}
	activate := nativeFixtureTool{definition: coretool.Def(names[1], "Activate a listed resource using its current version", struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}{}), run: func(_ context.Context, arguments string) (*coretool.Result, error) {
		var args struct{ ID, Version string }
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		if args.ID != resource || args.Version != version || lists != 1 || effects != 0 {
			return nil, fmt.Errorf("wrong resource, stale version or repeated effect: %s", arguments)
		}
		effects++
		return encode(map[string]string{"phase": "pending", "job": job}), nil
	}}
	receiptTool := nativeFixtureTool{definition: coretool.Def(names[2], "Read the receipt of an activation job without modifying it", struct {
		Job string `json:"job"`
	}{}), run: func(_ context.Context, arguments string) (*coretool.Result, error) {
		var args struct{ Job string }
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		if args.Job != job || effects != 1 {
			return nil, fmt.Errorf("receipt read without the actual job: %s", arguments)
		}
		polls++
		if polls < 3 {
			return encode(map[string]string{"phase": "pending", "job": job}), nil
		}
		return encode(map[string]string{"phase": "complete", "receipt": proof}), nil
	}}
	// Tool names come from definitions; identifiers, versions and job arguments
	// come exclusively from the latest associated native result.
	expression := `js:(() => {
const recent = history.length ? history[history.length - 1] : null;
const state = recent ? recent.data : {phase:"empty"};
const catalog = tools.find(t => t.description === "List current resource identifiers and labels").name;
const activate = tools.find(t => t.description === "Activate a listed resource using its current version").name;
const receipt = tools.find(t => t.description === "Read the receipt of an activation job without modifying it").name;
const candidates = state.phase === "empty" ? [bind(catalog,{},true)] :
state.phase === "ready" ? state.items.map(item => bind(activate,{id:item.id,version:state.version},false)) :
state.phase === "pending" ? [bind(receipt,{job:state.job},true)] : [];
return {state:state,candidates:choices(candidates)};
})()`
	client := fakeJEV(t, func(req jevapi.Request) map[string]jevapi.Answer {
		if !runtimeRequest(req) {
			return declarationAnswers(req, true)
		}
		var state struct {
			Candidates   map[string]string          `json:"candidates"`
			Observations map[string]json.RawMessage `json:"observations"`
		}
		if err := json.Unmarshal(req.State, &state); err != nil {
			t.Error(err)
			return runtimeAnswers(req, Defer)
		}
		for _, fact := range state.Observations {
			if strings.Contains(string(fact), `"phase":"complete"`) {
				return runtimeAnswers(req, report)
			}
		}
		mu.Lock()
		currentID := resource
		mu.Unlock()
		for id, candidate := range state.Candidates {
			var call []json.RawMessage
			var name string
			var args map[string]string
			if json.Unmarshal([]byte(candidate), &call) != nil || len(call) != 2 || json.Unmarshal(call[0], &name) != nil || json.Unmarshal(call[1], &args) != nil {
				t.Error("invalid shared native binding")
				continue
			}
			if name != names[1] || args["id"] == currentID {
				return runtimeAnswers(req, id)
			}
		}
		return runtimeAnswers(req, Defer)
	})
	registry, tools := corehooks.New(), coretool.NewToolRegistry()
	e = New(Config{Mode: "auto", Directory: t.TempDir()})
	set, err := extension.New(extension.Provided[*corehooks.Registry](registry), tools,
		extension.Func{LoadFunc: func(scope *extension.Scope) error {
			return extension.Add[coretool.Tool](scope, list, activate, receiptTool)
		}},
		extension.Provided[*jevapi.Client](client), e)
	if err != nil {
		t.Fatal(err)
	}
	if err = set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := set.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	if e.commands != nil || len(e.snapshot().Reflexes) != 0 {
		t.Fatal("native host acquired a command adapter or preinstalled scene")
	}
	llm := testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		switch provider.MessageText(req.Messages[0]) {
		case claimPrompt:
			claims.Add(1)
			return reply(provider.TextMessage("assistant", fixtureClaim)), nil
		case compilePrompt:
			compiles.Add(1)
			return reply(provider.TextMessage("assistant", expression)), nil
		}
		foreground.Add(1)
		mu.Lock()
		defer mu.Unlock()
		name, arguments := "", map[string]string{}
		if lists == 0 {
			name = names[0]
		} else if effects == 0 {
			name, arguments = names[1], map[string]string{"id": resource, "version": version}
		} else if polls < 3 {
			name, arguments = names[2], map[string]string{"job": job}
		}
		if name != "" {
			data, _ := json.Marshal(arguments)
			return reply(&aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: aop.EnvelopeID(), Name: name, Arguments: &aop.EncodedValue{Data: data, MediaType: aop.JSONMediaType}}}}}}), nil
		}
		last := req.Messages[len(req.Messages)-1]
		evidence := provider.MessageText(last)
		if result := provider.MessageToolResult(last); result != nil {
			evidence += coretool.ResultText(result)
		}
		if effects != 1 || polls != 3 || !strings.Contains(evidence, proof) {
			t.Error("LLM regained control before a verified receipt")
		}
		return reply(provider.TextMessage("assistant", proof)), nil
	})
	for i, label := range []string{"Archive", "Invoices"} {
		mu.Lock()
		target, version, resource, job, proof = label, aop.EnvelopeID(), aop.EnvelopeID(), aop.EnvelopeID(), aop.EnvelopeID()
		lists, effects, polls = 0, 0, 0
		wantProof := proof
		mu.Unlock()
		cfg := agent.Config{Provider: llm, Model: "test", Tools: tools, Hooks: registry, Loop: agent.StandardLoop{}, SessionID: fmt.Sprint(i), MaxTurns: 10, MaxTokens: agent.DefaultMaxTokens, MaxRetries: -1}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		result, err := agent.NewAgent(cfg).Run(ctx, agent.TextInput("Activate "+label+" and report its receipt"))
		cancel()
		if err != nil || result.Output != wantProof || (i == 0 && result.Turns != 6) || (i == 1 && result.Turns != 1) {
			t.Fatalf("run=%d result=%v error=%v", i, result, err)
		}
		settle(t, e)
		lib := e.snapshot()
		data, _ := json.Marshal(lib)
		if lib.Version != libraryVersion || len(lib.Reflexes) != 1 || strings.Contains(string(data), resource) || strings.Contains(string(data), job) {
			t.Fatal("scene was not reusable runtime data")
		}
	}
	if foreground.Load() != 7 || claims.Load() != 1 || compiles.Load() != 1 {
		t.Fatalf("foreground=%d claims=%d compile=%d", foreground.Load(), claims.Load(), compiles.Load())
	}
}

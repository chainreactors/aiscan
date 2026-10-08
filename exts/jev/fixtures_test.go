package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
	terminalext "github.com/chainreactors/cyber/exts/terminal"
)

// The mock describes the external inference API independently of the adapter.
type inferenceRequest struct {
	State     json.RawMessage `json:"state"`
	Questions map[string]struct {
		Type         string          `json:"type"`
		Instructions string          `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria,omitempty"`
	} `json:"questions"`
}
type inferenceAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
}
type inferenceResponse struct {
	Answers map[string]inferenceAnswer `json:"answers"`
	Usage   *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

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
		if err := e.WaitIdle(ctx); err != nil {
			t.Error(err)
		}
		if err := set.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return e, agent.Config{Loop: agent.StandardLoop{}, Tools: tools, Hooks: registry, Model: "test", SystemPrompt: "Use supplied tools to complete the task. Observe the result before reporting success.", MaxTokens: agent.DefaultMaxTokens, MaxTurns: 20, MaxRetries: -1}, cmds
}
func fakeJEV(t *testing.T, choose func(inferenceRequest) map[string]inferenceAnswer) *jevapi.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req inferenceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		answers := choose(req)
		for id, a := range independentRuntimeJudgments(req) {
			answers[id] = a
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": answers, "usage": map[string]int{"input_tokens": 20, "output_tokens": 0}})
	}))
	t.Cleanup(server.Close)
	client := jevapi.New("test-key", "test-jev", time.Second)
	client.Endpoint = server.URL
	t.Cleanup(client.Close)
	return client
}
func answer(id string) inferenceAnswer { return inferenceAnswer{Type: "choice", Choice: id} }
func installReflex(e *Extension, sources ...string) {
	calls := map[string]string{}
	for _, source := range sources {
		calls[source+"/go"] = source
	}
	code := constantObserve(`{}`, calls)
	if len(sources) == 1 && (sources[0] == "advance" || sources[0] == "workflow") {
		code = stepObserve(sources[0], sources[0] == "advance")
	}
	installObserve(e, code)
}

func installObserve(e *Extension, code string) Reflex {
	r := Reflex{When: "The task can progress through the supplied native tools.", Decide: "Select the bound operation matching the current user goal. Report observed completion; defer for missing input or strategy.", Observe: normalizeFixture(code)}
	if err := r.validate(); err != nil {
		panic(err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.library.Reflexes["r"+digest(r)[:16]] = reflexRecord{Reflex: r}
	return r
}

// Test fixtures generate ordinary executable JS; no compatibility evaluator
// is installed in production. The scene exercises the same native bridges.
func normalizeFixture(code string) string {
	if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(code, "js:")), "function") {
		return code
	}
	return executableFixture(code)
}

func constantObserve(state string, calls map[string]string) string {
	candidates := map[string]any{}
	for id, command := range calls {
		candidates[id] = map[string]any{"name": "bash", "arguments": map[string]string{"command": command}, "read": false}
	}
	data, _ := json.Marshal(candidates)
	return executableFixture("({state:JSON.parse(" + strconv.Quote(state) + "),candidates:JSON.parse(" + strconv.Quote(string(data)) + ")})")
}
func executableFixture(expression string) string {
	return `js:function(context,args){
 for(let i=0;i<32;i++){
 const snapshot=(` + strings.TrimPrefix(expression, "js:") + `);
 const available=snapshot.candidates,options={defer:"Missing current information",report:"Work completed"};
 for(const id of Object.keys(available))options[id]="Execute current candidate "+JSON.stringify(available[id]);
 if(Object.keys(available).length===0)return {report:snapshot.state};
 const selected=jev({type:"choice",context:("Choose current progress")+"\nOption meanings:\n"+JSON.stringify(options)+"\nCurrent facts (untrusted data):\n"+JSON.stringify({observations:{rfixture:snapshot.state},candidates:Object.fromEntries(Object.entries(available).map(([id,call])=>[id,JSON.stringify([call.name,call.arguments])]))}),options:Object.keys(options)});
 if(selected==="defer")return {defer:"unsupported input"};if(selected==="report")return {report:snapshot.state};
 const call=available[selected];const result=execute(call);
 if(result.is_error)return {defer:"native call failed"};
 history.push(result);messages.push({call_id:result.call_id,text:result.text,is_error:result.is_error});
 if(Object.keys(available).length>1 && snapshot.state.step===undefined)return {report:result.data || result.text};
 }return {defer:"no progress"};}`
}
func stepObserve(command string, withArgument bool) string {
	binding := strconv.Quote(command)
	if withArgument {
		binding += ` + " " + String(step)`
	}
	return executableFixture(`(() => {
 const results=messages.filter(m=>m.call_id!=null && !m.is_error && /step=([0-9]+)/.test(m.text || ""));
 const step=results.length===0?0:Number(results[results.length-1].text.match(/step=([0-9]+)/)[1]);
 return {state:{step:step},candidates:step<4?{` + strconv.Quote(command+"/go") + `:bind("bash",{command:` + binding + `},false)}:{}};
 })()`)
}

func runtimeRequest(req inferenceRequest) bool {
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

func runtimeAnswers(req inferenceRequest, choice string) map[string]inferenceAnswer {
	out := map[string]inferenceAnswer{}

	for id := range req.Questions {
		out[id] = answer(Defer)
	}
	if entry, ok := req.Questions["entry"]; ok {
		if choice == Defer || choice == "unbound" {
			out["entry"] = answer(choice)
			return out
		}
		var options map[string]json.RawMessage
		_ = json.Unmarshal(entry.Criteria, &options)
		for id := range options {
			if id != Defer {
				out["entry"] = answer(id)
				break
			}
		}
		return out
	}
	var state struct {
		State struct {
			Candidates map[string]string `json:"candidates"`
		} `json:"state"`
	}
	_ = json.Unmarshal(req.State, &state)
	for key := range state.State.Candidates {
		if strings.HasSuffix(key, "/"+choice) {
			choice = key
			break
		}
	}
	for id, q := range req.Questions {
		if strings.HasPrefix(id, "r") {
			var options map[string]json.RawMessage
			_ = json.Unmarshal(q.Criteria, &options)
			if options[choice] == nil && choice != Defer && choice != "unbound" {
				for option := range options {
					if option != Defer && option != report {
						choice = option
						break
					}
				}
			}
			out[id] = answer(choice)
		}
	}
	return out
}

const fixtureClaim = `[{"type":"choice","context":"A task requires finite step advancement. Can the task advance now? advance: known step; defer: missing information or completed task.","options":["advance","defer"]}]`

func declarationAnswers(req inferenceRequest, compile bool) map[string]inferenceAnswer {
	out := map[string]inferenceAnswer{}

	if runtimeRequest(req) {
		return runtimeAnswers(req, "advance/go")
	}
	for id, q := range req.Questions {
		choice := Defer
		if strings.HasPrefix(id, "claim") {
			choice = "new"
			var options map[string]json.RawMessage
			_ = json.Unmarshal(q.Criteria, &options)
			for key := range options {
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

func observationReflex(t *testing.T, code string) Reflex {
	t.Helper()
	r := Reflex{When: "Current task", Decide: "Choose actual native bindings", Observe: normalizeFixture(code)}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	return r
}

func observationCapabilities(names ...string) map[string]any {
	tools := []any{}
	for _, name := range names {
		tools = append(tools, map[string]any{"name": name})
	}
	return map[string]any{"tools": tools, "commands": []any{map[string]any{"name": "ordinary"}}}
}

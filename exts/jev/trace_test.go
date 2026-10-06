package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/internal/jevwire"
)

func TestProjectionKeepsFullConstraintsAndEvictsCallResultGroups(t *testing.T) {
	constraint := strings.Repeat("system-rule ", 1600)
	messages := []*aop.Message{provider.TextMessage("system", constraint), provider.TextMessage("user", "Keep user authorization exactly")}
	for i := range 8 {
		call := action(fmt.Sprintf("step %d", i)).GetToolCall()
		result := coretool.TextResult(strings.Repeat("evidence ", 900))
		result.CallId = call.Id
		messages = append(messages, &aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: call}}}},
			&aop.Message{Role: "tool", Content: []*aop.Content{{Value: &aop.Content_ToolResult{ToolResult: result}}}})
	}
	data, ok := contextState(messages, 32<<10)
	if !ok || len(data) > 32<<10 {
		t.Fatalf("projection=%d ok=%t", len(data), ok)
	}
	var state struct {
		Messages []struct {
			Text   string `json:"text"`
			CallID string `json:"call_id"`
			Calls  []struct {
				ID string `json:"id"`
			} `json:"calls"`
		} `json:"messages"`
		Omitted int `json:"omitted_groups"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Messages[0].Text != constraint || state.Messages[1].Text != "Keep user authorization exactly" || state.Omitted == 0 {
		t.Fatal("constraints or omission metadata lost")
	}
	calls, results := map[string]bool{}, map[string]bool{}
	for _, message := range state.Messages {
		for _, call := range message.Calls {
			calls[call.ID] = true
		}
		if message.CallID != "" {
			results[message.CallID] = true
		}
	}
	if digest(calls) != digest(results) {
		t.Fatal("orphaned retained call/result evidence")
	}
}

func TestRuntimeBindingJudgmentKeepsConstraintsWithoutWholeCatalog(t *testing.T) {
	constraint := strings.Repeat("current authorization ", 1400)
	request := json.RawMessage(jsonText(map[string]any{"messages": []any{map[string]any{"role": "system", "text": constraint}, map[string]any{"role": "user", "text": "Read the current native receipt"}}}))
	call := binding{Name: "bash", Arguments: json.RawMessage(`{"command":"lab status current"}`), Read: true}
	catalog := map[string]any{
		"tools":            []any{map[string]any{"name": "bash", "description": "Dispatch the named native command", "input_schema": map[string]any{"type": "object"}}, map[string]any{"name": "unrelated", "description": strings.Repeat("unrelated manual ", 1600)}},
		"commands":         []any{map[string]any{"name": "lab", "usage": "lab status <actor>: read the actor's current receipt"}},
		"native_contracts": map[string]any{"unrelated": strings.Repeat("host-only verification ", 1600)},
	}
	if len(jsonText(map[string]any{"context": request, "capabilities": catalog})) <= 64<<10 {
		t.Fatal("fixture does not reproduce oversized binding context")
	}
	requests := 0
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		requests++
		data := req.Questions["binding"].Instructions + string(req.State)
		if strings.Count(data, constraint) != 1 || !strings.Contains(data, "read the actor's current receipt") || !strings.Contains(data, "lab status current") || strings.Contains(data, "unrelated manual") || strings.Contains(data, "host-only verification") {
			t.Error("binding judgment lost current authorization or selected protocol")
		}
		return map[string]jevwire.Answer{"binding": answer("accept")}
	})
	e, _, _ := testInstallation(t, Config{Mode: "off"}, client)
	e.client = client
	if err := e.judgeRuntime(t.Context(), "binding", request, map[string]any{"call": call, "capabilities": bindingCapabilities(call, catalog)}); err != nil || requests != 1 {
		t.Fatalf("binding context never reached the reviewer: requests=%d err=%v", requests, err)
	}
}

func TestCompilationKeepsHandoffBoundaryWithoutDuplicatingConstraints(t *testing.T) {
	constraint := strings.Repeat("preserve this authorization ", 1100)
	state, ok := contextState([]*aop.Message{provider.TextMessage("system", constraint), provider.TextMessage("user", "Inspect the current target")}, 32<<10)
	if !ok {
		t.Fatal("valid application context rejected")
	}
	var boundary map[string]any
	_ = json.Unmarshal(state, &boundary)
	boundary["messages"] = append(boundary["messages"].([]any), map[string]any{"call_id": "completed-before-handoff", "text": "actual result"})
	raw, _ := json.Marshal(map[string]any{"context": boundary, "observations": map[string]string{"r": "entry missing"}, "candidates": map[string]string{}})
	before := string(raw)
	projected, err := compilationHandoff(raw)
	if err != nil || string(raw) != before || strings.Contains(string(projected), constraint) || !strings.Contains(string(projected), "completed-before-handoff") || !strings.Contains(string(projected), "entry missing") {
		t.Fatalf("handoff projection lost evidence: %s, %v", projected, err)
	}
	requests := 0
	client := fakeJEV(t, func(req jevwire.Request) map[string]jevwire.Answer {
		requests++
		if len(req.State) > 64<<10 || strings.Count(string(req.State), constraint) != 1 || !strings.Contains(string(req.State), "entry missing") {
			t.Error("compilation request duplicated or lost task evidence")
		}
		return declarationAnswers(req, false)
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client,
		coretool.Command{Name: "catalog", Usage: "catalog [arguments]\n" + strings.Repeat("native documentation ", 1400), Run: func(context.Context, *coretool.Execution) (any, error) { return nil, nil }})
	c := choiceClaim("Native scene"+". "+"Can it progress?", map[string]string{Defer: "Missing facts", "inspect": "Read state"})
	e.library.Claims["c"] = claimRecord{Claim: c}
	e.library.Reflexes["r"] = reflexRecord{Reflex: Reflex{When: c.Context, Decide: "Use current native bindings", Observe: normalizeFixture(`js:({state:{},candidates:{}})`)}, Claims: []string{"c"}}
	catalog, err := e.capabilities(cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	duplicated, _ := json.Marshal(map[string]any{"context": state, "handoff": raw, "capabilities": catalog})
	if len(duplicated) <= 64<<10 {
		t.Fatal("fixture does not reproduce the provider's request limit")
	}
	if _, err := e.prepareCompilation(t.Context(), declaration{cfg: cfg, state: state, repair: "r", handoff: raw}, "c"); err != nil || requests != 1 {
		t.Fatalf("requests=%d err=%v", requests, err)
	}
}

func TestLargeCapabilityCatalogPreservesObservedNativeDocumentation(t *testing.T) {
	usage := "inspect [arguments]\n" + strings.Repeat("exact argument documentation ", 200)
	commands := []coretool.Command{{Name: "inspect", Usage: usage}}
	for i := range 12 {
		commands = append(commands, coretool.Command{Name: fmt.Sprintf("unrelated%d", i), Usage: "unrelated [arguments]\n" + strings.Repeat("scanner documentation ", 350)})
	}
	for i := range commands {
		commands[i].Run = func(context.Context, *coretool.Execution) (any, error) { return nil, nil }
	}
	client := fakeJEV(t, func(jevwire.Request) map[string]jevwire.Answer {
		t.Error("catalog inspection dispatched a judgment")
		return nil
	})
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, commands...)
	state := json.RawMessage(`{"messages":[{"calls":[{"name":"bash","arguments":{"command":"TOKEN=value inspect 'literal' && unrelated0 --help"}}]}]}`)
	catalog, err := e.capabilities(cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(catalog)
	if len(data) > 32<<10 || len(catalog["tools"].([]any)) != len(cfg.Tools.ToolDefinitions()) || len(catalog["commands"].([]any)) < len(commands) {
		t.Fatal("catalog exceeded budget or lost native schemas")
	}
	for _, item := range catalog["commands"].([]any) {
		command := item.(map[string]any)
		if command["name"] == "inspect" && command["usage"] != usage {
			t.Fatal("observed command documentation changed")
		}
		if command["name"] == "unrelated11" && (command["usage_complete"] != false || command["description_path"] == nil) {
			t.Fatal("unobserved command disappeared instead of retaining its documentation path")
		}
	}
	if digest(interactionCommands([]json.RawMessage{state})) != digest(map[string]bool{"inspect": true, "unrelated0": true}) {
		t.Fatal("shell syntax was not parsed faithfully")
	}
}

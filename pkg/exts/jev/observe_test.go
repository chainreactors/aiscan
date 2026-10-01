package jev

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestObserveInvalidBindingYieldsWithoutDispatch(t *testing.T) {
	for name, code := range map[string]string{
		"unknown tool":     `({state: {}, candidates: {go: {name: "invented", arguments: {}, read:false}}})`,
		"null arguments":   `({state: {}, candidates: {go: {name: "bash", arguments: null, read:false}}})`,
		"array arguments":  `({state: {}, candidates: {go: {name: "bash", arguments: [], read:false}}})`,
		"extra field":      `({state: {}, candidates: {go: {name: "bash", arguments: {}, read:false, execute: true}}})`,
		"reserved option":  `({state: {}, candidates: {report: {name: "bash", arguments: {}, read:false}}})`,
		"missing read":     `({state: {}, candidates: {go: {name:"bash", arguments:{}}}})`,
		"missing state":    `({candidates: {}})`,
		"missing choices":  `({state: {}})`,
		"invalid JSON":     `({state: JSON.parse("bad data"), candidates: {}})`,
		"oversized output": `({state: "x".repeat(40000), candidates: {}})`,
		"unbounded loop":   `(() => { while(true) {} })()`,
	} {
		t.Run(name, func(t *testing.T) {
			r := Reflex{When: "Current task", Decide: "Select a supplied candidate", Observe: "js:" + code}
			if err := r.validate(); err != nil {
				t.Fatal(err)
			}
			_, _, err := r.observe(t.Context(), json.RawMessage(`{"messages":[]}`), map[string]any{"tools": []any{map[string]any{"name": "bash"}}, "commands": []any{}})
			if err == nil {
				t.Fatal("invalid runtime observation accepted")
			}
		})
	}
}

func TestObserveHasNoToolOrHostExecutionAccess(t *testing.T) {
	for _, code := range []string{
		`js:({state: ExecuteTool("bash", "{}"), candidates: {}})`,
		`js:({state: commands[0].Run(), candidates: {}})`,
		`js:({state: now(), candidates: {}})`,
	} {
		r := Reflex{When: "Current task", Decide: "Select a supplied candidate", Observe: code}
		if err := r.validate(); err == nil {
			_, _, err = r.observe(t.Context(), json.RawMessage(`{"messages":[]}`), map[string]any{"tools": []any{}, "commands": []any{map[string]any{"name": "ordinary"}}})
			if err == nil {
				t.Fatal("expression obtained non-data host access")
			}
		}
	}
	r := Reflex{When: "Current task", Decide: "Select a supplied candidate", Observe: `js:(() => { while(true) {} })()`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := r.observe(ctx, json.RawMessage(`{"messages":[]}`), map[string]any{"tools": []any{}, "commands": []any{}}); err == nil {
		t.Fatal("canceled evaluation continued")
	}
}

func TestObserveUsesOnlyCurrentUserInteraction(t *testing.T) {
	oldCall, newCall := action("ordinary old"), action("ordinary current")
	result := func(call *aop.Content, text string) *aop.Message {
		value := coretool.TextResult(text)
		value.CallId = call.GetToolCall().Id
		return &aop.Message{Role: "tool", Content: []*aop.Content{{Value: &aop.Content_ToolResult{ToolResult: value}}}}
	}
	messages := []*aop.Message{
		provider.TextMessage("system", "Only act on explicitly requested resources"),
		provider.TextMessage("user", "Old task"),
		{Role: "assistant", Content: []*aop.Content{oldCall}}, result(oldCall, `{"id":"old-resource"}`),
		provider.TextMessage("user", "New task"),
		{Role: "assistant", Content: []*aop.Content{newCall}}, result(newCall, `{"id":"current-resource"}`),
	}
	projection, ok := contextState(messages)
	if !ok {
		t.Fatal("invalid test projection")
	}
	r := Reflex{When: "Current task", Decide: "Select a supplied candidate", Observe: `js:(() => {
const current = history[history.length - 1];
return {state: {user: user}, candidates: {go: bind("bash", {command: "ordinary " + quote(current.data.id)}, false)}};
})()`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	state, candidates, err := r.observe(t.Context(), projection, map[string]any{"tools": []any{map[string]any{"name": "bash"}}, "commands": []any{}})
	if err != nil || !strings.Contains(string(state), "New task") || strings.Contains(string(candidates["go"].Arguments), "old-resource") || !strings.Contains(string(candidates["go"].Arguments), "current-resource") {
		t.Fatalf("state=%s choices=%v error=%v", state, candidates, err)
	}
	if !strings.Contains(string(projection), "Only act on explicitly requested resources") {
		t.Fatal("separate decision context lost system constraints")
	}
}

func TestLegacyToolObserversRetainClaimsForRecompilation(t *testing.T) {
	claim := Claim{When: "Known operation", Question: "Can it progress?", Options: map[string]string{"go": "Progress", Defer: "Missing information"}}
	id := "c" + digest(claim)[:16]
	legacy := map[string]any{"version": 1, "claims": map[string]claimRecord{id: {Claim: claim, Task: "old", Consumed: true}}, "reflexes": map[string]any{"old-tool-scene": map[string]any{"when": "Tool scene", "decide": "Choose a tool candidate", "sources": []string{"old-tool"}, "claims": []string{id}}}, "compiled": map[string]bool{"old-group": true}}
	directory := t.TempDir()
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(directory, "library.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	e := New(Config{Directory: directory})
	if err := e.loadLibrary(); err != nil {
		t.Fatal(err)
	}
	lib := e.snapshot()
	if lib.Version != libraryVersion || len(lib.Reflexes) != 0 || len(lib.Compiled) != 0 || !lib.Claims[id].Consumed || lib.Claims[id].Task != "old" {
		t.Fatalf("unsafe legacy reuse or lost declarations: %+v", lib)
	}
}

func TestNativeArgumentsRemainOpaque(t *testing.T) {
	call := &aop.ToolCall{Name: "native", Arguments: &aop.EncodedValue{Data: []byte(`{"command":"opaque $VALUE | \"data\"","id":9007199254740993}`)}}
	encoded := canonical(call)
	if encoded != `["native",{"command":"opaque $VALUE | \"data\"","id":9007199254740993}]` {
		t.Fatalf("native argument semantics changed: %s", encoded)
	}
}

func TestProjectionBudgetsStructuredResultsBeforeReaderEchoes(t *testing.T) {
	messages := []*aop.Message{provider.TextMessage("user", "Operate the current resource")}
	appendResult := func(command, text string) {
		call := action(command)
		value := coretool.TextResult(text)
		value.CallId = call.GetToolCall().Id
		messages = append(messages,
			&aop.Message{Role: "assistant", Content: []*aop.Content{call}},
			&aop.Message{Role: "tool", Content: []*aop.Content{{Value: &aop.Content_ToolResult{ToolResult: value}}}})
	}
	appendResult("ordinary acquire resource --handle live", `{"handle":"live"}`)
	payload := `{"text":"actual receipt","version":9007199254740993}`
	encoded, _ := json.Marshal(payload)
	raw := strings.Repeat("ordinary generated program echo\n", 300) + "---\n" + string(encoded)
	for range 4 {
		appendResult("ordinary inspect live", raw)
	}
	projection, ok := contextState(messages)
	if !ok {
		t.Fatal("structured native projection failed")
	}
	input, err := observeInput(projection, map[string]any{"tools": []any{}, "commands": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	history := input["history"].([]map[string]any)
	if len(history) != 5 || input["omitted_evidence"].(int) != 0 || !strings.Contains(string(projection), "9007199254740993") {
		t.Fatalf("echo displaced actual handle/result evidence: history=%d projection=%s", len(history), projection)
	}
	if history[0]["arguments"].(map[string]any)["command"] != "ordinary acquire resource --handle live" {
		t.Fatal("initial actual resource acquisition was lost")
	}
	if coretool.ResultText(provider.MessageToolResult(messages[len(messages)-1])) != raw {
		t.Fatal("private projection modified original evidence")
	}
	compact := evidenceMessages(messages)
	before, _ := json.Marshal(messages)
	after, _ := json.Marshal(compact)
	if len(before) <= 32<<10 || len(after) > 32<<10 {
		t.Fatalf("program echoes still overflow private evidence cache: before=%d after=%d", len(before), len(after))
	}
	for i, message := range compact {
		if result := provider.MessageToolResult(message); result != nil {
			original := provider.MessageToolResult(messages[i])
			if result.CallId != original.CallId || result.IsError != original.IsError || result.Terminate != original.Terminate {
				t.Fatal("private normalization changed native association/status")
			}
		} else if calls := provider.MessageToolCalls(message); len(calls) > 0 && canonical(calls[0]) != canonical(provider.MessageToolCalls(messages[i])[0]) {
			t.Fatal("private normalization changed opaque call arguments")
		}
	}
	if coretool.ResultText(provider.MessageToolResult(messages[len(messages)-1])) != raw {
		t.Fatal("private cache normalization modified original evidence")
	}
}

package jev

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestObserveInvalidBindingYieldsWithoutDispatch(t *testing.T) {
	for name, code := range map[string]string{
		"unknown tool":       `({state: {}, candidates: {go: {name: "invented", arguments: {}, read:false}}})`,
		"null arguments":     `({state: {}, candidates: {go: {name: "bash", arguments: null, read:false}}})`,
		"array arguments":    `({state: {}, candidates: {go: {name: "bash", arguments: [], read:false}}})`,
		"extra field":        `({state: {}, candidates: {go: {name: "bash", arguments: {}, read:false, execute: true}}})`,
		"reserved option":    `({state: {}, candidates: {report: {name: "bash", arguments: {}, read:false}}})`,
		"missing read":       `({state: {}, candidates: {go: {name:"bash", arguments:{}}}})`,
		"missing state":      `({candidates: {}})`,
		"missing choices":    `({state: {}})`,
		"invalid JSON":       `({state: JSON.parse("bad data"), candidates: {}})`,
		"oversized output":   `({state: "x".repeat(40000), candidates: {}})`,
		"too many choices":   `({state:{},candidates:choices(new Array(65).fill(bind("native",{},false)))})`,
		"helper read absent": `({state:{},candidates:choices([bind("native",{})])})`,
		"helper read string": `({state:{},candidates:choices([bind("native",{},"false")])})`,
		"null read":          `({state:{},candidates:choices([{name:"native",arguments:{},read:null}])})`,
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

func TestObserveSandbox(t *testing.T) {
	for name, script := range map[string]string{
		"tool access":       `ExecuteTool("native", {})`,
		"command execution": `commands[0].Run()`,
		"host clock":        `now()`,
		"filesystem":        `require("fs")`,
		"network":           `fetch("https://example.invalid")`,
		"date":              `new Date()`,
		"randomness":        `Math.random()`,
		"expression loop":   `(() => { while(true) {} })()`,
		"function loop":     `() => { while(true) {} }`,
	} {
		t.Run(name, func(t *testing.T) {
			r := observationReflex(t, "js:"+script)
			if _, _, err := r.observe(t.Context(), json.RawMessage(`{"messages":[]}`), observationCapabilities("native")); err == nil {
				t.Fatal("external or unbounded observation accepted")
			}
		})
	}
	t.Run("canceled", func(t *testing.T) {
		r := observationReflex(t, "js:(() => { while(true) {} })()")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, _, err := r.observe(ctx, json.RawMessage(`{"messages":[]}`), observationCapabilities()); err == nil {
			t.Fatal("canceled evaluation continued")
		}
	})
}

func TestObserveProgramEntry(t *testing.T) {
	for name, code := range map[string]string{
		"expression": `js:(() => ({state:{content:user},candidates:choices([bind("ordinary",{resource:user},true)])}))()`,
		"function":   `js:() => ({state:{content:user},candidates:choices([bind("ordinary",{resource:user},true)])})`,
	} {
		t.Run(name, func(t *testing.T) {
			r := observationReflex(t, code)
			input := json.RawMessage(`{"messages":[{"role":"user","text":"current resource"}]}`)
			facts, candidates, err := r.observe(t.Context(), input, observationCapabilities("ordinary"))
			if err != nil || len(candidates) != 1 || !candidates["c0"].Read || !strings.Contains(string(facts), "current resource") || !strings.Contains(string(candidates["c0"].Arguments), "current resource") {
				t.Fatalf("entry lost actual data: facts=%s candidates=%v error=%v", facts, candidates, err)
			}
		})
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

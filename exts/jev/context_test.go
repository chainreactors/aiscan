package jev

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestResultNormalization(t *testing.T) {
	payload := `{"text":"actual receipt","version":9007199254740993,"items":[]}`
	wrapped, _ := json.Marshal(payload)
	items := `{"items":[{"id":"live","label":"Current item"}]}`
	wrappedItems, _ := json.Marshal(items)
	const receipt = `{"items":[],"text":"actual receipt","version":9007199254740993}`
	for _, tc := range []struct {
		name, text, normalized string
	}{
		{"plain text", "ordinary text", ""},
		{"incomplete JSON", "header\n{\"items\":", ""},
		{"trailing prose", "header\n{\"items\":[]}\ntrailing non-JSON", ""},
		{"invalid JSON", "{bad data}", ""},
		{"object", `{"phase":"done"}`, `{"phase":"done"}`},
		{"multiline object", "header\n---\n{\n\"phase\":\"done\"\n}", `{"phase":"done"}`},
		{"array", "header\n[1,2]", `[1,2]`},
		{"encoded object", "header\n\"{\\\"phase\\\":\\\"done\\\"}\"", `{"phase":"done"}`},
		{"program echo", "Ordinary program echo: (() => { return {unrelated:true}; })()\n---\n" + string(wrappedItems), items},
		{"long program echo", strings.Repeat("program echo\n", 1000) + "---\n" + string(wrapped), receipt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			valid := tc.normalized != ""
			if got := resultJSON(tc.text); (got != nil) != valid {
				t.Fatalf("JSON acceptance changed: %+v", got)
			}
			normalized, data := normalizedResult(tc.text)
			want := tc.normalized
			if !valid {
				want = tc.text
			}
			if (data != nil) != valid || normalized != want {
				t.Fatalf("normalization changed evidence: text=%s data=%v", normalized, data)
			}
			input, _ := json.Marshal(map[string]any{"messages": []any{
				map[string]any{"role": "user", "text": "Choose an item"},
				map[string]any{"role": "assistant", "calls": []any{map[string]any{"id": "read", "name": "arbitrary", "arguments": map[string]any{}}}},
				map[string]any{"role": "tool", "call_id": "read", "text": tc.text},
			}})
			env, err := observeInput(input, observationCapabilities())
			if err != nil {
				t.Fatal(err)
			}
			history := env["history"].([]map[string]any)
			original := env["messages"].([]map[string]any)[2]["text"]
			if len(history) != 1 || history[0]["text"] != normalized || original != tc.text || (history[0]["data"] != nil) != valid {
				t.Fatalf("lost original or normalized evidence: %+v", env)
			}
			if resultSummary(tc.text) != clip(normalized, 2048) {
				t.Fatal("summary differs from normalized result")
			}
		})
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

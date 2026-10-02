package agent

import (
	"encoding/json"
	"testing"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestUnknownToolReturnsSemanticErrorAndAllowsSelfCorrection(t *testing.T) {
	echo := &recordingTool{name: "echo", output: "echoed"}
	llm := &scriptedProvider{responses: []*ChatCompletionResponse{
		chatResponse(ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{
			ID: "bad-1", Type: "function", Function: FunctionCall{
				Name: "echo_typo", Arguments: `{"value":"first"}`,
			},
		}}}),
		chatResponse(ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{
			ID: "bad-2", Type: "function", Function: FunctionCall{
				Name: "echo_still_wrong", Arguments: `{"value":"second"}`,
			},
		}}}),
		chatResponse(ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{
			ID: "good", Type: "function", Function: FunctionCall{
				Name: "echo", Arguments: `{"value":"corrected"}`,
			},
		}}}),
		chatResponse(NewTextMessage("assistant", "done")),
	}}

	result, err := NewAgent(Config{
		Loop:     StandardLoop{},
		Provider: llm,
		Tools:    newTestTools(t, echo),
		Model:    "test",
	}).Run(t.Context(), TextInput("use the echo tool"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "done" {
		t.Fatalf("output = %q, want done", result.Output)
	}
	if calls := echo.callsSnapshot(); len(calls) != 1 || calls[0] != `{"value":"corrected"}` {
		t.Fatalf("tool calls = %#v, want only corrected call", calls)
	}

	requests := llm.requestsSnapshot()
	if len(requests) != 4 {
		t.Fatalf("provider requests = %d, want four turns", len(requests))
	}
	assertUnknownToolResult(t, requests[1], "bad-1", "echo_typo", map[string]any{"value": "first"})
	assertUnknownToolResult(t, requests[2], "bad-2", "echo_still_wrong", map[string]any{"value": "second"})
}

func TestMalformedToolCallReturnsSemanticErrorAndAllowsSelfCorrection(t *testing.T) {
	echo := &recordingTool{name: "echo", output: "echoed"}
	llm := &scriptedProvider{responses: []*ChatCompletionResponse{
		{
			Choices: []Choice{{
				Message: ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{
					ID: "invalid", Type: "function", Function: FunctionCall{
						Name: "echo", Arguments: `{"value":`,
					},
				}}}.toAOP(),
				FinishReason: "tool_calls",
			}}},
		chatResponse(ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{
			ID: "valid", Type: "function", Function: FunctionCall{
				Name: "echo", Arguments: `{"value":"fixed"}`,
			},
		}}}),
		chatResponse(NewTextMessage("assistant", "done")),
	}}
	result, err := NewAgent(Config{
		Loop:     StandardLoop{},
		Provider: llm,
		Tools:    newTestTools(t, echo),
		Model:    "test",
	}).Run(t.Context(), TextInput("use echo"))
	if err != nil || result.Output != "done" {
		t.Fatalf("Run() = output %q, err %v", result.Output, err)
	}
	if calls := echo.callsSnapshot(); len(calls) != 1 || calls[0] != `{"value":"fixed"}` {
		t.Fatalf("tool calls = %#v, want only corrected call", calls)
	}
	requests := llm.requestsSnapshot()
	if len(requests) != 3 {
		t.Fatalf("provider requests = %d, want three turns", len(requests))
	}
	var resultMessage *aop.ToolResult
	for _, message := range requests[1].Messages {
		if message.Role == "tool" {
			resultMessage = provider.MessageToolResult(message)
			if resultMessage != nil && resultMessage.CallId == "invalid" {
				break
			}
		}
	}
	if resultMessage == nil || !resultMessage.IsError {
		t.Fatalf("invalid call result = %#v", resultMessage)
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Requested struct {
			Arguments map[string]any `json:"arguments"`
		} `json:"requested"`
	}
	if err := json.Unmarshal([]byte(coretool.ResultText(resultMessage)), &payload); err != nil {
		t.Fatalf("invalid call result is not JSON: %v", err)
	}
	if payload.Error.Code != "invalid_tool_call" || len(payload.Requested.Arguments) != 0 {
		t.Fatalf("invalid call payload = %+v", payload)
	}
}

func assertUnknownToolResult(t *testing.T, req *ChatCompletionRequest, callID, name string, wantArguments map[string]any) {
	t.Helper()
	var found *aop.ToolResult
	for _, message := range req.Messages {
		if message == nil || message.Role != "tool" {
			continue
		}
		if result := provider.MessageToolResult(message); result != nil && result.CallId == callID {
			found = result
			break
		}
	}
	if found == nil || !found.IsError || found.Name != name {
		t.Fatalf("semantic tool result = %#v, want error for %s/%s", found, callID, name)
	}
	var payload struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Requested struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"requested"`
		Available []struct {
			Name string `json:"name"`
		} `json:"available_tools"`
	}
	if err := json.Unmarshal([]byte(coretool.ResultText(found)), &payload); err != nil {
		t.Fatalf("semantic result is not JSON: %v; output=%q", err, coretool.ResultText(found))
	}
	if payload.OK || payload.Error.Code != "unknown_tool" || payload.Requested.Name != name {
		t.Fatalf("semantic payload = %+v", payload)
	}
	if len(payload.Requested.Arguments) != len(wantArguments) || payload.Requested.Arguments["value"] != wantArguments["value"] {
		t.Fatalf("requested arguments = %#v, want %#v", payload.Requested.Arguments, wantArguments)
	}
	if len(payload.Available) != 1 || payload.Available[0].Name != "echo" {
		t.Fatalf("available tools = %#v", payload.Available)
	}
}

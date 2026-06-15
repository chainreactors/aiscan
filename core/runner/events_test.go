package runner

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/agent"
)

func TestEventsFileSubscriberAppendsJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := newEventsFileSubscriber(path)
	if err != nil {
		t.Fatalf("newEventsFileSubscriber() error = %v", err)
	}
	defer w.Close()

	content := "spray returned no results"
	events := []agent.Event{
		{Type: agent.EventAgentStart},
		{Type: agent.EventTurnStart, Turn: 1},
		{
			Type:      agent.EventToolExecutionStart,
			Turn:      1,
			ToolName:  "bash",
			Arguments: `{"command":"spray -u http://x"}`,
		},
		{
			Type:    agent.EventToolExecutionEnd,
			Turn:    1,
			Result:  "ok",
			IsError: false,
		},
		{
			Type: agent.EventMessageEnd,
			Turn: 1,
			Message: agent.ChatMessage{
				Role:    "assistant",
				Content: &content,
			},
		},
		{Type: agent.EventAgentEnd, Turn: 1, Stop: agent.StopReasonCompleted, NewMessages: make([]agent.ChatMessage, 3)},
	}
	for _, e := range events {
		w.HandleEvent(e)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open events file: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var lines []map[string]any
	for scanner.Scan() {
		var m map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			t.Fatalf("invalid JSON line %q: %v", scanner.Text(), err)
		}
		lines = append(lines, m)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan events file: %v", err)
	}
	if got, want := len(lines), len(events); got != want {
		t.Fatalf("line count = %d, want %d", got, want)
	}

	if lines[0]["type"] != string(agent.EventAgentStart) {
		t.Errorf("line[0].type = %v, want %s", lines[0]["type"], agent.EventAgentStart)
	}
	if _, ok := lines[0]["ts"].(string); !ok {
		t.Errorf("line[0] missing ts field")
	}
	if lines[2]["tool_name"] != "bash" {
		t.Errorf("line[2].tool_name = %v, want bash", lines[2]["tool_name"])
	}
	if v, _ := lines[5]["new_messages"].(float64); v != 3 {
		t.Errorf("line[5].new_messages = %v, want 3", lines[5]["new_messages"])
	}
	if v, _ := lines[5]["stop"].(string); v != "completed" {
		t.Errorf("line[5].stop = %v, want completed", lines[5]["stop"])
	}
}

func TestEventsFileSubscriberLargeFieldsPassThrough(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := newEventsFileSubscriber(path)
	if err != nil {
		t.Fatalf("newEventsFileSubscriber() error = %v", err)
	}
	defer w.Close()

	huge := strings.Repeat("a", 20*1024)
	w.HandleEvent(agent.Event{
		Type:   agent.EventToolExecutionEnd,
		Result: huge,
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), huge) {
		t.Fatalf("expected full result in event log")
	}
}

func TestEventsFileSubscriberLLMRequest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := newEventsFileSubscriber(path)
	if err != nil {
		t.Fatalf("newEventsFileSubscriber() error = %v", err)
	}
	defer w.Close()

	w.HandleEvent(agent.Event{
		Type: agent.EventLLMRequest,
		Turn: 1,
		Request: &agent.ChatCompletionRequest{
			Model:    "deepseek-v4-pro",
			Messages: make([]agent.ChatMessage, 5),
			Tools:    make([]agent.ToolDefinition, 3),
		},
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, _ := m["request_model"].(string); v != "deepseek-v4-pro" {
		t.Errorf("request_model = %v, want deepseek-v4-pro", m["request_model"])
	}
	if v, _ := m["request_messages"].(float64); v != 5 {
		t.Errorf("request_messages = %v, want 5", m["request_messages"])
	}
	if v, _ := m["request_tools"].(float64); v != 3 {
		t.Errorf("request_tools = %v, want 3", m["request_tools"])
	}
}

func TestEventsFileSubscriberToolEndNoArgs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := newEventsFileSubscriber(path)
	if err != nil {
		t.Fatalf("newEventsFileSubscriber() error = %v", err)
	}
	defer w.Close()

	w.HandleEvent(agent.Event{
		Type:       agent.EventToolExecutionEnd,
		Turn:       1,
		ToolCallID: "call-1",
		ToolName:   "bash",
		Result:     "ok",
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if strings.Contains(string(data), "arguments") {
		t.Errorf("tool_execution_end should not contain arguments field, got: %s", data)
	}
}

func TestEventsFileSubscriberErrorField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := newEventsFileSubscriber(path)
	if err != nil {
		t.Fatalf("newEventsFileSubscriber() error = %v", err)
	}
	defer w.Close()

	w.HandleEvent(agent.Event{
		Type:    agent.EventToolExecutionEnd,
		Turn:    1,
		IsError: true,
		Err:     fmt.Errorf("connection refused"),
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, _ := m["error"].(string); v != "connection refused" {
		t.Errorf("error = %v, want connection refused", m["error"])
	}
}

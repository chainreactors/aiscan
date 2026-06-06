package runner

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/provider"
)

func TestEventsFileSubscriberAppendsJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := newEventsFileSubscriber(path)
	if err != nil {
		t.Fatalf("newEventsFileSubscriber() error = %v", err)
	}

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
			Message: provider.ChatMessage{
				Role:    "assistant",
				Content: &content,
			},
		},
		{Type: agent.EventAgentEnd, Turn: 1, NewMessages: make([]provider.ChatMessage, 3)},
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
}

func TestEventsFileSubscriberTruncatesLargeFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	w, err := newEventsFileSubscriber(path)
	if err != nil {
		t.Fatalf("newEventsFileSubscriber() error = %v", err)
	}

	huge := strings.Repeat("a", agent.EventResultLimit+1024)
	w.HandleEvent(agent.Event{
		Type:   agent.EventToolExecutionEnd,
		Result: huge,
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "[truncated") {
		t.Fatalf("expected truncation marker in: %s", string(data))
	}
	if len(data) > agent.EventResultLimit+2048 {
		t.Fatalf("written line should be bounded; got %d bytes", len(data))
	}
}

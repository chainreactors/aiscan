package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/provider"
)

const (
	eventPreviewLimit = 4 * 1024
	eventResultLimit  = 16 * 1024
)

type eventsFileSubscriber struct {
	mu   sync.Mutex
	file *os.File
}

func newEventsFileSubscriber(path string) (*eventsFileSubscriber, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open events file %s: %w", path, err)
	}
	return &eventsFileSubscriber{file: f}, nil
}

func (w *eventsFileSubscriber) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
}

func (w *eventsFileSubscriber) HandleEvent(event agent.Event) {
	line, err := json.Marshal(serializableEvent(event))
	if err != nil {
		return
	}
	line = append(line, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.file.Write(line)
	_ = w.file.Sync()
}

type eventJSON struct {
	Timestamp     string          `json:"ts"`
	Type          agent.EventType `json:"type"`
	Turn          int             `json:"turn,omitempty"`
	Message       *messageJSON    `json:"message,omitempty"`
	ToolResults   []messageJSON   `json:"tool_results,omitempty"`
	ToolCallID    string          `json:"tool_call_id,omitempty"`
	ToolName      string          `json:"tool_name,omitempty"`
	Arguments     string          `json:"arguments,omitempty"`
	Result        string          `json:"result,omitempty"`
	IsError       bool            `json:"is_error,omitempty"`
	Error         string          `json:"error,omitempty"`
	NewMessages   int             `json:"new_messages,omitempty"`
	Usage         *provider.Usage `json:"usage,omitempty"`
	ContextTokens int             `json:"context_tokens,omitempty"`
}

type messageJSON struct {
	Role       string                `json:"role"`
	Content    string                `json:"content,omitempty"`
	ToolCalls  []messageToolCallJSON `json:"tool_calls,omitempty"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
}

type messageToolCallJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func serializableEvent(e agent.Event) eventJSON {
	out := eventJSON{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Type:       e.Type,
		Turn:       e.Turn,
		ToolCallID: e.ToolCallID,
		ToolName:   e.ToolName,
		Arguments:  truncate(e.Arguments, eventPreviewLimit),
		Result:     truncate(e.Result, eventResultLimit),
		IsError:    e.IsError,
	}
	if e.Err != nil {
		out.Error = e.Err.Error()
	}
	if m := toMessageJSON(e.Message); m != nil {
		out.Message = m
	}
	for _, msg := range e.ToolResults {
		if m := toMessageJSON(msg); m != nil {
			out.ToolResults = append(out.ToolResults, *m)
		}
	}
	if len(e.NewMessages) > 0 {
		out.NewMessages = len(e.NewMessages)
	}
	if e.Usage != nil {
		out.Usage = e.Usage
		out.ContextTokens = e.ContextTokens
	}
	return out
}

func toMessageJSON(msg provider.ChatMessage) *messageJSON {
	if msg.Role == "" && msg.Content == nil && len(msg.ContentParts) == 0 && len(msg.ToolCalls) == 0 && msg.ToolCallID == "" {
		return nil
	}
	out := &messageJSON{
		Role:       msg.Role,
		ToolCallID: msg.ToolCallID,
	}
	if len(msg.ContentParts) > 0 {
		for _, part := range msg.ContentParts {
			if part.Type == "text" {
				out.Content += part.Text
			} else if part.Type == "image_url" && part.ImageURL != nil {
				mediaType, _ := provider.ParseDataURI(part.ImageURL.URL)
				out.Content += fmt.Sprintf("[image: %s]", mediaType)
			}
		}
		out.Content = truncate(out.Content, eventPreviewLimit)
	} else if msg.Content != nil {
		out.Content = truncate(*msg.Content, eventPreviewLimit)
	}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, messageToolCallJSON{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: truncate(tc.Function.Arguments, eventPreviewLimit),
		})
	}
	return out
}

func truncate(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("...[truncated %d bytes]", len(s)-limit)
}

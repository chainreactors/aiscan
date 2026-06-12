package agent

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	EventPreviewLimit = 4 * 1024
	EventResultLimit  = 16 * 1024
)

// MessageJSON is the JSONL serialization shape for a ChatMessage.
// Exported so external consumers (harness, monitors) can deserialize event files.
type MessageJSON struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// MarshalJSON serializes an Event to compact JSONL.
// Heavy fields (Request body, full Messages history) are reduced to metadata;
// Arguments and Result are truncated to bounded limits.
func (e Event) MarshalJSON() ([]byte, error) {
	ts := e.EmittedAt
	if ts.IsZero() {
		ts = time.Now()
	}

	out := struct {
		Timestamp       string       `json:"ts"`
		Type            EventType    `json:"type"`
		SessionID       string       `json:"session_id,omitempty"`
		ParentSessionID string       `json:"parent_session_id,omitempty"`
		Turn            int          `json:"turn,omitempty"`
		Message         *MessageJSON `json:"message,omitempty"`
		ToolResults     []MessageJSON `json:"tool_results,omitempty"`
		ToolCallID      string       `json:"tool_call_id,omitempty"`
		ToolName        string       `json:"tool_name,omitempty"`
		Arguments       string       `json:"arguments,omitempty"`
		Result          string       `json:"result,omitempty"`
		IsError         bool         `json:"is_error,omitempty"`
		Error           string       `json:"error,omitempty"`
		Stop            StopReason   `json:"stop,omitempty"`
		NewMessages     int          `json:"new_messages,omitempty"`
		Usage           *Usage       `json:"usage,omitempty"`
		ContextTokens   int          `json:"context_tokens,omitempty"`
		RequestModel    string       `json:"request_model,omitempty"`
		RequestMessages int          `json:"request_messages,omitempty"`
		RequestTools    int          `json:"request_tools,omitempty"`
	}{
		Timestamp:       ts.UTC().Format(time.RFC3339Nano),
		Type:            e.Type,
		SessionID:       e.SessionID,
		ParentSessionID: e.ParentSessionID,
		Turn:            e.Turn,
		ToolCallID:    e.ToolCallID,
		ToolName:      e.ToolName,
		Arguments:     TruncateField(e.Arguments, EventPreviewLimit),
		Result:        TruncateField(e.Result, EventResultLimit),
		IsError:       e.IsError,
		Stop:          e.Stop,
		ContextTokens: e.ContextTokens,
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
	}
	if e.Request != nil {
		out.RequestModel = e.Request.Model
		out.RequestMessages = len(e.Request.Messages)
		out.RequestTools = len(e.Request.Tools)
	}

	return json.Marshal(out)
}

func toMessageJSON(msg ChatMessage) *MessageJSON {
	if msg.Role == "" && msg.Content == nil && len(msg.ContentParts) == 0 && len(msg.ToolCalls) == 0 && msg.ToolCallID == "" {
		return nil
	}
	out := &MessageJSON{
		Role:       msg.Role,
		ToolCallID: msg.ToolCallID,
	}
	if len(msg.ContentParts) > 0 {
		for _, part := range msg.ContentParts {
			if part.Type == "text" {
				out.Content += part.Text
			} else if part.Type == "image_url" && part.ImageURL != nil {
				mediaType, _ := ParseDataURI(part.ImageURL.URL)
				out.Content += fmt.Sprintf("[image: %s]", mediaType)
			}
		}
		out.Content = TruncateField(out.Content, EventPreviewLimit)
	} else if msg.Content != nil {
		out.Content = TruncateField(*msg.Content, EventPreviewLimit)
	}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: FunctionCall{
				Name:      tc.Function.Name,
				Arguments: TruncateField(tc.Function.Arguments, EventPreviewLimit),
			},
		})
	}
	return out
}

func TruncateField(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + fmt.Sprintf("...[truncated %d bytes]", len(s)-limit)
}

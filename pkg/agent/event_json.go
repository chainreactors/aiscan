package agent

import (
	"fmt"
	"time"
)

const (
	EventPreviewLimit = 4 * 1024
	EventResultLimit  = 16 * 1024
)

type EventJSON struct {
	Timestamp     string        `json:"ts"`
	Type          EventType     `json:"type"`
	SessionID     string        `json:"session_id,omitempty"`
	Turn          int           `json:"turn,omitempty"`
	Message       *MessageJSON  `json:"message,omitempty"`
	ToolResults   []MessageJSON `json:"tool_results,omitempty"`
	ToolCallID    string        `json:"tool_call_id,omitempty"`
	ToolName      string        `json:"tool_name,omitempty"`
	Arguments     string        `json:"arguments,omitempty"`
	Result        string        `json:"result,omitempty"`
	IsError       bool          `json:"is_error,omitempty"`
	Error         string        `json:"error,omitempty"`
	NewMessages   int           `json:"new_messages,omitempty"`
	Usage         *Usage        `json:"usage,omitempty"`
	ContextTokens int           `json:"context_tokens,omitempty"`
}

type MessageJSON struct {
	Role             string                `json:"role"`
	Content          string                `json:"content,omitempty"`
	ReasoningContent string                `json:"reasoning_content,omitempty"`
	ToolCalls        []MessageToolCallJSON `json:"tool_calls,omitempty"`
	ToolCallID       string                `json:"tool_call_id,omitempty"`
	StopReason       string                `json:"stop_reason,omitempty"`
}

type MessageToolCallJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func SerializableEvent(e Event) EventJSON {
	out := EventJSON{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Type:       e.Type,
		SessionID:  e.SessionID,
		Turn:       e.Turn,
		ToolCallID: e.ToolCallID,
		ToolName:   e.ToolName,
		Arguments:  TruncateField(e.Arguments, EventPreviewLimit),
		Result:     TruncateField(e.Result, EventResultLimit),
		IsError:    e.IsError,
	}
	if e.Err != nil {
		out.Error = e.Err.Error()
	}
	if m := ToMessageJSON(e.Message); m != nil {
		out.Message = m
	}
	for _, msg := range e.ToolResults {
		if m := ToMessageJSON(msg); m != nil {
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

func ToMessageJSON(msg ChatMessage) *MessageJSON {
	if msg.Role == "" && msg.Content == nil && len(msg.ContentParts) == 0 && len(msg.ToolCalls) == 0 && msg.ToolCallID == "" {
		return nil
	}
	out := &MessageJSON{
		Role:       msg.Role,
		ToolCallID: msg.ToolCallID,
		StopReason: msg.StopReason,
	}
	if msg.ReasoningContent != nil {
		out.ReasoningContent = TruncateField(*msg.ReasoningContent, EventPreviewLimit)
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
		out.ToolCalls = append(out.ToolCalls, MessageToolCallJSON{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: TruncateField(tc.Function.Arguments, EventPreviewLimit),
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

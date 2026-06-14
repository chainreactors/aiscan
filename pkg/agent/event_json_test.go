package agent

import "testing"

func TestSerializableEventIncludesReasoningContent(t *testing.T) {
	reasoning := "model reasoning preview"
	event := SerializableEvent(Event{
		Type: EventMessageEnd,
		Turn: 1,
		Message: ChatMessage{
			Role:             "assistant",
			ReasoningContent: &reasoning,
		},
	})

	if event.Message == nil {
		t.Fatal("Message is nil")
	}
	if event.Message.ReasoningContent != reasoning {
		t.Fatalf("ReasoningContent = %q, want %q", event.Message.ReasoningContent, reasoning)
	}
}

func TestSerializableEventIncludesStopAndDetail(t *testing.T) {
	event := SerializableEvent(Event{
		Type:   EventAgentEnd,
		Stop:   StopReasonCompleted,
		Detail: "assistant response had no tool calls",
	})

	if event.Stop != StopReasonCompleted {
		t.Fatalf("Stop = %q, want %q", event.Stop, StopReasonCompleted)
	}
	if event.Detail != "assistant response had no tool calls" {
		t.Fatalf("Detail = %q", event.Detail)
	}
}

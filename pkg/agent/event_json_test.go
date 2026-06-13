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

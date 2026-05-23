package agent

import (
	"sync"
)

// InboxMessage is an external input delivered to an active agent run. It keeps
// source metadata until the loop renders it into an LLM chat message.
type InboxMessage struct {
	Source     string
	Kind       string
	Sender     string
	MessageID  string
	Content    string
	Attributes map[string]string
	Targets    []string
	Refs       map[string][]string
	Meta       map[string]any
	RawContent map[string]any
}

// Inbox is the unified interface for injecting external messages into the
// agent's conversation loop. Producers (swarm bridge, task manager) call Push;
// the agent loop calls Drain at the start of every turn.
type Inbox interface {
	Push(msg InboxMessage) bool
	Close()
	Closed() bool
	Drain() []InboxMessage
}

// BufferedInbox is a mutex-protected, bounded message buffer that implements
// Inbox. It replaces the raw channel previously used for agent inbox delivery,
// eliminating close-panic issues and unifying backpressure policy.
type BufferedInbox struct {
	mu       sync.Mutex
	buf      []InboxMessage
	capacity int
	closed   bool
}

// NewBufferedInbox returns an Inbox with the given maximum capacity.
// Non-positive capacities are treated as 1. Push returns false when the buffer
// is full or closed.
func NewBufferedInbox(capacity int) *BufferedInbox {
	if capacity <= 0 {
		capacity = 1
	}
	return &BufferedInbox{
		buf:      make([]InboxMessage, 0, capacity),
		capacity: capacity,
	}
}

func (b *BufferedInbox) Push(msg InboxMessage) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || len(b.buf) >= b.capacity {
		return false
	}
	b.buf = append(b.buf, cloneInboxMessage(msg))
	return true
}

// PushMessage adapts generic producers such as task.Manager without making
// those packages depend on agent.InboxMessage.
func (b *BufferedInbox) PushMessage(source, kind, content string, attrs map[string]string) bool {
	return b.Push(InboxMessage{
		Source:     source,
		Kind:       kind,
		Content:    content,
		Attributes: attrs,
	})
}

func (b *BufferedInbox) Drain() []InboxMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) == 0 {
		return nil
	}
	out := b.buf
	b.buf = make([]InboxMessage, 0, b.capacity)
	return out
}

func (b *BufferedInbox) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
}

func (b *BufferedInbox) Closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *BufferedInbox) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}

func cloneInboxMessage(msg InboxMessage) InboxMessage {
	cloned := msg
	cloned.Attributes = cloneStringMap(msg.Attributes)
	cloned.Targets = append([]string(nil), msg.Targets...)
	cloned.Refs = cloneStringSliceMap(msg.Refs)
	cloned.Meta = cloneAnyMap(msg.Meta)
	cloned.RawContent = cloneAnyMap(msg.RawContent)
	return cloned
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringSliceMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

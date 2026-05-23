package agent

import (
	"context"
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
	Len() int
	Wait(ctx context.Context) bool
}

// BufferedInbox is a mutex-protected, bounded message buffer that implements
// Inbox. It replaces the raw channel previously used for agent inbox delivery,
// eliminating close-panic issues and unifying backpressure policy.
type BufferedInbox struct {
	mu       sync.Mutex
	buf      []InboxMessage
	capacity int
	closed   bool
	notify   chan struct{}
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
		notify:   make(chan struct{}),
	}
}

func (b *BufferedInbox) Push(msg InboxMessage) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || len(b.buf) >= b.capacity {
		return false
	}
	wasEmpty := len(b.buf) == 0
	b.buf = append(b.buf, cloneInboxMessage(msg))
	if wasEmpty {
		b.wakeLocked()
	}
	return true
}

// PushMessage adapts generic producers such as task.Manager without making
// those packages depend on agent.InboxMessage.
func (b *BufferedInbox) PushMessage(source, kind, content string, attrs map[string]string) bool {
	msg := InboxMessage{
		Source:     source,
		Kind:       kind,
		Content:    content,
		Attributes: attrs,
	}
	if source == "task" && kind == "reminder" {
		return b.pushCoalesced(msg, func(existing InboxMessage) bool {
			return existing.Source == "task" && existing.Kind == "reminder"
		})
	}
	if source == "task" && kind == "completion" {
		return b.pushDropping(msg, func(existing InboxMessage) bool {
			return existing.Source == "task" && existing.Kind == "reminder"
		})
	}
	return b.Push(msg)
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
	if b.closed {
		return
	}
	b.closed = true
	if b.notify != nil {
		close(b.notify)
		b.notify = nil
	}
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

func (b *BufferedInbox) Wait(ctx context.Context) bool {
	for {
		b.mu.Lock()
		if len(b.buf) > 0 {
			b.mu.Unlock()
			return true
		}
		if b.closed {
			b.mu.Unlock()
			return false
		}
		ch := b.notify
		b.mu.Unlock()

		select {
		case <-ctx.Done():
			return false
		case <-ch:
		}
	}
}

func (b *BufferedInbox) pushCoalesced(msg InboxMessage, match func(InboxMessage) bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	cloned := cloneInboxMessage(msg)
	for i, existing := range b.buf {
		if match(existing) {
			b.buf[i] = cloned
			return true
		}
	}
	if len(b.buf) >= b.capacity {
		return false
	}
	wasEmpty := len(b.buf) == 0
	b.buf = append(b.buf, cloned)
	if wasEmpty {
		b.wakeLocked()
	}
	return true
}

func (b *BufferedInbox) pushDropping(msg InboxMessage, drop func(InboxMessage) bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	if len(b.buf) >= b.capacity {
		dropIndex := -1
		for i, existing := range b.buf {
			if drop(existing) {
				dropIndex = i
				break
			}
		}
		if dropIndex == -1 {
			return false
		}
		b.buf = append(b.buf[:dropIndex], b.buf[dropIndex+1:]...)
	}
	wasEmpty := len(b.buf) == 0
	b.buf = append(b.buf, cloneInboxMessage(msg))
	if wasEmpty {
		b.wakeLocked()
	}
	return true
}

func (b *BufferedInbox) wakeLocked() {
	if b.notify == nil {
		return
	}
	close(b.notify)
	b.notify = make(chan struct{})
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

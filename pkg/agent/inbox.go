package agent

import (
	"sync"

	"github.com/chainreactors/aiscan/pkg/provider"
)

// Inbox is the unified interface for injecting external messages into the
// agent's conversation loop. Producers (swarm bridge, task manager) call Push;
// the agent loop calls Drain at the start of every turn.
type Inbox interface {
	Push(msg provider.ChatMessage) bool
	Drain() []provider.ChatMessage
	Close()
	Closed() bool
}

// BufferedInbox is a mutex-protected, bounded message buffer that implements
// Inbox. It replaces the raw channel previously used for agent inbox delivery,
// eliminating close-panic issues and unifying backpressure policy.
type BufferedInbox struct {
	mu       sync.Mutex
	buf      []provider.ChatMessage
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
		buf:      make([]provider.ChatMessage, 0, capacity),
		capacity: capacity,
	}
}

func (b *BufferedInbox) Push(msg provider.ChatMessage) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || len(b.buf) >= b.capacity {
		return false
	}
	b.buf = append(b.buf, msg)
	return true
}

func (b *BufferedInbox) Drain() []provider.ChatMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) == 0 {
		return nil
	}
	out := b.buf
	b.buf = make([]provider.ChatMessage, 0, b.capacity)
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

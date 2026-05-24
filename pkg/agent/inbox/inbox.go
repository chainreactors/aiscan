package inbox

import (
	"sort"
	"sync"
)

type Inbox interface {
	Push(msg Message) bool
	Drain() []Message
	Close()
	Closed() bool
}

type Buffered struct {
	mu       sync.Mutex
	buf      []Message
	capacity int
	closed   bool
}

func NewBuffered(capacity int) *Buffered {
	if capacity <= 0 {
		capacity = 1
	}
	return &Buffered{
		buf:      make([]Message, 0, capacity),
		capacity: capacity,
	}
}

func (b *Buffered) Push(msg Message) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || len(b.buf) >= b.capacity {
		return false
	}
	b.buf = append(b.buf, msg)
	return true
}

func (b *Buffered) Drain() []Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) == 0 {
		return nil
	}
	out := b.buf
	b.buf = make([]Message, 0, b.capacity)
	if needsSort(out) {
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].Priority > out[j].Priority
		})
	}
	return out
}

func needsSort(msgs []Message) bool {
	for i := 1; i < len(msgs); i++ {
		if msgs[i].Priority > msgs[i-1].Priority {
			return true
		}
	}
	return false
}

func (b *Buffered) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
}

func (b *Buffered) Closed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *Buffered) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.buf)
}

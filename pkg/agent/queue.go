package agent

import (
	"sync"

	"github.com/chainreactors/aiscan/pkg/agent/inbox"
)

// QueueMode controls how many queued messages are delivered at each drain point.
type QueueMode string

const (
	QueueModeOneAtATime QueueMode = "one-at-a-time"
	QueueModeAll        QueueMode = "all"
)

type pendingMessageQueue struct {
	mu       sync.Mutex
	mode     QueueMode
	messages []inbox.Message
}

func newPendingMessageQueue(mode QueueMode) *pendingMessageQueue {
	if mode == "" {
		mode = QueueModeOneAtATime
	}
	return &pendingMessageQueue{mode: mode}
}

func (q *pendingMessageQueue) enqueue(msg inbox.Message) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, msg)
}

func (q *pendingMessageQueue) drain() []inbox.Message {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.messages) == 0 {
		return nil
	}
	if q.mode == QueueModeAll {
		out := append([]inbox.Message(nil), q.messages...)
		q.messages = nil
		return out
	}
	out := []inbox.Message{q.messages[0]}
	q.messages = append([]inbox.Message(nil), q.messages[1:]...)
	return out
}

func (q *pendingMessageQueue) clear() []inbox.Message {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	out := append([]inbox.Message(nil), q.messages...)
	q.messages = nil
	return out
}

func (q *pendingMessageQueue) len() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.messages)
}

func (q *pendingMessageQueue) setMode(mode QueueMode) {
	if q == nil || mode == "" {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.mode = mode
}

func (q *pendingMessageQueue) getMode() QueueMode {
	if q == nil {
		return QueueModeOneAtATime
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.mode == "" {
		return QueueModeOneAtATime
	}
	return q.mode
}

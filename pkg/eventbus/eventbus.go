package eventbus

import "sync"

type entry[T any] struct {
	id      int
	handler func(T)
}

type Bus[T any] struct {
	mu   sync.RWMutex
	subs []entry[T]
	next int
}

func New[T any]() *Bus[T] {
	return &Bus[T]{}
}

func (b *Bus[T]) Subscribe(handler func(T)) func() {
	b.mu.Lock()
	id := b.next
	b.next++
	b.subs = append(b.subs, entry[T]{id: id, handler: handler})
	b.mu.Unlock()
	return func() { b.unsubscribe(id) }
}

func (b *Bus[T]) unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.subs {
		if s.id == id {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			return
		}
	}
}

func (b *Bus[T]) Emit(event T) {
	b.mu.RLock()
	snapshot := make([]func(T), len(b.subs))
	for i, s := range b.subs {
		snapshot[i] = s.handler
	}
	b.mu.RUnlock()

	for _, h := range snapshot {
		h(event)
	}
}

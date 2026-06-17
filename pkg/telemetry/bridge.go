package telemetry

import "sync"

// Envelope is the unit of telemetry data sent from an aiscan process to
// external sinks (WebSocket, file, etc). It carries no serialization
// dependency — the Sink decides how to encode Data.
type Envelope struct {
	Source string // origin: "agent", "scanner", "log", ...
	Type   string // event type: "turn_start", "tool_call", "info", ...
	Data   any    // payload, interpreted by the sink
}

// Sink receives telemetry envelopes. Implementations must be safe for
// concurrent calls.
type Sink func(Envelope)

type sinkEntry struct {
	id int
	fn Sink
}

// Bridge fans out Envelopes to registered Sinks. It is the single
// collection point for all telemetry within a process — agent events,
// scanner observations, log lines — and forwards them to any number of
// external consumers.
type Bridge struct {
	mu    sync.RWMutex
	sinks []sinkEntry
	next  int
}

func NewBridge() *Bridge { return &Bridge{} }

// AddSink registers a sink and returns an unsubscribe function.
func (b *Bridge) AddSink(sink Sink) func() {
	b.mu.Lock()
	id := b.next
	b.next++
	b.sinks = append(b.sinks, sinkEntry{id: id, fn: sink})
	b.mu.Unlock()
	return func() { b.removeSink(id) }
}

func (b *Bridge) removeSink(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, s := range b.sinks {
		if s.id == id {
			b.sinks = append(b.sinks[:i], b.sinks[i+1:]...)
			return
		}
	}
}

// Send dispatches an envelope to all registered sinks.
func (b *Bridge) Send(e Envelope) {
	b.mu.RLock()
	snapshot := make([]Sink, len(b.sinks))
	for i, s := range b.sinks {
		snapshot[i] = s.fn
	}
	b.mu.RUnlock()
	for _, fn := range snapshot {
		fn(e)
	}
}

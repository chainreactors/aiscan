package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// HubEvent is the unit broadcast through the SSE hub. Type is the SSE
// event name, Data is pre-serialized JSON written directly to the stream.
type HubEvent struct {
	Type string
	Data json.RawMessage
	// Reliable marks an event that must never be dropped under backpressure.
	// The terminal "complete"/"error" events set it: losing one strands the
	// client at its last-seen progress with no recovery, because the SSE
	// connection stays alive on keepalives so the browser never reconnects to
	// re-derive terminal state via catch-up. Lossy stream events (progress/
	// stats) leave it false — a full buffer drops them, which a later snapshot
	// or the final result supersedes.
	Reliable bool
}

type BroadcastCallback func(id string, event HubEvent)

type Hub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan HubEvent]struct{}
	callback    BroadcastCallback
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]map[chan HubEvent]struct{}),
	}
}

func (h *Hub) OnBroadcast(cb BroadcastCallback) {
	h.mu.Lock()
	h.callback = cb
	h.mu.Unlock()
}

func (h *Hub) Subscribe(id string) (<-chan HubEvent, func()) {
	ch := make(chan HubEvent, 64)
	h.mu.Lock()
	if _, ok := h.subscribers[id]; !ok {
		h.subscribers[id] = make(map[chan HubEvent]struct{})
	}
	h.subscribers[id][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if bucket, ok := h.subscribers[id]; ok {
			delete(bucket, ch)
			if len(bucket) == 0 {
				delete(h.subscribers, id)
			}
		}
		close(ch)
		h.mu.Unlock()
	}
}

func (h *Hub) Broadcast(id string, event HubEvent) {
	h.mu.Lock()
	cb := h.callback
	for ch := range h.subscribers[id] {
		enqueue(ch, event)
	}
	h.mu.Unlock()
	if cb != nil {
		cb(id, event)
	}
}

// enqueue puts event on one subscriber channel without ever blocking the
// producer. A lossy event (progress/stats) is dropped when the subscriber is
// behind and its buffer is full — a later snapshot or the final result
// supersedes it. A Reliable event (terminal complete/error) must not be
// dropped: losing it leaves the client stuck at its last progress forever. When
// the buffer is full, evict the oldest (necessarily lossy) event to make room,
// then enqueue. This runs under h.mu, which gates unsubscribe's close(ch) so the
// channel is never closed mid-send, and blocks any other Broadcast from filling
// the buffer concurrently — so the eviction loop is bounded by buffer capacity
// (a live consumer only drains it faster) and every branch is non-blocking.
func enqueue(ch chan HubEvent, event HubEvent) {
	if !event.Reliable {
		select {
		case ch <- event:
		default:
		}
		return
	}
	for {
		select {
		case ch <- event:
			return
		default:
		}
		select {
		case <-ch: // drop the oldest buffered event, then retry the send
		default:
		}
	}
}

// ServeSSE streams a topic's hub events to an SSE client until the request is
// canceled or a terminal event is seen. catchUp (may be nil) runs immediately
// after subscribing and returns events to replay for a client that connected
// after they were broadcast: the hub is broadcast-only with no backlog, so a
// topic that reached a terminal state during connection setup would otherwise
// never deliver it. Correctness relies on the producer updating durable state
// to terminal BEFORE broadcasting the terminal event — subscribing first, then
// deriving catch-up from that state, closes the race in both directions.
func ServeSSE(w http.ResponseWriter, r *http.Request, hub *Hub, id string, catchUp func() []HubEvent, terminalEvents ...string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Subscribe before announcing the stream is open, so no event broadcast
	// during connection setup can slip through the gap between flush and
	// subscribe (a fast scan can finish in that window).
	ch, unsubscribe := hub.Subscribe(id)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Replay any terminal state reached before we subscribed. Anything that
	// happens after Subscribe still arrives on ch below, so at worst a client
	// sees the terminal event once here and we return before draining ch.
	if catchUp != nil {
		for _, event := range catchUp() {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.Data)
			flusher.Flush()
			if isTerminalEvent(event.Type, terminalEvents) {
				return
			}
		}
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case event, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.Data)
			flusher.Flush()
			if isTerminalEvent(event.Type, terminalEvents) {
				return
			}
		}
	}
}

func isTerminalEvent(eventType string, terminalEvents []string) bool {
	if len(terminalEvents) == 0 {
		return eventType == "complete" || eventType == "error"
	}
	for _, t := range terminalEvents {
		if eventType == t {
			return true
		}
	}
	return false
}

// mustJSON marshals v to json.RawMessage. Panics on error (should never
// happen with map/struct inputs).
func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

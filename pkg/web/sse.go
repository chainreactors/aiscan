package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type BroadcastCallback func(scanID string, event ScanEvent)

type Hub struct {
	mu          sync.Mutex
	subscribers map[string]map[chan ScanEvent]struct{}
	callback    BroadcastCallback
}

func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]map[chan ScanEvent]struct{}),
	}
}

func (h *Hub) OnBroadcast(cb BroadcastCallback) {
	h.mu.Lock()
	h.callback = cb
	h.mu.Unlock()
}

func (h *Hub) Subscribe(scanID string) (<-chan ScanEvent, func()) {
	ch := make(chan ScanEvent, 64)
	h.mu.Lock()
	if _, ok := h.subscribers[scanID]; !ok {
		h.subscribers[scanID] = make(map[chan ScanEvent]struct{})
	}
	h.subscribers[scanID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if bucket, ok := h.subscribers[scanID]; ok {
			delete(bucket, ch)
			if len(bucket) == 0 {
				delete(h.subscribers, scanID)
			}
		}
		close(ch)
		h.mu.Unlock()
	}
}

func (h *Hub) Broadcast(scanID string, event ScanEvent) {
	h.mu.Lock()
	cb := h.callback
	for ch := range h.subscribers[scanID] {
		select {
		case ch <- event:
		default:
		}
	}
	h.mu.Unlock()
	if cb != nil {
		cb(scanID, event)
	}
}

func ServeSSE(w http.ResponseWriter, r *http.Request, hub *Hub, scanID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, unsubscribe := hub.Subscribe(scanID)
	defer unsubscribe()

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
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
			if event.Type == "complete" || event.Type == "error" {
				return
			}
		}
	}
}

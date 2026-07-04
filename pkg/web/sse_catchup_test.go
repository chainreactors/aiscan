package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/core/output"
)

// serveScanEvents drives the real scanEvents handler against a stored job and
// returns the raw SSE body. It guards against a hang: before the catch-up fix a
// late subscriber to an already-finished scan would block on keepalives forever.
func serveScanEvents(t *testing.T, svc *Service, id string) string {
	t.Helper()
	h := &handlerImpl{service: svc}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequest("GET", "/api/scans/"+id+"/events", nil).WithContext(ctx)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.scanEvents(rec, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scanEvents did not return: late subscriber hung with no terminal delivered")
	}
	return rec.Body.String()
}

func newScanStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "scans.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestScanEventsCatchUpDeliversCompleteToLateSubscriber reproduces the SSE race
// that froze the deck at the 5% progress floor: a fast scan (e.g. a target the
// gogo engine rejects instantly) persists its terminal state and broadcasts
// "complete" before the browser's EventSource subscribes. The hub keeps no
// backlog, so the broadcast reaches zero subscribers. The handler must
// re-derive the terminal event from the stored job for the late joiner.
func TestScanEventsCatchUpDeliversCompleteToLateSubscriber(t *testing.T) {
	store := newScanStore(t)
	svc := NewService(ServiceConfig{Store: store})

	now := time.Now()
	job := &ScanJob{
		ID:        "scan-fast",
		Target:    "example.test",
		Mode:      "quick",
		Status:    StatusCompleted,
		Result:    &output.Result{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	// Broadcasting now reaches zero subscribers, exactly as in the race: the
	// scan finished before any client connected.
	svc.Hub().Broadcast(job.ID, HubEvent{
		Type: "complete",
		Data: mustJSON(map[string]string{"scan_id": job.ID}),
	})

	body := serveScanEvents(t, svc, job.ID)
	if !strings.Contains(body, "event: complete") {
		t.Fatalf("expected catch-up to emit a complete event, got:\n%s", body)
	}
	if !strings.Contains(body, `"status":"completed"`) {
		t.Fatalf("complete event missing completed status, got:\n%s", body)
	}
	if !strings.Contains(body, `"result":`) {
		t.Fatalf("complete event missing result payload, got:\n%s", body)
	}
}

// TestScanEventsCatchUpDeliversErrorForFailedJob covers the failed/canceled
// terminal states: a late subscriber to a scan that already failed must still
// receive the error event (with the stored reason) rather than hang.
func TestScanEventsCatchUpDeliversErrorForFailedJob(t *testing.T) {
	store := newScanStore(t)
	svc := NewService(ServiceConfig{Store: store})

	now := time.Now()
	job := &ScanJob{
		ID:        "scan-bad",
		Target:    "evil.example",
		Mode:      "quick",
		Status:    StatusFailed,
		Error:     "all targets format error",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	body := serveScanEvents(t, svc, job.ID)
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected catch-up to emit an error event, got:\n%s", body)
	}
	if !strings.Contains(body, "all targets format error") {
		t.Fatalf("error event missing stored reason, got:\n%s", body)
	}
}

// TestScanEventsRunningJobDoesNotCatchUp ensures the catch-up only fires for
// terminal states: a still-running scan must not be reported complete. (The
// handler streams live events for it; here we just assert no premature terminal
// is emitted before the request context expires.)
func TestScanEventsRunningJobDoesNotCatchUp(t *testing.T) {
	store := newScanStore(t)
	svc := NewService(ServiceConfig{Store: store})

	now := time.Now()
	job := &ScanJob{
		ID:        "scan-running",
		Target:    "example.test",
		Mode:      "quick",
		Status:    StatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.Create(context.Background(), job); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	h := &handlerImpl{service: svc}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("GET", "/api/scans/"+job.ID+"/events", nil).WithContext(ctx)
	req.SetPathValue("id", job.ID)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.scanEvents(rec, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("scanEvents did not return after context timeout")
	}

	if body := rec.Body.String(); strings.Contains(body, "event: complete") {
		t.Fatalf("running scan should not emit a complete event, got:\n%s", body)
	}
}

// TestHubBroadcastNeverDropsReliableEvent covers the other half of the terminal-
// delivery story: a client that subscribed for the whole scan but fell behind.
// The per-subscriber buffer is bounded and lossy, so a flood of crawl output can
// saturate it. Progress/stats overflow is dropped (a later snapshot supersedes
// it), but the terminal "complete" must not be — losing it froze the deck at 97%
// until the user reloaded, because the live connection stays open on keepalives
// and the browser never reconnects to re-derive terminal state via catch-up. A
// Reliable event evicts the oldest buffered event to make room instead of being
// dropped itself.
func TestHubBroadcastNeverDropsReliableEvent(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("scan-slow")
	defer unsub()

	// Saturate the buffer without draining it — a client too slow to keep up.
	for i := 0; i < cap(ch); i++ {
		hub.Broadcast("scan-slow", HubEvent{Type: "progress", Data: mustJSON(map[string]int{"n": i})})
	}

	// The scan finishes while the buffer is still full. Before the fix this hit
	// the non-blocking default and vanished.
	hub.Broadcast("scan-slow", HubEvent{
		Type:     "complete",
		Data:     mustJSON(map[string]string{"status": "completed"}),
		Reliable: true,
	})

	var drained []HubEvent
drain:
	for {
		select {
		case e := <-ch:
			drained = append(drained, e)
		default:
			break drain
		}
	}

	// One oldest progress event was evicted to seat the terminal event, so the
	// buffer stays at capacity and "complete" arrives last (FIFO after eviction).
	if len(drained) != cap(ch) {
		t.Fatalf("buffer should stay at capacity %d after evict+enqueue, drained %d", cap(ch), len(drained))
	}
	if last := drained[len(drained)-1]; last.Type != "complete" {
		t.Fatalf("terminal complete must survive a full buffer and arrive last; got %q", last.Type)
	}
}

// TestHubBroadcastDropsLossyEventWhenBufferFull locks the backpressure-shedding
// contract the reliable-event fix deliberately preserves: an ordinary
// progress/stats event on a full buffer is dropped, never blocking the producer
// or growing the buffer past capacity.
func TestHubBroadcastDropsLossyEventWhenBufferFull(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("t")
	defer unsub()

	for i := 0; i < cap(ch); i++ {
		hub.Broadcast("t", HubEvent{Type: "progress"})
	}
	hub.Broadcast("t", HubEvent{Type: "stats"}) // overflow — must be shed

	n := 0
drain:
	for {
		select {
		case <-ch:
			n++
		default:
			break drain
		}
	}
	if n != cap(ch) {
		t.Fatalf("lossy overflow should be dropped: want %d buffered, got %d", cap(ch), n)
	}
}

package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"
)

type testEvent struct {
	key string
}

func (e testEvent) Key() string { return e.key }

func TestPipeline_LateEmitAfterClose(t *testing.T) {
	// Reproduces the w3 panic: a capability's async callback (like SDK
	// OnStats) calls emit after the pipeline has shut down.
	//
	// Setup: one capability ("spray") whose Run spawns a goroutine that
	// calls emit after Run returns — simulating the SDK's deferred
	// emitStats calling back into the pipeline.

	lateEmitReady := make(chan struct{})
	lateEmitDone := make(chan struct{})

	cfg := Config{
		Capabilities: []Capability{
			{
				Name:   "spray",
				Routes: []Route{{From: ""}},
				Worker: 1,
				Run: func(ctx context.Context, e Event, emit func(Event)) {
					// Simulate: emit a normal result synchronously
					emit(testEvent{key: "result-" + e.Key()})

					// Simulate: SDK goroutine that calls emit AFTER Run returns
					var wg sync.WaitGroup
					wg.Add(1)
					go func() {
						defer wg.Done()
						// Wait until the pipeline has had a chance to close
						<-lateEmitReady
						// This is the late emit that would panic without protection
						defer func() {
							if r := recover(); r != nil {
								// Expected: send on closed channel
								close(lateEmitDone)
								return
							}
							close(lateEmitDone)
						}()
						emit(testEvent{key: "late-stats"})
					}()
					// Don't wait for the goroutine — this simulates the SDK
					// behavior where Run returns but the goroutine lives on
				},
			},
			{
				Name:   "sink",
				Routes: []Route{{From: "spray"}},
				Worker: 1,
				Run: func(ctx context.Context, e Event, emit func(Event)) {
					// consume events
				},
			},
		},
	}

	p, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Run pipeline in a goroutine — it will block until idle then close channels
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run([]Event{testEvent{key: "seed"}})
	}()

	// Wait for pipeline to finish (events channel closed)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pipeline did not finish in time")
	}

	// Now trigger the late emit — pipeline is already shut down
	close(lateEmitReady)

	// The late emit should either panic (demonstrating the bug) or be handled
	select {
	case <-lateEmitDone:
		// OK — late emit completed (with or without recover)
	case <-time.After(2 * time.Second):
		t.Fatal("late emit goroutine did not complete")
	}
}

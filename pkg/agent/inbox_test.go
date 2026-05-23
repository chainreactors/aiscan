package agent

import (
	"sync"
	"testing"

	"github.com/chainreactors/aiscan/pkg/provider"
)

func TestBufferedInboxPushAndDrain(t *testing.T) {
	inbox := NewBufferedInbox(8)
	inbox.Push(provider.NewTextMessage("user", "a"))
	inbox.Push(provider.NewTextMessage("user", "b"))
	inbox.Push(provider.NewTextMessage("user", "c"))

	msgs := inbox.Drain()
	if len(msgs) != 3 {
		t.Fatalf("Drain() returned %d messages, want 3", len(msgs))
	}
	if *msgs[0].Content != "a" || *msgs[1].Content != "b" || *msgs[2].Content != "c" {
		t.Fatalf("unexpected message order: %v", msgs)
	}

	msgs = inbox.Drain()
	if msgs != nil {
		t.Fatalf("second Drain() = %v, want nil", msgs)
	}
}

func TestBufferedInboxBackpressure(t *testing.T) {
	inbox := NewBufferedInbox(2)
	if !inbox.Push(provider.NewTextMessage("user", "1")) {
		t.Fatal("Push 1 should succeed")
	}
	if !inbox.Push(provider.NewTextMessage("user", "2")) {
		t.Fatal("Push 2 should succeed")
	}
	if inbox.Push(provider.NewTextMessage("user", "3")) {
		t.Fatal("Push 3 should fail (capacity=2)")
	}
	if inbox.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", inbox.Len())
	}
}

func TestBufferedInboxNonPositiveCapacity(t *testing.T) {
	for _, capacity := range []int{0, -1} {
		inbox := NewBufferedInbox(capacity)
		if !inbox.Push(provider.NewTextMessage("user", "first")) {
			t.Fatalf("Push with capacity %d should accept one message", capacity)
		}
		if inbox.Push(provider.NewTextMessage("user", "second")) {
			t.Fatalf("second Push with capacity %d should fail", capacity)
		}
	}
}

func TestBufferedInboxClose(t *testing.T) {
	inbox := NewBufferedInbox(8)
	inbox.Push(provider.NewTextMessage("user", "before"))
	inbox.Close()

	if !inbox.Closed() {
		t.Fatal("Closed() should be true after Close()")
	}
	if inbox.Push(provider.NewTextMessage("user", "after")) {
		t.Fatal("Push after Close should return false")
	}

	msgs := inbox.Drain()
	if len(msgs) != 1 || *msgs[0].Content != "before" {
		t.Fatalf("Drain after Close should return buffered messages, got %v", msgs)
	}
}

func TestBufferedInboxConcurrency(t *testing.T) {
	inbox := NewBufferedInbox(1000)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				inbox.Push(provider.NewTextMessage("user", "msg"))
			}
		}()
	}

	drained := 0
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	for {
		select {
		case <-done:
			msgs := inbox.Drain()
			drained += len(msgs)
			if drained != 1000 {
				t.Errorf("total drained = %d, want 1000", drained)
			}
			return
		default:
			msgs := inbox.Drain()
			drained += len(msgs)
		}
	}
}

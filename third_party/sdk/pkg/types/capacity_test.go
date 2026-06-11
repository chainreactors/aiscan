package types

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCapacity_BasicAcquireRelease(t *testing.T) {
	c := NewCapacity(100)
	if err := c.Acquire(context.Background(), 30); err != nil {
		t.Fatal(err)
	}
	if got := c.Available(); got != 70 {
		t.Fatalf("available = %d, want 70", got)
	}
	c.Release(30)
	if got := c.Available(); got != 100 {
		t.Fatalf("available = %d, want 100", got)
	}
}

func TestCapacity_AcquireZeroOrNegative(t *testing.T) {
	c := NewCapacity(10)
	if err := c.Acquire(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Acquire(context.Background(), -5); err != nil {
		t.Fatal(err)
	}
	if got := c.Available(); got != 10 {
		t.Fatalf("available = %d, want 10", got)
	}
}

func TestCapacity_AcquireExceedsTotalIsCapped(t *testing.T) {
	c := NewCapacity(10)
	if err := c.Acquire(context.Background(), 999); err != nil {
		t.Fatal(err)
	}
	if got := c.Available(); got != 0 {
		t.Fatalf("available = %d, want 0 (capped to total)", got)
	}
	c.Release(10)
	if got := c.Available(); got != 10 {
		t.Fatalf("available = %d, want 10", got)
	}
}

func TestCapacity_BlocksUntilReleased(t *testing.T) {
	c := NewCapacity(100)
	if err := c.Acquire(context.Background(), 80); err != nil {
		t.Fatal(err)
	}

	var acquired int32
	go func() {
		if err := c.Acquire(context.Background(), 50); err == nil {
			atomic.StoreInt32(&acquired, 1)
		}
	}()

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&acquired) != 0 {
		t.Fatal("acquire should be blocked")
	}

	c.Release(30)
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&acquired) != 1 {
		t.Fatal("acquire should have succeeded after release")
	}
}

func TestCapacity_ContextCancellation(t *testing.T) {
	c := NewCapacity(10)
	if err := c.Acquire(context.Background(), 10); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Acquire(ctx, 5)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	err := <-errCh
	if err != context.Canceled {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if got := c.Available(); got != 0 {
		t.Fatalf("available = %d, want 0 (no units should have been taken)", got)
	}
}

func TestCapacity_ConcurrentAcquireRelease(t *testing.T) {
	c := NewCapacity(100)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if err := c.Acquire(context.Background(), 5); err != nil {
					t.Errorf("acquire: %v", err)
					return
				}
				c.Release(5)
			}
		}()
	}
	wg.Wait()
	if got := c.Available(); got != 100 {
		t.Fatalf("available = %d, want 100 after all releases", got)
	}
}

func TestCapacity_Resize(t *testing.T) {
	c := NewCapacity(100)
	if err := c.Acquire(context.Background(), 60); err != nil {
		t.Fatal(err)
	}

	c.Resize(200)
	if got := c.Total(); got != 200 {
		t.Fatalf("total = %d, want 200", got)
	}
	if got := c.Available(); got != 140 {
		t.Fatalf("available = %d, want 140 (40 + 100 added)", got)
	}

	c.Resize(50)
	if got := c.Total(); got != 50 {
		t.Fatalf("total = %d, want 50", got)
	}
	if got := c.Available(); got != 0 {
		t.Fatalf("available = %d, want 0 (clamped, 60 outstanding)", got)
	}
}

func TestCapacity_ReleaseOverTotal(t *testing.T) {
	c := NewCapacity(10)
	c.Release(100)
	if got := c.Available(); got != 10 {
		t.Fatalf("available = %d, want 10 (clamped to total)", got)
	}
}

func TestCapacity_ReleaseZeroOrNegative(t *testing.T) {
	c := NewCapacity(10)
	if err := c.Acquire(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	c.Release(0)
	c.Release(-3)
	if got := c.Available(); got != 5 {
		t.Fatalf("available = %d, want 5", got)
	}
}

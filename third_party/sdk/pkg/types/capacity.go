package types

import (
	"context"
	"sync"
)

// Capacity is a weighted semaphore that limits the total concurrency across
// multiple concurrent engine invocations. Each Acquire takes N units from the
// bucket and blocks until they are available; Release returns them.
type Capacity struct {
	mu        sync.Mutex
	cond      *sync.Cond
	total     int
	available int
}

// NewCapacity creates a Capacity bucket with the given total units.
func NewCapacity(total int) *Capacity {
	if total <= 0 {
		total = 1
	}
	c := &Capacity{
		total:     total,
		available: total,
	}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// Acquire blocks until n units are available, then decrements the bucket.
// If n exceeds total, it is capped to total to prevent deadlock.
// Returns ctx.Err() if the context is cancelled while waiting.
func (c *Capacity) Acquire(ctx context.Context, n int) error {
	if n <= 0 {
		return nil
	}

	c.mu.Lock()
	if n > c.total {
		n = c.total
	}
	c.mu.Unlock()

	done := ctx.Done()
	if done != nil {
		// Start a goroutine that broadcasts on context cancellation so
		// the waiting goroutine can re-check and bail out.
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			select {
			case <-done:
				c.cond.Broadcast()
			case <-stop:
			}
		}()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for c.available < n {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		c.cond.Wait()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	c.available -= n
	return nil
}

// Release returns n units to the bucket and wakes any blocked Acquire callers.
func (c *Capacity) Release(n int) {
	if n <= 0 {
		return
	}
	c.mu.Lock()
	c.available += n
	if c.available > c.total {
		c.available = c.total
	}
	c.mu.Unlock()
	c.cond.Broadcast()
}

// Available returns the number of currently free units.
func (c *Capacity) Available() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.available
}

// Total returns the total capacity.
func (c *Capacity) Total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

// Resize adjusts the total capacity. If the new total is larger, the
// difference is added to the available units. If smaller, available is
// clamped (outstanding acquisitions are not revoked).
func (c *Capacity) Resize(total int) {
	if total <= 0 {
		total = 1
	}
	c.mu.Lock()
	diff := total - c.total
	c.total = total
	c.available += diff
	if c.available < 0 {
		c.available = 0
	}
	if c.available > c.total {
		c.available = c.total
	}
	c.mu.Unlock()
	if diff > 0 {
		c.cond.Broadcast()
	}
}

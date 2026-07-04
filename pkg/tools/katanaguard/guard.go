package katanaguard

import "context"

var sem = make(chan struct{}, 1)

// Acquire serializes katana usage inside this process.
//
// Katana v1.6.1 keeps custom fields in a package-global map. It writes that map
// during NewCrawlerOptions and reads it from parser goroutines while crawling,
// so every in-process katana caller must hold the same guard for the full
// crawler lifetime.
func Acquire(ctx context.Context) (func(), error) {
	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

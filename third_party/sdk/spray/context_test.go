package spray

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/chainreactors/sdk/pkg/types"
)

func TestEmitStats_NilContext(t *testing.T) {
	var c *Context
	c.emitStats(types.Stats{})
}

func TestEmitStats_NilHandler(t *testing.T) {
	c := &Context{ctx: context.Background()}
	c.emitStats(types.Stats{})
}

func TestEmitStats_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var called atomic.Bool
	c := &Context{
		ctx: ctx,
		statsHandler: func(stats types.Stats) {
			called.Store(true)
		},
	}

	c.emitStats(types.Stats{Engine: "test"})
	if called.Load() {
		t.Fatal("statsHandler must not be called when context is cancelled")
	}
}

func TestEmitStats_ContextActive(t *testing.T) {
	var called atomic.Bool
	c := &Context{
		ctx: context.Background(),
		statsHandler: func(stats types.Stats) {
			called.Store(true)
		},
	}

	c.emitStats(types.Stats{Engine: "test"})
	if !called.Load() {
		t.Fatal("statsHandler should be called when context is active")
	}
}

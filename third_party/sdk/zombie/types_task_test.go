package zombie

import (
	"context"
	"testing"

	"github.com/chainreactors/sdk/pkg/types"
)

func TestContextSettersCloneAndClamp(t *testing.T) {
	ctx := NewContext().
		SetThreads(12).
		SetTimeout(7).
		SetTop(3).
		SetFirstOnly(false).
		SetNoUnauth(true)

	ctx.SetThreads(0).SetTimeout(0).SetTop(-1)
	if ctx.opt.Threads != 12 || ctx.opt.Timeout != 7 || ctx.opt.Top != 3 {
		t.Fatalf("invalid setter values should be ignored: threads=%d timeout=%d top=%d",
			ctx.opt.Threads, ctx.opt.Timeout, ctx.opt.Top)
	}

	child := ctx.WithContext(context.Background())
	if child == ctx {
		t.Fatal("WithContext should clone the SDK context")
	}
	if child.opt.Threads != 12 || child.opt.Timeout != 7 || child.opt.Top != 3 ||
		child.opt.FirstOnly || !child.opt.NoUnAuth {
		t.Fatalf("clone lost fields: threads=%d timeout=%d top=%d firstOnly=%v noUnauth=%v",
			child.opt.Threads, child.opt.Timeout, child.opt.Top,
			child.opt.FirstOnly, child.opt.NoUnAuth)
	}
}

func TestTargetAddressAndBruteTaskValidation(t *testing.T) {
	if got := (&Target{IP: "127.0.0.1"}).Address(); got != "127.0.0.1:" {
		t.Fatalf("address without port = %q", got)
	}
	if got := (&Target{IP: "127.0.0.1", Port: "22"}).Address(); got != "127.0.0.1:22" {
		t.Fatalf("address with port = %q", got)
	}

	if err := NewBruteTask(nil).Validate(); err == nil {
		t.Fatal("expected empty targets to fail")
	}
	if err := NewBruteTask([]Target{{Service: "ssh"}}).Validate(); err == nil {
		t.Fatal("expected missing IP to fail")
	}
	if err := NewBruteTask([]Target{{IP: "127.0.0.1"}}).Validate(); err == nil {
		t.Fatal("expected missing service to fail")
	}
	if err := NewBruteTask([]Target{{IP: "127.0.0.1", Service: "ssh"}}).Validate(); err != nil {
		t.Fatalf("valid brute task failed: %v", err)
	}
}

func TestConvertTargetsNormalizesService(t *testing.T) {
	targets := convertTargets([]Target{
		{IP: "127.0.0.1", Service: "SSH"},
		{IP: "127.0.0.2", Service: ""},
	})
	if len(targets) != 1 {
		t.Fatalf("expected 1 target (empty service filtered), got %d", len(targets))
	}
	if targets[0].Service != "ssh" {
		t.Fatalf("expected normalized service 'ssh', got %q", targets[0].Service)
	}
	if targets[0].Port != "22" {
		t.Fatalf("expected default port '22', got %q", targets[0].Port)
	}
}

func TestSetOptionOverrides(t *testing.T) {
	ctx := NewContext().SetThreads(50).SetTimeout(10)

	if ctx.opt.Threads != 50 || ctx.opt.Timeout != 10 {
		t.Fatalf("setter failed: threads=%d timeout=%d", ctx.opt.Threads, ctx.opt.Timeout)
	}

	custom := &types.ZombieOption{Threads: 200, Timeout: 30, Mod: "sniper", FirstOnly: false}
	ctx.SetOption(custom)

	if ctx.opt.Threads != 200 || ctx.opt.Timeout != 30 || ctx.opt.Mod != "sniper" {
		t.Fatalf("SetOption failed: threads=%d timeout=%d mod=%s",
			ctx.opt.Threads, ctx.opt.Timeout, ctx.opt.Mod)
	}
}

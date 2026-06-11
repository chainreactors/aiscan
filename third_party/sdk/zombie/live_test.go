package zombie

import (
	"context"
	"testing"
	"time"

	"github.com/chainreactors/sdk/pkg/types"
)

func TestLiveBruteSSH(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	targets := []Target{{IP: "127.0.0.1", Port: "22", Service: "ssh"}}
	zctx := NewContext().SetThreads(4).SetTimeout(3).SetNoUnauth(true).WithContext(ctx)

	ch, err := engine.BruteStream(zctx, targets, []string{"root"}, []string{"wrongpass1", "wrongpass2"})
	if err != nil {
		t.Fatalf("brute stream: %v", err)
	}

	count := 0
	for r := range ch {
		count++
		t.Logf("result: %s:%s user=%s service=%s", r.IP, r.Port, r.Username, r.Service)
	}
	t.Logf("total results: %d", count)
}

func TestLiveSniperSSH(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	targets := []Target{
		{IP: "127.0.0.1", Port: "22", Service: "ssh", Username: "root", Password: "wrongpass"},
	}
	zctx := NewContext().SetThreads(2).SetTimeout(3).WithContext(ctx)

	results, err := engine.Sniper(zctx, targets)
	if err != nil {
		t.Fatalf("sniper: %v", err)
	}
	for _, r := range results {
		t.Logf("sniper result: %s:%s user=%s service=%s", r.IP, r.Port, r.Username, r.Service)
	}
}

func TestLivePitchforkSSH(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	targets := []Target{{IP: "127.0.0.1", Port: "22", Service: "ssh"}}
	auths := []Auth{
		{Username: "root", Password: "wrong1"},
		{Username: "admin", Password: "wrong2"},
	}
	zctx := NewContext().SetThreads(2).SetTimeout(3).WithContext(ctx)

	results, err := engine.Pitchfork(zctx, targets, auths)
	if err != nil {
		t.Fatalf("pitchfork: %v", err)
	}
	for _, r := range results {
		t.Logf("pitchfork result: %s:%s user=%s service=%s", r.IP, r.Port, r.Username, r.Service)
	}
}

func TestLiveBruteWithStats(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var stats types.Stats
	zctx := NewContext().
		SetThreads(4).
		SetTimeout(3).
		SetNoUnauth(true).
		SetStatsHandler(func(s types.Stats) { stats = s }).
		WithContext(ctx)

	targets := []Target{{IP: "127.0.0.1", Port: "22", Service: "ssh"}}

	_, err := engine.Brute(zctx, targets, []string{"root", "admin"}, []string{"wrong1", "wrong2"})
	if err != nil {
		t.Fatalf("brute: %v", err)
	}

	t.Logf("stats: targets=%d tasks=%d requests=%d results=%d duration=%v",
		stats.Targets, stats.Tasks, stats.Requests, stats.Results, stats.Duration)

	if stats.Targets != 1 {
		t.Errorf("expected 1 target, got %d", stats.Targets)
	}
	if stats.Tasks == 0 {
		t.Error("expected tasks > 0")
	}
	if stats.Duration == 0 {
		t.Error("expected duration > 0")
	}
}

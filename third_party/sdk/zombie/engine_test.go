package zombie

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/sdk/pkg/types"
)

func TestEngineInitAndValidate(t *testing.T) {
	engine := NewEngine(NewConfig())
	if err := engine.Init(); err != nil {
		t.Fatalf("init zombie engine: %v", err)
	}
	if engine.Name() != "zombie" {
		t.Fatalf("unexpected engine name: %s", engine.Name())
	}

	_, err := engine.Execute(NewContext().WithContext(context.Background()), NewBruteTask(nil))
	if err == nil {
		t.Fatal("expected empty brute task to fail validation")
	}
}

func TestConfigWithCapacity(t *testing.T) {
	engine := NewEngine(NewConfig().WithCapacity(200))
	if engine.Capacity() == nil {
		t.Fatal("engine should have a capacity after WithCapacity()")
	}
	if engine.Capacity().Total() != 200 {
		t.Fatalf("capacity total = %d, want 200", engine.Capacity().Total())
	}
}

func TestSetCapacityPostCreation(t *testing.T) {
	engine := NewEngine(nil)
	if engine.Capacity() != nil {
		t.Fatal("engine should have no capacity by default")
	}
	engine.SetCapacity(300)
	if engine.Capacity() == nil {
		t.Fatal("engine should have a capacity after SetCapacity()")
	}
	if engine.Capacity().Total() != 300 {
		t.Fatalf("capacity total = %d, want 300", engine.Capacity().Total())
	}
}

func TestCapacityContextCancellation(t *testing.T) {
	engine := NewEngine(NewConfig().WithCapacity(10))
	if err := engine.Init(); err != nil {
		t.Fatalf("init zombie engine: %v", err)
	}

	if err := engine.Capacity().Acquire(context.Background(), 10); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	zctx := NewContext().SetThreads(5).WithContext(ctx)

	_, err := engine.Execute(zctx, NewBruteTask([]Target{{IP: "127.0.0.1", Port: "22", Service: "ssh"}}))
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	engine.Capacity().Release(10)
}

func TestNoCapacityByDefault(t *testing.T) {
	engine := NewEngine(nil)
	if engine.Capacity() != nil {
		t.Fatal("engine should have no capacity by default")
	}
}

func TestExecuteAutoInit(t *testing.T) {
	engine := NewEngine(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets := []Target{{IP: "127.0.0.1", Port: "1", Service: "redis"}}
	results, err := engine.Brute(
		NewContext().SetThreads(2).SetTimeout(1).WithContext(ctx),
		targets, nil, []string{"x"},
	)
	if err != nil {
		t.Fatalf("brute: %v", err)
	}
	_ = results
}

func TestExecuteNilContext(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	task := NewBruteTask([]Target{{IP: "127.0.0.1", Port: "1", Service: "redis"}})
	task.Passwords = []string{"x"}

	resultCh, err := engine.Execute(nil, task)
	if err != nil {
		t.Fatalf("execute with nil context: %v", err)
	}
	for range resultCh {
	}
}

func TestExecuteUnsupportedTaskType(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	_, err := engine.Execute(NewContext(), &dummyTask{})
	if err == nil {
		t.Fatal("expected error for unsupported task type")
	}
}

type dummyTask struct{}

func (d *dummyTask) Type() string    { return "dummy" }
func (d *dummyTask) Validate() error { return nil }

func TestExecuteUnsupportedContextType(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	task := NewBruteTask([]Target{{IP: "127.0.0.1", Service: "ssh"}})
	_, err := engine.Execute(&dummyContext{}, task)
	if err == nil {
		t.Fatal("expected error for unsupported context type")
	}
}

type dummyContext struct{}

func (d *dummyContext) Context() context.Context { return context.Background() }

func TestBruteWithCustomUsersPasswords(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets := []Target{{IP: "127.0.0.1", Port: "1", Service: "redis"}}
	zctx := NewContext().SetThreads(2).SetTimeout(1).WithContext(ctx)

	results, err := engine.Brute(zctx, targets, []string{"admin"}, []string{"pass1", "pass2"})
	if err != nil {
		t.Fatalf("brute: %v", err)
	}
	_ = results
}

func TestBruteStream(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets := []Target{{IP: "127.0.0.1", Port: "1", Service: "redis"}}
	ch, err := engine.BruteStream(
		NewContext().SetThreads(2).SetTimeout(1).WithContext(ctx),
		targets, nil, []string{"x"},
	)
	if err != nil {
		t.Fatalf("brute stream: %v", err)
	}
	for range ch {
	}
}

func TestPitchfork(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets := []Target{{IP: "127.0.0.1", Port: "1", Service: "redis"}}
	auths := []Auth{{Username: "root", Password: "toor"}}
	zctx := NewContext().SetThreads(2).SetTimeout(1).WithContext(ctx)

	results, err := engine.Pitchfork(zctx, targets, auths)
	if err != nil {
		t.Fatalf("pitchfork: %v", err)
	}
	_ = results
}

func TestPitchforkStream(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets := []Target{{IP: "127.0.0.1", Port: "1", Service: "redis"}}
	auths := []Auth{{Username: "root", Password: "toor"}}

	ch, err := engine.PitchforkStream(
		NewContext().SetThreads(2).SetTimeout(1).WithContext(ctx),
		targets, auths,
	)
	if err != nil {
		t.Fatalf("pitchfork stream: %v", err)
	}
	for range ch {
	}
}

func TestSniper(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets := []Target{
		{IP: "127.0.0.1", Port: "1", Service: "redis", Username: "root", Password: "toor"},
	}
	zctx := NewContext().SetThreads(2).SetTimeout(1).WithContext(ctx)

	results, err := engine.Sniper(zctx, targets)
	if err != nil {
		t.Fatalf("sniper: %v", err)
	}
	_ = results
}

func TestSniperStream(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	targets := []Target{
		{IP: "127.0.0.1", Port: "1", Service: "redis", Username: "root", Password: "toor"},
	}

	ch, err := engine.SniperStream(
		NewContext().SetThreads(2).SetTimeout(1).WithContext(ctx),
		targets,
	)
	if err != nil {
		t.Fatalf("sniper stream: %v", err)
	}
	for range ch {
	}
}

func TestStatsCallback(t *testing.T) {
	engine := NewEngine(nil)
	if err := engine.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var called int32
	zctx := NewContext().
		SetThreads(2).
		SetTimeout(1).
		SetStatsHandler(func(s types.Stats) {
			atomic.StoreInt32(&called, 1)
			if s.Engine != "zombie" {
				t.Errorf("stats engine = %q, want zombie", s.Engine)
			}
		}).
		WithContext(ctx)

	targets := []Target{{IP: "127.0.0.1", Port: "1", Service: "redis"}}
	_, err := engine.Brute(zctx, targets, nil, []string{"x"})
	if err != nil {
		t.Fatalf("brute: %v", err)
	}

	if atomic.LoadInt32(&called) != 1 {
		t.Fatal("stats handler was not called")
	}
}

package gogo

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkfingers "github.com/chainreactors/sdk/fingers"
	sdkneutron "github.com/chainreactors/sdk/neutron"
	"github.com/chainreactors/sdk/pkg/provider"
	"github.com/chainreactors/sdk/pkg/types"
)

func testConfig() *Config {
	return NewConfig().WithProvider(provider.NewEmbedProvider())
}

// TestNewGogoEngine 测试引擎创建
func TestNewEngine(t *testing.T) {
	// 测试使用默认配置
	engine1 := NewEngine(nil)
	if engine1 == nil {
		t.Fatal("NewEngine(nil) should not return nil")
	}
	if engine1.inited {
		t.Error("Engine should not be initialized by default")
	}

	// 测试使用自定义配置
	opt := &types.GogoOption{
		VersionLevel: 2,
		Exploit:      "auto",
	}
	engine2 := NewEngine(NewConfig())
	_ = opt
	if engine2 == nil {
		t.Fatal("NewEngine(NewConfig()) should not return nil")
	}

	// 测试空配置
	engine3 := NewEngine(nil)
	if engine3 == nil {
		t.Fatal("NewEngine(nil) should not return nil")
	}
}

// TestGogoEngineName 测试引擎名称
func TestGogoEngineName(t *testing.T) {
	engine := NewEngine(testConfig())
	if engine.Name() != "gogo" {
		t.Errorf("Expected engine name 'gogo', got '%s'", engine.Name())
	}
}

func TestInitWithEmptyNeutronEngineDoesNotFail(t *testing.T) {
	emptyNeutron := &sdkneutron.Engine{}
	engine := NewEngine(NewConfig().WithNeutronEngine(emptyNeutron))
	if err := engine.Init(); err != nil {
		t.Fatalf("Init() with empty neutron engine returned error: %v", err)
	}
}

func TestInitWithEmptyCustomFingersDoesNotFail(t *testing.T) {
	engine := NewEngine(NewConfig().WithFingersEngine(&sdkfingers.Engine{}))
	if err := engine.Init(); err != nil {
		t.Fatalf("Init() with empty fingers engine returned error: %v", err)
	}
}

func TestInitConcurrent(t *testing.T) {
	engine := NewEngine(testConfig())
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- engine.Init()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Init() error = %v", err)
		}
	}
}

func TestGogoEngineConcurrentScanScenarios(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("gogo-sdk-concurrency"))
	}))
	defer server.Close()

	host, port := splitTestServerHostPort(t, server.URL)
	engine := NewEngine(testConfig())
	if err := engine.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	const workers = 12
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			gogoCtx := NewContext().SetThreads(4).SetDelay(1).SetVersionLevel(0).WithContext(ctx)

			switch i % 3 {
			case 0:
				result := engine.ScanOne(gogoCtx, host, port)
				if result == nil || result.Port != port {
					errs <- fmt.Errorf("ScanOne worker %d returned %#v", i, result)
				}
			case 1:
				results, err := engine.Scan(gogoCtx, host, port)
				if err != nil {
					errs <- err
					return
				}
				if len(results) == 0 {
					errs <- fmt.Errorf("Scan worker %d produced no open-port results", i)
				}
			default:
				workflow := &types.Workflow{Name: "concurrent", IP: host, Ports: port}
				ch, err := engine.WorkflowStream(gogoCtx, workflow)
				if err != nil {
					errs <- err
					return
				}
				var count int
				for range ch {
					count++
				}
				if count == 0 {
					errs <- fmt.Errorf("WorkflowStream worker %d produced no open-port results", i)
				}
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestGogoEngineConcurrentCancelledContexts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(25 * time.Millisecond)
		_, _ = w.Write([]byte("delayed"))
	}))
	defer server.Close()

	host, port := splitTestServerHostPort(t, server.URL)
	engine := NewEngine(testConfig())
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			gogoCtx := NewContext().SetThreads(2).SetDelay(1).WithContext(cancelled)

			var ch <-chan *types.GOGOResult
			var err error
			if i%2 == 0 {
				ch, err = engine.ScanStream(gogoCtx, host, port)
			} else {
				workflow := &types.Workflow{Name: "cancelled", IP: host, Ports: port}
				ch, err = engine.WorkflowStream(gogoCtx, workflow)
			}
			if err != nil {
				errs <- err
				return
			}

			done := make(chan struct{})
			go func() {
				for range ch {
				}
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				errs <- fmt.Errorf("worker %d did not stop after cancellation", i)
			}
		}()
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func splitTestServerHostPort(t *testing.T, rawURL string) (string, string) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split test server host/port: %v", err)
	}
	return host, port
}

// TestGogoEngineSetThreads 测试设置线程数
func TestContextSetThreads(t *testing.T) {
	ctx := NewContext()

	ctx.SetThreads(500)
	if ctx.threads != 500 {
		t.Errorf("Expected threads 500, got %d", ctx.threads)
	}

	ctx.SetThreads(2000)
	if ctx.threads != 2000 {
		t.Errorf("Expected threads 2000, got %d", ctx.threads)
	}
}

// TestContext 测试 Context 实现
func TestContext(t *testing.T) {
	ctx := NewContext()

	// 测试 Context()
	if ctx.Context() == nil {
		t.Error("Context() should not return nil")
	}

	// 测试 WithTimeout
	timeoutCtx, _ := context.WithTimeout(ctx.Context(), 5*time.Second)
	ctx2 := ctx.WithContext(timeoutCtx)
	if ctx2 == nil {
		t.Error("WithTimeout should not return nil")
	}

	// 测试 WithCancel
	cancelCtx, cancel := context.WithCancel(ctx.Context())
	ctx3 := ctx.WithContext(cancelCtx)
	if ctx3 == nil {
		t.Error("WithCancel should not return nil context")
	}
	if cancel == nil {
		t.Error("WithCancel should not return nil cancel func")
	}
	cancel() // 清理

	// 测试链式调用
	baseCtx := NewContext().SetThreads(500).SetVersionLevel(3)
	chainCtx, _ := context.WithTimeout(baseCtx.Context(), 10*time.Second)
	ctx4 := baseCtx.WithContext(chainCtx)
	runCtx := ctx4
	if runCtx.threads != 500 {
		t.Errorf("Expected threads 500 after chain call, got %d", runCtx.threads)
	}
	if runCtx.opt.VersionLevel != 3 {
		t.Errorf("Expected VersionLevel 3 after chain call, got %d", runCtx.opt.VersionLevel)
	}
}

// TestConfig 测试 Config 实现
func TestConfig(t *testing.T) {
	config := NewConfig()

	// 测试 Validate
	if err := config.Validate(); err != nil {
		t.Errorf("Valid config should pass validation: %v", err)
	}
}

func TestConfigWithCapacity(t *testing.T) {
	engine := NewEngine(NewConfig().WithCapacity(100))
	if engine.Capacity() == nil {
		t.Fatal("engine should have a capacity after WithCapacity()")
	}
	if engine.Capacity().Total() != 100 {
		t.Fatalf("capacity total = %d, want 100", engine.Capacity().Total())
	}
}

func TestSetCapacityPostCreation(t *testing.T) {
	engine := NewEngine(testConfig())
	if engine.Capacity() != nil {
		t.Fatal("engine should have no capacity by default")
	}
	engine.SetCapacity(200)
	if engine.Capacity() == nil {
		t.Fatal("engine should have a capacity after SetCapacity()")
	}
	if engine.Capacity().Total() != 200 {
		t.Fatalf("capacity total = %d, want 200", engine.Capacity().Total())
	}
}

func TestCapacityThrottlesConcurrentScans(t *testing.T) {
	var maxConcurrent int32
	var currentConcurrent int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&currentConcurrent, 1)
		defer atomic.AddInt32(&currentConcurrent, -1)
		for {
			old := atomic.LoadInt32(&maxConcurrent)
			if cur <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("capacity-test"))
	}))
	defer server.Close()

	host, port := splitTestServerHostPort(t, server.URL)

	// Capacity=4, each scan uses 4 threads → only 1 scan at a time
	engine := NewEngine(NewConfig().WithCapacity(4))
	if err := engine.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	const workers = 3
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			gogoCtx := NewContext().SetThreads(4).SetDelay(1).WithContext(ctx)

			ch, err := engine.ScanStream(gogoCtx, host, port)
			if err != nil {
				t.Errorf("ScanStream: %v", err)
				return
			}
			for range ch {
			}
		}()
	}

	wg.Wait()

	got := atomic.LoadInt32(&maxConcurrent)
	t.Logf("max concurrent HTTP requests = %d (capacity=4, threads=4 per scan)", got)
	// With capacity=4 and threads=4, at most 4 concurrent requests should be inflight
	// (1 scan at a time, each using 4 threads)
	if got > 4 {
		t.Fatalf("max concurrent requests = %d, expected <= 4 (capacity should throttle)", got)
	}
	// All scans should complete and capacity should be fully released
	if avail := engine.Capacity().Available(); avail != 4 {
		t.Fatalf("capacity available = %d, want 4 (all released)", avail)
	}
}

func TestCapacityContextCancellation(t *testing.T) {
	engine := NewEngine(NewConfig().WithCapacity(4))
	if err := engine.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Exhaust the capacity
	if err := engine.Capacity().Acquire(context.Background(), 4); err != nil {
		t.Fatal(err)
	}

	// Try to scan with cancelled context — should fail immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gogoCtx := NewContext().SetThreads(4).WithContext(ctx)

	_, err := engine.ScanStream(gogoCtx, "127.0.0.1", "65535")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	engine.Capacity().Release(4)
}

// TestScanTask 测试 ScanTask
func TestScanTask(t *testing.T) {
	// 测试有效任务
	task1 := NewScanTask("127.0.0.1", "80,443")
	if task1.Type() != "scan" {
		t.Errorf("Expected task type 'scan', got '%s'", task1.Type())
	}
	if err := task1.Validate(); err != nil {
		t.Errorf("Valid task should pass validation: %v", err)
	}

	// 测试无效任务 - 空 IP
	task2 := NewScanTask("", "80")
	if err := task2.Validate(); err == nil {
		t.Error("Task with empty IP should fail validation")
	}

	// 测试无效任务 - 空端口
	task3 := NewScanTask("127.0.0.1", "")
	if err := task3.Validate(); err == nil {
		t.Error("Task with empty ports should fail validation")
	}
}

// TestWorkflowTask 测试 WorkflowTask
func TestWorkflowTask(t *testing.T) {
	// 测试有效任务
	workflow := &types.Workflow{
		Name:  "test-workflow",
		IP:    "127.0.0.1",
		Ports: "top100",
	}
	task1 := NewWorkflowTask(workflow)
	if task1.Type() != "workflow" {
		t.Errorf("Expected task type 'workflow', got '%s'", task1.Type())
	}
	if err := task1.Validate(); err != nil {
		t.Errorf("Valid task should pass validation: %v", err)
	}

	// 测试无效任务 - nil workflow
	task2 := NewWorkflowTask(nil)
	if err := task2.Validate(); err == nil {
		t.Error("Task with nil workflow should fail validation")
	}
}

// TestResult 测试 Result
func TestResult(t *testing.T) {
	// 测试成功结果
	result1 := newResult(true, nil, nil)
	if !result1.Success() {
		t.Error("Result with success=true should return true from Success()")
	}
	if result1.Error() != nil {
		t.Error("Result with err=nil should return nil from Error()")
	}

	// 测试失败结果
	result2 := newResult(false, context.DeadlineExceeded, nil)
	if result2.Success() {
		t.Error("Result with success=false should return false from Success()")
	}
	if result2.Error() == nil {
		t.Error("Result with error should return non-nil from Error()")
	}
}

// TestScanOne 测试单目标扫描
func TestScanOne(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	engine := NewEngine(testConfig())
	if err := engine.Init(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	ctx := NewContext()

	// 扫描本地端口（应该很快）
	result := engine.ScanOne(ctx, "127.0.0.1", "65535")
	if result == nil {
		t.Fatal("ScanOne should not return nil")
	}

	// 注意：这里不检查端口是否开放，因为取决于实际环境
	t.Logf("ScanOne result: %s:%s - Status: %s", result.Ip, result.Port, result.Status)
}

// TestScanIntegration 测试实际扫描功能
func TestScanIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("gogo integration"))
	}))
	defer server.Close()
	host, port := splitTestServerHostPort(t, server.URL)

	engine := NewEngine(testConfig())
	if err := engine.Init(); err != nil {
		t.Skipf("Init failed (may need finger database): %v", err)
	}

	ctx := NewContext().SetThreads(10).SetDelay(1)
	timeoutCtx, cancel := context.WithTimeout(ctx.Context(), 30*time.Second)
	defer cancel()
	ctx = ctx.WithContext(timeoutCtx)

	results, err := engine.Scan(ctx, host, port)
	if err != nil {
		t.Fatalf("Scan failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected local httptest server port to be open")
	}
	if results[0].Port != port {
		t.Fatalf("expected port %s, got %+v", port, results[0])
	}
}

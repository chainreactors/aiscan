package spray

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkfingers "github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/pkg/types"
)

// TestNewSprayEngine 测试引擎创建
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
	opt := NewDefaultOption()
	opt.Threads = 200
	engine2 := NewEngine(NewConfig())
	_ = opt
	if engine2 == nil {
		t.Fatal("NewEngine(NewConfig()) should not return nil")
	}

	// 测试兼容性 API
	engine3 := NewEngine(nil)
	if engine3 == nil {
		t.Fatal("NewEngine(nil) should not return nil")
	}
}

// TestSprayEngineName 测试引擎名称
func TestSprayEngineName(t *testing.T) {
	engine := NewEngine(nil)
	if engine.Name() != "spray" {
		t.Errorf("Expected engine name 'spray', got '%s'", engine.Name())
	}
}

func TestExecuteInitializesWithEmptyCustomFingers(t *testing.T) {
	engine := NewEngine(NewConfig().WithFingersEngine(&sdkfingers.Engine{}))
	ctx := NewContext()
	timeoutCtx, cancel := context.WithTimeout(ctx.Context(), time.Second)
	defer cancel()
	ctx = ctx.WithContext(timeoutCtx).SetTimeout(1)

	resultCh, err := engine.CheckStream(ctx, []string{"http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("CheckStream() returned error: %v", err)
	}
	for range resultCh {
	}
}

func TestInitConcurrent(t *testing.T) {
	engine := NewEngine(nil)
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

func TestSprayEngineConcurrentCheckAndBrute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte("home"))
		case "/admin":
			_, _ = w.Write([]byte("admin panel admin panel admin panel admin panel admin panel"))
		case "/api":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"service":"spray-sdk-concurrency","ok":true,"padding":"xxxxxxxxxxxxxxxxxxxxxxxx"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	engine := NewEngine(nil)
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
			sprayCtx := NewContext().SetThreads(4).SetTimeout(1).WithContext(ctx)

			var count int
			switch i % 3 {
			case 0:
				ch, err := engine.Execute(sprayCtx, NewCheckTask([]string{server.URL, server.URL + "/admin"}))
				if err != nil {
					errs <- err
					return
				}
				for range ch {
					count++
				}
			case 1:
				ch, err := engine.Execute(sprayCtx, NewBruteTask(server.URL, []string{"admin", "api", "missing"}))
				if err != nil {
					errs <- err
					return
				}
				for range ch {
					count++
				}
			default:
				ch, err := engine.BruteStream(sprayCtx, server.URL, []string{"admin", "api"})
				if err != nil {
					errs <- err
					return
				}
				for range ch {
					count++
				}
			}

			if count == 0 {
				errs <- fmt.Errorf("worker %d produced no spray results", i)
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

func TestSprayEngineConcurrentCancelledContexts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(25 * time.Millisecond)
		_, _ = w.Write([]byte("delayed"))
	}))
	defer server.Close()

	engine := NewEngine(nil)
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
			sprayCtx := NewContext().SetThreads(2).SetTimeout(1).WithContext(cancelled)

			var ch <-chan *types.SprayResult
			var err error
			if i%2 == 0 {
				ch, err = engine.CheckStream(sprayCtx, []string{server.URL})
			} else {
				ch, err = engine.BruteStream(sprayCtx, server.URL, []string{"admin"})
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

func TestSprayEngineBruteBatchUsesRunnerTaskPool(t *testing.T) {
	type overlapTracker struct {
		mu               sync.Mutex
		activeByServer   map[int]int
		activeServers    int
		maxActiveServers int
	}

	tracker := &overlapTracker{activeByServer: make(map[int]int)}
	enter := func(id int) {
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		if tracker.activeByServer[id] == 0 {
			tracker.activeServers++
			if tracker.activeServers > tracker.maxActiveServers {
				tracker.maxActiveServers = tracker.activeServers
			}
		}
		tracker.activeByServer[id]++
	}
	leave := func(id int) {
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		tracker.activeByServer[id]--
		if tracker.activeByServer[id] == 0 {
			tracker.activeServers--
		}
	}

	var servers []*httptest.Server
	for i := 0; i < 3; i++ {
		id := i
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			enter(id)
			defer leave(id)
			time.Sleep(80 * time.Millisecond)

			switch r.URL.Path {
			case "/":
				_, _ = w.Write([]byte("home"))
			case "/admin":
				_, _ = w.Write([]byte("admin panel for runner task pool concurrency"))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()
		servers = append(servers, server)
	}

	baseURLs := make([]string, 0, len(servers))
	for _, server := range servers {
		baseURLs = append(baseURLs, server.URL)
	}

	engine := NewEngine(nil)
	ctx := NewContext().
		SetThreads(6).
		SetTimeout(2).
		SetMatch(`current.Path == "/admin"`)

	resultCh, err := engine.Execute(ctx, NewBruteTasks(baseURLs, []string{"admin"}))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	adminHits := make(map[string]bool)
	for result := range resultCh {
		sprayResult, _ := types.ResultData[*types.SprayResult](result)
		if result.Success() && sprayResult != nil && strings.HasSuffix(sprayResult.UrlString, "/admin") {
			adminHits[pkgBaseURL(sprayResult.UrlString)] = true
		}
	}

	if len(adminHits) != len(baseURLs) {
		t.Fatalf("admin hits = %d, want %d (%v)", len(adminHits), len(baseURLs), adminHits)
	}
	if tracker.maxActiveServers < 2 {
		t.Fatalf("runner task pool did not overlap targets; max active servers = %d", tracker.maxActiveServers)
	}
}

func TestSprayEngineConcurrentExecuteWithSharedContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte("home"))
		case "/admin":
			_, _ = w.Write([]byte("shared context admin panel admin panel"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	engine := NewEngine(nil)
	sharedCtx := NewContext().SetThreads(4).SetTimeout(1)

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			var resultCh <-chan types.Result
			var err error
			if i%2 == 0 {
				resultCh, err = engine.Execute(sharedCtx, NewCheckTask([]string{server.URL + "/admin"}))
			} else {
				resultCh, err = engine.Execute(sharedCtx, NewBruteTask(server.URL, []string{"admin"}))
			}
			if err != nil {
				errs <- err
				return
			}
			for range resultCh {
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

func pkgBaseURL(rawURL string) string {
	if idx := strings.LastIndex(rawURL, "/"); idx > len("http://") {
		return rawURL[:idx]
	}
	return rawURL
}

// TestDefaultConfig 测试默认配置
func TestDefaultConfig(t *testing.T) {
	config := NewDefaultOption()

	if config.Method != "GET" {
		t.Errorf("Expected default Method 'GET', got '%s'", config.Method)
	}
	if config.MaxBodyLength != 100 {
		t.Errorf("Expected default MaxBodyLength 100, got %d", config.MaxBodyLength)
	}
	if config.RandomUserAgent != false {
		t.Error("Expected default RandomUserAgent false")
	}
	if config.BlackStatus != "400,410" {
		t.Errorf("Expected default BlackStatus '400,410', got '%s'", config.BlackStatus)
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
	baseCtx := NewContext().SetThreads(300).SetTimeout(20)
	chainCtx, _ := context.WithTimeout(baseCtx.Context(), 10*time.Second)
	ctx4 := baseCtx.WithContext(chainCtx)
	runCtx := ctx4
	if runCtx.opt.Threads != 300 {
		t.Errorf("Expected threads 300 after chain call, got %d", runCtx.opt.Threads)
	}
	if runCtx.opt.Timeout != 20 {
		t.Errorf("Expected timeout 20 after chain call, got %d", runCtx.opt.Timeout)
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
	engine := NewEngine(NewConfig().WithCapacity(50))
	if engine.Capacity() == nil {
		t.Fatal("engine should have a capacity after WithCapacity()")
	}
	if engine.Capacity().Total() != 50 {
		t.Fatalf("capacity total = %d, want 50", engine.Capacity().Total())
	}
}

func TestSetCapacityPostCreation(t *testing.T) {
	engine := NewEngine(nil)
	if engine.Capacity() != nil {
		t.Fatal("engine should have no capacity by default")
	}
	engine.SetCapacity(100)
	if engine.Capacity() == nil {
		t.Fatal("engine should have a capacity after SetCapacity()")
	}
	if engine.Capacity().Total() != 100 {
		t.Fatalf("capacity total = %d, want 100", engine.Capacity().Total())
	}
}

func TestCapacityThrottlesConcurrentChecks(t *testing.T) {
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
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte("capacity-test"))
	}))
	defer server.Close()

	// Capacity=4, threads=4 per check → only 1 check at a time
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
			sprayCtx := NewContext().SetThreads(4).SetTimeout(2).WithContext(ctx)

			ch, err := engine.CheckStream(sprayCtx, []string{server.URL})
			if err != nil {
				t.Errorf("CheckStream: %v", err)
				return
			}
			for range ch {
			}
		}()
	}

	wg.Wait()

	got := atomic.LoadInt32(&maxConcurrent)
	t.Logf("max concurrent HTTP requests = %d (capacity=4, threads=4 per check)", got)
	if got > 4 {
		t.Fatalf("max concurrent requests = %d, expected <= 4 (capacity should throttle)", got)
	}
	if avail := engine.Capacity().Available(); avail != 4 {
		t.Fatalf("capacity available = %d, want 4 (all released)", avail)
	}
}

func TestCapacityContextCancellation(t *testing.T) {
	engine := NewEngine(NewConfig().WithCapacity(4))
	if err := engine.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Exhaust capacity
	if err := engine.Capacity().Acquire(context.Background(), 4); err != nil {
		t.Fatal(err)
	}

	// Cancelled context should fail Acquire
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sprayCtx := NewContext().SetThreads(4).SetTimeout(1).WithContext(ctx)

	_, err := engine.CheckStream(sprayCtx, []string{"http://127.0.0.1:1"})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	engine.Capacity().Release(4)
}

// TestCheckTask 测试 CheckTask
func TestCheckTask(t *testing.T) {
	// 测试有效任务
	urls := []string{"http://example.com", "https://google.com"}
	task1 := NewCheckTask(urls)
	if task1.Type() != "check" {
		t.Errorf("Expected task type 'check', got '%s'", task1.Type())
	}
	if err := task1.Validate(); err != nil {
		t.Errorf("Valid task should pass validation: %v", err)
	}

	// 测试无效任务 - 空 URL 列表
	task2 := NewCheckTask([]string{})
	if err := task2.Validate(); err == nil {
		t.Error("Task with empty URLs should fail validation")
	}

	// 测试无效任务 - nil URL 列表
	task3 := NewCheckTask(nil)
	if err := task3.Validate(); err == nil {
		t.Error("Task with nil URLs should fail validation")
	}
}

// TestBruteTask 测试 BruteTask
func TestBruteTask(t *testing.T) {
	// 测试有效任务
	wordlist := []string{"admin", "api", "test"}
	task1 := NewBruteTask("http://example.com", wordlist)
	if task1.Type() != "brute" {
		t.Errorf("Expected task type 'brute', got '%s'", task1.Type())
	}
	if err := task1.Validate(); err != nil {
		t.Errorf("Valid task should pass validation: %v", err)
	}

	// 测试无效任务 - 空 BaseURL
	task2 := NewBruteTask("", wordlist)
	if err := task2.Validate(); err == nil {
		t.Error("Task with empty BaseURL should fail validation")
	}

	// 测试无效任务 - 空字典
	task3 := NewBruteTask("http://example.com", []string{})
	if err := task3.Validate(); err == nil {
		t.Error("Task with empty wordlist should fail validation")
	}

	// 测试无效任务 - nil字典
	task4 := NewBruteTask("http://example.com", nil)
	if err := task4.Validate(); err == nil {
		t.Error("Task with nil wordlist should fail validation")
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

func TestSpray(t *testing.T) {
	engine := NewEngine(nil)

	// 2. 初始化（加载指纹库等）
	if err := engine.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			_, _ = w.Write([]byte("home"))
		case "/admin":
			_, _ = w.Write([]byte("admin panel"))
		case "/api":
			_, _ = w.Write([]byte("api"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// 3. 使用
	ctx := NewContext().SetThreads(2).SetTimeout(2)

	// URL 检测
	urls := []string{server.URL, server.URL + "/admin"}
	resultCh1, err := engine.CheckStream(ctx, urls)
	if err != nil {
		t.Fatalf("CheckStream() error = %v", err)
	}
	var checkCount int
	for result := range resultCh1 {
		if result.Status == http.StatusOK {
			checkCount++
		}
	}
	if checkCount != len(urls) {
		t.Fatalf("CheckStream() status 200 count = %d, want %d", checkCount, len(urls))
	}

	// 路径暴力破解
	wordlist := []string{"admin", "api", "missing"}
	bruteCtx := NewContext().SetThreads(2).SetTimeout(2).SetMatch(`current.Path == "/admin"`)
	resultCh2, err := engine.BruteStream(bruteCtx, server.URL, wordlist)
	if err != nil {
		t.Fatalf("BruteStream() error = %v", err)
	}
	bruteHits := make(map[string]int)
	for result := range resultCh2 {
		bruteHits[result.UrlString] = result.Status
	}
	if bruteHits[server.URL+"/admin"] != http.StatusOK {
		t.Fatalf("expected admin brute hit, got %+v", bruteHits)
	}

	// 同步检测（等待所有结果）
	results, err := engine.Check(ctx, urls)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(results) != len(urls) {
		t.Fatalf("Check() results = %d, want %d", len(results), len(urls))
	}
}

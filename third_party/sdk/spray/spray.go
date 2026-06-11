package spray

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chainreactors/logs"
	sdkfingers "github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/pkg/types"
	"github.com/chainreactors/spray/core"
	"github.com/chainreactors/spray/core/baseline"
	"github.com/chainreactors/spray/core/ihttp"
	"github.com/chainreactors/spray/pkg"
)

// ========================================
// Engine 实现
// ========================================

// SprayEngine Spray 引擎实现
type SprayEngine struct {
	inited           bool
	providers        []types.Provider
	fingersEngine    *sdkfingers.Engine // 可选的自定义 fingers 引擎
	resourceProvider func(string) []byte
	capacity         *types.Capacity
	matchDetail      bool
	proxy            []string // 引擎级默认代理
	mu               sync.Mutex
}

// NewSprayEngine 创建 Spray 引擎
func NewSprayEngine(config *Config) *SprayEngine {
	if config == nil {
		config = NewConfig()
	}

	e := &SprayEngine{
		inited:           false,
		providers:        config.Providers,
		fingersEngine:    config.FingersEngine,
		resourceProvider: config.ResourceProvider,
		matchDetail:      config.MatchDetail,
		proxy:            config.Proxy,
	}
	if config.Capacity > 0 {
		e.capacity = types.NewCapacity(config.Capacity)
	}
	return e
}

// NewEngine 创建 Spray 引擎
func NewEngine(config *Config) *SprayEngine {
	return NewSprayEngine(config)
}

// Init 初始化引擎（加载指纹库等）
func (e *SprayEngine) Init() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.installResourceProvider()
	if e.inited {
		return nil
	}

	if err := pkg.Load(); err != nil {
		return fmt.Errorf("load config failed: %v", err)
	}

	// 从 Providers 自动创建 fingers 引擎
	if len(e.providers) > 0 && e.fingersEngine == nil {
		fc := sdkfingers.NewConfig().WithProvider(e.providers...)
		if eng, err := sdkfingers.NewEngine(fc); err == nil {
			e.fingersEngine = eng
		}
	}

	e.applyInjectedFingers()
	e.applyMatchDetail()
	e.refreshActivePath()
	e.configureSDKGlobals()
	e.inited = true
	return nil
}

func (e *SprayEngine) configureSDKGlobals() {
	opt := NewDefaultOption()
	baseline.Distance = uint8(opt.SimhashDistance)
	if opt.MaxBodyLength == -1 {
		ihttp.DefaultMaxBodySize = -1
	} else {
		ihttp.DefaultMaxBodySize = opt.MaxBodyLength * 1024
	}
	pkg.BlackStatus = pkg.ParseStatus(pkg.DefaultBlackStatus, opt.BlackStatus)
	pkg.WhiteStatus = pkg.ParseStatus(pkg.DefaultWhiteStatus, opt.WhiteStatus)
	pkg.FuzzyStatus = pkg.ParseStatus(pkg.DefaultFuzzyStatus, opt.FuzzyStatus)
	pkg.UniqueStatus = pkg.ParseStatus(pkg.DefaultUniqueStatus, opt.UniqueStatus)
	pkg.EnableAllFingerEngine = true
	pkg.Extractors["recon"] = combinedReconExtractors()
	logs.Log.SetQuiet(true)
	logs.Log.SetColor(false)
}

func combinedReconExtractors() []*types.Extractor {
	pentestExtractors := pkg.ExtractRegexps["pentest"]
	infoExtractors := pkg.ExtractRegexps["info"]
	reconExtractors := make([]*types.Extractor, 0, len(pentestExtractors)+len(infoExtractors))
	reconExtractors = append(reconExtractors, pentestExtractors...)
	reconExtractors = append(reconExtractors, infoExtractors...)
	return reconExtractors
}

func (e *SprayEngine) installResourceProvider() {
	if e.resourceProvider == nil {
		return
	}
	pkg.SetResourceProvider(e.resourceProvider)
}

// InstallResourceProvider installs the configured resource provider without
// initializing scanner globals. CLI wrappers call this before core parsing so
// direct commands load aiscan-managed templates during their own Prepare path.
func (e *SprayEngine) InstallResourceProvider() {
	if e == nil {
		return
	}
	e.installResourceProvider()
}

func (e *SprayEngine) applyInjectedFingers() bool {
	if e.fingersEngine == nil {
		return false
	}
	libEngine := e.fingersEngine.Get()
	if libEngine == nil {
		return false
	}
	pkg.FingerEngine = libEngine
	pkg.ActivePath = pkg.ActivePath[:0]
	logs.Log.Infof("resources type=fingers source=custom %s", libEngine.String())
	e.refreshActivePath()
	return true
}

func (e *SprayEngine) applyMatchDetail() {
	if !e.matchDetail || pkg.FingerEngine == nil {
		return
	}
	fingersEngine := pkg.FingerEngine.Fingers()
	if fingersEngine == nil {
		return
	}
	fingersEngine.SetMatchDetailEnabled(true)
}

func (e *SprayEngine) refreshActivePath() {
	if pkg.FingerEngine != nil {
		if fingers := pkg.FingerEngine.Fingers(); fingers != nil {
			seen := make(map[string]struct{}, len(pkg.ActivePath))
			for _, path := range pkg.ActivePath {
				seen[path] = struct{}{}
			}
			for _, f := range fingers.HTTPFingers {
				if f.SendDataStr != "" {
					if _, ok := seen[f.SendDataStr]; !ok {
						pkg.ActivePath = append(pkg.ActivePath, f.SendDataStr)
						seen[f.SendDataStr] = struct{}{}
					}
				}
				for _, rule := range f.Rules {
					if rule.SendDataStr != "" {
						if _, ok := seen[rule.SendDataStr]; ok {
							continue
						}
						pkg.ActivePath = append(pkg.ActivePath, rule.SendDataStr)
						seen[rule.SendDataStr] = struct{}{}
					}
				}
			}
		}
	}
}

func (e *SprayEngine) Name() string {
	return "spray"
}

// SetCapacity configures a capacity limit on an already-created engine.
func (e *SprayEngine) SetCapacity(total int) {
	if total > 0 {
		e.capacity = types.NewCapacity(total)
	}
}

// Capacity returns the engine's capacity bucket, or nil if unconfigured.
func (e *SprayEngine) Capacity() *types.Capacity {
	return e.capacity
}

func (e *SprayEngine) Execute(ctx types.Context, task types.Task) (<-chan types.Result, error) {
	if e == nil {
		return nil, fmt.Errorf("spray engine is nil")
	}
	if task == nil {
		return nil, fmt.Errorf("task cannot be nil")
	}
	if err := e.Init(); err != nil {
		return nil, err
	}
	if err := task.Validate(); err != nil {
		return nil, err
	}

	var runCtx *Context
	if ctx == nil {
		runCtx = NewContext()
	} else {
		var ok bool
		runCtx, ok = ctx.(*Context)
		if !ok {
			return nil, fmt.Errorf("unsupported context type: %T", ctx)
		}
		if runCtx == nil {
			runCtx = NewContext()
		}
	}

	switch t := task.(type) {
	case *CheckTask:
		return e.executeCheck(runCtx, t)
	case *BruteTask:
		return e.executeBrute(runCtx, t)
	default:
		return nil, fmt.Errorf("unsupported task type: %s", task.Type())
	}
}

func (e *SprayEngine) Close() error {
	return nil
}

// ========================================
// Result 实现
// ========================================

func newResult(success bool, err error, data *types.SprayResult) types.Result {
	return types.NewResult(success, err, data)
}

func (e *SprayEngine) handler(ctx context.Context, runner *core.Runner, ch chan types.Result) {
	// 启动结果处理 goroutine - 处理 OutputCh
	go func() {
		for bl := range runner.OutputCh {
			select {
			case ch <- newResult(bl.IsValid, nil, bl.SprayResult):
				runner.OutWg.Done()
			case <-ctx.Done():
				runner.OutWg.Done()
				continue
			}
		}
	}()

	// 启动结果处理 goroutine - 处理 FuzzyCh
	go func() {
		for bl := range runner.FuzzyCh {
			select {
			case ch <- newResult(bl.IsValid, nil, bl.SprayResult):
				runner.OutWg.Done()
			case <-ctx.Done():
				runner.OutWg.Done()
				continue
			}
		}
	}()
}

func (e *SprayEngine) executeCheck(ctx *Context, task *CheckTask) (<-chan types.Result, error) {
	return e.execute(ctx, task.Type(), task.URLs, nil)
}

func (e *SprayEngine) executeBrute(ctx *Context, task *BruteTask) (<-chan types.Result, error) {
	return e.execute(ctx, task.Type(), task.urls(), task.Wordlist)
}

// execute 是 check/brute 的统一执行路径.
// wordlist == nil 表示 check 模式, 否则 brute 模式.
func (e *SprayEngine) execute(ctx *Context, taskType string, urls []string, wordlist []string) (<-chan types.Result, error) {
	opt := cloneOption(ctx.opt)
	opt.URL = urls
	opt.PortRange = ""
	// 解析代理：Context（已写入 opt.Proxies）> Config。Client 级代理在
	// ensureSpray 时已下沉到 e.proxy。spray 在 NewRunner 内部自行构建代理链。
	if len(opt.Proxies) == 0 && len(e.proxy) > 0 {
		opt.Proxies = e.proxy
	}
	if opt.PoolSize <= 0 || opt.PoolSize > len(opt.URL) {
		opt.PoolSize = len(opt.URL)
	}

	threads := opt.Threads * opt.PoolSize
	if e.capacity != nil {
		if err := e.capacity.Acquire(ctx.Context(), threads); err != nil {
			return nil, err
		}
	}

	runner, err := opt.NewRunner()
	if err != nil {
		if e.capacity != nil {
			e.capacity.Release(threads)
		}
		return nil, fmt.Errorf("create runner failed: %v", err)
	}

	if wordlist != nil {
		runner.Wordlist = wordlist
		runner.Total = len(wordlist)
		runner.IsCheck = false
	}

	ch := make(chan types.Result)
	go func() {
		defer e.closeRunner(runner)
		defer close(ch)
		if e.capacity != nil {
			defer e.capacity.Release(threads)
		}
		started := time.Now()
		defer func() {
			stats := runner.Stats()
			ctx.emitStats(types.Stats{
				Engine:   e.Name(),
				Task:     taskType,
				Targets:  stats.Targets,
				Tasks:    stats.Tasks,
				Requests: stats.Requests,
				Results:  stats.Results,
				Errors:   stats.Errors,
				Duration: time.Since(started),
			})
		}()
		e.handler(ctx.Context(), runner, ch)

		if runner.IsCheck {
			runner.RunWithCheck(ctx.Context())
		} else {
			runner.RunWithBrute(ctx.Context())
		}
	}()
	return ch, nil
}

// ========================================
// 便捷 API（保持原有使用习惯）
// ========================================

// Check URL 批量检测（同步）
func (e *SprayEngine) Check(ctx *Context, urls []string) ([]*types.SprayResult, error) {
	task := NewCheckTask(urls)
	resultCh, err := e.Execute(ctx, task)
	if err != nil {
		return nil, err
	}

	var sprayResults []*types.SprayResult
	for r := range resultCh {
		// 返回所有结果，无论是否成功/有效
		// URL存活检测需要看到所有URL的状态，而不仅仅是有效的
		if result, ok := types.ResultData[*types.SprayResult](r); ok && result != nil {
			sprayResults = append(sprayResults, result)
		}
	}

	return sprayResults, nil
}

// CheckStream URL 批量检测（流式）
func (e *SprayEngine) CheckStream(ctx *Context, urls []string) (<-chan *types.SprayResult, error) {
	task := NewCheckTask(urls)
	resultCh, err := e.Execute(ctx, task)
	if err != nil {
		return nil, err
	}

	// 转换为 SprayResult channel
	sprayResultCh := make(chan *types.SprayResult, 1)
	go func() {
		defer close(sprayResultCh)
		for result := range resultCh {
			if data, ok := types.ResultData[*types.SprayResult](result); result.Success() && ok && data != nil {
				sprayResultCh <- data
			}
		}
	}()

	return sprayResultCh, nil
}

// Brute 暴力破解（同步）
func (e *SprayEngine) Brute(ctx *Context, baseURL string, wordlist []string) ([]*types.SprayResult, error) {
	task := NewBruteTask(baseURL, wordlist)
	resultCh, err := e.Execute(ctx, task)
	if err != nil {
		return nil, err
	}

	var sprayResults []*types.SprayResult
	for r := range resultCh {
		// 返回所有结果，无论是否成功/有效
		if result, ok := types.ResultData[*types.SprayResult](r); ok && result != nil {
			sprayResults = append(sprayResults, result)
		}
	}

	return sprayResults, nil
}

func (e *SprayEngine) BruteMany(ctx *Context, baseURLs []string, wordlist []string) ([]*types.SprayResult, error) {
	task := NewBruteTasks(baseURLs, wordlist)
	resultCh, err := e.Execute(ctx, task)
	if err != nil {
		return nil, err
	}

	var sprayResults []*types.SprayResult
	for r := range resultCh {
		if result, ok := types.ResultData[*types.SprayResult](r); ok && result != nil {
			sprayResults = append(sprayResults, result)
		}
	}

	return sprayResults, nil
}

// BruteStream 暴力破解（流式）
func (e *SprayEngine) BruteStream(ctx *Context, baseURL string, wordlist []string) (<-chan *types.SprayResult, error) {
	task := NewBruteTask(baseURL, wordlist)
	resultCh, err := e.Execute(ctx, task)
	if err != nil {
		return nil, err
	}

	// 转换为 SprayResult channel
	sprayResultCh := make(chan *types.SprayResult)
	go func() {
		defer close(sprayResultCh)
		for result := range resultCh {
			if data, ok := types.ResultData[*types.SprayResult](result); result.Success() && ok && data != nil {
				sprayResultCh <- data
			}
		}
	}()

	return sprayResultCh, nil
}

func (e *SprayEngine) BruteManyStream(ctx *Context, baseURLs []string, wordlist []string) (<-chan *types.SprayResult, error) {
	task := NewBruteTasks(baseURLs, wordlist)
	resultCh, err := e.Execute(ctx, task)
	if err != nil {
		return nil, err
	}

	sprayResultCh := make(chan *types.SprayResult)
	go func() {
		defer close(sprayResultCh)
		for result := range resultCh {
			if data, ok := types.ResultData[*types.SprayResult](result); result.Success() && ok && data != nil {
				sprayResultCh <- data
			}
		}
	}()

	return sprayResultCh, nil
}

func (e *SprayEngine) closeRunner(runner *core.Runner) {
	if runner.OutputFile != nil {
		runner.OutputFile.Close()
	}
	if runner.DumpFile != nil {
		runner.DumpFile.Close()
	}
	if runner.StatFile != nil {
		runner.StatFile.Close()
	}
	if runner.Progress != nil {
		runner.Progress.Wait()
	}
}

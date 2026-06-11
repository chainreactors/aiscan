package fingers

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encoding/json"

	fingersLib "github.com/chainreactors/fingers"
	"github.com/chainreactors/fingers/alias"
	"github.com/chainreactors/fingers/common"
	"github.com/chainreactors/fingers/favicon"
	"github.com/chainreactors/fingers/fingerprinthub"
	fingersEngine "github.com/chainreactors/fingers/fingers"
	"github.com/chainreactors/fingers/resources"
	"github.com/chainreactors/fingers/xray"
	"github.com/chainreactors/logs"
	sdkhttpx "github.com/chainreactors/sdk/pkg/httpx"
	"github.com/chainreactors/sdk/pkg/types"
	"github.com/chainreactors/utils/httputils"
	"gopkg.in/yaml.v3"
)

// resolveClient 选择主动指纹探测使用的 HTTP 客户端：
// Context 显式设置了 client 或 proxy 时用 Context 的客户端（ctx.proxy 优先）；
// 否则回退到引擎级默认代理（Config.Proxy）；都没有则用 Context 默认客户端。
func (e *Engine) resolveClient(ctx *Context) *http.Client {
	if ctx.client != nil || ctx.proxy != "" {
		return ctx.GetClient()
	}
	if e.config != nil && len(e.config.Proxy) > 0 {
		timeout := time.Duration(ctx.timeout) * time.Second
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		if client, err := sdkhttpx.NewClient(sdkhttpx.Config{
			Timeout:            timeout,
			Proxy:              e.config.Proxy,
			FollowRedirects:    true,
			InsecureSkipVerify: true,
		}); err == nil {
			return client
		}
	}
	return ctx.GetClient()
}

// ========================================
// Engine - 统一的指纹引擎
// ========================================

// Engine 是对 fingers 库的封装，支持多种数据源加载
type Engine struct {
	engine  *fingersLib.Engine
	config  *Config
	aliases []*types.Alias // 原始别名数据
}

// NewEngine 创建一个新的 Engine 实例
// 根据 config 自动选择加载方式（本地/远程）
func NewEngine(config *Config) (*Engine, error) {
	if config == nil {
		config = NewConfig()
	}

	// 尝试加载配置，如果失败则创建空引擎
	if err := config.Load(context.Background()); err != nil {
		// 返回空引擎，允许后续配置
		return &Engine{
			config: config,
			engine: nil,
		}, nil
	}

	if config.FullFingers.Len() == 0 {
		return &Engine{
			config: config,
			engine: nil,
		}, nil
	}

	e := &Engine{
		config: config,
	}

	engine, err := buildEngineFromFullFingers(config.FullFingers, config.MatchDetail)
	if err != nil {
		return nil, err
	}

	e.aliases = config.FullFingers.Aliases()
	e.engine = engine

	return e, nil
}

// NewEngineWithFingers creates an Engine using FullFingers directly.
func NewEngineWithFingers(fingers FullFingers) (*Engine, error) {
	if fingers.Len() == 0 {
		return nil, fmt.Errorf("fingers data is empty")
	}

	config := NewConfig()
	config.FullFingers = fingers

	engine, err := buildEngineFromFullFingers(fingers, false)
	if err != nil {
		return nil, err
	}

	return &Engine{
		engine:  engine,
		config:  config,
		aliases: fingers.Aliases(),
	}, nil
}

// ========================================
// 统一 API - 只提供一种加载方式
// ========================================

// Get 获取底层的 fingers.Engine
func (e *Engine) Get() *types.FingersLibEngine {
	return e.engine
}

// Aliases 获取原始别名数据
func (e *Engine) Aliases() []*types.Alias {
	return e.aliases
}

// Fingers 获取原始指纹数据
func (e *Engine) Fingers() types.Fingers {
	if e == nil || e.config == nil {
		return nil
	}
	return e.config.FullFingers.Fingers()
}

// GetFingersEngine 获取 FingersEngine（用于 gogo 集成）
func (e *Engine) GetFingersEngine() (*types.FingersMatchEngine, error) {
	if e.engine == nil {
		// 返回 nil，允许引擎在未配置时也能使用
		return nil, nil
	}

	impl := e.engine.GetEngine("fingers")
	if impl == nil {
		return nil, nil
	}

	return impl.(*types.FingersMatchEngine), nil
}

// Reload 重新加载指纹
func (e *Engine) Reload(ctx context.Context) error {
	if e.config == nil {
		return fmt.Errorf("config is nil")
	}
	if err := e.config.Load(ctx); err != nil {
		return err
	}

	engine, err := buildEngineFromFullFingers(e.config.FullFingers, e.config.MatchDetail)
	if err != nil {
		return err
	}

	e.aliases = e.config.FullFingers.Aliases()
	e.engine = engine
	return nil
}

// buildEngineFromFingers 兼容 wrapper，供 additions.go 等旧路径调用。
func buildEngineFromFingers(fingers types.Fingers, aliases []*types.Alias, matchDetail bool) (*fingersLib.Engine, error) {
	ff := (FullFingers{}).Merge(fingers, aliases)
	return buildEngineFromFullFingers(ff, matchDetail)
}

// buildEngineFromFullFingers 从 FullFingers 构建多引擎。
// 自动将原生 fingers 和 template 指纹分流到对应的底层引擎。
func buildEngineFromFullFingers(fullFingers FullFingers, matchDetail bool) (*fingersLib.Engine, error) {
	engine := &fingersLib.Engine{
		EnginesImpl:  make(map[string]fingersLib.EngineImpl),
		Enabled:      make(map[string]bool),
		Capabilities: make(map[string]common.EngineCapability),
	}

	faviconEngine := favicon.NewFavicons()
	engine.EnginesImpl["favicon"] = faviconEngine
	engine.Capabilities["favicon"] = faviconEngine.Capability()

	// --- 原生 fingers 引擎 ---
	nativeFingers := fullFingers.NativeFingers()
	var fEngine *fingersEngine.FingersEngine
	if len(nativeFingers) > 0 {
		var httpFingers, socketFingers types.Fingers
		for _, finger := range nativeFingers {
			protocol := finger.Protocol
			if protocol == "" {
				protocol = "http" // 与 Finger.Compile() 默认值一致
			}
			if protocol == "http" {
				httpFingers = append(httpFingers, finger)
			} else if protocol == "tcp" {
				socketFingers = append(socketFingers, finger)
			}
		}
		_, err := resources.LoadPorts()
		if err != nil {
			return nil, err
		}
		fEngine, err = fingersEngine.NewEngine(httpFingers, socketFingers)
		if err != nil {
			return nil, err
		}
		engine.Register(fEngine)
		if matchDetail {
			fEngine.SetMatchDetailEnabled(true)
		}
	}

	// --- fingerprinthub 模板引擎 ---
	fpHubItems := fullFingers.TemplateItems("fingerprinthub")
	if len(fpHubItems) > 0 {
		fpHubEngine, err := buildFingerPrintHubFromTemplates(fpHubItems)
		if err != nil {
			logs.Log.Warnf("fingerprinthub engine build failed: %v", err)
		} else if fpHubEngine != nil && fpHubEngine.Len() > 0 {
			engine.Register(fpHubEngine)
		}
	}

	// --- xray 模板引擎 ---
	xrayItems := fullFingers.TemplateItems("xray")
	if len(xrayItems) > 0 {
		xrayEngine, err := buildXrayFromTemplates(xrayItems)
		if err != nil {
			logs.Log.Warnf("xray engine build failed: %v", err)
		} else if xrayEngine != nil && xrayEngine.Len() > 0 {
			engine.Register(xrayEngine)
		}
	}

	// --- Favicon hash 填充 ---
	if impl := engine.Fingers(); impl != nil {
		for hash, name := range impl.Favicons.Md5Fingers {
			engine.Favicon().Md5Fingers[hash] = name
		}
		for hash, name := range impl.Favicons.Mmh3Fingers {
			engine.Favicon().Mmh3Fingers[hash] = name
		}
	}
	engine.Enabled["favicon"] = false

	// --- Alias 构建 ---
	aliases := fullFingers.Aliases()
	var baseAliases []*alias.Alias
	if impl := engine.Fingers(); impl != nil {
		for _, finger := range impl.HTTPFingers {
			baseAliases = append(baseAliases, &alias.Alias{
				Name:       finger.Name,
				Attributes: finger.Attributes,
				AliasMap: map[string][]string{
					"fingers": {finger.Name},
				},
			})
		}
	}
	for _, item := range fullFingers.TemplateItems() {
		name := templateItemName(item)
		if name == "" {
			continue
		}
		engineKey := item.Engine
		if engineKey == "" {
			engineKey = "fingerprinthub"
		}
		a := &alias.Alias{
			Name: name,
			AliasMap: map[string][]string{
				engineKey: {name},
			},
		}
		if item.Finger != nil {
			a.Attributes = item.Finger.Attributes
		}
		baseAliases = append(baseAliases, a)
	}
	aliasEngine := &alias.Aliases{
		Aliases: make(map[string]*alias.Alias, len(baseAliases)+len(aliases)),
		Map:     make(map[string]map[string]string),
	}
	aliasEngine.Compile(baseAliases)
	aliasEngine.Compile(aliases)
	engine.Aliases = aliasEngine

	return engine, nil
}

// rawContentToMap 从原始 YAML 解析为 map，保留 variables 等不可序列化的字段。
func rawContentToMap(rawYAML string) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := yaml.Unmarshal([]byte(rawYAML), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// buildFingerPrintHubFromTemplates 从原始 RawContent 构建 FingerPrintHubEngine。
func buildFingerPrintHubFromTemplates(items []*FullFinger) (*fingerprinthub.FingerPrintHubEngine, error) {
	var webMaps, serviceMaps []map[string]interface{}
	for _, item := range items {
		if item.RawContent == "" {
			continue
		}
		tmplMap, err := rawContentToMap(item.RawContent)
		if err != nil {
			continue
		}
		if isWebTemplate(tmplMap) {
			webMaps = append(webMaps, tmplMap)
		} else {
			serviceMaps = append(serviceMaps, tmplMap)
		}
	}

	if len(webMaps) == 0 && len(serviceMaps) == 0 {
		return nil, nil
	}
	if webMaps == nil {
		webMaps = []map[string]interface{}{}
	}
	if serviceMaps == nil {
		serviceMaps = []map[string]interface{}{}
	}

	webJSON, _ := json.Marshal(webMaps)
	svcJSON, _ := json.Marshal(serviceMaps)
	return fingerprinthub.NewFingerPrintHubEngine(webJSON, svcJSON)
}

// buildXrayFromTemplates 从原始 RawContent 构建 XrayEngine。
func buildXrayFromTemplates(items []*FullFinger) (*xray.XrayEngine, error) {
	var tmplMaps []map[string]interface{}
	for _, item := range items {
		if item.RawContent == "" {
			continue
		}
		tmplMap, err := rawContentToMap(item.RawContent)
		if err != nil {
			continue
		}
		tmplMaps = append(tmplMaps, tmplMap)
	}
	if len(tmplMaps) == 0 {
		return nil, nil
	}
	jsonData, _ := json.Marshal(tmplMaps)
	return xray.NewXrayEngine(jsonData)
}

func templateItemName(item *FullFinger) string {
	if item.Finger != nil && item.Finger.Name != "" {
		return item.Finger.Name
	}
	if item.Template != nil {
		if item.Template.Info.Name != "" {
			return item.Template.Info.Name
		}
		return item.Template.Id
	}
	return ""
}

// safeHTTPActiveMatch is a thin, non-load-bearing resilience boundary. The
// concurrency root causes are fixed (per-execution client via ScanContext) and
// known panic points are guarded, but the template engines also run untrusted /
// wild templates, so one malformed template must not abort a whole multi-target
// scan. Unlike before, a recovered panic is surfaced (Warn + which engine), not
// silently swallowed — it signals a bug to harden, not a crutch to rely on.
func safeHTTPActiveMatch(engine string, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logs.Log.Warnf("%s active match panicked on a template, skipped: %v", engine, r)
		}
	}()
	fn()
}

func isWebTemplate(tmpl map[string]interface{}) bool {
	if _, ok := tmpl["http"]; ok {
		return true
	}
	if _, ok := tmpl["requests"]; ok {
		return true
	}
	return false
}

// Count 获取指纹总数
func (e *Engine) Count() int {
	if e.config == nil {
		return 0
	}
	return len(e.config.FullFingers.Fingers())
}

// Close 关闭引擎
func (e *Engine) Close() error {
	return nil
}

// ========================================
// 核心匹配 API - 原子化设计
// ========================================

// Match 匹配单个 HTTP 响应原始数据（被动指纹识别 - Level 0）
func (e *Engine) Match(data []byte) (types.Frameworks, error) {
	if e.engine == nil {
		// 返回空结果，允许引擎在未配置时也能使用
		return nil, nil
	}
	return e.engine.DetectContent(data)
}

// MatchFavicon 匹配 Favicon 指纹（被动识别）
// 参数:
//   - data: favicon 图标的原始字节数据
//
// 返回:
//   - types.Frameworks: 匹配到的指纹列表
//   - error: 错误信息
func (e *Engine) MatchFavicon(data []byte) (types.Frameworks, error) {
	if e.engine == nil {
		// 返回空结果，允许引擎在未配置时也能使用
		return nil, nil
	}
	return e.engine.MatchFavicon(data), nil
}

// MatchHTTP 匹配 HTTP 响应指纹（被动识别）
// 参数:
//   - resp: HTTP 响应对象
//
// 返回:
//   - types.Frameworks: 匹配到的指纹列表
//   - error: 错误信息
func (e *Engine) MatchHTTP(resp *http.Response) (types.Frameworks, error) {
	if e.engine == nil {
		// 返回空结果，允许引擎在未配置时也能使用
		return nil, nil
	}

	// 读取响应原始数据
	data := httputils.ReadRaw(resp)

	// 调用底层引擎进行匹配
	return e.engine.DetectContent(data)
}

// HTTPMatch HTTP/HTTPS 主动探测指纹识别（批量同步版本）
// 参数:
//   - ctx: 上下文（包含 timeout、level 等配置）
//   - urls: 目标URL列表（如 []string{"http://example.com", "https://example.com:8080"}）
//
// 返回:
//   - []*TargetResult: 每个目标的探测结果
//   - error: 错误信息（仅在引擎初始化失败等严重错误时返回）
func (e *Engine) HTTPMatch(ctx *Context, urls []string) ([]*TargetResult, error) {
	// 调用流式版本
	resultCh, err := e.HTTPMatchStream(ctx, urls)
	if err != nil {
		return nil, err
	}

	// 收集所有结果
	var results []*TargetResult
	for result := range resultCh {
		results = append(results, result)
	}

	return results, nil
}

// scanHTTPTarget 扫描单个 HTTP 目标，自动对所有注册引擎执行主动探测。
func (e *Engine) scanHTTPTarget(ctx *Context, url string, level int) *TargetResult {
	result := &TargetResult{
		Target: url,
	}

	parsedURL, err := parseURL(url)
	if err != nil {
		result.Err = fmt.Errorf("invalid url: %w", err)
		return result
	}
	baseURL := parsedURL.Scheme + "://" + parsedURL.Host
	if parsedURL.Port != "" && parsedURL.Port != "80" && parsedURL.Port != "443" {
		baseURL += ":" + parsedURL.Port
	}
	client := ctx.GetClient()

	// 1. 原生 fingers 引擎
	if fEngine := e.engine.Fingers(); fEngine != nil {
		sender := fingersEngine.Sender(func(data []byte) ([]byte, bool) {
			sendPath := string(data)
			fullPath := pathJoin(parsedURL.Path, sendPath)
			fullURL := baseURL + fullPath

			req, err := http.NewRequest(http.MethodGet, fullURL, nil)
			if err != nil {
				return nil, false
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

			resp, err := client.Do(req)
			if err != nil {
				return nil, false
			}
			defer resp.Body.Close()

			return httputils.ReadRaw(resp), true
		})

		for _, finger := range fEngine.HTTPFingers {
			frame, vuln, ok := finger.ActiveMatch(level, sender)
			if ok && frame != nil {
				result.Results = append(result.Results, &types.ServiceResult{
					Framework: frame,
					Vuln:      vuln,
				})
			}
		}
	}

	// 2. fingerprinthub / xray 模板引擎（共享同一个 Transport）
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	activeCallback := func(frame *common.Framework, vuln *common.Vuln) {
		if frame != nil {
			result.Results = append(result.Results, &types.ServiceResult{
				Framework: frame,
				Vuln:      vuln,
			})
		}
	}

	if fpHub := e.engine.FingerPrintHub(); fpHub != nil {
		safeHTTPActiveMatch("fingerprinthub", func() {
			fpHub.HTTPActiveMatch(baseURL, level, transport, activeCallback)
		})
	}

	if xrayEng := e.engine.Xray(); xrayEng != nil {
		safeHTTPActiveMatch("xray", func() {
			xrayEng.HTTPActiveMatch(baseURL, level, transport, activeCallback)
		})
	}

	return result
}

// scanServiceTarget 扫描单个 Service 目标（内部方法）
func (e *Engine) scanServiceTarget(ctx *Context, target string, level int) *TargetResult {
	result := &TargetResult{
		Target: target,
	}

	// 解析target获取host和port
	host, port, err := parseTarget(target)
	if err != nil {
		result.Err = fmt.Errorf("invalid target: %w", err)
		return result
	}

	// 从 Context 获取 timeout
	timeout := ctx.GetTimeout()
	if timeout <= 0 {
		timeout = 10 // 默认10秒
	}

	// 创建默认的 ServiceSender
	sender := common.NewServiceSender(time.Duration(timeout) * time.Second)

	// 执行主动探测
	serviceResults, err := e.engine.DetectService(host, port, level, sender, nil)
	if err != nil {
		result.Err = err
		return result
	}

	result.Results = serviceResults
	return result
}

// ServiceMatch 通用服务主动探测指纹识别（批量同步版本）
// 参数:
//   - ctx: 上下文（包含 timeout、level 等配置）
//   - targets: 目标地址列表（格式：ip:port 或 host:port，如 []string{"192.168.1.1:80", "example.com:443"}）
//
// 返回:
//   - []*TargetResult: 每个目标的探测结果
//   - error: 错误信息（仅在引擎初始化失败等严重错误时返回）
func (e *Engine) ServiceMatch(ctx *Context, targets []string) ([]*TargetResult, error) {
	// 调用流式版本
	resultCh, err := e.ServiceMatchStream(ctx, targets)
	if err != nil {
		return nil, err
	}

	// 收集所有结果
	var results []*TargetResult
	for result := range resultCh {
		results = append(results, result)
	}

	return results, nil
}

// HTTPMatchStream HTTP/HTTPS 主动探测指纹识别（批量流式版本）
// 参数:
//   - ctx: 上下文（包含 timeout、level 等配置）
//   - urls: 目标URL列表（如 []string{"http://example.com", "https://example.com:8080"}）
//
// 返回:
//   - <-chan *TargetResult: 结果 channel（每个目标扫描完成后立即发送）
//   - error: 错误信息（仅在引擎初始化失败等严重错误时返回）
func (e *Engine) HTTPMatchStream(ctx *Context, urls []string) (<-chan *TargetResult, error) {
	if e.engine == nil {
		ch := make(chan *TargetResult)
		close(ch)
		return ch, nil
	}

	level := ctx.GetLevel()
	if level < 0 || level > 3 {
		return nil, fmt.Errorf("invalid level: %d, must be 0-3", level)
	}

	// 统一解析 client 并注入 ctx，所有引擎共享
	resolvedClient := e.resolveClient(ctx)
	ctx = ctx.clone().withResolvedClient(resolvedClient)

	resultCh := make(chan *TargetResult)

	go func() {
		defer close(resultCh)

		for _, url := range urls {
			targetResult := e.scanHTTPTarget(ctx, url, level)

			select {
			case resultCh <- targetResult:
			case <-ctx.Context().Done():
				return
			}
		}
	}()

	return resultCh, nil
}

// ServiceMatchStream 通用服务主动探测指纹识别（流式版本）
// 参数:
//   - ctx: 上下文（包含 timeout、level 等配置）
//   - target: 目标地址（格式：ip:port 或 host:port，如 "192.168.1.1:80", "example.com:443"）
//
// 返回:
//   - <-chan *types.ServiceResult: 结果 channel
//   - error: 错误信息
//
// ServiceMatchStream 通用服务主动探测指纹识别（批量流式版本）
// 参数:
//   - ctx: 上下文（包含 timeout、level 等配置）
//   - targets: 目标地址列表（格式：ip:port 或 host:port，如 []string{"192.168.1.1:80", "example.com:443"}）
//
// 返回:
//   - <-chan *TargetResult: 结果 channel（每个目标扫描完成后立即发送）
//   - error: 错误信息（仅在引擎初始化失败等严重错误时返回）
func (e *Engine) ServiceMatchStream(ctx *Context, targets []string) (<-chan *TargetResult, error) {
	if e.engine == nil {
		// 返回空 channel，允许引擎在未配置时也能使用
		ch := make(chan *TargetResult)
		close(ch)
		return ch, nil
	}

	// 从 Context 获取 level (Service: 0-9)
	level := ctx.GetLevel()
	if level < 0 || level > 9 {
		return nil, fmt.Errorf("invalid level: %d, must be 0-9", level)
	}

	// 创建结果 channel
	resultCh := make(chan *TargetResult)

	// 在 goroutine 中执行批量探测
	go func() {
		defer close(resultCh)

		for _, target := range targets {
			// 扫描单个目标
			targetResult := e.scanServiceTarget(ctx, target, level)

			// 发送结果到 channel
			select {
			case resultCh <- targetResult:
			case <-ctx.Context().Done():
				return
			}
		}
	}()

	return resultCh, nil
}

// ========================================
// SDK Engine 接口实现（可选）
// ========================================

// Name 返回引擎名称（实现 types.Engine 接口）
func (e *Engine) Name() string {
	return "fingers"
}

// Execute 执行任务（实现 types.Engine 接口）
func (e *Engine) Execute(ctx types.Context, task types.Task) (<-chan types.Result, error) {
	// 确保引擎已初始化
	if e.engine == nil {
		// 返回空 channel，允许引擎在未配置时也能使用
		ch := make(chan types.Result)
		close(ch)
		return ch, nil
	}

	// 验证任务
	if err := task.Validate(); err != nil {
		return nil, err
	}

	// 只支持 MatchTask
	matchTask, ok := task.(*MatchTask)
	if !ok {
		return nil, fmt.Errorf("unsupported task type: %s", task.Type())
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
	}

	return e.executeMatch(runCtx, matchTask)
}

// executeMatch 执行单个指纹匹配任务
func (e *Engine) executeMatch(ctx *Context, task *MatchTask) (<-chan types.Result, error) {
	resultCh := make(chan types.Result, 1)

	go func() {
		defer close(resultCh)

		frameworks, err := e.Match(task.Data)

		// 发送结果
		select {
		case resultCh <- &MatchResult{
			success:    err == nil,
			err:        err,
			frameworks: frameworks,
		}:
		case <-ctx.Context().Done():
		}
	}()

	return resultCh, nil
}

// ========================================
// 辅助函数
// ========================================

// parseURL 解析URL，提取scheme、host、port和path
type parsedURL struct {
	Scheme string
	Host   string
	Port   string
	Path   string
}

func parseURL(rawURL string) (*parsedURL, error) {
	// 确保URL有协议前缀
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "http://" + rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	host := u.Hostname()
	port := u.Port()

	// 如果没有指定端口，根据协议使用默认端口
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	return &parsedURL{
		Scheme: u.Scheme,
		Host:   host,
		Port:   port,
		Path:   u.Path,
	}, nil
}

// parseTarget 解析target（ip:port或host:port格式），提取host和port
func parseTarget(target string) (host, port string, err error) {
	// 使用net.SplitHostPort解析
	host, port, err = net.SplitHostPort(target)
	if err != nil {
		return "", "", fmt.Errorf("invalid target format, expected 'host:port': %w", err)
	}

	if host == "" {
		return "", "", fmt.Errorf("host cannot be empty")
	}

	if port == "" {
		return "", "", fmt.Errorf("port cannot be empty")
	}

	return host, port, nil
}

// pathJoin 连接两个URL路径
// 参数:
//   - base: 基础路径（如 "", "/", "/aaa", "/aaa/"）
//   - append: 要追加的路径（如 "/nacos/"）
//
// 返回:
//   - 连接后的路径
//
// 示例:
//   - pathJoin("", "/nacos/") → "/nacos/"
//   - pathJoin("/", "/nacos/") → "/nacos/"
//   - pathJoin("/aaa", "/nacos/") → "/aaa/nacos/"
//   - pathJoin("/aaa/", "/nacos/") → "/aaa/nacos/"
func pathJoin(base, append string) string {
	// 去除 base 的尾部斜杠
	base = strings.TrimSuffix(base, "/")

	// 如果 base 为空，直接返回 append
	if base == "" {
		return append
	}

	// 确保 append 以 / 开头
	if !strings.HasPrefix(append, "/") {
		append = "/" + append
	}

	return base + append
}


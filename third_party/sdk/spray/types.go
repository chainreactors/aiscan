package spray

import (
	"context"
	"fmt"

	sdkfingers "github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/pkg/types"
)

// ========================================
// Context 实现
// ========================================

// Context Spray 上下文
type Context struct {
	ctx          context.Context
	opt          *Option
	statsHandler func(types.Stats)
}

var _ types.Context = (*Context)(nil)

// NewContext 创建 Spray 上下文
func NewContext() *Context {
	return &Context{
		ctx: context.Background(),
		opt: NewDefaultOption(),
	}
}

// WithContext 基于给定的 context.Context 复制 Context
func (c *Context) WithContext(ctx context.Context) *Context {
	return &Context{
		ctx:          ctx,
		opt:          cloneOption(c.opt),
		statsHandler: c.statsHandler,
	}
}

func (c *Context) Context() context.Context {
	return c.ctx
}

// SetThreads 设置线程数
func (c *Context) SetThreads(threads int) *Context {
	c.opt.Threads = threads
	return c
}

// SetTimeout 设置超时时间（秒）
func (c *Context) SetTimeout(timeout int) *Context {
	c.opt.Timeout = timeout
	return c
}

// SetMethod 设置 HTTP 方法
func (c *Context) SetMethod(method string) *Context {
	c.opt.Method = method
	return c
}

// SetHeaders 设置自定义请求头
func (c *Context) SetHeaders(headers []string) *Context {
	c.opt.Headers = headers
	return c
}

// SetHost 设置自定义Host头
func (c *Context) SetHost(host string) *Context {
	c.opt.Host = host
	return c
}

// SetMod 设置运行模式 (path/host/param)
func (c *Context) SetMod(mod string) *Context {
	c.opt.Mod = mod
	return c
}

// SetFilter 设置过滤规则
func (c *Context) SetFilter(filter string) *Context {
	c.opt.Filter = filter
	return c
}

// SetMatch 设置匹配规则
func (c *Context) SetMatch(match string) *Context {
	c.opt.Match = match
	return c
}

// SetProxy 设置本次执行使用的代理（支持多级代理链）。
// spray 在内部 NewRunner 时自行解析 opt.Proxies 构建 proxyclient 链。
// 传入空参数表示清除 Context 级代理，回退到 Config / Client 级配置。
func (c *Context) SetProxy(proxies ...string) *Context {
	c.opt.Proxies = proxies
	return c
}

// SetOption 设置完整选项
func (c *Context) SetOption(opt *Option) *Context {
	c.opt = cloneOption(opt)
	return c
}

func (c *Context) SetStatsHandler(handler func(types.Stats)) *Context {
	c.statsHandler = handler
	return c
}

func (c *Context) emitStats(stats types.Stats) {
	if c == nil || c.statsHandler == nil || c.ctx.Err() != nil {
		return
	}
	c.statsHandler(stats)
}

// ========================================
// Plugin 配置方法
// ========================================

// SetAdvance 启用所有插件
func (c *Context) SetAdvance(enable bool) *Context {
	c.opt.Advance = enable
	return c
}

// SetActivePlugin 启用主动指纹路径插件
func (c *Context) SetActivePlugin(enable bool) *Context {
	c.opt.ActivePlugin = enable
	return c
}

// SetReconPlugin 启用信息提取插件
func (c *Context) SetReconPlugin(enable bool) *Context {
	c.opt.ReconPlugin = enable
	return c
}

// SetBakPlugin 启用备份文件发现插件
func (c *Context) SetBakPlugin(enable bool) *Context {
	c.opt.BakPlugin = enable
	return c
}

// SetFuzzuliPlugin 启用 Fuzzuli 插件
func (c *Context) SetFuzzuliPlugin(enable bool) *Context {
	c.opt.FuzzuliPlugin = enable
	return c
}

// SetCommonPlugin 启用常见文件发现插件
func (c *Context) SetCommonPlugin(enable bool) *Context {
	c.opt.CommonPlugin = enable
	return c
}

// SetCrawlPlugin 启用爬虫插件
func (c *Context) SetCrawlPlugin(enable bool) *Context {
	c.opt.CrawlPlugin = enable
	return c
}

// SetCrawlDepth 设置爬虫深度
func (c *Context) SetCrawlDepth(depth int) *Context {
	c.opt.CrawlDepth = depth
	return c
}

// SetFinger 启用主动指纹检测
func (c *Context) SetFinger(enable bool) *Context {
	c.opt.Finger = enable
	return c
}

// SetExtracts 设置信息提取规则
func (c *Context) SetExtracts(extracts []string) *Context {
	c.opt.Extracts = extracts
	return c
}

// SetRecursiveDepth 设置递归深度
func (c *Context) SetRecursiveDepth(depth int) *Context {
	c.opt.Depth = depth
	return c
}

// ========================================
// 字典 / 规则配置方法
// ========================================

func (c *Context) SetDictionaries(dicts []string) *Context {
	c.opt.Dictionaries = dicts
	return c
}

func (c *Context) SetRules(rules []string) *Context {
	c.opt.Rules = rules
	return c
}

func (c *Context) SetWord(word string) *Context {
	c.opt.Word = word
	return c
}

func (c *Context) SetDefaultDict(enable bool) *Context {
	c.opt.DefaultDict = enable
	return c
}

// ========================================
// Config 实现
// ========================================

// Config Spray 配置
type Config struct {
	Providers        []types.Provider
	FingersEngine    *sdkfingers.Engine
	ResourceProvider func(string) []byte
	Capacity         int
	MatchDetail      bool
	Proxy            []string // 引擎级默认代理，作用于该引擎所有执行（可被 Context 覆盖）
}

// NewConfig 创建默认配置
func NewConfig() *Config {
	return &Config{}
}

func (c *Config) Validate() error {
	return nil
}

// WithProvider 追加数据源，支持多次调用自动合并
func (c *Config) WithProvider(providers ...types.Provider) *Config {
	c.Providers = append(c.Providers, providers...)
	return c
}

// WithFingersEngine 设置自定义 fingers 引擎
func (c *Config) WithFingersEngine(engine *sdkfingers.Engine) *Config {
	c.FingersEngine = engine
	return c
}

// WithMatchDetail enables matcher metadata on spray fingerprint results.
func (c *Config) WithMatchDetail() *Config {
	c.MatchDetail = true
	return c
}

// WithResourceProvider sets a provider used by the underlying spray package.
func (c *Config) WithResourceProvider(provider func(string) []byte) *Config {
	c.ResourceProvider = provider
	return c
}

// WithCapacity sets the total capacity for concurrent thread usage across all
// simultaneous invocations. When set, each Execute call acquires its thread
// count from this shared bucket and blocks if capacity is exhausted.
func (c *Config) WithCapacity(total int) *Config {
	c.Capacity = total
	return c
}

// WithProxy 设置引擎级默认代理（支持多级代理链）。可被 Context.SetProxy 覆盖。
func (c *Config) WithProxy(proxies ...string) *Config {
	c.Proxy = proxies
	return c
}

// ========================================
// Task 实现
// ========================================

// CheckTask URL 检测任务
type CheckTask struct {
	URLs []string
}

// NewCheckTask 创建 URL 检测任务
func NewCheckTask(urls []string) *CheckTask {
	return &CheckTask{URLs: urls}
}

func (t *CheckTask) Type() string {
	return "check"
}

func (t *CheckTask) Validate() error {
	if len(t.URLs) == 0 {
		return fmt.Errorf("URLs cannot be empty")
	}
	return nil
}

// BruteTask 暴力破解任务
type BruteTask struct {
	BaseURL  string   // kept for compatibility with older single-target callers
	BaseURLs []string // optional batch targets; executed by spray Runner's task pool
	Wordlist []string
}

// NewBruteTask 创建暴力破解任务
func NewBruteTask(baseURL string, wordlist []string) *BruteTask {
	return &BruteTask{
		BaseURL:  baseURL,
		BaseURLs: []string{baseURL},
		Wordlist: wordlist,
	}
}

func NewBruteTasks(baseURLs []string, wordlist []string) *BruteTask {
	return &BruteTask{
		BaseURL:  firstString(baseURLs),
		BaseURLs: append([]string(nil), baseURLs...),
		Wordlist: wordlist,
	}
}

func (t *BruteTask) Type() string {
	return "brute"
}

func (t *BruteTask) Validate() error {
	if len(t.urls()) == 0 {
		return fmt.Errorf("BaseURLs cannot be empty")
	}
	if len(t.Wordlist) == 0 {
		return fmt.Errorf("Wordlist cannot be empty")
	}
	return nil
}

func (t *BruteTask) urls() []string {
	if t == nil {
		return nil
	}
	urls := make([]string, 0, len(t.BaseURLs)+1)
	for _, u := range t.BaseURLs {
		if u != "" {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 && t.BaseURL != "" {
		urls = append(urls, t.BaseURL)
	}
	return urls
}

func firstString(values []string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

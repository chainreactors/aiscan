package gogo

import (
	"context"
	"fmt"

	gogopkg "github.com/chainreactors/gogo/v2/pkg"
	sdkfingers "github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/neutron"
	"github.com/chainreactors/sdk/pkg/types"
)

const (
	ModDefault    = gogopkg.Default
	ModSmart      = gogopkg.SMART
	ModSuperSmart = gogopkg.SUPERSMART
	ModSmartB     = gogopkg.SUPERSMARTB
)

// ========================================
// Context 实现
// ========================================

// Context GoGo 上下文
type Context struct {
	ctx          context.Context
	threads      int
	mod          string
	excludes     []string
	opt          *types.GogoOption
	statsHandler func(types.Stats)
	proxy        []string // per-execution 代理覆盖（优先级高于 Config / Client）
}

var _ types.Context = (*Context)(nil)

// NewContext 创建 GoGo 上下文
func NewContext() *Context {
	return &Context{
		ctx:     context.Background(),
		threads: 1000,
		opt:     types.NewDefaultGogoOption(),
	}
}

// WithContext 基于给定的 context.Context 复制 Context
func (c *Context) WithContext(ctx context.Context) *Context {
	return &Context{
		ctx:          ctx,
		threads:      c.threads,
		mod:          c.mod,
		excludes:     c.excludes,
		opt:          types.CloneGogoOption(c.opt),
		statsHandler: c.statsHandler,
		proxy:        c.proxy,
	}
}

// SetMod 设置扫描模式 ("s"=smart, "ss"=super-smart, "sc"=super-smart-B, ""=default)
func (c *Context) SetMod(mod string) *Context {
	c.mod = mod
	return c
}

// SetExcludes 设置排除的 IP/CIDR 列表
func (c *Context) SetExcludes(excludes ...string) *Context {
	c.excludes = excludes
	return c
}

func (c *Context) Context() context.Context {
	return c.ctx
}

// SetThreads 设置线程数
func (c *Context) SetThreads(threads int) *Context {
	if threads > 0 {
		c.threads = threads
	}
	return c
}

// SetProxy 设置本次执行使用的代理（支持多级代理链）。
// 例如 SetProxy("socks5://127.0.0.1:1080") 或 SetProxy("http://a:8080", "socks5://b:1080")。
// 传入空参数表示清除 Context 级代理，回退到 Config / Client 级配置。
func (c *Context) SetProxy(proxies ...string) *Context {
	c.proxy = proxies
	return c
}

// SetOption 设置运行选项
func (c *Context) SetOption(opt *types.GogoOption) *Context {
	c.opt = types.CloneGogoOption(opt)
	return c
}

func (c *Context) SetStatsHandler(handler func(types.Stats)) *Context {
	c.statsHandler = handler
	return c
}

func (c *Context) emitStats(stats types.Stats) {
	if c != nil && c.statsHandler != nil {
		c.statsHandler(stats)
	}
}

// SetVersionLevel 设置指纹识别级别
func (c *Context) SetVersionLevel(level int) *Context {
	if c.opt == nil {
		c.opt = types.NewDefaultGogoOption()
	}
	c.opt.VersionLevel = level
	return c
}

// SetExploit 设置漏洞检测模式
func (c *Context) SetExploit(exploit string) *Context {
	if c.opt == nil {
		c.opt = types.NewDefaultGogoOption()
	}
	c.opt.Exploit = exploit
	return c
}

// SetDelay 设置超时时间（秒）
func (c *Context) SetDelay(delay int) *Context {
	if c.opt == nil {
		c.opt = types.NewDefaultGogoOption()
	}
	c.opt.Delay = delay
	return c
}

// ========================================
// Config 实现
// ========================================

// Config GoGo 配置
type Config struct {
	Providers        []types.Provider
	FingersEngine    *sdkfingers.Engine
	NeutronEngine    *neutron.Engine
	ResourceProvider func(string) []byte
	Capacity         int
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

// WithNeutronEngine 设置自定义 neutron 引擎
func (c *Config) WithNeutronEngine(engine *neutron.Engine) *Config {
	c.NeutronEngine = engine
	return c
}

// WithResourceProvider sets a provider used by the underlying gogo package.
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

// ScanTask 扫描任务
type ScanTask struct {
	IP    string
	Ports string
}

// NewScanTask 创建扫描任务
func NewScanTask(ip, ports string) *ScanTask {
	return &ScanTask{IP: ip, Ports: ports}
}

func (t *ScanTask) Type() string {
	return "scan"
}

func (t *ScanTask) Validate() error {
	if t.IP == "" {
		return fmt.Errorf("IP cannot be empty")
	}
	if t.Ports == "" {
		return fmt.Errorf("Ports cannot be empty")
	}
	return nil
}

// WorkflowTask 工作流任务
type WorkflowTask struct {
	Workflow *types.Workflow
}

// NewWorkflowTask 创建工作流任务
func NewWorkflowTask(workflow *types.Workflow) *WorkflowTask {
	return &WorkflowTask{Workflow: workflow}
}

func (t *WorkflowTask) Type() string {
	return "workflow"
}

func (t *WorkflowTask) Validate() error {
	if t.Workflow == nil {
		return fmt.Errorf("Workflow cannot be nil")
	}
	return nil
}

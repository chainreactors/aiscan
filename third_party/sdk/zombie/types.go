package zombie

import (
	"context"
	"fmt"

	"github.com/chainreactors/sdk/pkg/types"
)

type Context struct {
	ctx          context.Context
	opt          *types.ZombieOption
	statsHandler func(types.Stats)
	proxy        []string // per-execution 代理覆盖（优先级高于 Config / Client）
}

var _ types.Context = (*Context)(nil)

func NewContext() *Context {
	return &Context{
		ctx: context.Background(),
		opt: types.NewDefaultZombieOption(),
	}
}

func (c *Context) WithContext(ctx context.Context) *Context {
	return &Context{
		ctx:          ctx,
		opt:          types.CloneZombieOption(c.opt),
		statsHandler: c.statsHandler,
		proxy:        c.proxy,
	}
}

// SetProxy 设置本次执行使用的代理（支持多级代理链）。仅对支持代理的插件生效
// （ssh/smb/vnc/ftp/rsync/redis 等原生 TCP 或可注入拨号器的插件）。
// 传入空参数表示清除 Context 级代理，回退到 Config / Client 级配置。
func (c *Context) SetProxy(proxies ...string) *Context {
	c.proxy = proxies
	return c
}

func (c *Context) Context() context.Context {
	return c.ctx
}

func (c *Context) SetOption(opt *types.ZombieOption) *Context {
	c.opt = types.CloneZombieOption(opt)
	return c
}

func (c *Context) SetThreads(threads int) *Context {
	if threads > 0 {
		c.opt.Threads = threads
	}
	return c
}

func (c *Context) SetTimeout(timeout int) *Context {
	if timeout > 0 {
		c.opt.Timeout = timeout
	}
	return c
}

func (c *Context) SetTop(top int) *Context {
	if top >= 0 {
		c.opt.Top = top
	}
	return c
}

func (c *Context) SetFirstOnly(firstOnly bool) *Context {
	c.opt.FirstOnly = firstOnly
	return c
}

func (c *Context) SetNoUnauth(noUnauth bool) *Context {
	c.opt.NoUnAuth = noUnauth
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

type Config struct {
	Capacity         int
	ResourceProvider func(string) []byte
	Proxy            []string // 引擎级默认代理，作用于该引擎所有执行（可被 Context 覆盖）
}

func NewConfig() *Config {
	return &Config{}
}

func (c *Config) Validate() error {
	return nil
}

func (c *Config) WithCapacity(total int) *Config {
	c.Capacity = total
	return c
}

func (c *Config) WithResourceProvider(provider func(string) []byte) *Config {
	c.ResourceProvider = provider
	return c
}

// WithProxy 设置引擎级默认代理（支持多级代理链）。可被 Context.SetProxy 覆盖。
func (c *Config) WithProxy(proxies ...string) *Config {
	c.Proxy = proxies
	return c
}

type Target = types.ZombieTarget

type Auth struct {
	Username string
	Password string
}

type BruteTask struct {
	Targets   []Target
	Users     []string
	Passwords []string
	Auths     []Auth
	mod       string
}

func NewBruteTask(targets []Target) *BruteTask {
	return &BruteTask{Targets: targets}
}

func (t *BruteTask) Type() string {
	if t.mod != "" {
		return t.mod
	}
	return "brute"
}

func (t *BruteTask) Validate() error {
	if len(t.Targets) == 0 {
		return fmt.Errorf("targets cannot be empty")
	}
	for i, target := range t.Targets {
		if target.IP == "" {
			return fmt.Errorf("targets[%d].IP cannot be empty", i)
		}
		if target.Service == "" {
			return fmt.Errorf("targets[%d].Service cannot be empty", i)
		}
	}
	return nil
}

package client

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/neutron"
	"github.com/chainreactors/sdk/pkg/association"
	"github.com/chainreactors/sdk/pkg/types"
	"github.com/chainreactors/sdk/spray"
	"github.com/chainreactors/sdk/zombie"
)

type Option func(*options)

type options struct {
	providers        []types.Provider
	resourceProvider func(string) []byte
	indexOptions     *association.IndexOptions
	proxy            []string // 全局默认代理，下沉到各引擎（可被引擎 Config / Context 覆盖）

	fingersConfig *fingers.Config
	neutronConfig *neutron.Config
	gogoConfig    *gogo.Config
	sprayConfig   *spray.Config
	zombieConfig  *zombie.Config
}

func WithProvider(providers ...types.Provider) Option {
	return func(o *options) { o.providers = append(o.providers, providers...) }
}

func WithResourceProvider(rp func(string) []byte) Option {
	return func(o *options) { o.resourceProvider = rp }
}

// WithProxy 设置全局默认代理（支持多级代理链），应用于 gogo / spray / zombie
// 所有引擎。各引擎 Config.WithProxy 或 Context.SetProxy 可覆盖此默认值。
// 支持 proxyclient 的全部协议，例如：
//
//	client.New(client.WithProxy("socks5://127.0.0.1:1080"))
//	client.New(client.WithProxy("http://a:8080", "socks5://b:1080")) // 代理链
func WithProxy(proxies ...string) Option {
	return func(o *options) { o.proxy = proxies }
}

// WithIndex enables the association index on this client.
// Pass nil to use default IndexOptions; pass a pointer to customize.
func WithIndex(opts *association.IndexOptions) Option {
	return func(o *options) {
		if opts == nil {
			opts = &association.IndexOptions{}
		}
		o.indexOptions = opts
	}
}

func WithFingersConfig(cfg *fingers.Config) Option {
	return func(o *options) { o.fingersConfig = cfg }
}

func WithNeutronConfig(cfg *neutron.Config) Option {
	return func(o *options) { o.neutronConfig = cfg }
}

func WithGogoConfig(cfg *gogo.Config) Option {
	return func(o *options) { o.gogoConfig = cfg }
}

func WithSprayConfig(cfg *spray.Config) Option {
	return func(o *options) { o.sprayConfig = cfg }
}

func WithZombieConfig(cfg *zombie.Config) Option {
	return func(o *options) { o.zombieConfig = cfg }
}

type Client struct {
	opts options
	mu   sync.Mutex

	fingers *fingers.Engine
	neutron *neutron.Engine
	gogo    *gogo.GogoEngine
	spray   *spray.SprayEngine
	zombie  *zombie.Engine
	index   *association.Index
}

func New(opts ...Option) *Client {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return &Client{opts: o}
}

// ========================================
// 依赖解析
// ========================================

func (c *Client) ensureFingers() error {
	if c.fingers != nil {
		return nil
	}

	cfg := c.opts.fingersConfig
	if cfg == nil {
		cfg = fingers.NewConfig()
		if len(c.opts.providers) > 0 {
			cfg.WithProvider(c.opts.providers...)
		}
	}
	if len(cfg.Proxy) == 0 && len(c.opts.proxy) > 0 {
		cfg.Proxy = c.opts.proxy
	}

	eng, err := fingers.NewEngine(cfg)
	if err != nil {
		return fmt.Errorf("create fingers engine: %w", err)
	}
	c.fingers = eng
	return nil
}

func (c *Client) ensureNeutron() error {
	if c.neutron != nil {
		return nil
	}

	cfg := c.opts.neutronConfig
	if cfg == nil {
		cfg = neutron.NewConfig()
		if len(c.opts.providers) > 0 {
			cfg.WithProvider(c.opts.providers...)
		}
	}
	if len(cfg.Proxy) == 0 && len(c.opts.proxy) > 0 {
		cfg.Proxy = c.opts.proxy
	}

	eng, err := neutron.NewEngine(cfg)
	if err != nil {
		return fmt.Errorf("create neutron engine: %w", err)
	}
	c.neutron = eng
	return nil
}

func (c *Client) ensureIndex() error {
	if c.index != nil {
		return nil
	}
	if c.opts.indexOptions == nil {
		return nil
	}

	if err := c.ensureFingers(); err != nil {
		return fmt.Errorf("index requires fingers: %w", err)
	}
	if err := c.ensureNeutron(); err != nil {
		return fmt.Errorf("index requires neutron: %w", err)
	}

	idx := association.NewIndexWithOptions(*c.opts.indexOptions)
	idx.BuildWithFingers(c.fingers.Fingers(), c.fingers.Aliases(), c.neutron.Get())
	c.index = idx
	return nil
}

func (c *Client) ensureGogo() error {
	if c.gogo != nil {
		return nil
	}

	if err := c.ensureFingers(); err != nil {
		return fmt.Errorf("gogo requires fingers: %w", err)
	}
	if err := c.ensureNeutron(); err != nil {
		return fmt.Errorf("gogo requires neutron: %w", err)
	}

	cfg := c.opts.gogoConfig
	if cfg == nil {
		cfg = gogo.NewConfig()
	}
	if cfg.FingersEngine == nil {
		cfg.WithFingersEngine(c.fingers)
	}
	if cfg.NeutronEngine == nil {
		cfg.WithNeutronEngine(c.neutron)
	}
	if len(cfg.Providers) == 0 && len(c.opts.providers) > 0 {
		cfg.WithProvider(c.opts.providers...)
	}
	if cfg.ResourceProvider == nil && c.opts.resourceProvider != nil {
		cfg.WithResourceProvider(c.opts.resourceProvider)
	}
	if len(cfg.Proxy) == 0 && len(c.opts.proxy) > 0 {
		cfg.Proxy = c.opts.proxy
	}

	eng := gogo.NewGogoEngine(cfg)
	if err := eng.Init(); err != nil {
		return fmt.Errorf("init gogo engine: %w", err)
	}
	c.gogo = eng
	return nil
}

func (c *Client) ensureSpray() error {
	if c.spray != nil {
		return nil
	}

	if err := c.ensureFingers(); err != nil {
		return fmt.Errorf("spray requires fingers: %w", err)
	}

	cfg := c.opts.sprayConfig
	if cfg == nil {
		cfg = spray.NewConfig()
	}
	if len(cfg.Providers) == 0 && len(c.opts.providers) > 0 {
		cfg.WithProvider(c.opts.providers...)
	}
	if cfg.FingersEngine == nil {
		cfg.WithFingersEngine(c.fingers)
	}
	if cfg.ResourceProvider == nil && c.opts.resourceProvider != nil {
		cfg.WithResourceProvider(c.opts.resourceProvider)
	}
	if len(cfg.Proxy) == 0 && len(c.opts.proxy) > 0 {
		cfg.Proxy = c.opts.proxy
	}

	eng := spray.NewSprayEngine(cfg)
	if err := eng.Init(); err != nil {
		return fmt.Errorf("init spray engine: %w", err)
	}
	c.spray = eng
	return nil
}

func (c *Client) ensureZombie() error {
	if c.zombie != nil {
		return nil
	}

	cfg := c.opts.zombieConfig
	if cfg == nil {
		cfg = zombie.NewConfig()
	}
	if cfg.ResourceProvider == nil && c.opts.resourceProvider != nil {
		cfg.WithResourceProvider(c.opts.resourceProvider)
	}
	if len(cfg.Proxy) == 0 && len(c.opts.proxy) > 0 {
		cfg.Proxy = c.opts.proxy
	}

	eng := zombie.NewEngine(cfg)
	if err := eng.Init(); err != nil {
		return fmt.Errorf("init zombie engine: %w", err)
	}
	c.zombie = eng
	return nil
}

// ========================================
// 引擎访问
// ========================================

func (c *Client) Fingers() (*fingers.Engine, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureFingers(); err != nil {
		return nil, err
	}
	return c.fingers, nil
}

func (c *Client) Gogo() (*gogo.GogoEngine, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureGogo(); err != nil {
		return nil, err
	}
	return c.gogo, nil
}

func (c *Client) Spray() (*spray.SprayEngine, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureSpray(); err != nil {
		return nil, err
	}
	return c.spray, nil
}

func (c *Client) Neutron() (*neutron.Engine, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureNeutron(); err != nil {
		return nil, err
	}
	return c.neutron, nil
}

func (c *Client) Zombie() (*zombie.Engine, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureZombie(); err != nil {
		return nil, err
	}
	return c.zombie, nil
}

// ========================================
// 关联索引
// ========================================

// Index returns the association index, initializing it if WithIndex
// was set. Returns nil if the index is not enabled.
func (c *Client) Index() (*association.Index, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureIndex(); err != nil {
		return nil, err
	}
	return c.index, nil
}

// Lookup queries the association index with the given query.
// Shorthand for c.Index() + idx.Lookup(q).
func (c *Client) Lookup(q *association.Query) (*association.QueryResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ensureIndex(); err != nil {
		return nil, err
	}
	if c.index == nil {
		return nil, fmt.Errorf("index not enabled: use WithIndex() when creating client")
	}
	return c.index.Lookup(q), nil
}

// LookupResult extracts association terms from an engine result and
// queries the index. Accepts GOGOResult, SprayResult, or ZombieResult
// wrapped in a types.Result.
func (c *Client) LookupResult(r types.Result) (*association.QueryResult, error) {
	return c.Lookup(association.QueryFromResult(r))
}

// LookupByFinger queries the index by fingerprint names.
func (c *Client) LookupByFinger(names ...string) (*association.QueryResult, error) {
	return c.Lookup(association.NewQuery().WithFingers(names...))
}

// LookupByCVE queries the index by CVE IDs.
func (c *Client) LookupByCVE(cves ...string) (*association.QueryResult, error) {
	return c.Lookup(association.NewQuery().WithCVEs(cves...))
}

// BuildIndex constructs a standalone association index from the
// Provider, independent of the client's index lifecycle. Useful when
// you need an index with different options than the client's.
func (c *Client) BuildIndex(ctx context.Context, opts ...association.IndexOption) (*association.Index, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureFingers(); err != nil {
		return nil, err
	}
	if err := c.ensureNeutron(); err != nil {
		return nil, err
	}

	idx := association.NewIndex(opts...)
	idx.BuildWithFingers(c.fingers.Fingers(), c.fingers.Aliases(), c.neutron.Get())
	return idx, nil
}

// ========================================
// 生命周期
// ========================================

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var errs []error
	if c.gogo != nil {
		errs = append(errs, c.gogo.Close())
	}
	if c.spray != nil {
		errs = append(errs, c.spray.Close())
	}
	if c.neutron != nil {
		errs = append(errs, c.neutron.Close())
	}
	if c.fingers != nil {
		errs = append(errs, c.fingers.Close())
	}
	if c.zombie != nil {
		errs = append(errs, c.zombie.Close())
	}

	c.gogo = nil
	c.spray = nil
	c.neutron = nil
	c.fingers = nil
	c.zombie = nil
	c.index = nil

	return errors.Join(errs...)
}

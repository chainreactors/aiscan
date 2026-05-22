package ani

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var (
	ErrUnknownSource   = errors.New("ani: unknown source")
	ErrCompanyNotFound = errors.New("ani: company not found")
)

// SourceClient 由各数据源 (aqc_unauth / tyc_unauth / ...) 实现。
type SourceClient interface {
	Name() string
	// Run 根据公司名做查询, depth 控制投资链路递归层数, percent 是入选子公司
	// 的最小持股比例 (0.0 - 1.0)。返回扁平的 CompanyAsset 列表 (每个 ICP 一条)。
	Run(ctx context.Context, name string, depth int, percent float64) ([]CompanyAsset, error)
}

// SourceFactory 由 source 子包通过 RegisterSource 注入 (Phase 1 没必要门槛, 但
// 保持与 ina-go 同构)。
type SourceFactory func(cfg *Config) SourceClient

var registeredSources = map[string]SourceFactory{}

func RegisterSource(name string, factory SourceFactory) {
	registeredSources[name] = factory
}

// Config 通用配置。链式 With* 风格, 与 ina-go 对齐。
type Config struct {
	depth     int
	percent   float64
	logger    Logger
	http      *http.Client
	proxy     string
	tycToken  string // 天眼查 auth_token JWT (用于 tyc 源)
	qccCookie string // 企查查 QCCSESSID (用于 qcc 源)
	aqcCookie string // 爱企查 BAIDUID (用于 aqc auth 源)
}

func NewConfig() *Config {
	return &Config{
		depth:   1,
		percent: 0.5,
		logger:  NopLogger(),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Config) WithDepth(d int) *Config {
	if d >= 0 {
		c.depth = d
	}
	return c
}

func (c *Config) WithPercent(p float64) *Config {
	if p >= 0 && p <= 1 {
		c.percent = p
	}
	return c
}

func (c *Config) WithLogger(l Logger) *Config {
	if l != nil {
		c.logger = l
	}
	return c
}

func (c *Config) WithHTTPClient(h *http.Client) *Config {
	if h != nil {
		c.http = h
	}
	return c
}

func (c *Config) WithProxy(p string) *Config {
	c.proxy = p
	return c
}

// WithTycToken 设置天眼查 auth_token JWT (用于 tyc 源, www.tianyancha.com HTML 爬取)。
func (c *Config) WithTycToken(token string) *Config {
	c.tycToken = token
	return c
}

// WithQccCookie 设置企查查 QCCSESSID cookie (用于 qcc 源)。
func (c *Config) WithQccCookie(cookie string) *Config {
	c.qccCookie = cookie
	return c
}

// WithAqcCookie 设置爱企查 BAIDUID cookie (用于 aqc auth 源)。
func (c *Config) WithAqcCookie(cookie string) *Config {
	c.aqcCookie = cookie
	return c
}

// 公开访问器 (供子包读取)。
func (c *Config) Depth() int         { return c.depth }
func (c *Config) Percent() float64   { return c.percent }
func (c *Config) Logger() Logger     { return c.logger }
func (c *Config) HTTP() *http.Client { return c.http }
func (c *Config) Proxy() string      { return c.proxy }
func (c *Config) TycToken() string   { return c.tycToken }
func (c *Config) QccCookie() string  { return c.qccCookie }
func (c *Config) AqcCookie() string  { return c.aqcCookie }

// Engine 入口。每个 source 实例化一个 client, 共享配置。
type Engine struct {
	cfg     *Config
	clients map[string]SourceClient
}

func NewEngine(cfg *Config) *Engine {
	if cfg == nil {
		cfg = NewConfig()
	}
	e := &Engine{cfg: cfg, clients: map[string]SourceClient{}}
	for name, factory := range registeredSources {
		client := factory(cfg)
		if client == nil {
			continue
		}
		e.clients[name] = client
	}
	return e
}

func (e *Engine) Sources() []string {
	out := make([]string, 0, len(e.clients))
	for name := range e.clients {
		out = append(out, name)
	}
	return out
}

// Query 按 source 跑一次公司测绘, 返回扁平 CompanyAsset 列表。
// depth / percent 来自 Config; 用 WithDepth/WithPercent 改。
func (e *Engine) Query(ctx context.Context, source, name string) ([]CompanyAsset, error) {
	c, ok := e.clients[source]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownSource, source)
	}
	return c.Run(ctx, name, e.cfg.depth, e.cfg.percent)
}

func (e *Engine) Close() error {
	if t, ok := e.cfg.http.Transport.(*http.Transport); ok && t != nil {
		t.CloseIdleConnections()
	}
	return nil
}

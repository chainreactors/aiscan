package ina

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrUnknownSource 表示请求了未注册的 source。
var ErrUnknownSource = errors.New("ina: unknown source")

// ErrNoCredentials 表示 source 已注册但凭证缺失或 login 失败。
var ErrNoCredentials = errors.New("ina: source has no valid credentials")

// SourceClient 是各 source (fofa/zoomeye/hunter) 实现的接口。
// 外部用户不直接调用; 仅 source 子包用来 implement, Engine 门面用来 dispatch。
type SourceClient interface {
	Name() string
	CheckLogin(ctx context.Context) error
	Query(ctx context.Context, raw string) ([]Asset, error)
}

// FofaCreds 是 fofa 的认证信息。Email 与 Key 任一为空都被视为未配置。
type FofaCreds struct {
	Email string
	Key   string
}

// HunterCreds 是 hunter 的认证信息。Token 与 APIKey 至少配一个 (Token 优先,
// 对应抓包出来的登录 token; APIKey 对应华顺信安后台的 API 管理生成的 key)。
type HunterCreds struct {
	Token  string
	APIKey string
}

// Config 是 Engine 的构造参数。链式 With* API 与 chainreactors/sdk 风格一致。
type Config struct {
	fofa   FofaCreds
	hunter HunterCreds
	limit  int
	logger Logger
	http   *http.Client
	proxy  string
}

func NewConfig() *Config {
	return &Config{
		logger: NopLogger(),
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Config) WithFofa(email, key string) *Config {
	c.fofa = FofaCreds{Email: email, Key: key}
	return c
}

func (c *Config) WithHunterToken(token string) *Config {
	c.hunter.Token = token
	return c
}

func (c *Config) WithHunterAPIKey(apiKey string) *Config {
	c.hunter.APIKey = apiKey
	return c
}

func (c *Config) WithLimit(n int) *Config {
	if n >= 0 {
		c.limit = n
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

// WithProxy 通过 URL 配置出站代理, 支持 http://, https://, socks5://, socks5h://。
// 留空表示不用代理。立即生效, 覆盖 WithHTTPClient 之前设置的 client transport。
// 注意: socks5h 强制由代理端解析 DNS, 翻墙场景一般用这个。
func (c *Config) WithProxy(proxyURL string) *Config {
	c.proxy = proxyURL
	if proxyURL == "" {
		return c
	}
	if h, err := buildHTTPClientWithProxy(proxyURL, c.http.Timeout); err == nil {
		c.http = h
	} else if c.logger != nil {
		c.logger.Warnf("ina: WithProxy %q failed: %v (using direct connection)", proxyURL, err)
	}
	return c
}

// EngineStatus 是 Engine.Status 返回的逐源状态。
type EngineStatus struct {
	OK     bool
	Reason string
}

// Engine 是 ina-go 的入口。线程安全 (内部 client 自行串行)。
type Engine struct {
	cfg     *Config
	clients map[string]SourceClient
}

// SourceFactory 由 source 子包在 init() 里通过 RegisterSource 注入。
// 返回 nil 表示凭证缺失, 不会被注册到 Engine。
type SourceFactory func(cfg *Config) SourceClient

var registeredSources = map[string]SourceFactory{}

// RegisterSource 由 source 子包在 init() 里调用, 把自己注册到门面。
// 顶层 ina 包不 import 子包, 避免循环依赖。
func RegisterSource(name string, factory SourceFactory) {
	registeredSources[name] = factory
}

// Cfg 暴露 Config 内部字段给 source 子包读取 (仅同包内有效)。
// fofa/zoomeye/hunter 子包通过本包重新导出的访问器读取配置。
func (c *Config) Fofa() FofaCreds     { return c.fofa }
func (c *Config) Hunter() HunterCreds { return c.hunter }
func (c *Config) Limit() int          { return c.limit }
func (c *Config) Logger() Logger      { return c.logger }
func (c *Config) HTTP() *http.Client  { return c.http }
func (c *Config) Proxy() string       { return c.proxy }

// NewEngine 构造 Engine。不发起网络调用; CheckLogin 推迟到 Status() / Query()。
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

// Sources 返回当前已注册且凭证非空的 source 名列表 (顺序不固定)。
func (e *Engine) Sources() []string {
	out := make([]string, 0, len(e.clients))
	for name := range e.clients {
		out = append(out, name)
	}
	return out
}

// Status 对每个 source 调用 CheckLogin, 返回逐源状态。耗时操作, 调用方自行决定时机。
func (e *Engine) Status(ctx context.Context) map[string]EngineStatus {
	out := make(map[string]EngineStatus, len(e.clients))
	for name, c := range e.clients {
		if err := c.CheckLogin(ctx); err != nil {
			out[name] = EngineStatus{OK: false, Reason: err.Error()}
			e.cfg.logger.Warnf("source=%s status=fail reason=%q", name, err.Error())
			continue
		}
		out[name] = EngineStatus{OK: true}
		e.cfg.logger.Infof("source=%s status=ok", name)
	}
	return out
}

// Query 用语义化 Code 查询指定 source。
func (e *Engine) Query(ctx context.Context, source string, code *Code) ([]Asset, error) {
	raw := code.String(source)
	if raw == "" {
		return nil, fmt.Errorf("ina: empty query for source %q", source)
	}
	return e.QueryRaw(ctx, source, raw)
}

// QueryRaw 透传 source 原生语法查询字符串。
func (e *Engine) QueryRaw(ctx context.Context, source string, raw string) ([]Asset, error) {
	c, ok := e.clients[source]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownSource, source)
	}
	return c.Query(ctx, raw)
}

// Close 释放各 source client 持有的资源 (HTTP keepalive 等)。
func (e *Engine) Close() error {
	if t, ok := e.cfg.http.Transport.(*http.Transport); ok && t != nil {
		t.CloseIdleConnections()
	}
	return nil
}

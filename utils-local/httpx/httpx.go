// Package httpx provides injection-based, zero-global HTTP client / transport
// and raw socket constructors shared across the chainreactors stack.
//
// Design invariants:
//   - 绝不引用或改写任何包级/全局 transport；每次调用都返回全新实例，
//     因此天然并发安全（多个调用方使用不同代理互不干扰）。
//   - 不依赖 proxyclient（保持 go1.10 兼容）。代理由调用方以 DialContext /
//     DialTimeout 注入；nil 时回退到标准库直连。
package httpx

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// DialContextFunc 与 http.Transport.DialContext 兼容，也与 proxyclient.Dial 兼容。
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// DialTimeoutFunc 用于 socket 风格的拨号注入（network, address, timeout）。
type DialTimeoutFunc func(network, address string, timeout time.Duration) (net.Conn, error)

// ClientConfig 描述一个 HTTP 客户端的构造参数。零值给出合理默认。
type ClientConfig struct {
	Timeout         time.Duration
	FollowRedirects bool
	MaxRedirects    int
	// InsecureSkipVerify 默认即为安全扫描场景的 true；如需校验证书请显式置 false
	// 并通过 TLSConfig 提供配置。
	InsecureSkipVerify bool
	TLSConfig          *tls.Config
	// DialContext 非 nil 时作为 Transport.DialContext（用于代理）。nil 则默认直连。
	DialContext         DialContextFunc
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	DisableKeepAlives   bool
}

const defaultMaxRedirects = 5

// DefaultConfig 返回一组通用默认参数（10s 超时、跳过证书校验、合理连接池）。
// 它是一个普通构造函数——每次返回新的值，不持有任何全局状态。
func DefaultConfig() ClientConfig {
	return ClientConfig{
		Timeout:             10 * time.Second,
		FollowRedirects:     false,
		InsecureSkipVerify:  true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
}

// DefaultClient 用默认参数构造一个【全新的】*http.Client。
// 注意：这不是全局单例——每次调用都返回独立实例，避免共享/相互覆盖。
func DefaultClient() *http.Client {
	return NewHTTPClient(DefaultConfig())
}

// DefaultClientWithDialer 同 DefaultClient，但注入自定义 DialContext（可为代理）。
func DefaultClientWithDialer(dialContext DialContextFunc) *http.Client {
	cfg := DefaultConfig()
	cfg.DialContext = dialContext
	return NewHTTPClient(cfg)
}

// NewTransport 依据 cfg 构造一个全新的 *http.Transport。
// 它不读取、不修改任何包级状态。
func NewTransport(cfg ClientConfig) *http.Transport {
	tlsConfig := cfg.TLSConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify}
	}
	return newTransport(cfg, tlsConfig)
}

// NewHTTPClient 依据 cfg 构造一个全新的 *http.Client（含全新 transport）。
func NewHTTPClient(cfg ClientConfig) *http.Client {
	return &http.Client{
		Transport:     NewTransport(cfg),
		Timeout:       cfg.Timeout,
		CheckRedirect: redirectFunc(cfg.FollowRedirects, cfg.MaxRedirects),
	}
}

// NewHTTPClientWithTransport 用调用方提供的 transport 构造 client（不接管/克隆）。
// 当调用方已有定制 transport 时使用。
func NewHTTPClientWithTransport(tr http.RoundTripper, timeout time.Duration, followRedirects bool, maxRedirects int) *http.Client {
	return &http.Client{
		Transport:     tr,
		Timeout:       timeout,
		CheckRedirect: redirectFunc(followRedirects, maxRedirects),
	}
}

func redirectFunc(follow bool, max int) func(req *http.Request, via []*http.Request) error {
	if !follow {
		return func(req *http.Request, via []*http.Request) error {
			return errUseLastResponse()
		}
	}
	if max <= 0 {
		max = defaultMaxRedirects
	}
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= max {
			return errUseLastResponse()
		}
		return nil
	}
}

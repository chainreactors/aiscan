// Package httpx is the SDK-layer bridge that turns proxyclient proxy specs
// (`[]string`) into concurrency-safe *http.Client instances built on the
// shared, zero-global utils/httpx foundation.
//
// proxyclient 的依赖只停留在本层（SDK，go1.24）；utils/httpx 保持 go1.10、
// 不感知 proxyclient。
package httpx

import (
	"net/http"
	"time"

	utilshttpx "github.com/chainreactors/utils/httpx"

	"github.com/chainreactors/sdk/pkg/types"
)

// Config 描述一个 SDK HTTP 客户端的构造参数。
type Config struct {
	Timeout             time.Duration
	Proxy               []string // 经 types.NewProxyDialer 解析，支持多级链/全协议
	FollowRedirects     bool
	InsecureSkipVerify  bool
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	DisableKeepAlives   bool
}

// DefaultConfig 返回一组通用默认参数（无代理）。每次返回新值，无全局状态。
func DefaultConfig() Config {
	return Config{
		Timeout:             10 * time.Second,
		FollowRedirects:     false,
		InsecureSkipVerify:  true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
}

// DefaultClient 用默认参数构造一个【全新的】*http.Client（无代理）。
// 非全局单例——每次返回独立实例。需要代理时用 NewClient(Config{Proxy: ...})。
func DefaultClient() *http.Client {
	c, _ := NewClient(DefaultConfig())
	return c
}

// NewClient 构造一个 *http.Client：若 Proxy 非空则注入 proxyclient 拨号器，
// 底层委托 utils/httpx（每次全新实例，零全局，并发安全）。
func NewClient(cfg Config) (*http.Client, error) {
	uc := utilshttpx.ClientConfig{
		Timeout:             cfg.Timeout,
		FollowRedirects:     cfg.FollowRedirects,
		InsecureSkipVerify:  cfg.InsecureSkipVerify,
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		DisableKeepAlives:   cfg.DisableKeepAlives,
	}
	if len(cfg.Proxy) > 0 {
		dialer, err := types.NewProxyDialer(cfg.Proxy)
		if err != nil {
			return nil, err
		}
		if dialer != nil {
			uc.DialContext = dialer.DialContext
		}
	}
	return utilshttpx.NewHTTPClient(uc), nil
}

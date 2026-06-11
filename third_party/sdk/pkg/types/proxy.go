package types

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/chainreactors/proxyclient"
)

// ProxyDialer 持有由 proxy URL 字符串构建出的各种拨号函数，供不同引擎按需取用。
type ProxyDialer struct {
	// Dial 是 proxyclient 的核心拨号器（也可直接赋给 spray 等需要 proxyclient.Dial 的字段）。
	Dial proxyclient.Dial
	// DialContext 形如 func(ctx, network, address) (net.Conn, error)，
	// 兼容 gogo RunnerOption.ProxyDialContext 与 zombie pkg.DialFunc。
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
	// DialTimeout 形如 func(network, address, timeout) (net.Conn, error)，
	// 兼容 gogo RunnerOption.ProxyDialTimeout。
	DialTimeout func(network, address string, timeout time.Duration) (net.Conn, error)
}

// NewProxyDialer 解析 proxy URL 列表并构建链式拨号器。proxies 为空时返回 (nil, nil)。
// 支持 proxyclient 的全部协议（socks5/http/trojan/vless/hysteria2/clash 订阅等）以及多级代理链。
func NewProxyDialer(proxies []string) (*ProxyDialer, error) {
	if len(proxies) == 0 {
		return nil, nil
	}
	urls, err := proxyclient.ParseProxyURLs(proxies)
	if err != nil {
		return nil, fmt.Errorf("parse proxy urls: %w", err)
	}
	dial, err := proxyclient.NewClientChain(urls)
	if err != nil {
		return nil, fmt.Errorf("create proxy chain: %w", err)
	}
	return &ProxyDialer{
		Dial:        dial,
		DialContext: dial.DialContext,
		DialTimeout: func(network, address string, timeout time.Duration) (net.Conn, error) {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			return dial(ctx, network, address)
		},
	}, nil
}

// ResolveProxy 按优先级返回第一个非空的 proxy 列表。
// 调用约定：ResolveProxy(ctxProxy, configProxy, clientProxy) —— Context > Config > Client。
func ResolveProxy(candidates ...[]string) []string {
	for _, c := range candidates {
		if len(c) > 0 {
			return c
		}
	}
	return nil
}

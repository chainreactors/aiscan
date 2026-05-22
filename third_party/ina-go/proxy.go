package ina

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// buildHTTPClientWithProxy 把 proxyURL 拆出 scheme, 构造对应 http.Client。
// http(s):// 走 http.Transport.Proxy, socks5(h):// 走 dialer.Dial。
func buildHTTPClientWithProxy(proxyURL string, timeout time.Duration) (*http.Client, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("parse proxy url: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
		return &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy: http.ProxyURL(u),
			},
		}, nil
	case "socks5", "socks5h":
		dialer, err := proxy.FromURL(u, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("socks5 dialer: %w", err)
		}
		ctxDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return &http.Client{
				Timeout: timeout,
				Transport: &http.Transport{
					Dial: dialer.Dial,
				},
			}, nil
		}
		return &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return ctxDialer.DialContext(ctx, network, addr)
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q (use http/https/socks5/socks5h)", u.Scheme)
	}
}

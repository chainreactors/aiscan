package fingers

import (
	"fmt"
	"io"
	"net/http"
	"time"

	sdkhttpx "github.com/chainreactors/sdk/pkg/httpx"
)

// ========================================
// HTTPSender 接口和实现
// ========================================

// HTTPSender HTTP请求发送器接口
type HTTPSender interface {
	Send(url string) (*http.Response, error)
}

// DefaultHTTPSender 默认的HTTP发送器实现
type DefaultHTTPSender struct {
	client  *http.Client
	timeout time.Duration
	proxy   string
}

// NewDefaultHTTPSender 创建默认的HTTPSender
// 参数:
//   - timeout: 超时时间
//   - proxy: 代理地址（可选，如 "socks5://127.0.0.1:1080"）
func NewDefaultHTTPSender(timeout time.Duration, proxy string) *DefaultHTTPSender {
	if timeout <= 0 {
		timeout = 10 * time.Second // 默认10秒超时
	}

	var proxies []string
	if proxy != "" {
		proxies = []string{proxy}
	}
	// 委托 SDK 统一 httpx 桥接（底层 utils/httpx，零全局、并发安全）。
	// 代理解析失败时回退到无代理客户端，保持原有“尽力而为”语义。
	client, err := sdkhttpx.NewClient(sdkhttpx.Config{
		Timeout:             timeout,
		Proxy:               proxies,
		FollowRedirects:     false,
		InsecureSkipVerify:  true,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	})
	if err != nil {
		client, _ = sdkhttpx.NewClient(sdkhttpx.Config{
			Timeout:             timeout,
			FollowRedirects:     false,
			InsecureSkipVerify:  true,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		})
	}

	return &DefaultHTTPSender{
		timeout: timeout,
		proxy:   proxy,
		client:  client,
	}
}

// Send 发送HTTP请求
func (s *DefaultHTTPSender) Send(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	// 设置默认User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request failed: %w", err)
	}

	return resp, nil
}

// SendWithMethod 使用指定方法发送HTTP请求
func (s *DefaultHTTPSender) SendWithMethod(url, method string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request failed: %w", err)
	}

	return resp, nil
}

// SetTimeout 设置超时时间
func (s *DefaultHTTPSender) SetTimeout(timeout time.Duration) {
	s.timeout = timeout
	s.client.Timeout = timeout
}

// SetClient 设置自定义HTTP客户端
func (s *DefaultHTTPSender) SetClient(client *http.Client) {
	s.client = client
}

package httpx

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// miniSocks5 启动一个最小化 SOCKS5 服务器（无认证、CONNECT、含命中计数），
// 返回监听地址。用于验证连接确实经过指定代理。
func miniSocks5(t *testing.T, hits *int32) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go socks5Handle(c, hits)
		}
	}()
	return ln.Addr().String()
}

func socks5Handle(c net.Conn, hits *int32) {
	defer c.Close()
	buf := make([]byte, 262)
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return
	}
	n := int(buf[1])
	if _, err := io.ReadFull(c, buf[:n]); err != nil {
		return
	}
	c.Write([]byte{0x05, 0x00})
	if _, err := io.ReadFull(c, buf[:4]); err != nil {
		return
	}
	var host string
	switch buf[3] {
	case 0x01:
		if _, err := io.ReadFull(c, buf[:4]); err != nil {
			return
		}
		host = net.IP(append([]byte(nil), buf[:4]...)).String()
	case 0x03:
		if _, err := io.ReadFull(c, buf[:1]); err != nil {
			return
		}
		l := int(buf[0])
		if _, err := io.ReadFull(c, buf[:l]); err != nil {
			return
		}
		host = string(buf[:l])
	case 0x04:
		if _, err := io.ReadFull(c, buf[:16]); err != nil {
			return
		}
		host = net.IP(append([]byte(nil), buf[:16]...)).String()
	default:
		c.Write([]byte{0x05, 0x08, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	if _, err := io.ReadFull(c, buf[:2]); err != nil {
		return
	}
	port := int(buf[0])<<8 | int(buf[1])
	remote, err := net.Dial("tcp", net.JoinHostPort(host, itoa(port)))
	if err != nil {
		c.Write([]byte{0x05, 0x01, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	atomic.AddInt32(hits, 1)
	c.Write([]byte{0x05, 0x00, 0, 1, 0, 0, 0, 0, 0, 0})
	done := make(chan struct{}, 2)
	go func() { io.Copy(remote, c); done <- struct{}{} }()
	go func() { io.Copy(c, remote); done <- struct{}{} }()
	<-done
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [6]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestConcurrentProxyIsolation 验证：并发构造并使用不同代理的客户端互不串扰，
// 且无代理客户端不经过任一代理。这是“底层去全局”的核心回归。
func TestConcurrentProxyIsolation(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer target.Close()

	var hitsA, hitsB int32
	proxyA := miniSocks5(t, &hitsA)
	proxyB := miniSocks5(t, &hitsB)

	// 关闭 keep-alive，使每个请求都新建连接 → 代理命中数与请求数一致，断言确定。
	clientA, err := NewClient(Config{Timeout: 3 * time.Second, Proxy: []string{"socks5://" + proxyA}, DisableKeepAlives: true})
	if err != nil {
		t.Fatal(err)
	}
	clientB, err := NewClient(Config{Timeout: 3 * time.Second, Proxy: []string{"socks5://" + proxyB}, DisableKeepAlives: true})
	if err != nil {
		t.Fatal(err)
	}
	clientDirect, err := NewClient(Config{Timeout: 3 * time.Second, DisableKeepAlives: true})
	if err != nil {
		t.Fatal(err)
	}

	const rounds = 20
	var wg sync.WaitGroup
	do := func(client *http.Client) {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			resp, err := client.Get(target.URL)
			if err == nil {
				resp.Body.Close()
			}
		}
	}
	wg.Add(3)
	go do(clientA)
	go do(clientB)
	go do(clientDirect)
	wg.Wait()

	if got := atomic.LoadInt32(&hitsA); got != rounds {
		t.Errorf("proxyA hits=%d, want %d", got, rounds)
	}
	if got := atomic.LoadInt32(&hitsB); got != rounds {
		t.Errorf("proxyB hits=%d, want %d", got, rounds)
	}
	// 直连客户端不应命中任一代理（总命中数恰为 2*rounds）。
	if total := atomic.LoadInt32(&hitsA) + atomic.LoadInt32(&hitsB); total != 2*rounds {
		t.Errorf("total proxy hits=%d, want %d (direct client must not use any proxy)", total, 2*rounds)
	}
}

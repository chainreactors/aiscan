package types

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolveProxy(t *testing.T) {
	ctxP := []string{"socks5://ctx:1080"}
	cfgP := []string{"socks5://cfg:1080"}
	cliP := []string{"socks5://cli:1080"}

	if got := ResolveProxy(ctxP, cfgP, cliP); got[0] != "socks5://ctx:1080" {
		t.Fatalf("expected ctx proxy first, got %v", got)
	}
	if got := ResolveProxy(nil, cfgP, cliP); got[0] != "socks5://cfg:1080" {
		t.Fatalf("expected cfg proxy when ctx empty, got %v", got)
	}
	if got := ResolveProxy(nil, nil, cliP); got[0] != "socks5://cli:1080" {
		t.Fatalf("expected cli proxy as fallback, got %v", got)
	}
	if got := ResolveProxy(nil, nil, nil); got != nil {
		t.Fatalf("expected nil when all empty, got %v", got)
	}
}

func TestNewProxyDialerEmpty(t *testing.T) {
	d, err := NewProxyDialer(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != nil {
		t.Fatalf("expected nil dialer for empty proxies, got %v", d)
	}
}

// TestNewProxyDialerSocks5 spins up an in-process SOCKS5 server and verifies the
// dialer routes a TCP connection through it. It exercises all three exported
// dial helpers (DialContext / DialTimeout / Dial).
func TestNewProxyDialerSocks5(t *testing.T) {
	// 目标 echo 服务
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				io.Copy(conn, conn)
			}(c)
		}
	}()

	var proxyHits int32
	proxyAddr := startMiniSocks5(t, &proxyHits)

	d, err := NewProxyDialer([]string{"socks5://" + proxyAddr})
	if err != nil {
		t.Fatalf("create dialer: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil dialer")
	}

	conn, err := d.DialContext(context.Background(), "tcp", target.Addr().String())
	if err != nil {
		t.Fatalf("dial through proxy: %v", err)
	}
	defer conn.Close()

	msg := []byte("ping")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(msg))
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("echo mismatch: %q", buf)
	}
	if atomic.LoadInt32(&proxyHits) == 0 {
		t.Fatal("connection did not go through the proxy")
	}
}

// startMiniSocks5 启动一个最小化的 SOCKS5 服务器（无认证、仅 CONNECT、仅 IPv4），
// 用于验证拨号确实经过代理。返回监听地址。
func startMiniSocks5(t *testing.T, hits *int32) string {
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
			go handleSocks5(c, hits)
		}
	}()
	return ln.Addr().String()
}

func handleSocks5(c net.Conn, hits *int32) {
	defer c.Close()
	br := make([]byte, 262)

	// greeting: VER, NMETHODS, METHODS...
	if _, err := io.ReadFull(c, br[:2]); err != nil {
		return
	}
	nmethods := int(br[1])
	if _, err := io.ReadFull(c, br[:nmethods]); err != nil {
		return
	}
	// 回复 no-auth
	c.Write([]byte{0x05, 0x00})

	// request: VER CMD RSV ATYP ...
	if _, err := io.ReadFull(c, br[:4]); err != nil {
		return
	}
	atyp := br[3]
	var host string
	switch atyp {
	case 0x01: // IPv4
		if _, err := io.ReadFull(c, br[:4]); err != nil {
			return
		}
		host = net.IP(append([]byte(nil), br[:4]...)).String()
	case 0x03: // domain
		if _, err := io.ReadFull(c, br[:1]); err != nil {
			return
		}
		n := int(br[0])
		if _, err := io.ReadFull(c, br[:n]); err != nil {
			return
		}
		host = string(br[:n])
	case 0x04: // IPv6
		if _, err := io.ReadFull(c, br[:16]); err != nil {
			return
		}
		host = net.IP(append([]byte(nil), br[:16]...)).String()
	default:
		c.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	if _, err := io.ReadFull(c, br[:2]); err != nil {
		return
	}
	port := int(br[0])<<8 | int(br[1])
	addr := net.JoinHostPort(host, itoa(port))

	remote, err := net.Dial("tcp", addr)
	if err != nil {
		c.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	atomic.AddInt32(hits, 1)
	// success reply
	c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

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

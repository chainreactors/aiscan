package httpx

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewHTTPClientInjectedDialer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	var hits int32
	client := NewHTTPClient(ClientConfig{
		Timeout:            2 * time.Second,
		InsecureSkipVerify: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			atomic.AddInt32(&hits, 1)
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
	})
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("injected dialer not used, hits=%d", hits)
	}
}

func TestNewHTTPClientNoRedirect(t *testing.T) {
	client := NewHTTPClient(ClientConfig{Timeout: time.Second, FollowRedirects: false})
	if client.CheckRedirect == nil {
		t.Fatal("expected CheckRedirect set")
	}
	if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("expected ErrUseLastResponse, got %v", err)
	}
}

func TestNewTransportFreshInstances(t *testing.T) {
	a := NewTransport(ClientConfig{})
	b := NewTransport(ClientConfig{})
	if a == b {
		t.Fatal("expected distinct transport instances")
	}
}

func TestNewSocketInjectedDialer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		io.Copy(io.Discard, c)
		c.Close()
	}()

	var hits int32
	s, err := NewSocket("tcp", ln.Addr().String(), SocketConfig{
		Timeout: 2 * time.Second,
		DialTimeout: func(network, address string, timeout time.Duration) (net.Conn, error) {
			atomic.AddInt32(&hits, 1)
			return net.DialTimeout(network, address, timeout)
		},
	})
	if err != nil {
		t.Fatalf("new socket: %v", err)
	}
	s.Close()
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("injected dialer not used, hits=%d", hits)
	}
}

func TestNewSocketDirect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.Close()
	}()
	s, err := NewSocket("tcp", ln.Addr().String(), SocketConfig{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("new socket direct: %v", err)
	}
	s.Close()
}

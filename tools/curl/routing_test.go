package curl

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestParseConnectTo(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want ConnectToEntry
	}{
		{"example.com:443:backend.test:8443", ConnectToEntry{"example.com", "443", "backend.test", "8443"}},
		{"::127.0.0.1:", ConnectToEntry{"", "", "127.0.0.1", ""}},
		{"example.com:::8443", ConnectToEntry{"example.com", "", "", "8443"}},
		{"[::1]:443:[2001:db8::1]:8443", ConnectToEntry{"::1", "443", "2001:db8::1", "8443"}},
		{":::035", ConnectToEntry{"", "", "", "35"}},
		{":::", ConnectToEntry{}},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			req, err := Parse([]string{"--connect-to=" + tc.raw, "--connect-to", ":::", "http://example.com"})
			if err != nil || len(req.ConnectTo) != 2 || req.ConnectTo[0] != tc.want {
				t.Fatalf("parse = %+v, %v", req, err)
			}
		})
	}
	for _, raw := range []string{"", "host:80:target", "host:80:target:81:82", "host:0:target:80", "host:80:target:65536", "host:-1:target:80", "host:80:target:http", "host:80:target:+80", "[::1:80:host:80", "[no-ip]:80:host:80", "host:80:bad/name:80", "host:80:bad\r\nhost:80"} {
		if _, err := Parse([]string{"--connect-to", raw, "http://example.com"}); err == nil {
			t.Errorf("accepted invalid mapping %q", raw)
		}
	}
	if _, err := Parse([]string{"http://example.com", "--connect-to"}); err == nil {
		t.Error("accepted missing mapping")
	}
}

func TestConnectionMappingsPreserveTLSIdentityAndVerification(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s|%s", r.Host, r.TLS.ServerName)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		host string
	}{
		{"resolve", []string{"--resolve", "example.com:" + u.Port() + ":127.0.0.1", "https://example.com:" + u.Port()}, "example.com:" + u.Port()},
		{"connect", []string{"--connect-to", "example.com:443:" + u.Host, "https://example.com"}, "example.com"},
		{"combined", []string{"--connect-to", "example.com:443:backend.test:" + u.Port(), "--resolve", "backend.test:" + u.Port() + ":127.0.0.1", "https://example.com"}, "example.com"},
		{"first-match", []string{"--connect-to", ":::" + u.Port(), "--connect-to", "::bad.invalid:1", "--resolve", "example.com:" + u.Port() + ":127.0.0.1", "https://example.com"}, "example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := Parse(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			err = New().do(t.Context(), req, coretool.Egress{CAPath: ca}, "", &out, io.Discard)
			if err != nil || out.String() != tc.host+"|example.com" {
				t.Fatalf("body=%q error=%v", out.String(), err)
			}
		})
	}
	req, _ := Parse([]string{"--connect-to", "::" + u.Host, "https://wrong.invalid"})
	err := New().do(t.Context(), req, coretool.Egress{CAPath: ca}, "", io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("wrong TLS name accepted: %v", err)
	}
}

func TestProxyMappingDenialCannotFallBackToDirect(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer target.Close()
	u, _ := url.Parse(target.URL)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != u.Host {
			t.Errorf("proxy got %s %s", r.Method, r.Host)
		}
		// Proxy URL userinfo must be decoded before building Basic credentials.
		if r.Header.Get("Proxy-Authorization") != "Basic Y2FsbDppZDpwQHNzL3dvcmQ=" {
			t.Error("proxy credentials changed")
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxyServer.Close()
	p, _ := url.Parse(proxyServer.URL)
	p.User = url.UserPassword("call:id", "p@ss/word")
	for _, mapping := range [][]string{
		{"--resolve", "route.test:" + u.Port() + ":127.0.0.1"},
		{"--connect-to", "::" + u.Host},
	} {
		args := append(append([]string{}, mapping...), "-x", p.String(), "http://route.test:"+u.Port())
		_, _, err := run(t, args, "", "")
		if err == nil || !strings.Contains(err.Error(), "HTTP 502") || strings.Contains(err.Error(), "word") {
			t.Fatalf("denial = %v", err)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("bypassed the proxy")
	}
}

func TestMappingProxyHandshakeTimeoutAndCancellation(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelEarly), func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release }))
			defer srv.Close()
			defer close(release)
			u, _ := url.Parse(srv.URL)
			dial, err := mappedDialer(&Request{}, u, &tls.Config{}, 150*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelEarly {
				go func() { <-entered; cancel() }()
			}
			start := time.Now()
			conn, err := dial(ctx, "tcp", "example.com:80")
			if conn != nil {
				conn.Close()
				t.Error("received tunnel after failure")
			}
			if err == nil || time.Since(start) > time.Second {
				t.Fatalf("timeout/cancel failed: %v", err)
			}
			if !cancelEarly {
				if nerr, ok := err.(net.Error); !ok || !nerr.Timeout() {
					t.Fatalf("not timeout: %v", err)
				}
			}
		})
	}
}

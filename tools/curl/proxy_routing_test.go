package curl

import (
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
	"testing"

	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestConnectionMappingsThroughHTTPAndHTTPSProxies(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credential reached target")
		}
		fmt.Fprint(w, r.Host)
	}))
	defer target.Close()
	u, _ := url.Parse(target.URL)
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprint(secure), func(t *testing.T) {
			proxyServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodConnect || r.Host != u.Host {
					t.Errorf("proxy got %s %s", r.Method, r.Host)
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				upstream, err := net.Dial("tcp", r.Host)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				defer upstream.Close()
				conn, buffer, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
				_ = buffer.Flush()
				done := make(chan struct{})
				go func() { _, _ = io.Copy(upstream, buffer); _ = upstream.Close(); close(done) }()
				_, _ = io.Copy(conn, upstream)
				_ = conn.Close()
				<-done
			}))
			if secure {
				proxyServer.StartTLS()
			} else {
				proxyServer.Start()
			}
			defer proxyServer.Close()
			egress := coretool.Egress{ProxyURL: proxyServer.URL}
			if secure {
				egress.CAPath = filepath.Join(t.TempDir(), "proxy-ca.pem")
				if err := os.WriteFile(egress.CAPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: proxyServer.Certificate().Raw}), 0600); err != nil {
					t.Fatal(err)
				}
			}
			req, err := Parse([]string{"--connect-to", "::" + u.Host, "http://origin.test"})
			if err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			if err := New().do(t.Context(), req, egress, "", &out, io.Discard); err != nil || out.String() != "origin.test" {
				t.Fatalf("proxy output=%q error=%v", out.String(), err)
			}
		})
	}
}

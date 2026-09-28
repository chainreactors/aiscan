package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	curltool "github.com/chainreactors/cyber/tools/curl"
)

func TestCurlConnectionMappingsThroughHub(t *testing.T) {
	for _, capture := range []bool{false, true} {
		for _, secure := range []bool{false, true} {
			scheme, port := "http", "80"
			if secure {
				scheme, port = "https", "443"
			}
			for _, mapping := range []string{"resolve", "connect-to", "combined", "redirect"} {
				t.Run(fmt.Sprintf("capture=%t/%s/%s", capture, scheme, mapping), func(t *testing.T) {
					target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Header.Get("Proxy-Authorization") != "" {
							t.Error("proxy credentials reached the destination")
						}
						if r.URL.Path == "/redirect" {
							http.Redirect(w, r, scheme+"://redirect.test/final", http.StatusFound)
							return
						}
						sni := ""
						if r.TLS != nil {
							sni = r.TLS.ServerName
						}
						fmt.Fprintf(w, "%s|%s|%s", r.Host, sni, r.URL.Path)
					}))
					if secure {
						target.StartTLS()
					} else {
						target.Start()
					}
					defer target.Close()
					u, err := url.Parse(target.URL)
					if err != nil {
						t.Fatal(err)
					}
					origin, path, exchanges := "origin.test", "/final", 1
					args := []string{"--connect-to", "origin.test:" + port + ":" + u.Host}
					switch mapping {
					case "resolve":
						origin += ":" + u.Port()
						args = []string{"--resolve", origin + ":127.0.0.1"}
					case "combined":
						args = []string{"--connect-to", "origin.test:" + port + ":backend.test:" + u.Port(), "--resolve", "backend.test:" + u.Port() + ":127.0.0.1"}
					case "redirect":
						path, exchanges = "/redirect", 2
						args = append(args, "-L", "--connect-to", "redirect.test:"+port+":"+u.Host)
					}
					args = append(args, "-k", scheme+"://"+origin+path)
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					ctx = operation.ContextWithInvocation(ctx, operation.Invocation{CallID: "mapped-call"})
					ctx, finish := operation.Begin(ctx, "test", "curl")
					defer finish(nil)
					hub := startHub(t, capture)
					route, ca, release := hub.Egress(ctx)
					defer release()
					var output strings.Builder
					_, err = curltool.New().Run(ctx, &coretool.Execution{
						Args: args, Env: coretool.EgressEnvironment(route, ca), Stdout: &output, Stderr: io.Discard,
					})
					if mapping == "redirect" {
						origin = "redirect.test"
					}
					sni := ""
					if secure {
						sni = strings.Split(origin, ":")[0]
					}
					want := origin + "|" + sni + "|/final"
					if err != nil || output.String() != want {
						t.Fatalf("mapped request: body=%q want=%q error=%v", output.String(), want, err)
					}
					if !capture {
						return
					}
					flows := waitForFlows(t, hub.store, exchanges)
					if len(flows) != exchanges {
						t.Fatalf("captured %d exchanges, want %d", len(flows), exchanges)
					}
					for _, flow := range flows {
						if flow.Operation.GetCallId() != "mapped-call" || flow.Request == nil ||
							!strings.Contains(flow.Request.Url, ".test") || flow.Response == nil || flow.Response.StatusCode == 0 {
							t.Fatalf("mapping lost the original URL, response or invocation attribution: %+v", flow)
						}
					}
				})
			}
		}
	}
}

package search

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/resources"
	"github.com/chainreactors/fingers/common"
	fingerslib "github.com/chainreactors/fingers/fingers"
	"github.com/chainreactors/neutron/templates"
	sdkfingers "github.com/chainreactors/sdk/fingers"
	sdkneutron "github.com/chainreactors/sdk/neutron"
)

func TestCyberhubSearchesFingerprints(t *testing.T) {
	cmd := newTestSearchCommand()

	var buf strings.Builder
	err := cmd.Execute(context.Background(), []string{"cyberhub", "search", "finger", "nginx"}, &buf)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[cyberhub] finger nginx http focus active 1") {
		t.Fatalf("output missing nginx fingerprint: %q", out)
	}
	if strings.Contains(out, "spring-rce") {
		t.Fatalf("finger search included poc: %q", out)
	}
	if !strings.Contains(out, "[cyberhub] search finger 1 1") {
		t.Fatalf("output missing summary: %q", out)
	}
}

func TestCyberhubListsPOCsWithFilters(t *testing.T) {
	cmd := newTestSearchCommand()

	var buf strings.Builder
	err := cmd.Execute(context.Background(), []string{"cyberhub", "list", "poc", "--severity", "critical,high", "--finger", "spring", "--limit", "0"}, &buf)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "spring-rce critical spring") {
		t.Fatalf("output missing spring poc: %q", out)
	}
	if strings.Contains(out, "tomcat-leak") {
		t.Fatalf("poc filter included tomcat: %q", out)
	}
}

func TestCyberhubSearchJSONLines(t *testing.T) {
	cmd := newTestSearchCommand()

	var buf strings.Builder
	err := cmd.Execute(context.Background(), []string{"cyberhub", "search", "poc", "spring", "--json"}, &buf)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1: %q", len(lines), out)
	}
	var got cyberhubItem
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("json unmarshal error = %v", err)
	}
	if got.Kind != typePOC || got.ID != "spring-rce" || got.Severity != "critical" {
		t.Fatalf("json item = %#v", got)
	}
}

func TestCyberhubLoadsRemoteFingerprintsAndPOCs(t *testing.T) {
	server := newCyberhubFixtureServer(t)
	defer server.Close()

	set, err := resources.Init(context.Background(), resources.Options{
		CyberhubURL: server.URL,
		APIKey:      "test-key",
		Mode:        resources.ModeOverride,
	})
	if err != nil {
		t.Fatalf("resources.Init() error = %v", err)
	}
	if set.Fingers != nil {
		t.Cleanup(func() { _ = set.Fingers.Close() })
	}
	if set.Neutron != nil {
		t.Cleanup(func() { _ = set.Neutron.Close() })
	}
	if !set.RemoteEnabled {
		t.Fatal("remote cyberhub should be enabled")
	}
	if set.RemoteFingers != 1 || set.RemoteNeutron != 1 {
		t.Fatalf("remote counts fingers=%d neutron=%d errors fingers=%v neutron=%v",
			set.RemoteFingers, set.RemoteNeutron, set.RemoteFingersErr, set.RemoteNeutronErr)
	}

	var buf strings.Builder
	cyberhub := NewCyberhubCommand(set)
	if err := cyberhub.Execute(context.Background(), []string{"search", "finger", "cyberhub-test-app"}, &buf); err != nil {
		t.Fatalf("finger cyberhub.Execute() error = %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[cyberhub] finger cyberhub-test-app http focus 1 acme hubapp cyberhub,web") {
		t.Fatalf("finger output missing remote item: %q", out)
	}

	buf.Reset()
	if err := cyberhub.Execute(context.Background(), []string{"list", "poc", "--finger", "cyberhub-test-app", "--severity", "critical", "--limit", "0"}, &buf); err != nil {
		t.Fatalf("poc cyberhub.Execute() error = %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "[cyberhub] poc \"Cyberhub Test POC\" cyberhub-test-poc critical cyberhub-test-app cyberhub,rce") {
		t.Fatalf("poc output missing remote item: %q", out)
	}
}

func newTestSearchCommand() *Command {
	fingerCfg := sdkfingers.NewConfig().WithFingers(fingerslib.Fingers{
		{
			Name:        "nginx",
			Protocol:    "http",
			Tags:        []string{"web", "server"},
			Focus:       true,
			IsActive:    true,
			Level:       1,
			Description: "nginx web server",
			Attributes: common.Attributes{
				Vendor:  "nginx",
				Product: "nginx",
			},
		},
		{
			Name:        "redis",
			Protocol:    "tcp",
			Tags:        []string{"database"},
			Description: "redis service",
		},
	})
	neutronCfg := sdkneutron.NewConfig().WithTemplates([]*templates.Template{
		{
			Id:      "spring-rce",
			Fingers: []string{"spring"},
			Info: templates.Info{
				Name:        "Spring RCE",
				Severity:    "critical",
				Tags:        "spring,rce",
				Description: "spring remote code execution",
			},
		},
		{
			Id:      "tomcat-leak",
			Fingers: []string{"tomcat"},
			Info: templates.Info{
				Name:     "Tomcat Leak",
				Severity: "low",
				Tags:     "tomcat,exposure",
			},
		},
	})
	return New(Opts{
		Resources: &resources.Set{
			FingersConfig: fingerCfg,
			NeutronConfig: neutronCfg,
		},
	})
}

func newCyberhubFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("unexpected api key %q", r.Header.Get("X-API-Key"))
			http.Error(w, "unexpected api key", http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/api/v1/fingerprints/export":
			if r.URL.Query().Get("with_fingerprint") != "true" {
				t.Errorf("missing with_fingerprint=true: %s", r.URL.RawQuery)
				http.Error(w, "missing with_fingerprint", http.StatusBadRequest)
				return
			}
			writeCyberhubResponse(t, w, false, map[string]any{
				"fingerprints": []map[string]any{{
					"name":     "cyberhub-test-app",
					"protocol": "http",
					"tag":      []string{"cyberhub", "web"},
					"focus":    true,
					"level":    1,
					"attributes": map[string]string{
						"vendor":  "acme",
						"product": "hubapp",
					},
					"description": "fixture fingerprint from cyberhub",
					"rule": []map[string]any{{
						"regexps": map[string]any{
							"body": []string{"CyberHubTestApp"},
						},
					}},
				}},
				"total":     1,
				"page":      1,
				"page_size": 1,
			})
		case "/api/v1/pocs/export":
			if r.URL.Query().Get("status") != "active" {
				t.Errorf("missing default active status: %s", r.URL.RawQuery)
				http.Error(w, "missing status", http.StatusBadRequest)
				return
			}
			writeCyberhubResponse(t, w, true, map[string]any{
				"pocs": []map[string]any{{
					"raw_content": cyberhubFixturePOC(),
				}},
				"total":    1,
				"exported": 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeCyberhubResponse(t *testing.T, w http.ResponseWriter, gzipBody bool, data any) {
	t.Helper()
	body := map[string]any{
		"code":    0,
		"message": "ok",
		"data":    data,
	}
	w.Header().Set("Content-Type", "application/json")
	if gzipBody {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		if err := json.NewEncoder(gz).Encode(body); err != nil {
			t.Fatalf("encode gzip response: %v", err)
		}
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func cyberhubFixturePOC() string {
	return `id: cyberhub-test-poc
info:
  name: Cyberhub Test POC
  severity: critical
  tags: cyberhub,rce
finger:
  - cyberhub-test-app
http:
  - method: GET
    path:
      - "{{BaseURL}}/cyberhub-poc"
    matchers:
      - type: word
        words:
          - cyberhub-ok
`
}

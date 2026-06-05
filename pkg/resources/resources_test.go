package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chainreactors/fingers/common"
	fingerresources "github.com/chainreactors/fingers/resources"
	"github.com/chainreactors/utils"
)

func TestInitUsesAiscanEmbeddedResources(t *testing.T) {
	oldUtilsPrePort := utils.PrePort
	oldFingerPrePort := fingerresources.PrePort
	oldFingerPortData := cloneBytes(fingerresources.PortData)
	t.Cleanup(func() {
		utils.PrePort = oldUtilsPrePort
		fingerresources.PrePort = oldFingerPrePort
		fingerresources.PortData = oldFingerPortData
	})

	set, err := Init(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if set.Fingers != nil {
		t.Cleanup(func() { _ = set.Fingers.Close() })
	}
	if set.Neutron != nil {
		t.Cleanup(func() { _ = set.Neutron.Close() })
	}

	if set.Fingers == nil || set.Fingers.Count() == 0 {
		t.Fatalf("fingers engine count = 0")
	}
	if set.Neutron == nil || set.Neutron.Count() == 0 {
		t.Fatalf("neutron engine count = 0")
	}
	if len(set.GogoConfig("http")) == 0 || len(set.GogoConfig("socket")) == 0 || len(set.GogoConfig("neutron")) == 0 {
		t.Fatalf("gogo provider is missing local resources")
	}
	if len(set.SprayConfig("spray_rule")) == 0 || len(set.SprayConfig("spray_dict")) == 0 || len(set.SprayConfig("spray_common")) == 0 {
		t.Fatalf("spray provider is missing local resources")
	}
	for _, name := range []string{"zombie_common", "zombie_default", "zombie_rule", "zombie_template"} {
		data := set.ZombieConfig(name)
		if len(data) == 0 {
			t.Fatalf("zombie provider missing %s", name)
		}
		switch string(data) {
		case "[]", "{}":
			t.Fatalf("zombie provider %s returned fallback only — embedded data not generated", name)
		}
	}
	if len(set.ZombieConfig("http")) == 0 || len(set.ZombieConfig("socket")) == 0 || len(set.ZombieConfig("port")) == 0 {
		t.Fatalf("zombie provider missing shared resources")
	}
	if string(set.GogoConfig("fingerprinthub_web")) != "[]" || string(set.GogoConfig("fingerprinthub_service")) != "[]" {
		t.Fatalf("fingerprinthub fallback data should be empty JSON")
	}
	if utils.PrePort == nil || fingerresources.PrePort == nil || len(fingerresources.PortData) == 0 {
		t.Fatalf("local port preset was not installed")
	}
}

func TestInitPreservesCyberHubTemplateFingerprints(t *testing.T) {
	oldUtilsPrePort := utils.PrePort
	oldFingerPrePort := fingerresources.PrePort
	oldFingerPortData := cloneBytes(fingerresources.PortData)
	t.Cleanup(func() {
		utils.PrePort = oldUtilsPrePort
		fingerresources.PrePort = oldFingerPrePort
		fingerresources.PortData = oldFingerPortData
	})

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/fingerprints/export":
			writeCyberHubAPIResponse(t, w, map[string]any{
				"fingerprints": []map[string]any{
					cyberHubFingerprintExport("sentinel", sentinelTemplateYAML()),
					cyberHubFingerprintExport("gitea", giteaTemplateYAML()),
				},
				"total":     2,
				"page":      1,
				"page_size": 2,
			})
		case "/api/v1/pocs/export":
			writeCyberHubAPIResponse(t, w, map[string]any{
				"pocs":     []any{},
				"total":    0,
				"exported": 0,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	set, err := Init(context.Background(), Options{
		CyberhubURL: hub.URL,
		APIKey:      "test-key",
		Mode:        ModeOverride,
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if set.Fingers != nil {
		t.Cleanup(func() { _ = set.Fingers.Close() })
	}
	if set.Neutron != nil {
		t.Cleanup(func() { _ = set.Neutron.Close() })
	}

	if set.RemoteFingers != 2 {
		t.Fatalf("remote fingers = %d, want 2", set.RemoteFingers)
	}
	if got := len(set.FingersConfig.FullFingers.TemplateItems("fingerprinthub")); got != 2 {
		t.Fatalf("fingerprinthub template items = %d, want 2", got)
	}

	libEngine := set.Fingers.Get()
	if libEngine == nil || libEngine.FingerPrintHub() == nil {
		t.Fatalf("fingerprinthub engine was not preserved after resources.Init")
	}

	assertFingerPrintHubMatch(t, libEngine.FingerPrintHub().WebMatch(rawHTTPFixture("Sentinel Dashboard")), "sentinel")
	assertFingerPrintHubMatch(t, libEngine.FingerPrintHub().WebMatch(rawHTTPFixture("Gitea")), "gitea")
}

func writeCyberHubAPIResponse(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{
		"code":    0,
		"message": "ok",
		"data":    data,
	}); err != nil {
		t.Fatalf("write cyberhub response: %v", err)
	}
}

func cyberHubFingerprintExport(name, rawContent string) map[string]any {
	return map[string]any{
		"name":        name,
		"protocol":    "http",
		"engine":      "fingerprinthub",
		"raw_content": rawContent,
	}
}

func assertFingerPrintHubMatch(t *testing.T, frames common.Frameworks, name string) {
	t.Helper()
	if _, ok := frames[name]; !ok {
		t.Fatalf("fingerprinthub match missing %q: %#v", name, frames)
	}
}

func rawHTTPFixture(marker string) []byte {
	return []byte("HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<html><title>" + marker + "</title><body>" + marker + "</body></html>")
}

func sentinelTemplateYAML() string {
	return `id: sentinel-dashboard
info:
  name: sentinel
  severity: info
http:
  - method: GET
    path:
      - "{{BaseURL}}/"
    matchers:
      - type: word
        words:
          - "Sentinel Dashboard"
`
}

func giteaTemplateYAML() string {
	return `id: gitea-web
info:
  name: gitea
  severity: info
http:
  - method: GET
    path:
      - "{{BaseURL}}/"
    matchers:
      - type: word
        words:
          - "Gitea"
`
}

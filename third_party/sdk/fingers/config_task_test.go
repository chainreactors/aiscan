package fingers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainreactors/sdk/pkg/cyberhub"
	"github.com/chainreactors/sdk/pkg/types"
)

func TestConfigSourceSelectionAndFiltering(t *testing.T) {
	cfg := NewConfig().
		WithFingers(types.Fingers{
			{Name: "web-app", Protocol: "http"},
			{Name: "service-app", Protocol: "tcp"},
		}).
		WithAliases([]*types.Alias{{Name: "web-app", Pocs: []string{"CVE-1"}}}).
		WithFilter(func(item *FullFinger) bool {
			return item != nil && item.Finger != nil && item.Finger.Protocol == "http"
		})

	if cfg.FullFingers.Len() != 1 {
		t.Fatalf("filtered fingers len = %d, want 1", cfg.FullFingers.Len())
	}
	item := cfg.FullFingers.Items["web-app"]
	if item == nil || item.Alias == nil || len(item.Alias.Pocs) != 1 {
		t.Fatalf("expected alias to be preserved after filter: %+v", item)
	}

	cfg.WithProvider(cyberhub.NewProvider("https://cyberhub.test", "key"))
	if len(cfg.Providers) == 0 {
		t.Fatalf("Provider assignment did not work: %+v", cfg)
	}
}

func TestMergeExportsSource1OverridesEngineToXray(t *testing.T) {
	rawTemplate := `id: source1-route-test
info:
  name: source1 route test
  author: tester
  severity: info
http:
  - method: GET
    path:
      - "{{BaseURL}}/"
    matchers:
      - type: word
        words:
          - source1-route-marker
`

	exports := []cyberhub.FingerprintExport{{
		Finger: &types.Finger{
			Name:     "source1 route test",
			Protocol: "http",
			Tags:     []string{"neutron", "source1"},
		},
		Engine:     "fingerprinthub",
		RawContent: rawTemplate,
	}}

	full := (FullFingers{}).MergeExports(exports, false)
	if got := len(full.TemplateItems("fingerprinthub")); got != 0 {
		t.Fatalf("fingerprinthub template count = %d, want 0 (source1 overrides to xray)", got)
	}
	if got := len(full.TemplateItems("xray")); got != 1 {
		t.Fatalf("xray template count = %d, want 1", got)
	}
}

func TestMergeExportsWithoutSource1StaysFingerprinthub(t *testing.T) {
	rawTemplate := `id: no-source1-test
info:
  name: no source1 test
  author: tester
  severity: info
http:
  - method: GET
    path:
      - "{{BaseURL}}/"
    matchers:
      - type: word
        words:
          - marker
`

	exports := []cyberhub.FingerprintExport{{
		Finger: &types.Finger{
			Name:     "no source1 test",
			Protocol: "http",
			Tags:     []string{"neutron"},
		},
		Engine:     "fingerprinthub",
		RawContent: rawTemplate,
	}}

	full := (FullFingers{}).MergeExports(exports, false)
	if got := len(full.TemplateItems("fingerprinthub")); got != 1 {
		t.Fatalf("fingerprinthub template count = %d, want 1", got)
	}
	if got := len(full.TemplateItems("xray")); got != 0 {
		t.Fatalf("xray template count = %d, want 0 (no source1 tag)", got)
	}
}

func TestHasTag(t *testing.T) {
	if hasTag(&types.Finger{Tags: []string{"xray"}}, "source1") {
		t.Fatal("xray tag should not match source1")
	}
	if !hasTag(&types.Finger{Tags: []string{" source1 "}}, "source1") {
		t.Fatal("source1 tag with whitespace should match")
	}
	if hasTag(nil, "source1") {
		t.Fatal("nil finger should return false")
	}
}

func TestExecuteMatchTaskUsesSDKResult(t *testing.T) {
	eng := newDetailTestEngine(t, NewConfig(), &types.Finger{
		Name:     "execute-app",
		Protocol: "http",
		Rules: types.FingerRules{{
			Regexps: &types.FingerRegexps{Body: []string{"ExecuteMarker"}},
		}},
	})

	resultCh, err := eng.Execute(NewContext(), NewMatchTask(rawHTTP("ExecuteMarker")))
	if err != nil {
		t.Fatal(err)
	}
	result := <-resultCh
	if result == nil || !result.Success() {
		t.Fatalf("expected successful result, got %#v err=%v", result, result.Error())
	}
	match := result.(*MatchResult)
	if !match.HasMatch() || match.Count() != 1 {
		t.Fatalf("expected one match, got %+v", match.Frameworks())
	}
	if match.Frameworks()["execute-app"] == nil {
		t.Fatalf("expected execute-app match, got %+v", match.Frameworks())
	}
}

func TestExecuteRejectsInvalidTaskAndContext(t *testing.T) {
	eng := newDetailTestEngine(t, NewConfig(), &types.Finger{
		Name:     "task-app",
		Protocol: "http",
		Rules: types.FingerRules{{
			Regexps: &types.FingerRegexps{Body: []string{"TaskMarker"}},
		}},
	})

	if _, err := eng.Execute(NewContext(), NewMatchTask(nil)); err == nil {
		t.Fatal("expected empty match task to fail validation")
	}
	if _, err := eng.Execute(fakeContext{ctx: context.Background()}, NewMatchTask(rawHTTP("TaskMarker"))); err == nil {
		t.Fatal("expected unsupported context type")
	}
	if _, err := eng.Execute(NewContext(), fakeTask{typ: "unknown"}); err == nil {
		t.Fatal("expected unsupported task type")
	}
}

func TestNewMatchTaskFromResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-SDK-Test", "ok")
		_, _ = w.Write([]byte("response-body"))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	task := NewMatchTaskFromResponse(resp)
	if err := task.Validate(); err != nil {
		t.Fatal(err)
	}
	data := string(task.Data)
	if !containsAll(data, "X-Sdk-Test: ok", "response-body") {
		t.Fatalf("raw response missing expected content:\n%s", data)
	}
}

type fakeContext struct {
	ctx context.Context
}

func (f fakeContext) Context() context.Context {
	return f.ctx
}

type fakeTask struct {
	typ string
}

func (f fakeTask) Type() string {
	return f.typ
}

func (f fakeTask) Validate() error {
	return nil
}

var _ types.Context = fakeContext{}
var _ types.Task = fakeTask{}

func containsAll(s string, values ...string) bool {
	for _, value := range values {
		if !strings.Contains(s, value) {
			return false
		}
	}
	return true
}

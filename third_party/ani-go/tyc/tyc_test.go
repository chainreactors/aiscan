package tyc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ani "github.com/chainreactors/ani-go"
)

func newTestEngineWithToken(t *testing.T, token string, handler http.HandlerFunc) *ani.Engine {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	prevS, prevIc, prevIn := configuredSearchURL, configuredICPURL, configuredInvestURL
	configuredSearchURL = srv.URL + "/search"
	configuredICPURL = srv.URL + "/icp"
	configuredInvestURL = srv.URL + "/invest"
	t.Cleanup(func() {
		configuredSearchURL = prevS
		configuredICPURL = prevIc
		configuredInvestURL = prevIn
	})
	cfg := ani.NewConfig().WithDepth(1).WithPercent(0.5).WithTycToken(token)
	return ani.NewEngine(cfg)
}

const searchHTML = `<html><body>
<div class="index_name"><a href="https://www.tianyancha.com/company/100">默安科技</a></div>
</body></html>`

const icpHTML = `<html><body>
<ul page-total="1"><li>1</li></ul>
<table><tbody>
<tr><td>1</td><td>2024-01-01</td><td><span>moan</span></td><td>type</td><td>moresec.cn</td><td><span>浙ICP备16020926号-1</span></td></tr>
<tr><td>2</td><td>2024-01-01</td><td><span>moan2</span></td><td>type</td><td>moresec.com</td><td><span>浙ICP备16020926号-2</span></td></tr>
</tbody></table>
</body></html>`

const investHTML = `<html><body><table><tbody></tbody></table></body></html>`

func TestTycBasic(t *testing.T) {
	e := newTestEngineWithToken(t, "fake-jwt", func(w http.ResponseWriter, r *http.Request) {
		// 验证 cookie 透传
		if c, _ := r.Cookie("auth_token"); c == nil || c.Value != "fake-jwt" {
			t.Errorf("missing/wrong auth_token cookie: %+v", c)
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			fmt.Fprint(w, searchHTML)
		case strings.HasPrefix(r.URL.Path, "/icp"):
			fmt.Fprint(w, icpHTML)
		case strings.HasPrefix(r.URL.Path, "/invest"):
			fmt.Fprint(w, investHTML)
		}
	})
	assets, err := e.Query(context.Background(), "tyc", "默安科技")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("want 2 assets, got %d: %+v", len(assets), assets)
	}
	if assets[0].ICP != "浙ICP备16020926号" || assets[0].Domain != "moresec.cn" {
		t.Errorf("asset[0] mismatch: %+v", assets[0])
	}
	if assets[0].Source != "tyc" {
		t.Errorf("source = %q", assets[0].Source)
	}
}

func TestTycSkipsWhenNoToken(t *testing.T) {
	e := ani.NewEngine(ani.NewConfig())
	for _, s := range e.Sources() {
		if s == "tyc" {
			t.Fatal("tyc should not register without auth_token")
		}
	}
}

func TestTycHelpers(t *testing.T) {
	if got := icpFormat("浙ICP备123号-2"); got != "浙ICP备123号" {
		t.Errorf("icpFormat: %q", got)
	}
	if got := percentToFloat("33%"); got != 0.33 {
		t.Errorf("percentToFloat: %f", got)
	}
	if got := pathLast("https://www.tianyancha.com/company/12345"); got != "12345" {
		t.Errorf("pathLast: %q", got)
	}
}

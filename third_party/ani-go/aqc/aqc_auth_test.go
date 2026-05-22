package aqc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ani "github.com/chainreactors/ani-go"
)

func newAuthTestEngine(t *testing.T, cookie string, handler http.HandlerFunc) *ani.Engine {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	prevSearch, prevBeian, prevInvest := configuredAqcSearchURL, configuredBeianURL, configuredInvestAPI
	configuredAqcSearchURL = srv.URL + "/aiqicha/s"
	configuredBeianURL = srv.URL + "/beian"
	configuredInvestAPI = srv.URL + "/invest"
	t.Cleanup(func() {
		configuredAqcSearchURL = prevSearch
		configuredBeianURL = prevBeian
		configuredInvestAPI = prevInvest
	})
	cfg := ani.NewConfig().WithDepth(1).WithPercent(0.5).WithAqcCookie(cookie)
	return ani.NewEngine(cfg)
}

const aqcAuthSearchHTML = `<html><head></head><body>
<script>
window.pageData = {"result":{"resultList":[{"entName":"<em>默安</em>科技","pid":"PID-A"}]}};
window.isSpider = null;
</script>
</body></html>`

const beianHTML = `<html><body>
<div class="ranking-content"><table><tbody>
<tr>
  <td>1</td>
  <td><a href="#">浙ICP备16020926号-1</a></td>
  <td>2024-01-01</td>
  <td><span>moan</span></td>
  <td><span>moresec.cn</span></td>
</tr>
<tr>
  <td>2</td>
  <td><a href="#">浙ICP备16020926号-2</a></td>
  <td>2024-01-01</td>
  <td><span>moan2</span></td>
  <td><span>moresec.com</span></td>
</tr>
</tbody></table></div>
<ul class="pagination"><li>1</li><li>></li></ul>
</body></html>`

func TestAqcAuthBasic(t *testing.T) {
	e := newAuthTestEngine(t, "BAIDUID=abc", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/aiqicha/s"):
			// 搜索接口要带 BAIDUID
			if !strings.Contains(r.Header.Get("Cookie"), "BAIDUID=abc") {
				t.Errorf("aqc search missing BAIDUID cookie, got %q", r.Header.Get("Cookie"))
			}
			fmt.Fprint(w, aqcAuthSearchHTML)
		case strings.HasPrefix(r.URL.Path, "/beian"):
			fmt.Fprint(w, beianHTML)
		case strings.HasPrefix(r.URL.Path, "/invest"):
			fmt.Fprint(w, `{"status":0,"data":{"investRecordData":{"list":[]}}}`)
		}
	})
	assets, err := e.Query(context.Background(), "aqc", "默安科技")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("want 2 assets, got %d: %+v", len(assets), assets)
	}
	if assets[0].Name != "默安科技" {
		t.Errorf("stripEmTags failed: %q", assets[0].Name)
	}
	if assets[0].ICP != "浙ICP备16020926号" || assets[0].Domain != "moresec.cn" {
		t.Errorf("asset[0] mismatch: %+v", assets[0])
	}
	if assets[0].Source != "aqc" {
		t.Errorf("source = %q", assets[0].Source)
	}
}

func TestAqcAuthSkipsWhenNoCookie(t *testing.T) {
	e := ani.NewEngine(ani.NewConfig())
	for _, s := range e.Sources() {
		if s == "aqc" {
			t.Fatal("aqc (authed) should not register without BAIDUID")
		}
	}
}

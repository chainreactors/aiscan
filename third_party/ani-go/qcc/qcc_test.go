package qcc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ani "github.com/chainreactors/ani-go"
)

func newTestEngineWithCookie(t *testing.T, cookie string, handler http.HandlerFunc) *ani.Engine {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	prevS, prevH, prevB := configuredSearchURL, configuredHoldingAPI, configuredBigsearchAPI
	configuredSearchURL = srv.URL + "/search"
	configuredHoldingAPI = srv.URL + "/holding"
	configuredBigsearchAPI = srv.URL + "/bigsearch"
	t.Cleanup(func() {
		configuredSearchURL = prevS
		configuredHoldingAPI = prevH
		configuredBigsearchAPI = prevB
	})
	cfg := ani.NewConfig().WithDepth(1).WithPercent(0.5).WithQccCookie(cookie)
	return ani.NewEngine(cfg)
}

const qccSearchHTML = `<html><body>
<div><a class="title copy-value" href="https://www.qcc.com/firm/KEY-1.html">默安科技 (杭州) 有限公司</a></div>
</body></html>`

func TestQccBasic(t *testing.T) {
	e := newTestEngineWithCookie(t, "QCCSESSID=abc", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cookie"); !strings.Contains(got, "QCCSESSID=abc") {
			t.Errorf("missing cookie, got %q", got)
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			fmt.Fprint(w, qccSearchHTML)
		case strings.HasPrefix(r.URL.Path, "/holding"):
			fmt.Fprint(w, `{"Status":201}`)
		case strings.HasPrefix(r.URL.Path, "/bigsearch"):
			fmt.Fprint(w, `{"Status":200,"Result":[
                {"ICPNo":"浙ICP备16020926号-1","DomainName":"moresec.cn","WebSiteName":"<em>moan</em>"},
                {"ICPNo":"浙ICP备16020926号-2","DomainName":"moresec.com","WebSiteName":"moan2"}
            ],"Paging":{"TotalRecords":2}}`)
		}
	})
	assets, err := e.Query(context.Background(), "qcc", "默安科技")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("want 2 assets, got %d: %+v", len(assets), assets)
	}
	if assets[0].ICP != "浙ICP备16020926号" || assets[0].Domain != "moresec.cn" {
		t.Errorf("asset[0] mismatch: %+v", assets[0])
	}
	if assets[0].Title != "moan" {
		t.Errorf("html tags should be stripped, got %q", assets[0].Title)
	}
	if assets[0].Source != "qcc" {
		t.Errorf("source = %q", assets[0].Source)
	}
}

func TestQccSkipsWhenNoCookie(t *testing.T) {
	e := ani.NewEngine(ani.NewConfig())
	for _, s := range e.Sources() {
		if s == "qcc" {
			t.Fatal("qcc should not register without QCCSESSID")
		}
	}
}

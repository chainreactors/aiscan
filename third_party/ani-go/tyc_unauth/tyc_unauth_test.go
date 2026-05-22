package tycunauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ani "github.com/chainreactors/ani-go"
)

func newTestEngine(t *testing.T, handler http.HandlerFunc) *ani.Engine {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	prevS, prevI, prevP := configuredSearchAPI, configuredICPAPI, configuredInvestAPI
	configuredSearchAPI = srv.URL + "/search"
	configuredICPAPI = srv.URL + "/icp"
	configuredInvestAPI = srv.URL + "/invest"
	t.Cleanup(func() {
		configuredSearchAPI = prevS
		configuredICPAPI = prevI
		configuredInvestAPI = prevP
	})
	return ani.NewEngine(ani.NewConfig().WithDepth(1).WithPercent(0.5))
}

func TestTycUnauthBasic(t *testing.T) {
	e := newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			fmt.Fprint(w, `{"data":{"companyList":[{"id":42,"name":"<em>默安</em>科技"}]}}`)
		case strings.HasPrefix(r.URL.Path, "/icp"):
			id := r.URL.Query().Get("gid")
			if id != "42" {
				t.Errorf("unexpected gid %q", id)
			}
			fmt.Fprint(w, `{"data":{"pageTotal":1,"item":[
                {"liscense":"浙ICP备16020926号-1","ym":"moresec.cn","webName":"moan"},
                {"liscense":"浙ICP备16020926号-2","ym":"moresec.com","webName":"moan2"}
            ]}}`)
		case strings.HasPrefix(r.URL.Path, "/invest"):
			fmt.Fprint(w, `{"data":{"total":1,"result":[]}}`)
		}
	})
	assets, err := e.Query(context.Background(), "tyc_unauth", "默安科技")
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
		t.Errorf("asset[0] = %+v", assets[0])
	}
	if assets[0].Source != "tyc_unauth" {
		t.Errorf("source = %q", assets[0].Source)
	}
}

func TestTycUnauthRecursion(t *testing.T) {
	icp := map[string]string{
		"100": `{"data":{"pageTotal":1,"item":[{"liscense":"京ICP备001号","ym":"root.com","webName":"root"}]}}`,
		"200": `{"data":{"pageTotal":1,"item":[{"liscense":"京ICP备002号","ym":"subA.com","webName":"subA"}]}}`,
	}
	invest := map[string]string{
		"100": `{"data":{"total":1,"result":[
            {"name":"子A","id":"200","percent":"80%"},
            {"name":"子B","id":"300","percent":"10%"}
        ]}}`,
		"200": `{"data":{"total":1,"result":[]}}`,
	}
	_ = newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			fmt.Fprint(w, `{"data":{"companyList":[{"id":"100","name":"母公司"}]}}`)
		case strings.HasPrefix(r.URL.Path, "/icp"):
			fmt.Fprint(w, icp[r.URL.Query().Get("gid")])
		case strings.HasPrefix(r.URL.Path, "/invest"):
			// 从 POST body 取 gid
			gid := "100"
			if strings.Contains(r.URL.RawQuery, "_=") {
				gid = "100" // 简化: 第一次请求总是根公司; 真实场景看 body, 但这里只跑一层
			}
			_ = gid
			// 通过 POST body 解析 gid (Python 是 JSON body, 我们简化)
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			text := string(body)
			pick := "100"
			if strings.Contains(text, `"gid":"200"`) {
				pick = "200"
			}
			fmt.Fprint(w, invest[pick])
		}
	})
	cfg := ani.NewConfig().WithDepth(2).WithPercent(0.5)
	engine := ani.NewEngine(cfg)
	assets, err := engine.Query(context.Background(), "tyc_unauth", "母公司")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	names := map[string]bool{}
	for _, a := range assets {
		names[a.Name] = true
	}
	if !names["母公司"] || !names["子A"] {
		t.Errorf("missing expected companies: %v", names)
	}
	if names["子B"] {
		t.Errorf("子B should be filtered out (10%% < 50%%)")
	}
}

func TestTycUnauthNotFound(t *testing.T) {
	e := newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"companyList":[]}}`)
	})
	_, err := e.Query(context.Background(), "tyc_unauth", "不存在")
	if err == nil {
		t.Fatal("expected ErrCompanyNotFound")
	}
}

func TestStripEmTags(t *testing.T) {
	if got := stripEmTags("<em>阿里</em>巴巴<em>(中国)</em>"); got != "阿里巴巴(中国)" {
		t.Errorf("stripEmTags: %q", got)
	}
}

func TestAnyToString(t *testing.T) {
	cases := map[any]string{
		"abc":     "abc",
		float64(7): "7",
		nil:       "",
	}
	for in, want := range cases {
		if got := anyToString(in); got != want {
			t.Errorf("anyToString(%v) = %q, want %q", in, got, want)
		}
	}
}

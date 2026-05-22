package aqc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ani "github.com/chainreactors/ani-go"
)

// newTestEngine 起一个 mock server, 把三个 endpoint 都接到同一个 handler 上。
func newTestEngine(t *testing.T, handler http.HandlerFunc) *ani.Engine {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	prevSearch, prevICP, prevInvest := configuredSearchAPI, configuredICPAPI, configuredInvestAPI
	configuredSearchAPI = srv.URL + "/search"
	configuredICPAPI = srv.URL + "/icp"
	configuredInvestAPI = srv.URL + "/invest"
	t.Cleanup(func() {
		configuredSearchAPI = prevSearch
		configuredICPAPI = prevICP
		configuredInvestAPI = prevInvest
	})
	return ani.NewEngine(ani.NewConfig().WithDepth(1).WithPercent(0.5))
}

func TestRunSimple(t *testing.T) {
	e := newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			fmt.Fprint(w, `{"code":"0","data":{"dataList":[{"name":"默安科技","sourceId":"PID-1"}]}}`)
		case strings.HasPrefix(r.URL.Path, "/icp"):
			pid := r.URL.Query().Get("pid")
			if pid != "PID-1" {
				t.Errorf("unexpected pid %s", pid)
			}
			fmt.Fprint(w, `{"status":0,"data":{"total":2,"pageCount":1,"list":[
                {"icpNo":"浙ICP备16020926号-1","siteName":"moan","domain":["moresec.cn"]},
                {"icpNo":"浙ICP备16020926号-2","siteName":"moan2","domain":["moresec.com"]}
            ]}}`)
		case strings.HasPrefix(r.URL.Path, "/invest"):
			// depth=1 上层不递归子公司 (depth=0 已经处理), 但 fetchInvestments 还是会被调用并取忽略
			fmt.Fprint(w, `{"status":0,"data":{"investRecordData":{"list":[]}}}`)
		}
	})
	assets, err := e.Query(context.Background(), "aqc_unauth", "默安科技")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets, got %d: %+v", len(assets), assets)
	}
	if assets[0].Name != "默安科技" || assets[0].ICP != "浙ICP备16020926号" || assets[0].Domain != "moresec.cn" {
		t.Errorf("asset[0] mismatch: %+v", assets[0])
	}
	if assets[0].Source != sourceName || assets[0].Depth != 0 {
		t.Errorf("source/depth mismatch: %+v", assets[0])
	}
}

func TestRunRecursion(t *testing.T) {
	icpResponses := map[string]string{
		"PID-1": `{"status":0,"data":{"total":1,"pageCount":1,"list":[{"icpNo":"京ICP备111号","siteName":"root","domain":["root.com"]}]}}`,
		"PID-2": `{"status":0,"data":{"total":1,"pageCount":1,"list":[{"icpNo":"京ICP备222号","siteName":"sub","domain":["sub.com"]}]}}`,
	}
	investResponses := map[string]string{
		"PID-1": `{"status":0,"data":{"investRecordData":{"list":[
            {"entName":"子公司A","pid":"PID-2","regRate":"80%"},
            {"entName":"子公司B","pid":"PID-3","regRate":"10%"}
        ]}}}`,
		"PID-2": `{"status":0,"data":{"investRecordData":{"list":[]}}}`,
	}
	_ = newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			fmt.Fprint(w, `{"code":"0","data":{"dataList":[{"name":"母公司","sourceId":"PID-1"}]}}`)
		case strings.HasPrefix(r.URL.Path, "/icp"):
			pid := r.URL.Query().Get("pid")
			fmt.Fprint(w, icpResponses[pid])
		case strings.HasPrefix(r.URL.Path, "/invest"):
			pid := r.URL.Query().Get("pid")
			fmt.Fprint(w, investResponses[pid])
		}
	})
	// depth=2, percent=0.5 → 子公司 A 入围 (80%), 子公司 B 不入围 (10%)
	cfg := ani.NewConfig().WithDepth(2).WithPercent(0.5)
	engine := ani.NewEngine(cfg)
	assets, err := engine.Query(context.Background(), "aqc_unauth", "母公司")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets (root + 子A), got %d: %+v", len(assets), assets)
	}
	names := map[string]bool{}
	for _, a := range assets {
		names[a.Name] = true
	}
	if !names["母公司"] || !names["子公司A"] {
		t.Errorf("missing expected company: %v", names)
	}
	if names["子公司B"] {
		t.Errorf("子公司B should be filtered (regRate=10%% < 50%% threshold)")
	}
}

func TestRunCompanyNotFound(t *testing.T) {
	e := newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/search") {
			fmt.Fprint(w, `{"code":"0","data":{"dataList":[]}}`)
			return
		}
		t.Errorf("unexpected request to %s", r.URL.Path)
	})
	_, err := e.Query(context.Background(), "aqc_unauth", "不存在")
	if err == nil {
		t.Fatal("expected ErrCompanyNotFound")
	}
}

func TestPercentToFloat(t *testing.T) {
	cases := map[string]float64{
		"33.33%": 0.3333,
		"100%":   1.0,
		"":       0.0,
		"-":      0.0,
		"50":     0.5,
	}
	for in, want := range cases {
		if got := percentToFloat(in); got != want {
			t.Errorf("percentToFloat(%q) = %f, want %f", in, got, want)
		}
	}
}

func TestICPFormat(t *testing.T) {
	if got := icpFormat("浙ICP备16020926号-2"); got != "浙ICP备16020926号" {
		t.Errorf("icpFormat: got %q", got)
	}
}

func TestEngineSources(t *testing.T) {
	e := ani.NewEngine(nil)
	sources := e.Sources()
	found := false
	for _, s := range sources {
		if s == "aqc_unauth" {
			found = true
		}
	}
	if !found {
		t.Errorf("aqc_unauth not in sources: %v", sources)
	}
}

// Encoding sanity: ensure CompanyAsset JSON is what we expect.
func TestCompanyAssetJSON(t *testing.T) {
	a := ani.CompanyAsset{Name: "x", PID: "p", ICP: "ICP1", Domain: "x.com", Percent: 0.5, Depth: 1, Source: "aqc_unauth"}
	b, _ := json.Marshal(a)
	got := string(b)
	for _, want := range []string{`"name":"x"`, `"icp":"ICP1"`, `"depth":1`, `"source":"aqc_unauth"`} {
		if !strings.Contains(got, want) {
			t.Errorf("JSON missing %q: %s", want, got)
		}
	}
}

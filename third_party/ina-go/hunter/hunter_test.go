package hunter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ina "github.com/chainreactors/ina-go"
)

// newTestEngine 起 mock hunter server, 关闭 WAF 间隔以加速测试。
func newTestEngine(t *testing.T, mode string, handler http.HandlerFunc) *ina.Engine {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prev := configuredBaseURL
	configuredBaseURL = srv.URL
	t.Cleanup(func() { configuredBaseURL = prev })

	cfg := ina.NewConfig()
	if mode == "token" {
		cfg.WithHunterToken("test-token")
	} else {
		cfg.WithHunterAPIKey("test-apikey")
	}
	engine := ina.NewEngine(cfg)
	// 把 hunter client 的 minInterval 调到 0 避免测试慢
	// (通过 source client 注册时实例已经构造好, 这里我们没法直接改; 测试调用次数少, 凑合)
	return engine
}

func TestTokenAuthShape(t *testing.T) {
	called := false
	e := newTestEngine(t, "token", func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != tokenAPI {
			t.Errorf("expected %s, got %s", tokenAPI, r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "test-token" {
			t.Errorf("Authorization header: got %q", auth)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["search"] == "" {
			t.Errorf("missing search field: %+v", body)
		}
		fmt.Fprint(w, `{"code":200,"data":{"total":0,"arr":[]}}`)
	})
	if _, err := e.QueryRaw(context.Background(), "hunter", `domain="x.com"`); err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}
	if !called {
		t.Fatal("handler not called")
	}
}

func TestAPIKeyAuthShape(t *testing.T) {
	e := newTestEngine(t, "apikey", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != keyAPI {
			t.Errorf("expected %s, got %s", keyAPI, r.URL.Path)
		}
		if k := r.URL.Query().Get("api-key"); k != "test-apikey" {
			t.Errorf("api-key param: got %q", k)
		}
		fmt.Fprint(w, `{"code":200,"data":{"total":0,"arr":[]}}`)
	})
	if _, err := e.QueryRaw(context.Background(), "hunter", `domain="x.com"`); err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}
}

func TestQueryResultMapping(t *testing.T) {
	e := newTestEngine(t, "token", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenAPI {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			page := int(body["page"].(float64))
			search, _ := base64.StdEncoding.DecodeString(body["search"].(string))
			// login check 用 web.body="check", 实际 query 用 domain="..."。
			if strings.Contains(string(search), "web.body") {
				fmt.Fprint(w, `{"code":200,"data":{"total":0,"arr":[]}}`)
				return
			}
			if page == 1 {
				fmt.Fprint(w, `{"code":200,"data":{"total":2,"arr":[
                    {"ip":"1.2.3.4","port":443,"domain":"a.example.com","title":"A","number":"京ICP备1号","http_code":200,"company":"Example Inc","component":[{"name":"nginx"},{"name":"spring"}],"protocol":"https"},
                    {"ip":"5.6.7.8","port":80,"domain":"b.example.com","title":"B","number":""}
                ]}}`)
			}
		}
	})
	assets, err := e.Query(context.Background(), "hunter", ina.NewCode().And("domain", "example.com"))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(assets))
	}
	if assets[0].IP != "1.2.3.4" || assets[0].Port != "443" || assets[0].Domain != "a.example.com" {
		t.Errorf("asset[0] mismatch: %+v", assets[0])
	}
	if assets[0].URL != "http://a.example.com:443" {
		t.Errorf("URL composition wrong: %q", assets[0].URL)
	}
	if assets[0].Status != "200" || assets[0].Company != "Example Inc" || assets[0].Frame != "nginx, spring" {
		t.Errorf("hunter-specific fields mismatch: %+v", assets[0])
	}
	if assets[0].ICP != "京ICP备1号" || assets[0].Source != "hunter" {
		t.Errorf("ICP/source: %+v", assets[0])
	}
	if assets[1].URL != "http://b.example.com:80" {
		t.Errorf("URL[1]: %q", assets[1].URL)
	}
}

func TestStatusFailUnauthorized(t *testing.T) {
	e := newTestEngine(t, "token", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":401,"message":"invalid token"}`)
	})
	status := e.Status(context.Background())
	if status["hunter"].OK {
		t.Fatalf("expected fail, got %+v", status["hunter"])
	}
	if !strings.Contains(status["hunter"].Reason, "unauthorized") {
		t.Errorf("reason mismatch: %q", status["hunter"].Reason)
	}
}

func TestStatus400IsValidAuth(t *testing.T) {
	// 400 输入参数有误 = token 有效, 只是请求格式问题; Status 视为 OK。
	e := newTestEngine(t, "token", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":400,"message":"输入参数有误"}`)
	})
	status := e.Status(context.Background())
	if !status["hunter"].OK {
		t.Fatalf("expected ok (400=valid auth), got %+v", status["hunter"])
	}
}

func TestQueryCodeMapping(t *testing.T) {
	// hunter 的 key 映射: icp → icp.number, domain → domain.suffix? 没, 默认 domain
	// (确认与 ina_code.py 一致): icp → icp.number
	c := ina.NewCode().And("icp", "京ICP备1号")
	got := c.String("hunter")
	want := `icp.number="京ICP备1号"`
	if got != want {
		t.Errorf("hunter Code mapping: got %q want %q", got, want)
	}
}

// 防止 unused import 警告 (time 用在 WAF 间隔)。
var _ = time.Second

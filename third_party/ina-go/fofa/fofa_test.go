package fofa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ina "github.com/chainreactors/ina-go"
)

// newTestEngine 起一个 mock fofa server, 注入 base URL, 返回 Engine + server。
func newTestEngine(t *testing.T, handler http.HandlerFunc) (*ina.Engine, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	prev := configuredBaseURL
	configuredBaseURL = srv.URL
	t.Cleanup(func() { configuredBaseURL = prev })

	cfg := ina.NewConfig().WithFofa("test@example.com", "deadbeef")
	return ina.NewEngine(cfg), srv
}

func TestCheckLoginSuccess(t *testing.T) {
	e, _ := newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != loginAPI {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"error":false,"isvip":true,"email":"test@example.com"}`)
	})
	status := e.Status(context.Background())
	if !status[sourceName].OK {
		t.Fatalf("expected ok, got %+v", status[sourceName])
	}
}

func TestCheckLoginFailure(t *testing.T) {
	e, _ := newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error":true,"errmsg":"bad key"}`)
	})
	status := e.Status(context.Background())
	if status[sourceName].OK {
		t.Fatalf("expected fail")
	}
	if !strings.Contains(status[sourceName].Reason, "bad key") {
		t.Fatalf("expected 'bad key' in reason, got %q", status[sourceName].Reason)
	}
}

func TestQuerySinglePage(t *testing.T) {
	e, _ := newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case loginAPI:
			fmt.Fprint(w, `{"error":false,"isvip":true}`)
		case searchAPI:
			qbase64 := r.URL.Query().Get("qbase64")
			decoded, _ := base64.StdEncoding.DecodeString(qbase64)
			if string(decoded) != `domain="example.com"` {
				t.Errorf("unexpected query: %s", decoded)
			}
			fmt.Fprint(w, `{
                "error":false,
                "size":2,
                "results":[
                    ["https://a.example.com","1.2.3.4","443","a.example.com","Title A","ICP123"],
                    ["http://b.example.com","5.6.7.8","80","b.example.com","Title B",""]
                ]
            }`)
		}
	})
	code := ina.NewCode().And("domain", "example.com")
	assets, err := e.Query(context.Background(), "fofa", code)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(assets) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(assets))
	}
	if assets[0].URL != "https://a.example.com" || assets[0].IP != "1.2.3.4" || assets[0].Port != "443" {
		t.Errorf("asset[0] mismatch: %+v", assets[0])
	}
	if assets[0].Source != "fofa" {
		t.Errorf("source field not set: %+v", assets[0])
	}
}

func TestQueryPagination(t *testing.T) {
	// 模拟 size=2500: page 1 满 999, page 2 满 999, page 3 剩 502 但因停止条件不再请求。
	// 停止条件: page*999 >= size/2 = 1250 → page=2 时 1998 >= 1250 触发停止。
	totalSize := 2500
	pagesSeen := 0
	e, _ := newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loginAPI {
			fmt.Fprint(w, `{"error":false,"isvip":true}`)
			return
		}
		pagesSeen++
		page := r.URL.Query().Get("page")
		results := make([][]any, pageSize)
		for i := range results {
			results[i] = []any{fmt.Sprintf("https://p%s-%d.example.com", page, i), "1.1.1.1", "80", "x", "t", ""}
		}
		resp := map[string]any{"error": false, "size": totalSize, "results": results}
		_ = json.NewEncoder(w).Encode(resp)
	})
	code := ina.NewCode().And("domain", "example.com")
	assets, err := e.Query(context.Background(), "fofa", code)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if pagesSeen != 2 {
		t.Errorf("expected 2 pages fetched, got %d", pagesSeen)
	}
	if len(assets) != 2*pageSize {
		t.Errorf("expected %d assets, got %d", 2*pageSize, len(assets))
	}
}

func TestQueryLimitCap(t *testing.T) {
	_, _ = newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loginAPI {
			fmt.Fprint(w, `{"error":false,"isvip":true}`)
			return
		}
		results := make([][]any, pageSize)
		for i := range results {
			results[i] = []any{"u", "1.1.1.1", "80", "x", "t", ""}
		}
		resp := map[string]any{"error": false, "size": 100000, "results": results}
		_ = json.NewEncoder(w).Encode(resp)
	})
	// Engine 默认 Limit=0 不会触发, 手动构造一个带 limit 的
	cfg := ina.NewConfig().WithFofa("a", "b").WithLimit(50)
	prev := configuredBaseURL
	defer func() { configuredBaseURL = prev }()
	// configuredBaseURL 已被 newTestEngine 改过, 复用即可
	engine := ina.NewEngine(cfg)
	assets, err := engine.QueryRaw(context.Background(), "fofa", `domain="example.com"`)
	if err != nil {
		t.Fatalf("QueryRaw: %v", err)
	}
	if len(assets) != 50 {
		t.Errorf("expected 50 assets, got %d", len(assets))
	}
}

func TestQueryAPIError(t *testing.T) {
	e, _ := newTestEngine(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loginAPI {
			fmt.Fprint(w, `{"error":false,"isvip":true}`)
			return
		}
		fmt.Fprint(w, `{"error":true,"errmsg":"quota exceeded"}`)
	})
	code := ina.NewCode().And("domain", "x")
	_, err := e.Query(context.Background(), "fofa", code)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNoCredentials(t *testing.T) {
	cfg := ina.NewConfig() // no fofa creds
	e := ina.NewEngine(cfg)
	if got := e.Sources(); len(got) != 0 {
		t.Errorf("expected zero sources without creds, got %v", got)
	}
	_, err := e.QueryRaw(context.Background(), "fofa", `domain="x"`)
	if err == nil {
		t.Fatal("expected ErrUnknownSource")
	}
}

func TestCodeStringFofa(t *testing.T) {
	c := ina.NewCode().And("domain", "example.com").And("icp", "京ICP123")
	got := c.String("fofa")
	want := `domain="example.com"&&icp="京ICP123"`
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	icoOnly := ina.NewCode().And("ico", "deadbeef")
	if got := icoOnly.String("fofa"); got != `icon_hash="deadbeef"` {
		t.Errorf("ico mapping wrong: %s", got)
	}
}

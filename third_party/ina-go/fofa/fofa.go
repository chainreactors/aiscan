// Package fofa 实现 fofa.info 的 ina-go engineClient。
// import 后通过 init() 自动注册到顶层门面, 上游 import _ "github.com/chainreactors/ina-go/fofa" 即可启用。
package fofa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	ina "github.com/chainreactors/ina-go"
)

const (
	sourceName  = "fofa"
	baseURL     = "https://api.fofa.info"
	pageSize    = 999
	queryFields = "host,ip,port,domain,title,icp"
	loginAPI    = "/api/v1/info/my"
	searchAPI   = "/api/v1/search/all"
)

func init() {
	ina.RegisterSource(sourceName, newClient)
}

// SetBaseURL 仅供测试覆盖 baseURL。生产代码不应调用。
var configuredBaseURL = baseURL

// engineClient 在 ina 包内是私有接口, 通过工厂函数注入。
type client struct {
	email   string
	key     string
	limit   int
	logger  ina.Logger
	http    *http.Client
	base    string
	gate    chan struct{} // 容量 1, 强制串行 (fofa 单 key 风控)
	loginMu sync.Mutex
	logged  bool
}

func newClient(cfg *ina.Config) ina.SourceClient {
	creds := cfg.Fofa()
	if creds.Email == "" || creds.Key == "" {
		return nil
	}
	return &client{
		email:  creds.Email,
		key:    creds.Key,
		limit:  cfg.Limit(),
		logger: cfg.Logger(),
		http:   cfg.HTTP(),
		base:   configuredBaseURL,
		gate:   make(chan struct{}, 1),
	}
}

func (c *client) Name() string { return sourceName }

func (c *client) acquire(ctx context.Context) error {
	select {
	case c.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *client) release() { <-c.gate }

func (c *client) CheckLogin(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	if c.logged {
		return nil
	}
	if err := c.acquire(ctx); err != nil {
		return err
	}
	defer c.release()
	body, err := c.do(ctx, loginAPI, nil)
	if err != nil {
		return err
	}
	var resp struct {
		Error  bool   `json:"error"`
		ErrMsg string `json:"errmsg"`
		IsVip  bool   `json:"isvip"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("fofa: parse login response: %w (body=%s)", err, truncate(body))
	}
	if resp.Error {
		return fmt.Errorf("fofa: login failed: %s", resp.ErrMsg)
	}
	if !resp.IsVip {
		// 非 vip 不算 fatal, fofa 允许 free 查询但配额低; 记 warn 但放行。
		c.logger.Warnf("source=fofa login=ok vip=false (free quota only)")
	}
	c.logged = true
	return nil
}

// Query 对外暴露的查询入口, 内部翻页, 总条数受 limit 控制。
func (c *client) Query(ctx context.Context, raw string) ([]ina.Asset, error) {
	if !c.logged {
		if err := c.CheckLogin(ctx); err != nil {
			return nil, err
		}
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	var all []ina.Asset
	for page := 1; ; page++ {
		batch, total, err := c.searchPage(ctx, encoded, page)
		if err != nil {
			return all, err
		}
		all = append(all, batch...)
		c.logger.Debugf("source=fofa page=%d batch=%d total=%d acc=%d", page, len(batch), total, len(all))

		if c.limit > 0 && len(all) >= c.limit {
			if len(all) > c.limit {
				all = all[:c.limit]
			}
			return all, nil
		}
		if len(batch) == 0 {
			return all, nil
		}
		// 与 Python 一致: 已取过半就停, 避免拉满 quota。
		if page*pageSize >= total/2 {
			return all, nil
		}
	}
}

func (c *client) searchPage(ctx context.Context, qbase64 string, page int) ([]ina.Asset, int, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, 0, err
	}
	defer c.release()

	params := url.Values{}
	params.Set("qbase64", qbase64)
	params.Set("page", strconv.Itoa(page))
	params.Set("size", strconv.Itoa(pageSize))
	params.Set("fields", queryFields)

	body, err := c.do(ctx, searchAPI, params)
	if err != nil {
		return nil, 0, err
	}

	var resp struct {
		Error   bool            `json:"error"`
		ErrMsg  string          `json:"errmsg"`
		Size    int             `json:"size"`
		Results [][]interface{} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("fofa: parse search response: %w (body=%s)", err, truncate(body))
	}
	if resp.Error {
		return nil, 0, fmt.Errorf("fofa: search failed: %s", resp.ErrMsg)
	}

	assets := make([]ina.Asset, 0, len(resp.Results))
	for _, row := range resp.Results {
		assets = append(assets, rowToAsset(row))
	}
	return assets, resp.Size, nil
}

// fields = host,ip,port,domain,title,icp 顺序
func rowToAsset(row []interface{}) ina.Asset {
	get := func(i int) string {
		if i >= len(row) || row[i] == nil {
			return ""
		}
		if s, ok := row[i].(string); ok {
			return s
		}
		return fmt.Sprint(row[i])
	}
	return ina.Asset{
		URL:    get(0),
		IP:     get(1),
		Port:   get(2),
		Domain: get(3),
		Title:  get(4),
		ICP:    get(5),
		Source: sourceName,
	}
}

func (c *client) do(ctx context.Context, api string, params url.Values) ([]byte, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("email", c.email)
	params.Set("key", c.key)

	u := c.base + api + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fofa: http: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fofa: read body: %w", err)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("fofa: server status %d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("fofa: client status %d: %s", resp.StatusCode, truncate(body))
	}
	return body, nil
}

func truncate(b []byte) string {
	s := string(b)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

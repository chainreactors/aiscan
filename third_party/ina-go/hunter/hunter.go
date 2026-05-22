// Package hunter implements the hunter.qianxin.com data source for ina-go.
//
// Hunter 有两种认证模式:
//
//	Token: POST /api/search, Authorization: <token> (Python hunter_token)
//	APIKey: GET /openApi/search?api-key=<key> (Python hunter_key)
//
// Token 优先 (前者抓包易得, 后者要后台生成)。两者都没配就不注册 client。
//
// Hunter 的 WAF 对短时间内多次请求很敏感, 每次 HTTP 调用前后强制 1 秒间隔
// (Python 原版 time.sleep(2), 这里 1s 已足够)。
package hunter

import (
	"bytes"
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
	"time"

	ina "github.com/chainreactors/ina-go"
)

const (
	sourceName = "hunter"
	baseURL    = "https://hunter.qianxin.com"
	tokenAPI   = "/api/search"
	keyAPI     = "/openApi/search"
	pageSize   = 100
)

var configuredBaseURL = baseURL

func init() {
	ina.RegisterSource(sourceName, newClient)
}

type authMode int

const (
	modeToken authMode = iota
	modeAPIKey
)

type client struct {
	mode    authMode
	token   string
	apiKey  string
	limit   int
	logger  ina.Logger
	http    *http.Client
	base    string
	gate    chan struct{}
	loginMu sync.Mutex
	logged  bool
	// minInterval 每次 HTTP 之间至少间隔多久, 防 WAF 拉黑。
	minInterval time.Duration
	lastCall    time.Time
}

func newClient(cfg *ina.Config) ina.SourceClient {
	creds := cfg.Hunter()
	if creds.Token == "" && creds.APIKey == "" {
		return nil
	}
	c := &client{
		token:       creds.Token,
		apiKey:      creds.APIKey,
		limit:       cfg.Limit(),
		logger:      cfg.Logger(),
		http:        cfg.HTTP(),
		base:        configuredBaseURL,
		gate:        make(chan struct{}, 1),
		minInterval: 1500 * time.Millisecond,
	}
	if creds.Token != "" {
		c.mode = modeToken
	} else {
		c.mode = modeAPIKey
	}
	return c
}

func (c *client) Name() string { return sourceName }

func (c *client) acquire(ctx context.Context) error {
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	// 强制 WAF 间隔: 距上次调用至少 minInterval。
	if elapsed := time.Since(c.lastCall); elapsed < c.minInterval {
		wait := c.minInterval - elapsed
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			<-c.gate
			return ctx.Err()
		}
	}
	return nil
}

func (c *client) release() {
	c.lastCall = time.Now()
	<-c.gate
}

func (c *client) CheckLogin(ctx context.Context) error {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	if c.logged {
		return nil
	}
	// 用一个 trivial 查询触发, code 200/400 都算认证通过 (400 = 参数错但 token 有效)。
	params := searchParams{
		Search:   base64.URLEncoding.EncodeToString([]byte(`web.body="check"`)),
		Page:     1,
		PageSize: 1,
		IsWeb:    3,
	}
	resp, err := c.do(ctx, params)
	if err != nil {
		return err
	}
	switch resp.Code {
	case 200, 400, 40205:
		c.logged = true
		c.logger.Infof("source=hunter login=ok mode=%s", c.modeName())
		return nil
	case 401, 4001:
		return fmt.Errorf("hunter: unauthorized (code=%d msg=%q)", resp.Code, resp.Message)
	default:
		return fmt.Errorf("hunter: login failed code=%d msg=%q", resp.Code, resp.Message)
	}
}

func (c *client) modeName() string {
	if c.mode == modeToken {
		return "token"
	}
	return "apikey"
}

func (c *client) Query(ctx context.Context, raw string) ([]ina.Asset, error) {
	if !c.logged {
		if err := c.CheckLogin(ctx); err != nil {
			return nil, err
		}
	}
	// Hunter 要求 urlsafe base64 (RFC 4648 base64url)。
	encoded := base64.URLEncoding.EncodeToString([]byte(raw))
	var all []ina.Asset
	for page := 1; ; page++ {
		params := searchParams{Search: encoded, Page: page, PageSize: pageSize, IsWeb: 3}
		resp, err := c.do(ctx, params)
		if err != nil {
			return all, err
		}
		if resp.Code != 200 && resp.Code != 40205 {
			return all, fmt.Errorf("hunter: search failed code=%d msg=%q", resp.Code, resp.Message)
		}
		for _, item := range resp.Data.Arr {
			all = append(all, recordToAsset(item))
		}
		c.logger.Debugf("source=hunter page=%d batch=%d total=%d acc=%d", page, len(resp.Data.Arr), resp.Data.Total, len(all))

		if c.limit > 0 && len(all) >= c.limit {
			if len(all) > c.limit {
				all = all[:c.limit]
			}
			return all, nil
		}
		if len(resp.Data.Arr) == 0 {
			return all, nil
		}
		if page*pageSize >= resp.Data.Total {
			return all, nil
		}
	}
}

// === Request / response shapes ===

type searchParams struct {
	Search   string `json:"search"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	IsWeb    int    `json:"is_web"`
}

type hunterRecord struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Domain    string `json:"domain"`
	Title     string `json:"title"`
	Number    string `json:"number"` // ICP
	Company   string `json:"company"`
	HTTPCode  any    `json:"http_code"`
	Component []struct {
		Name string `json:"name"`
	} `json:"component"`
	Protocol string `json:"protocol"`
}

type hunterResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Total int            `json:"total"`
		Arr   []hunterRecord `json:"arr"`
	} `json:"data"`
}

func (c *client) do(ctx context.Context, p searchParams) (*hunterResponse, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	// Hunter 要求 start_time / end_time, 否则 400 "输入参数有误"。默认查询过去 90 天。
	// 日期格式 YYYY-MM-DD (不带时间, 与官方 SDK hunter-sdk 对齐)。
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -90)
	fmtTime := "2006-01-02"

	var req *http.Request
	switch c.mode {
	case modeToken:
		body, _ := json.Marshal(map[string]any{
			"search":     p.Search,
			"page":       p.Page,
			"page_size":  p.PageSize,
			"is_web":     p.IsWeb,
			"start_time": startTime.Format(fmtTime),
			"end_time":   endTime.Format(fmtTime),
		})
		var err error
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, c.base+tokenAPI, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", c.token)
		req.Header.Set("Content-Type", "application/json")
	case modeAPIKey:
		q := url.Values{}
		q.Set("api-key", c.apiKey)
		q.Set("search", p.Search)
		q.Set("page", strconv.Itoa(p.Page))
		q.Set("page_size", strconv.Itoa(p.PageSize))
		q.Set("is_web", strconv.Itoa(p.IsWeb))
		q.Set("start_time", startTime.Format(fmtTime))
		q.Set("end_time", endTime.Format(fmtTime))
		var err error
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, c.base+keyAPI+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hunter: http: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hunter: read body: %w", err)
	}
	if resp.StatusCode == 403 {
		return nil, fmt.Errorf("hunter: 403 forbidden by WAF (rate-limited or bad UA); body=%s", truncate(body))
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("hunter: server %d", resp.StatusCode)
	}
	if resp.StatusCode >= 400 && resp.StatusCode != 400 {
		return nil, fmt.Errorf("hunter: status %d: %s", resp.StatusCode, truncate(body))
	}
	var out hunterResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("hunter: parse response: %w (body=%s)", err, truncate(body))
	}
	return &out, nil
}

func recordToAsset(r hunterRecord) ina.Asset {
	port := ""
	if r.Port > 0 {
		port = strconv.Itoa(r.Port)
	}
	host := r.Domain
	if host == "" {
		host = r.IP
	}
	u := ""
	if host != "" {
		// Match Python hunter/runner.py: always "http://<domain-or-ip>:<port>".
		u = fmt.Sprintf("http://%s:%s", host, port)
	}
	return ina.Asset{
		IP:      r.IP,
		Port:    port,
		URL:     u,
		Domain:  r.Domain,
		Title:   r.Title,
		ICP:     r.Number,
		Status:  valueToString(r.HTTPCode),
		Company: r.Company,
		Frame:   componentNames(r.Component),
		Source:  sourceName,
	}
}

func valueToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

func componentNames(components []struct {
	Name string `json:"name"`
}) string {
	if len(components) == 0 {
		return ""
	}
	names := make([]string, 0, len(components))
	for _, component := range components {
		if component.Name != "" {
			names = append(names, component.Name)
		}
	}
	return strings.Join(names, ", ")
}

func truncate(b []byte) string {
	s := string(b)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

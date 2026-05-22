// Package tycunauth implements the tyc_unauth data source for ani-go.
//
// 数据来源: 天眼查微信小程序后端 (api9.tianyancha.com, capi.tianyancha.com)。
// 不需要任何 cookie / token, 通过 WeChat mini-program 公开 JSON API。
//
// 三个 API:
//
//	POST https://api9.tianyancha.com/cloud-tempest/app/searchCompany/   公司名 → tycid
//	POST https://capi.tianyancha.com/.../investListV2/                   tycid → 投资子公司
//	GET  https://capi.tianyancha.com/.../icpRecordList/                  tycid → ICP / 域名
package tycunauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	ani "github.com/chainreactors/ani-go"
)

const (
	sourceName = "tyc_unauth"
	searchAPI  = "https://api9.tianyancha.com/cloud-tempest/app/searchCompany/"
	investAPI  = "https://capi.tianyancha.com/cloud-company-background/company/investListV2/"
	icpAPI     = "https://capi.tianyancha.com/cloud-intellectual-property/intellectualProperty/icpRecordList/"
)

// 测试可覆盖的 endpoint。
var (
	configuredSearchAPI = searchAPI
	configuredInvestAPI = investAPI
	configuredICPAPI    = icpAPI
)

func init() {
	ani.RegisterSource(sourceName, newClient)
}

type client struct {
	logger ani.Logger
	http   *http.Client
	gate   chan struct{}
}

func newClient(cfg *ani.Config) ani.SourceClient {
	return &client{
		logger: cfg.Logger(),
		http:   cfg.HTTP(),
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

// headers 直接照搬 Python tyc_unauth.py: WeChat 小程序 UA + Version + 伪造 IP 头
// 这套头是天眼查公开 JSON API 的默认入口, 不带这些会被反爬拦截。
func (c *client) headers() http.Header {
	h := http.Header{}
	h.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X) AppleWebKit/539 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36 MicroMessenger/6.8.0(0x16080000) NetType/WIFI MiniProgramEnv/Mac MacWechat/WMPF MacWechat/3.8.6(0x13080612) XWEB/1156")
	h.Set("X-Forwarded-For", "127.0.0.1")
	h.Set("X-Originating-IP", "127.0.0.1")
	h.Set("X-Remote-Addr", "127.0.0.1")
	h.Set("X-Remote-IP", "127.0.0.1")
	h.Set("Content-Type", "application/json")
	h.Set("Referer", "https://tianyancha.com/")
	h.Set("Version", "TYC-XCX-WEB")
	h.Set("Pragma", "no-cache")
	return h
}

type queueItem struct {
	name    string
	tycid   string
	parent  string
	percent float64
	depth   int
}

// Run BFS 投资链路扩展, 每层抓 ICP。
func (c *client) Run(ctx context.Context, name string, depth int, percent float64) ([]ani.CompanyAsset, error) {
	root, err := c.searchCompany(ctx, name)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("%w: %s", ani.ErrCompanyNotFound, name)
	}

	visited := map[string]bool{root.Name: true}
	queue := []queueItem{{name: root.Name, tycid: root.ID, parent: "", percent: 1.0, depth: 0}}

	var assets []ani.CompanyAsset
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		icps, err := c.fetchICPs(ctx, item.tycid)
		if err != nil {
			c.logger.Warnf("source=tyc_unauth tycid=%s icp_err=%q", item.tycid, err)
		}
		if len(icps) == 0 {
			assets = append(assets, ani.CompanyAsset{
				Name: item.name, PID: item.tycid, Parent: item.parent,
				Percent: item.percent, Depth: item.depth, Source: sourceName,
			})
		}
		for _, ic := range icps {
			assets = append(assets, ani.CompanyAsset{
				Name: item.name, PID: item.tycid,
				ICP: ic.ICP, Domain: ic.Domain, Title: ic.Title,
				Parent: item.parent, Percent: item.percent, Depth: item.depth,
				Source: sourceName,
			})
		}

		if item.depth >= depth {
			continue
		}
		invests, err := c.fetchInvestments(ctx, item.tycid)
		if err != nil {
			c.logger.Warnf("source=tyc_unauth tycid=%s invest_err=%q", item.tycid, err)
			continue
		}
		for _, inv := range invests {
			if inv.Percent < percent {
				continue
			}
			if visited[inv.Name] {
				continue
			}
			visited[inv.Name] = true
			queue = append(queue, queueItem{
				name: inv.Name, tycid: inv.ID, parent: item.name,
				percent: inv.Percent, depth: item.depth + 1,
			})
		}
	}
	return assets, nil
}

type searchHit struct {
	Name string
	ID   string
}

func (c *client) searchCompany(ctx context.Context, name string) (*searchHit, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	body := map[string]any{
		"word":              name,
		"pageNum":           1,
		"pageSize":          20,
		"sortType":          0,
		"allowModifyQuery":  1,
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, configuredSearchAPI, bytes.NewReader(raw))
	req.Header = c.headers()

	respBody, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("search company: %w", err)
	}
	var resp struct {
		Data struct {
			CompanyList []struct {
				ID   any    `json:"id"` // tyc 偶尔会返回字符串或数字
				Name string `json:"name"`
			} `json:"companyList"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode search: %w", err)
	}
	if len(resp.Data.CompanyList) == 0 {
		return nil, nil
	}
	hit := resp.Data.CompanyList[0]
	return &searchHit{
		Name: stripEmTags(hit.Name),
		ID:   anyToString(hit.ID),
	}, nil
}

type icpRecord struct {
	ICP    string
	Domain string
	Title  string
}

func (c *client) fetchICPs(ctx context.Context, tycid string) ([]icpRecord, error) {
	var out []icpRecord
	for page := 1; ; page++ {
		batch, pageTotal, err := c.fetchICPPage(ctx, tycid, page)
		if err != nil {
			return out, err
		}
		out = append(out, batch...)
		if page >= pageTotal {
			break
		}
	}
	return out, nil
}

func (c *client) fetchICPPage(ctx context.Context, tycid string, page int) ([]icpRecord, int, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, 0, err
	}
	defer c.release()

	q := url.Values{}
	q.Set("pageNum", strconv.Itoa(page))
	q.Set("pageSize", "30")
	q.Set("graphId", tycid)
	q.Set("gid", tycid)
	q.Set("id", tycid)
	q.Set("_", strconv.FormatInt(time.Now().Unix(), 10))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, configuredICPAPI+"?"+q.Encode(), nil)
	req.Header = c.headers()

	body, err := c.do(req)
	if err != nil {
		return nil, 0, err
	}
	var resp struct {
		Data struct {
			PageTotal int `json:"pageTotal"`
			Item      []struct {
				Liscense string `json:"liscense"`
				Ym       string `json:"ym"`
				WebName  string `json:"webName"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("decode icp: %w", err)
	}
	out := make([]icpRecord, 0, len(resp.Data.Item))
	for _, item := range resp.Data.Item {
		if item.Liscense == "" {
			continue
		}
		out = append(out, icpRecord{
			ICP:    icpFormat(item.Liscense),
			Domain: item.Ym,
			Title:  item.WebName,
		})
	}
	total := resp.Data.PageTotal
	if total == 0 {
		total = 1
	}
	return out, total, nil
}

type investHit struct {
	Name    string
	ID      string
	Percent float64
}

func (c *client) fetchInvestments(ctx context.Context, tycid string) ([]investHit, error) {
	var out []investHit
	for page := 1; ; page++ {
		batch, total, err := c.fetchInvestPage(ctx, tycid, page)
		if err != nil {
			return out, err
		}
		out = append(out, batch...)
		if page >= total {
			break
		}
	}
	return out, nil
}

func (c *client) fetchInvestPage(ctx context.Context, tycid string, page int) ([]investHit, int, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, 0, err
	}
	defer c.release()

	body := map[string]any{
		"gid":             tycid,
		"pageSize":        30,
		"pageNum":         page,
		"percentLevel":    "-100",
		"registation":     "-100",
		"province":        "-100",
		"category":        "-100",
		"fullSearchText":  "",
	}
	raw, _ := json.Marshal(body)
	api := configuredInvestAPI + "?_=" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, api, bytes.NewReader(raw))
	req.Header = c.headers()

	respBody, err := c.do(req)
	if err != nil {
		return nil, 0, err
	}
	var resp struct {
		Data struct {
			Total  int `json:"total"`
			Result []struct {
				Name    string `json:"name"`
				ID      any    `json:"id"`
				Percent string `json:"percent"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, 0, fmt.Errorf("decode invest: %w", err)
	}
	out := make([]investHit, 0, len(resp.Data.Result))
	for _, item := range resp.Data.Result {
		out = append(out, investHit{
			Name:    stripEmTags(item.Name),
			ID:      anyToString(item.ID),
			Percent: percentToFloat(item.Percent),
		})
	}
	total := resp.Data.Total
	if total == 0 {
		total = 1
	}
	return out, total, nil
}

func (c *client) do(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == 429 || strings.Contains(string(body), "请进行身份验证以继续使用") {
		return nil, fmt.Errorf("tyc 触发机器人验证 / 429 (api=%s)", req.URL)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(body))
	}
	return body, nil
}

// === helpers ===

func icpFormat(s string) string {
	parts := strings.Split(s, "-")
	if len(parts) >= 1 {
		return parts[0]
	}
	return s
}

func percentToFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0
	}
	s = strings.TrimSuffix(s, "%")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v / 100
}

// tyc 返回 name 字段里嵌 <em>...</em> 高亮, 剥掉。
func stripEmTags(s string) string {
	s = strings.ReplaceAll(s, "<em>", "")
	s = strings.ReplaceAll(s, "</em>", "")
	return s
}

func anyToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return string(t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func truncate(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

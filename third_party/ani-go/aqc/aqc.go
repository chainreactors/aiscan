// Package aqc implements the aqc_unauth (qiye.baidu.com) data source for ani-go.
//
// 三个 API:
//
//	POST https://xunkebao.baidu.com/...  搜公司名 → sourceId (pid)
//	GET  https://qiye.baidu.com/cs/icpInfoAjax  pid → ICP 列表
//	GET  https://aqc.baidu.com/stockchart/stockchartAjax  pid → 子公司列表
package aqc

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

	ani "github.com/chainreactors/ani-go"
)

const (
	sourceName = "aqc_unauth"
	searchAPI  = "https://xunkebao.baidu.com/crm/web/dgtsale/bizcrm/enterprise/na/search"
	icpAPI     = "https://qiye.baidu.com/cs/icpInfoAjax"
	investAPI  = "https://aqc.baidu.com/stockchart/stockchartAjax"
)

// configuredBaseSearch 等三个可由测试覆盖, 生产用上面的常量。
var (
	configuredSearchAPI = searchAPI
	configuredICPAPI    = icpAPI
	configuredInvestAPI = investAPI
)

func init() {
	ani.RegisterSource(sourceName, newClient)
}

type client struct {
	logger ani.Logger
	http   *http.Client
	gate   chan struct{}
	ua     string
}

func newClient(cfg *ani.Config) ani.SourceClient {
	return &client{
		logger: cfg.Logger(),
		http:   cfg.HTTP(),
		gate:   make(chan struct{}, 1),
		ua:     "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
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

// Run 实现投资链路递归扩展, BFS。
func (c *client) Run(ctx context.Context, name string, depth int, percent float64) ([]ani.CompanyAsset, error) {
	root, err := c.searchCompany(ctx, name)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("%w: %s", ani.ErrCompanyNotFound, name)
	}

	type queueItem struct {
		name    string
		pid     string
		parent  string
		percent float64
		depth   int
	}

	visited := map[string]bool{root.Name: true}
	queue := []queueItem{{name: root.Name, pid: root.SourceID, parent: "", percent: 1.0, depth: 0}}

	var assets []ani.CompanyAsset
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		// 抓 ICP
		icps, err := c.fetchICPs(ctx, item.pid)
		if err != nil {
			c.logger.Warnf("source=aqc_unauth pid=%s icp_err=%q", item.pid, err)
		}
		if len(icps) == 0 {
			// 公司没有 ICP 也保留一条占位记录, 便于上游知道这家公司存在。
			assets = append(assets, ani.CompanyAsset{
				Name: item.name, PID: item.pid, Parent: item.parent,
				Percent: item.percent, Depth: item.depth, Source: sourceName,
			})
		}
		for _, ic := range icps {
			assets = append(assets, ani.CompanyAsset{
				Name: item.name, PID: item.pid,
				ICP: ic.ICP, Domain: ic.Domain, Title: ic.Title,
				Parent: item.parent, Percent: item.percent, Depth: item.depth,
				Source: sourceName,
			})
		}

		// 递归子公司
		if item.depth >= depth {
			continue
		}
		invests, err := c.fetchInvestments(ctx, item.pid)
		if err != nil {
			c.logger.Warnf("source=aqc_unauth pid=%s invest_err=%q", item.pid, err)
			continue
		}
		for _, inv := range invests {
			if inv.RegRate < percent {
				continue
			}
			if visited[inv.EntName] {
				continue
			}
			visited[inv.EntName] = true
			queue = append(queue, queueItem{
				name: inv.EntName, pid: inv.PID, parent: item.name,
				percent: inv.RegRate, depth: item.depth + 1,
			})
		}
	}
	return assets, nil
}

// === Source-specific HTTP wrappers ===

type searchHit struct {
	Name     string
	SourceID string
}

func (c *client) searchCompany(ctx context.Context, name string) (*searchHit, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	body := map[string]any{
		"params": map[string]any{
			"pageNum":        1,
			"pageSize":       100,
			"searchTypeCode": "name",
			"searchValue":    name,
		},
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, configuredSearchAPI, bytes.NewReader(raw))
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")

	respBody, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("search company: %w", err)
	}
	var resp struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			DataList []struct {
				Name     string `json:"name"`
				SourceID string `json:"sourceId"`
			} `json:"dataList"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	if resp.Code != "0" {
		return nil, fmt.Errorf("search failed: %s", resp.Msg)
	}
	if len(resp.Data.DataList) == 0 {
		return nil, nil
	}
	hit := resp.Data.DataList[0]
	return &searchHit{Name: hit.Name, SourceID: hit.SourceID}, nil
}

type icpRecord struct {
	ICP    string
	Domain string
	Title  string
}

func (c *client) fetchICPs(ctx context.Context, pid string) ([]icpRecord, error) {
	var out []icpRecord
	for page := 1; ; page++ {
		batch, total, pageCount, err := c.fetchICPPage(ctx, pid, page)
		if err != nil {
			return out, err
		}
		out = append(out, batch...)
		_ = total
		if page >= pageCount {
			break
		}
	}
	return out, nil
}

func (c *client) fetchICPPage(ctx context.Context, pid string, page int) ([]icpRecord, int, int, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, 0, 0, err
	}
	defer c.release()

	q := url.Values{}
	q.Set("p", strconv.Itoa(page))
	q.Set("size", "100")
	q.Set("pid", pid)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, configuredICPAPI+"?"+q.Encode(), nil)
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Referer", "https://qiye.baidu.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	body, err := c.do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	var resp struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
		Data   struct {
			Total     int `json:"total"`
			PageCount int `json:"pageCount"`
			List      []struct {
				IcpNo    string   `json:"icpNo"`
				SiteName string   `json:"siteName"`
				Domain   []string `json:"domain"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, 0, fmt.Errorf("decode icp: %w", err)
	}
	if resp.Status != 0 {
		return nil, 0, 0, fmt.Errorf("icp api: %s", resp.Msg)
	}
	out := make([]icpRecord, 0, len(resp.Data.List))
	for _, item := range resp.Data.List {
		if item.IcpNo == "" {
			continue
		}
		dom := ""
		if len(item.Domain) > 0 {
			dom = item.Domain[0]
		}
		out = append(out, icpRecord{ICP: icpFormat(item.IcpNo), Domain: dom, Title: item.SiteName})
	}
	if resp.Data.PageCount == 0 {
		resp.Data.PageCount = 1
	}
	return out, resp.Data.Total, resp.Data.PageCount, nil
}

type investHit struct {
	EntName string
	PID     string
	RegRate float64
}

func (c *client) fetchInvestments(ctx context.Context, pid string) ([]investHit, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	q := url.Values{}
	q.Set("pid", pid)
	q.Set("drill", "2")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, configuredInvestAPI+"?"+q.Encode(), nil)
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Referer", "https://qiye.baidu.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Status int    `json:"status"`
		Msg    string `json:"msg"`
		Data   struct {
			InvestRecordData struct {
				List []struct {
					EntName string `json:"entName"`
					PID     string `json:"pid"`
					RegRate string `json:"regRate"`
				} `json:"list"`
			} `json:"investRecordData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode invest: %w", err)
	}
	if resp.Status != 0 {
		return nil, fmt.Errorf("invest api: %s", resp.Msg)
	}
	out := make([]investHit, 0, len(resp.Data.InvestRecordData.List))
	for _, item := range resp.Data.InvestRecordData.List {
		out = append(out, investHit{EntName: item.EntName, PID: item.PID, RegRate: percentToFloat(item.RegRate)})
	}
	return out, nil
}

// === helpers ===

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
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(body))
	}
	return body, nil
}

// icpFormat: '京ICP备12345号-2' → '京ICP备12345号' (剥掉子号)
func icpFormat(s string) string {
	parts := strings.Split(s, "-")
	if len(parts) >= 1 {
		return parts[0]
	}
	return s
}

// percentToFloat: "33.33%" → 0.3333; "" / "-" → 0.0
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

func truncate(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}


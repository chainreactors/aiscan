// Package qcc implements the 企查查 (qcc.com) data source for ani-go.
//
// 凭证: Config.WithQccCookie("QCCSESSID=..."). 空字符串时不注册该 source。
//
// 三个 API:
//
//	GET  https://www.qcc.com/web/search?key=NAME            HTML 公司名 → keyNo
//	POST https://www.qcc.com/api/charts/getHoldingCompany    JSON keyNo → 子公司
//	GET  https://www.qcc.com/api/bigsearch/net               JSON 公司名 → ICP 列表 (清爽路线)
//
// Python core/qcc.py 用 HTML 抓取 /company_getinfos 拿 ICP; 这里换成更稳定的
// bigsearch/net JSON API (Python 也有 qcc_request_beian 静态方法用同一接口)。
package qcc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	ani "github.com/chainreactors/ani-go"
)

const (
	sourceName    = "qcc"
	searchURL     = "https://www.qcc.com/web/search"
	holdingAPI    = "https://www.qcc.com/api/charts/getHoldingCompany"
	bigsearchAPI  = "https://www.qcc.com/api/bigsearch/net"
	firmHrefMatch = "https://www.qcc.com/firm/"
)

var (
	configuredSearchURL    = searchURL
	configuredHoldingAPI   = holdingAPI
	configuredBigsearchAPI = bigsearchAPI
)

func init() {
	ani.RegisterSource(sourceName, newClient)
}

type client struct {
	logger ani.Logger
	http   *http.Client
	gate   chan struct{}
	cookie string
}

func newClient(cfg *ani.Config) ani.SourceClient {
	cookie := cfg.QccCookie()
	if strings.TrimSpace(cookie) == "" {
		return nil
	}
	return &client{
		logger: cfg.Logger(),
		http:   cfg.HTTP(),
		gate:   make(chan struct{}, 1),
		cookie: cookie,
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

func (c *client) prepRequest(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:78.0) Gecko/20100101 Firefox/78.0")
	req.Header.Set("Cookie", c.cookie)
	req.Header.Set("Accept", "application/json, text/plain, */*")
}

type queueItem struct {
	name    string
	keyNo   string
	parent  string
	percent float64
	depth   int
}

func (c *client) Run(ctx context.Context, name string, depth int, percent float64) ([]ani.CompanyAsset, error) {
	root, err := c.searchCompany(ctx, name)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("%w: %s", ani.ErrCompanyNotFound, name)
	}

	visited := map[string]bool{root.Name: true}
	queue := []queueItem{{name: root.Name, keyNo: root.KeyNo, parent: "", percent: 1.0, depth: 0}}

	var assets []ani.CompanyAsset
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		icps, err := c.fetchICPs(ctx, item.name)
		if err != nil {
			c.logger.Warnf("source=qcc name=%s icp_err=%q", item.name, err)
		}
		if len(icps) == 0 {
			assets = append(assets, ani.CompanyAsset{
				Name: item.name, PID: item.keyNo, Parent: item.parent,
				Percent: item.percent, Depth: item.depth, Source: sourceName,
			})
		}
		for _, ic := range icps {
			assets = append(assets, ani.CompanyAsset{
				Name: item.name, PID: item.keyNo,
				ICP: ic.ICP, Domain: ic.Domain, Title: ic.Title,
				Parent: item.parent, Percent: item.percent, Depth: item.depth,
				Source: sourceName,
			})
		}

		if item.depth >= depth {
			continue
		}
		invests, err := c.fetchHolding(ctx, item.keyNo)
		if err != nil {
			c.logger.Warnf("source=qcc keyNo=%s holding_err=%q", item.keyNo, err)
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
				name: inv.Name, keyNo: inv.KeyNo, parent: item.name,
				percent: inv.Percent, depth: item.depth + 1,
			})
		}
	}
	return assets, nil
}

type searchHit struct {
	Name  string
	KeyNo string
}

// searchCompany 用 /web/search HTML 找第一个 a.title 链接, href 形如
// https://www.qcc.com/firm/<keyNo>.html。
func (c *client) searchCompany(ctx context.Context, name string) (*searchHit, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	q := url.Values{}
	q.Set("key", name)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, configuredSearchURL+"?"+q.Encode(), nil)
	c.prepRequest(req)
	body, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("search company: %w", err)
	}

	doc := ani.ParseHTML(string(body))
	if doc == nil {
		return nil, fmt.Errorf("search: parse html")
	}
	// XPath: //a[@class="title copy-value"]/@href
	links := ani.FindNodes(doc, "a", "class", "title")
	for _, link := range links {
		href := ani.Attr(link, "href")
		if !strings.HasPrefix(href, firmHrefMatch) {
			continue
		}
		text := strings.TrimSpace(ani.NodeText(link))
		keyNo := strings.TrimSuffix(strings.TrimPrefix(href, firmHrefMatch), ".html")
		if keyNo == "" {
			continue
		}
		return &searchHit{Name: text, KeyNo: keyNo}, nil
	}
	return nil, nil
}

type holdingHit struct {
	Name    string
	KeyNo   string
	Percent float64
}

// fetchHolding POST /api/charts/getHoldingCompany JSON {"keyNo": ...}
func (c *client) fetchHolding(ctx context.Context, keyNo string) ([]holdingHit, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	raw, _ := json.Marshal(map[string]string{"keyNo": keyNo})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, configuredHoldingAPI, bytes.NewReader(raw))
	c.prepRequest(req)
	req.Header.Set("Content-Type", "application/json")
	body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Status int `json:"Status"`
		Result struct {
			Names []struct {
				Name         string `json:"Name"`
				KeyNo        string `json:"KeyNo"`
				PercentTotal string `json:"PercentTotal"`
			} `json:"Names"`
		} `json:"Result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode holding: %w", err)
	}
	if resp.Status == 201 {
		// 201 = 无子公司, Python 视为正常返回。
		return nil, nil
	}
	if resp.Status != 200 {
		return nil, fmt.Errorf("qcc holding status=%d", resp.Status)
	}
	out := make([]holdingHit, 0, len(resp.Result.Names))
	for _, item := range resp.Result.Names {
		out = append(out, holdingHit{
			Name:    item.Name,
			KeyNo:   item.KeyNo,
			Percent: percentToFloat(item.PercentTotal),
		})
	}
	return out, nil
}

type icpRecord struct {
	ICP    string
	Domain string
	Title  string
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

// fetchICPs GET /api/bigsearch/net?searchKey=NAME&pageIndex=N&pageSize=20
// 翻页直到 Paging.TotalRecords 全部覆盖。
func (c *client) fetchICPs(ctx context.Context, name string) ([]icpRecord, error) {
	var out []icpRecord
	for page := 1; ; page++ {
		batch, total, err := c.fetchICPPage(ctx, name, page)
		if err != nil {
			return out, err
		}
		out = append(out, batch...)
		// 已取完全部
		if page*20 >= total {
			break
		}
	}
	return out, nil
}

func (c *client) fetchICPPage(ctx context.Context, name string, page int) ([]icpRecord, int, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, 0, err
	}
	defer c.release()

	q := url.Values{}
	q.Set("pageIndex", strconv.Itoa(page))
	q.Set("pageSize", "20")
	q.Set("searchKey", name)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, configuredBigsearchAPI+"?"+q.Encode(), nil)
	c.prepRequest(req)
	req.Header.Set("Referer", "https://www.qcc.com/web_net")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	body, err := c.do(req)
	if err != nil {
		return nil, 0, err
	}
	var resp struct {
		Status int `json:"Status"`
		Result []struct {
			ICPNo       string `json:"ICPNo"`
			DomainName  string `json:"DomainName"`
			WebSiteName string `json:"WebSiteName"`
		} `json:"Result"`
		Paging struct {
			TotalRecords int `json:"TotalRecords"`
		} `json:"Paging"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("decode bigsearch: %w", err)
	}
	if resp.Status != 200 {
		return nil, 0, fmt.Errorf("qcc bigsearch status=%d", resp.Status)
	}
	out := make([]icpRecord, 0, len(resp.Result))
	for _, item := range resp.Result {
		if item.ICPNo == "" {
			continue
		}
		out = append(out, icpRecord{
			ICP:    icpFormat(item.ICPNo),
			Domain: item.DomainName,
			Title:  htmlTagRe.ReplaceAllString(item.WebSiteName, ""),
		})
	}
	return out, resp.Paging.TotalRecords, nil
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
	switch {
	case resp.StatusCode == 401:
		return nil, fmt.Errorf("qcc cookie 过期 (401)")
	case resp.StatusCode == 403:
		return nil, fmt.Errorf("qcc IP 被禁/海外 (403)")
	case resp.StatusCode == 302:
		return nil, fmt.Errorf("qcc 免费额度用光, 需要更换 cookie (302)")
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("qcc http %d: %s", resp.StatusCode, truncate(body))
	}
	return body, nil
}

// === helpers ===

func icpFormat(s string) string {
	parts := strings.Split(s, "-")
	if len(parts) >= 1 {
		return strings.TrimSpace(parts[0])
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

func truncate(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// Package tyc implements the authenticated 天眼查 (www.tianyancha.com) data source.
//
// 这是 tyc_unauth 的高配版: 用 auth_token JWT cookie 提升单 IP 限额,
// 走 www.tianyancha.com 的 HTML 页面而不是 api9 小程序 JSON。
// 实现照搬 Python core/tyc.py (HTML XPath 抓取)。
//
// 凭证: Config.WithTycToken("eyJ...JWT...")。空字符串时不注册该 source。
package tyc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	ani "github.com/chainreactors/ani-go"
	"golang.org/x/net/html"
)

const (
	sourceName = "tyc"
	searchURL  = "https://www.tianyancha.com/search"
	investURL  = "https://www.tianyancha.com/pagination/invest.xhtml"
	icpURL     = "https://www.tianyancha.com/pagination/icp.xhtml"
)

var (
	configuredSearchURL = searchURL
	configuredInvestURL = investURL
	configuredICPURL    = icpURL
)

func init() {
	ani.RegisterSource(sourceName, newClient)
}

type client struct {
	logger ani.Logger
	http   *http.Client
	gate   chan struct{}
	token  string
}

// 凭证缺失返回 nil → Engine 不会注册 tyc 源。
func newClient(cfg *ani.Config) ani.SourceClient {
	token := cfg.TycToken()
	if strings.TrimSpace(token) == "" {
		return nil
	}
	return &client{
		logger: cfg.Logger(),
		http:   cfg.HTTP(),
		gate:   make(chan struct{}, 1),
		token:  token,
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
	req.AddCookie(&http.Cookie{Name: "auth_token", Value: c.token})
}

type queueItem struct {
	name    string
	tycid   string
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
	queue := []queueItem{{name: root.Name, tycid: root.ID, parent: "", percent: 1.0, depth: 0}}

	var assets []ani.CompanyAsset
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		icps, err := c.fetchICPs(ctx, item.tycid)
		if err != nil {
			c.logger.Warnf("source=tyc tycid=%s icp_err=%q", item.tycid, err)
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
			c.logger.Warnf("source=tyc tycid=%s invest_err=%q", item.tycid, err)
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

// searchCompany 抓取 /search?key=NAME, 取第一个 div.index_name a 的 href 末段为 tycid。
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
	// XPath: //div[contains(@class,'index_name')]
	hits := ani.FindNodes(doc, "div", "class", "index_name")
	if len(hits) == 0 {
		return nil, nil
	}
	first := hits[0]
	links := ani.FindAllByTag(first, "a")
	if len(links) == 0 {
		return nil, nil
	}
	href := ani.Attr(links[0], "href")
	id := strings.TrimPrefix(href, "https://www.tianyancha.com/company/")
	if id == "" || id == href {
		// 兼容 //www.tianyancha.com/company/123 等相对地址
		id = pathLast(href)
	}
	return &searchHit{Name: ani.NodeText(first), ID: id}, nil
}

type icpRecord struct {
	ICP    string
	Domain string
	Title  string
}

// fetchICPs 翻页 /pagination/icp.xhtml?ps=30&pn=N&id=...
func (c *client) fetchICPs(ctx context.Context, tycid string) ([]icpRecord, error) {
	var out []icpRecord
	for page := 1; ; page++ {
		batch, totalPages, err := c.fetchICPPage(ctx, tycid, page)
		if err != nil {
			return out, err
		}
		out = append(out, batch...)
		if page >= totalPages {
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
	q.Set("ps", "30")
	q.Set("pn", strconv.Itoa(page))
	q.Set("id", tycid)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, configuredICPURL+"?"+q.Encode(), nil)
	c.prepRequest(req)
	body, err := c.do(req)
	if err != nil {
		return nil, 0, err
	}

	doc := ani.ParseHTML(string(body))
	if doc == nil {
		return nil, 1, nil
	}

	// Python 用 XPath: //tbody/tr 取每列。Go 走相同结构。
	rows := ani.FindAllByTag(doc, "tr")
	var out []icpRecord
	for _, row := range rows {
		tds := tdChildren(row)
		if len(tds) < 6 {
			continue
		}
		title := ani.NodeText(firstByTag(tds[2], "span"))
		domain := ani.NodeText(tds[4])
		icp := ani.NodeText(firstByTag(tds[5], "span"))
		if icp == "" || domain == "" {
			continue
		}
		out = append(out, icpRecord{ICP: icpFormat(icp), Domain: domain, Title: title})
	}

	// page-total 在 //ul/@page-total
	totalPages := 1
	for _, ul := range ani.FindAllByTag(doc, "ul") {
		if v := ani.Attr(ul, "page-total"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				totalPages = n
				break
			}
		}
	}
	return out, totalPages, nil
}

type investHit struct {
	Name    string
	ID      string
	Percent float64
}

// fetchInvestments 翻页 /pagination/invest.xhtml?ps=30&pn=N&id=...
func (c *client) fetchInvestments(ctx context.Context, tycid string) ([]investHit, error) {
	var out []investHit
	for page := 1; ; page++ {
		batch, more, err := c.fetchInvestPage(ctx, tycid, page)
		if err != nil {
			return out, err
		}
		out = append(out, batch...)
		if !more {
			break
		}
	}
	return out, nil
}

func (c *client) fetchInvestPage(ctx context.Context, tycid string, page int) ([]investHit, bool, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, false, err
	}
	defer c.release()

	q := url.Values{}
	q.Set("ps", "30")
	q.Set("pn", strconv.Itoa(page))
	q.Set("id", tycid)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, configuredInvestURL+"?"+q.Encode(), nil)
	c.prepRequest(req)
	body, err := c.do(req)
	if err != nil {
		return nil, false, err
	}

	doc := ani.ParseHTML(string(body))
	if doc == nil {
		return nil, false, nil
	}

	rows := ani.FindAllByTag(doc, "tr")
	var out []investHit
	for _, row := range rows {
		tds := tdChildren(row)
		if len(tds) < 6 {
			continue
		}
		// 公司链接 + 名称在 td[1]/table/tr/td[1]/div/a (Python: td[2]/table/tr/td[2]/div/a)
		// 简化策略: 在 row 内取第一个 a, 它的 text 是公司名, href 末段是 tycid。
		inner := firstByTag(row, "a")
		if inner == nil {
			continue
		}
		name := strings.TrimSpace(ani.NodeText(inner))
		href := ani.Attr(inner, "href")
		id := pathLast(href)
		// 投资比例在最后一列 (Python 用 td[6])
		percent := percentToFloat(ani.NodeText(tds[len(tds)-1]))
		if name == "" || id == "" {
			continue
		}
		out = append(out, investHit{Name: name, ID: id, Percent: percent})
	}
	// Python: 翻下一页只在本页数据满 30 行且最后一条投资比例 >= 阈值。
	// 阈值过滤交给 Run 处理, 这里只看是否还有满页。
	more := len(rows) >= 30
	return out, more, nil
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
	text := string(body)
	switch {
	case resp.StatusCode == 401:
		return nil, fmt.Errorf("tyc auth_token 过期或无效 (401)")
	case resp.StatusCode == 403:
		return nil, fmt.Errorf("tyc IP 被禁/海外 (403)")
	case resp.StatusCode == 429:
		return nil, fmt.Errorf("tyc rate-limit (429)")
	case strings.Contains(text, "请进行身份验证以继续使用"):
		return nil, fmt.Errorf("tyc 触发机器人验证 (api=%s)", req.URL)
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("tyc http %d: %s", resp.StatusCode, truncate(body))
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

func pathLast(href string) string {
	href = strings.TrimSuffix(href, "/")
	if i := strings.LastIndex(href, "/"); i >= 0 {
		return href[i+1:]
	}
	return href
}

// tdChildren 返回 row 的直接 td 子节点 (按出现顺序)。
func tdChildren(row *html.Node) []*html.Node {
	var out []*html.Node
	for c := row.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "td" {
			out = append(out, c)
		}
	}
	return out
}

func firstByTag(n *html.Node, tag string) *html.Node {
	if n == nil {
		return nil
	}
	hits := ani.FindAllByTag(n, tag)
	if len(hits) == 0 {
		return nil
	}
	return hits[0]
}

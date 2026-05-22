// authed aqc (爱企查) 源 — 与 aqc_unauth 同一 invest API, 但搜索/ICP 走
// aiqicha.baidu.com 的 HTML 页面 (需要 BAIDUID cookie 才能稳定返回数据)。
// ICP 数据从 beian.tianyancha.com 抓 — 那个无需任何凭证。

package aqc

import (
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
	"golang.org/x/net/html"
)

const (
	authSourceName = "aqc"
	aqcSearchURL   = "https://aiqicha.baidu.com/s"
	beianURL       = "https://beian.tianyancha.com/search"
)

var (
	configuredAqcSearchURL = aqcSearchURL
	configuredBeianURL     = beianURL
)

func init() {
	ani.RegisterSource(authSourceName, newAuthClient)
}

type authClient struct {
	logger ani.Logger
	http   *http.Client
	gate   chan struct{}
	cookie string
}

// 凭证缺失 → 不注册 aqc 源 (但 aqc_unauth 仍可用)。
func newAuthClient(cfg *ani.Config) ani.SourceClient {
	cookie := cfg.AqcCookie()
	if strings.TrimSpace(cookie) == "" {
		return nil
	}
	return &authClient{
		logger: cfg.Logger(),
		http:   cfg.HTTP(),
		gate:   make(chan struct{}, 1),
		cookie: cookie,
	}
}

func (c *authClient) Name() string { return authSourceName }

func (c *authClient) acquire(ctx context.Context) error {
	select {
	case c.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *authClient) release() { <-c.gate }

func (c *authClient) prepRequest(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://aifanfan.baidu.com/")
	req.Header.Set("Cookie", c.cookie)
}

type authQueueItem struct {
	name    string
	pid     string
	parent  string
	percent float64
	depth   int
}

func (c *authClient) Run(ctx context.Context, name string, depth int, percent float64) ([]ani.CompanyAsset, error) {
	root, err := c.searchAuthCompany(ctx, name)
	if err != nil {
		return nil, err
	}
	if root == nil {
		return nil, fmt.Errorf("%w: %s", ani.ErrCompanyNotFound, name)
	}

	visited := map[string]bool{root.Name: true}
	queue := []authQueueItem{{name: root.Name, pid: root.PID, parent: "", percent: 1.0, depth: 0}}

	var assets []ani.CompanyAsset
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		// ICP 通过 beian.tianyancha.com 名字查 — 不需要 aqc cookie
		icps, err := c.fetchBeian(ctx, item.name)
		if err != nil {
			c.logger.Warnf("source=aqc name=%s beian_err=%q", item.name, err)
		}
		if len(icps) == 0 {
			assets = append(assets, ani.CompanyAsset{
				Name: item.name, PID: item.pid, Parent: item.parent,
				Percent: item.percent, Depth: item.depth, Source: authSourceName,
			})
		}
		for _, ic := range icps {
			assets = append(assets, ani.CompanyAsset{
				Name: item.name, PID: item.pid,
				ICP: ic.ICP, Domain: ic.Domain, Title: ic.Title,
				Parent: item.parent, Percent: item.percent, Depth: item.depth,
				Source: authSourceName,
			})
		}

		if item.depth >= depth {
			continue
		}
		// 投资递归走 aqc_unauth 的同一 stockchart 接口 (用 cookie 也能跑)
		invests, err := c.fetchAuthInvestments(ctx, item.pid)
		if err != nil {
			c.logger.Warnf("source=aqc pid=%s invest_err=%q", item.pid, err)
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
			queue = append(queue, authQueueItem{
				name: inv.EntName, pid: inv.PID, parent: item.name,
				percent: inv.RegRate, depth: item.depth + 1,
			})
		}
	}
	return assets, nil
}

type authSearchHit struct {
	Name string
	PID  string
}

// window.pageData = {...JSON...}; window.isSpider = ...; 中间嵌一坨 JSON。
var pageDataRe = regexp.MustCompile(`(?s)window\.pageData\s*=\s*(\{.*?\})\s*;\s*window\.isSpider`)

func (c *authClient) searchAuthCompany(ctx context.Context, name string) (*authSearchHit, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	q := url.Values{}
	q.Set("q", name)
	q.Set("t", "0")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, configuredAqcSearchURL+"?"+q.Encode(), nil)
	c.prepRequest(req)
	body, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("aqc auth search: %w", err)
	}

	m := pageDataRe.FindStringSubmatch(string(body))
	if len(m) < 2 {
		return nil, nil
	}
	// Python 还会剥几个回退兼容串 — 我们走 JSON 直接 decode, 容错由 Unmarshal 处理
	var page struct {
		Result struct {
			ResultList []struct {
				EntName string `json:"entName"`
				PID     any    `json:"pid"`
			} `json:"resultList"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(m[1]), &page); err != nil {
		return nil, fmt.Errorf("aqc auth: decode pageData: %w", err)
	}
	if len(page.Result.ResultList) == 0 {
		return nil, nil
	}
	first := page.Result.ResultList[0]
	return &authSearchHit{Name: stripEmTags(first.EntName), PID: anyToString(first.PID)}, nil
}

type authInvestHit struct {
	EntName string
	PID     string
	RegRate float64
}

// 投资链路接口与 aqc_unauth 同源, 这里复用相同 URL 与解析逻辑。
func (c *authClient) fetchAuthInvestments(ctx context.Context, pid string) ([]authInvestHit, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, err
	}
	defer c.release()

	q := url.Values{}
	q.Set("pid", pid)
	q.Set("drill", "2")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, configuredInvestAPI+"?"+q.Encode(), nil)
	c.prepRequest(req)
	body, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Status any    `json:"status"`
		Msg    string `json:"msg"`
		Data   struct {
			InvestRecordData struct {
				List []struct {
					EntName string `json:"entName"`
					PID     any    `json:"pid"`
					RegRate string `json:"regRate"`
				} `json:"list"`
			} `json:"investRecordData"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode invest: %w", err)
	}
	// Python: status == '1' 表示企业不存在
	if s := anyToString(resp.Status); s == "1" {
		return nil, nil
	}
	out := make([]authInvestHit, 0, len(resp.Data.InvestRecordData.List))
	for _, item := range resp.Data.InvestRecordData.List {
		out = append(out, authInvestHit{
			EntName: item.EntName,
			PID:     anyToString(item.PID),
			RegRate: percentToFloat(item.RegRate),
		})
	}
	return out, nil
}

// fetchBeian 通过 beian.tianyancha.com/search/NAME[/pN] HTML 抓 ICP/域名。
// 这个端点是天眼查的公共备案查询, 没有 cookie/token 需求。
func (c *authClient) fetchBeian(ctx context.Context, name string) ([]icpRecord, error) {
	var out []icpRecord
	for page := 1; ; page++ {
		batch, more, err := c.fetchBeianPage(ctx, name, page)
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

func (c *authClient) fetchBeianPage(ctx context.Context, name string, page int) ([]icpRecord, bool, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, false, err
	}
	defer c.release()

	target := configuredBeianURL + "/" + url.PathEscape(name)
	if page > 1 {
		target += "/p" + strconv.Itoa(page)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.0.0 Safari/537.36")
	req.Header.Set("Host", "beian.tianyancha.com")

	body, err := c.do(req)
	if err != nil {
		return nil, false, err
	}

	doc := ani.ParseHTML(string(body))
	if doc == nil {
		return nil, false, nil
	}
	// 备案表 row 在 //div[@class="ranking-content"]/table/tbody/tr
	var rows []*html.Node
	for _, container := range ani.FindNodes(doc, "div", "class", "ranking-content") {
		rows = append(rows, ani.FindAllByTag(container, "tr")...)
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	out := make([]icpRecord, 0, len(rows))
	for _, row := range rows {
		tds := directTDs(row)
		if len(tds) < 5 {
			continue
		}
		title := firstChildText(tds[3], "span")
		icp := firstChildText(tds[1], "a")
		domain := firstChildText(tds[4], "span")
		if icp == "" || domain == "" {
			continue
		}
		out = append(out, icpRecord{ICP: icpFormat(icp), Domain: domain, Title: title})
	}

	// 分页: //ul[@class="pagination"]/li 数量 - 1 是总页数
	totalPages := 0
	for _, ul := range ani.FindNodes(doc, "ul", "class", "pagination") {
		count := len(ani.FindAllByTag(ul, "li"))
		if count-1 > totalPages {
			totalPages = count - 1
		}
	}
	more := page < totalPages
	return out, more, nil
}

// === helpers shared by aqc_auth (some duplicate aqc.go's tyc-style helpers) ===

// directTDs 返回 tr 的直接 td 子节点 (按出现顺序)。
func directTDs(row *html.Node) []*html.Node {
	var out []*html.Node
	for c := row.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "td" {
			out = append(out, c)
		}
	}
	return out
}

// firstChildText 返回 n 下第一个 tag 标签的纯文本 (递归)。
func firstChildText(n *html.Node, tag string) string {
	hits := ani.FindAllByTag(n, tag)
	if len(hits) == 0 {
		return ""
	}
	return ani.NodeText(hits[0])
}

func anyToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func stripEmTags(s string) string {
	s = strings.ReplaceAll(s, "<em>", "")
	s = strings.ReplaceAll(s, "</em>", "")
	return s
}

func (c *authClient) do(req *http.Request) ([]byte, error) {
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
	case strings.Contains(string(body), "爱企查校验"):
		return nil, fmt.Errorf("aqc 触发校验, BAIDUID 可能失效")
	case resp.StatusCode == 302:
		return nil, fmt.Errorf("aqc 302 redirect — 百度验证")
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("aqc http %d: %s", resp.StatusCode, truncate(body))
	}
	return body, nil
}

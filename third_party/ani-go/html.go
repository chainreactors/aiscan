package ani

import (
	"strings"

	"golang.org/x/net/html"
)

// ParseHTML 解析 HTML 字符串, 返回根节点。失败返回 nil。
func ParseHTML(s string) *html.Node {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return nil
	}
	return doc
}

// FindNodes 递归收集所有 (tag, attrKey, attrValueContains) 命中的节点。
// attrKey == "" 时只匹配 tag; attrValueContains == "" 时只匹配 attrKey 存在。
func FindNodes(n *html.Node, tag, attrKey, attrValueContains string) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == tag {
			if attrKey == "" {
				out = append(out, node)
			} else {
				for _, a := range node.Attr {
					if a.Key == attrKey {
						if attrValueContains == "" || strings.Contains(a.Val, attrValueContains) {
							out = append(out, node)
							break
						}
					}
				}
			}
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

// NodeText 提取节点及其后代的纯文本 (类似 xpath string(.))。
func NodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(sb.String())
}

// Attr 返回节点上指定属性, 不存在返回 ""。
func Attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// FindAllByTag 收集 tag 的所有节点 (深度优先)。
func FindAllByTag(n *html.Node, tag string) []*html.Node {
	return FindNodes(n, tag, "", "")
}

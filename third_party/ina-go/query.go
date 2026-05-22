package ina

import "strings"

// Op 决定 Code 内 Pair 之间的逻辑连接。
type Op int

const (
	OpAnd Op = iota
	OpOr
)

// Pair 是一个键值对查询条件。Key 是语义 key (domain/ip/icp/cert/ico/cidr/...),
// 每个 source 子包会把它映射到具体字段名 (fofa 用 "=", zoomeye/hunter 用 ":")。
type Pair struct {
	Key   string
	Value string
}

// Code 是一棵简单的查询 AST。Phase 1 只支持 Pairs + Op + 可选 Filter 子句,
// 不支持嵌套。Filter 拼到主查询后, 对应 Python filter_code 行为。
type Code struct {
	Op     Op
	Pairs  []Pair
	Filter *Code
}

func NewCode() *Code { return &Code{Op: OpAnd} }

func (c *Code) And(key, value string) *Code {
	if c.Op == OpOr && len(c.Pairs) > 0 {
		c.Op = OpAnd
	}
	c.Pairs = append(c.Pairs, Pair{Key: key, Value: value})
	return c
}

func (c *Code) Or(key, value string) *Code {
	if c.Op == OpAnd && len(c.Pairs) > 0 {
		c.Op = OpOr
	}
	c.Pairs = append(c.Pairs, Pair{Key: key, Value: value})
	return c
}

func (c *Code) WithFilter(f *Code) *Code {
	c.Filter = f
	return c
}

// String 按 source 语法序列化。Phase 1 仅 fofa。
func (c *Code) String(source string) string {
	if c == nil || len(c.Pairs) == 0 {
		return ""
	}
	syn := syntaxFor(source)
	parts := make([]string, 0, len(c.Pairs))
	for _, p := range c.Pairs {
		mapped := mapKey(source, p.Key)
		parts = append(parts, mapped+syn.link+`"`+p.Value+`"`)
	}
	join := syn.and
	if c.Op == OpOr {
		join = syn.or
	}
	out := strings.Join(parts, join)
	if c.Filter != nil {
		if sub := c.Filter.String(source); sub != "" {
			out = "(" + out + ")" + syn.and + "(" + sub + ")"
		}
	}
	return out
}

type sourceSyntax struct {
	link string // key 与 value 之间的符号
	and  string
	or   string
}

func syntaxFor(source string) sourceSyntax {
	switch source {
	case "fofa":
		return sourceSyntax{link: "=", and: "&&", or: "||"}
	case "zoomeye":
		return sourceSyntax{link: ":", and: "&&", or: " "}
	case "hunter":
		// Hunter syntax uses '=' (e.g. domain="x.com", icp.number="京ICP123"),
		// despite Python ina_code.py using ':'. Verified via hunter.qianxin.com docs.
		return sourceSyntax{link: "=", and: "&&", or: "||"}
	default:
		return sourceSyntax{link: "=", and: "&&", or: "||"}
	}
}

// mapKey 把语义 key 翻成 source 专属字段名。表写死,与 Python ina_code.py 对齐。
// Phase 1 重点是 fofa, 其他源条目预留给 Phase 2。
func mapKey(source, key string) string {
	if m, ok := keyMaps[source]; ok {
		if mapped, ok := m[key]; ok {
			return mapped
		}
	}
	if mapped, ok := defaultKeyMap[key]; ok {
		return mapped
	}
	return key
}

var defaultKeyMap = map[string]string{
	"ico":      "ico",
	"icp":      "icp",
	"cert":     "cert",
	"domain":   "domain",
	"ip":       "ip",
	"cidr":     "cidr",
	"country":  "country",
	"province": "province",
	"city":     "city",
}

var keyMaps = map[string]map[string]string{
	"fofa": {
		"ico":  "icon_hash",
		"cidr": "ip",
	},
	"hunter": {
		"ico":      "web.icon",
		"cidr":     "ip",
		"icp":      "icp.number",
		"country":  "ip.country",
		"province": "ip.province",
		"city":     "ip.city",
	},
	"zoomeye": {
		"cert":   "ssl",
		"domain": "hostname",
		"ico":    "iconhash",
	},
}

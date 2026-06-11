package fingers

import (
	"context"
	"fmt"
	"strings"

	"github.com/chainreactors/logs"
	"github.com/chainreactors/neutron/protocols"
	"github.com/chainreactors/sdk/pkg/cyberhub"
	"github.com/chainreactors/sdk/pkg/types"
	"gopkg.in/yaml.v3"
)

const xrayRouteTag = "source1"

// NewConfig 创建默认配置
func NewConfig() *Config {
	return &Config{}
}

// Config Fingers SDK 配置
type Config struct {
	Providers     []types.Provider
	EnableEngines []string
	FullFingers   FullFingers
	MatchDetail   bool
	Proxy         []string // 引擎级默认代理，作用于主动指纹探测（可被 Context.WithProxy 覆盖）
}

// Validate 验证配置
func (c *Config) Validate() error {
	return nil
}

// SetEnableEngines 设置启用的引擎列表
func (c *Config) SetEnableEngines(engines []string) *Config {
	c.EnableEngines = engines
	return c
}

// WithProvider 追加数据源，支持多次调用自动合并
func (c *Config) WithProvider(providers ...types.Provider) *Config {
	c.Providers = append(c.Providers, providers...)
	return c
}

// WithFingers 设置指纹数据
func (c *Config) WithFingers(fingers types.Fingers) *Config {
	aliases := c.FullFingers.Aliases()
	c.FullFingers = (FullFingers{}).Merge(fingers, aliases)
	return c
}

// WithAliases 设置别名数据
func (c *Config) WithAliases(aliases []*types.Alias) *Config {
	fingers := c.FullFingers.Fingers()
	c.FullFingers = (FullFingers{}).Merge(fingers, aliases)
	return c
}

// WithMatchDetail enables matcher metadata on match results.
func (c *Config) WithMatchDetail() *Config {
	c.MatchDetail = true
	return c
}

// WithProxy 设置引擎级默认代理（支持多级代理链），作用于主动指纹探测。
// 可被 Context.WithProxy 覆盖。
func (c *Config) WithProxy(proxies ...string) *Config {
	c.Proxy = proxies
	return c
}

// WithFilter filters current FullFingers using predicate.
func (c *Config) WithFilter(predicate func(*FullFinger) bool) *Config {
	if c == nil {
		return c
	}
	c.FullFingers = c.FullFingers.Filter(predicate)
	return c
}

// Load 执行数据加载
func (c *Config) Load(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.FullFingers.Len() > 0 {
		return nil
	}
	if len(c.Providers) > 0 {
		for _, p := range c.Providers {
			if chp, ok := p.(*cyberhub.Provider); ok {
				exports, err := chp.ExportFingers(ctx)
				if err != nil {
					return err
				}
				useDraft := chp.Filter() != nil && chp.Filter().Draft
				c.FullFingers = c.FullFingers.MergeExports(exports, useDraft)
			} else {
				fingersData, aliases, err := p.Fingers(ctx)
				if err != nil {
					return err
				}
				c.FullFingers = c.FullFingers.Merge(fingersData, aliases)
			}
		}
		return nil
	}

	return fmt.Errorf("no data source configured: use WithProvider() or WithFingers()")
}

type FullFinger struct {
	Finger     *types.Finger
	Alias      *types.Alias
	Template   *types.Template // fingerprinthub/xray 引擎的已编译模板（用于被动匹配元数据）
	RawContent string          // 模板原始 YAML（用于构建底层引擎，避免序列化丢失 variables）
	Engine     string          // 模板引擎类型: "fingerprinthub" 或 "xray" (仅 Template 非 nil 时有效)
}

type FullFingers struct {
	Items map[string]*FullFinger
}

// Fingers returns finger list from FullFingers.
func (f FullFingers) Fingers() types.Fingers {
	if len(f.Items) == 0 {
		return nil
	}
	fingers := make(types.Fingers, 0, len(f.Items))
	for _, item := range f.Items {
		if item == nil || item.Finger == nil {
			continue
		}
		fingers = append(fingers, item.Finger)
	}
	return fingers
}

// Aliases returns alias list from FullFingers.
func (f FullFingers) Aliases() []*types.Alias {
	if len(f.Items) == 0 {
		return nil
	}
	aliases := make([]*types.Alias, 0, len(f.Items))
	for _, item := range f.Items {
		if item == nil || item.Alias == nil {
			continue
		}
		aliases = append(aliases, item.Alias)
	}
	return aliases
}

// Len returns item count.
func (f FullFingers) Len() int {
	return len(f.Items)
}

// Append adds a single FullFinger.
func (f FullFingers) Append(item *FullFinger) FullFingers {
	if item == nil {
		return f
	}
	if f.Items == nil {
		f.Items = make(map[string]*FullFinger)
	}
	key := fullFingerKey(item)
	if key == "" {
		return f
	}
	f.Items[key] = item
	return f
}

func fullFingerKey(item *FullFinger) string {
	if item.Finger != nil && item.Finger.Name != "" {
		return item.Finger.Name
	}
	if item.Template != nil {
		if item.Template.Id != "" {
			return item.Template.Id
		}
		if item.Template.Info.Name != "" {
			return item.Template.Info.Name
		}
	}
	return ""
}

// Merge appends fingers and aliases into FullFingers.
func (f FullFingers) Merge(fingers types.Fingers, aliases []*types.Alias) FullFingers {
	if len(fingers) == 0 && len(aliases) == 0 {
		return f
	}
	if f.Items == nil {
		f.Items = make(map[string]*FullFinger)
	}
	for _, finger := range fingers {
		f = f.Append(&FullFinger{Finger: finger})
	}
	for _, aliasItem := range aliases {
		if aliasItem == nil || aliasItem.Name == "" {
			continue
		}
		if item, ok := f.Items[aliasItem.Name]; ok && item != nil {
			item.Alias = aliasItem
		}
	}
	return f
}

// Filter returns a filtered copy of FullFingers using predicate.
func (f FullFingers) Filter(predicate func(*FullFinger) bool) FullFingers {
	if predicate == nil || len(f.Items) == 0 {
		return f
	}
	filtered := FullFingers{
		Items: make(map[string]*FullFinger),
	}
	for key, item := range f.Items {
		if predicate(item) {
			filtered.Items[key] = item
		}
	}
	return filtered
}

// NativeFingers returns Finger pointers for native fingers engine items.
func (f FullFingers) NativeFingers() types.Fingers {
	var result types.Fingers
	for _, item := range f.Items {
		if item == nil || item.Finger == nil {
			continue
		}
		if item.Template != nil {
			continue
		}
		result = append(result, item.Finger)
	}
	return result
}

// TemplateItems returns FullFinger items with parsed Template, optionally filtered by engine.
// If no engine names are given, returns all template items.
func (f FullFingers) TemplateItems(engines ...string) []*FullFinger {
	var result []*FullFinger
	for _, item := range f.Items {
		if item == nil || item.Template == nil {
			continue
		}
		if len(engines) > 0 {
			matched := false
			for _, eng := range engines {
				if item.Engine == eng {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		result = append(result, item)
	}
	return result
}


// MergeExports merges CyberHub FingerprintExport records into FullFingers.
// For fingerprinthub/xray engine records, RawContent is parsed into *Template.
func (f FullFingers) MergeExports(exports []cyberhub.FingerprintExport, useDraft bool) FullFingers {
	if len(exports) == 0 {
		return f
	}
	if f.Items == nil {
		f.Items = make(map[string]*FullFinger)
	}

	execOpts := &protocols.ExecuterOptions{
		Options: &protocols.Options{Timeout: 10},
	}

	for _, r := range exports {
		ff := &FullFinger{
			Finger: r.Finger,
		}
		if r.Alias != nil {
			ff.Alias = r.Alias
		}

		rawContent := r.RawContent
		if useDraft && r.RawContentDraft != "" {
			rawContent = r.RawContentDraft
		}

		engine := r.Engine
		if engine == "fingerprinthub" && hasTag(r.Finger, xrayRouteTag) {
			engine = "xray"
		}

		switch engine {
		case "fingerprinthub", "xray":
			if rawContent != "" {
				tmpl := parseTemplate(rawContent, execOpts)
				if tmpl != nil {
					ff.Template = tmpl
					ff.RawContent = rawContent
					ff.Engine = engine
				}
			}
		default:
			if useDraft && r.RawContentDraft != "" {
				var finger types.Finger
				if err := yaml.Unmarshal([]byte(r.RawContentDraft), &finger); err == nil && finger.Name != "" {
					ff.Finger = &finger
				}
			}
		}

		f = f.Append(ff)
	}
	return f
}

func hasTag(finger *types.Finger, tag string) bool {
	if finger == nil {
		return false
	}
	for _, t := range finger.Tags {
		if strings.EqualFold(strings.TrimSpace(t), tag) {
			return true
		}
	}
	return false
}

func parseTemplate(rawYAML string, opts *protocols.ExecuterOptions) *types.Template {
	tmpl := &types.Template{}
	if err := yaml.Unmarshal([]byte(rawYAML), tmpl); err != nil {
		logs.Log.Debugf("parse template YAML failed: %v", err)
		return nil
	}
	if tmpl.Id == "" && tmpl.Info.Name == "" {
		return nil
	}
	if err := tmpl.Compile(opts); err != nil {
		logs.Log.Debugf("compile template %s failed: %v", tmpl.Id, err)
		return nil
	}
	return tmpl
}

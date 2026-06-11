package neutron

import (
	"context"
	"fmt"
	"time"

	"github.com/chainreactors/sdk/pkg/types"
)

// NewConfig 创建默认配置
func NewConfig() *Config {
	return &Config{
		Timeout: 10 * time.Second,
	}
}

// Validate 验证配置
func (c *Config) Validate() error {
	return nil
}

// WithProvider 追加数据源，支持多次调用自动合并
func (c *Config) WithProvider(providers ...types.Provider) *Config {
	c.Providers = append(c.Providers, providers...)
	return c
}

// WithTemplates 设置已加载的模板
func (c *Config) WithTemplates(tpls []*types.Template) *Config {
	c.Templates = (Templates{}).Merge(tpls)
	return c
}

// WithFilter filters current Templates using predicate.
func (c *Config) WithFilter(predicate func(*types.Template) bool) *Config {
	if c == nil {
		return c
	}
	c.Templates = c.Templates.Filter(predicate)
	return c
}

// WithCapacity sets the total capacity for concurrent Execute calls.
func (c *Config) WithCapacity(total int) *Config {
	c.Capacity = total
	return c
}

// WithProxy 设置引擎级默认代理（支持多级链）。模板在编译期注入该代理，
// 故为 engine/Config 级粒度（编译期），不支持 per-Context 覆盖。
func (c *Config) WithProxy(proxies ...string) *Config {
	c.Proxy = proxies
	return c
}

// Load 执行数据加载
func (c *Config) Load(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	if c.Templates.Len() > 0 {
		return nil
	}
	if len(c.Providers) > 0 {
		for _, p := range c.Providers {
			tpls, err := p.POCs(ctx)
			if err != nil {
				return err
			}
			c.Templates = c.Templates.Merge(tpls)
		}
		return nil
	}

	return fmt.Errorf("no data source configured: use WithProvider() or WithTemplates()")
}

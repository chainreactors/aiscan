package neutron

import (
	"context"
	"fmt"
	"github.com/chainreactors/sdk/pkg/types"
	"time"
)

// ========================================
// Config 配置
// ========================================

// Config Neutron SDK 配置
type Config struct {
	Providers []types.Provider
	Templates Templates
	Capacity  int
	Timeout   time.Duration
	// Proxy 为引擎级默认代理（支持多级链）。注意：neutron 模板在编译期烘焙
	// http client，故代理为 engine/Config 级（编译期）粒度，不支持 per-Context 覆盖。
	Proxy []string
}

// ========================================
// Context 实现
// ========================================

// Context Neutron 上下文
type Context struct {
	ctx context.Context
}

var _ types.Context = (*Context)(nil)

// NewContext 创建 Neutron 上下文
func NewContext() *Context {
	return &Context{
		ctx: context.Background(),
	}
}

// WithContext 基于给定的 context.Context 复制 Context
func (c *Context) WithContext(ctx context.Context) *Context {
	return &Context{
		ctx: ctx,
	}
}

func (c *Context) Context() context.Context {
	return c.ctx
}

// ========================================
// Result 实现
// ========================================

// NeutronResult 聚合单次模板执行的完整结果
type NeutronResult struct {
	Result *types.OperatorResult
	Events []*types.ResultEvent
}

// ExecuteResult POC 执行结果
type ExecuteResult struct {
	success  bool
	err      error
	template *types.Template
	data     *NeutronResult
}

func (r *ExecuteResult) Success() bool {
	return r.success
}

func (r *ExecuteResult) Error() error {
	return r.err
}

func (r *ExecuteResult) Data() interface{} {
	return r.data
}

// Template 返回执行的模板
func (r *ExecuteResult) Template() *types.Template {
	return r.template
}

// Result 返回执行结果
func (r *ExecuteResult) Result() *NeutronResult {
	return r.data
}

// Matched 是否命中
func (r *ExecuteResult) Matched() bool {
	return r.data != nil && r.data.Result != nil && r.data.Result.Matched
}

// ========================================
// Task 实现
// ========================================

// ExecuteTask 执行任务
type ExecuteTask struct {
	Target    string
	Templates []*types.Template
	Payload   map[string]interface{}
}

// NewExecuteTask 创建执行任务
func NewExecuteTask(target string) *ExecuteTask {
	return &ExecuteTask{Target: target}
}

func (t *ExecuteTask) Type() string {
	return "execute"
}

func (t *ExecuteTask) Validate() error {
	if t.Target == "" {
		return fmt.Errorf("target cannot be empty")
	}
	if t.Templates != nil && len(t.Templates) == 0 {
		return fmt.Errorf("templates cannot be empty")
	}
	return nil
}

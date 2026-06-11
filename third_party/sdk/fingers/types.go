package fingers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	sdkhttpx "github.com/chainreactors/sdk/pkg/httpx"
	"github.com/chainreactors/sdk/pkg/types"
	"github.com/chainreactors/utils/httputils"
)

// ========================================
// Context 实现
// ========================================

// Context Fingers 上下文
type Context struct {
	ctx           context.Context
	httpSender    HTTPSender
	client        *http.Client // 用户自定义的 HTTP 客户端
	defaultClient *http.Client // 默认 HTTP 客户端（根据 timeout/proxy 自动构建）
	timeout       int          // 超时时间（秒）
	proxy         string       // 代理地址（如 "socks5://127.0.0.1:1080"）
	level         int          // 探测级别（0=被动, 1=基础, 2=深度）
}

var _ types.Context = (*Context)(nil)

// NewContext 创建 Fingers 上下文
func NewContext() *Context {
	return &Context{
		ctx:     context.Background(),
		timeout: 10, // 默认10秒超时
		level:   1,  // 默认 level 1（基础探测）
	}
}

// WithContext 基于给定的 context.Context 复制 Context
func (c *Context) WithContext(ctx context.Context) *Context {
	return &Context{
		ctx:           ctx,
		httpSender:    c.httpSender,
		client:        c.client,
		defaultClient: c.defaultClient,
		timeout:       c.timeout,
		proxy:         c.proxy,
		level:         c.level,
	}
}

func (c *Context) Context() context.Context {
	return c.ctx
}

// WithTimeout 设置超时时间（秒）
func (c *Context) WithTimeout(timeout int) *Context {
	c.timeout = timeout
	c.buildDefaultClient() // 重建默认客户端以应用新配置
	return c
}

// WithProxy 设置代理地址
// 支持的格式：
//   - socks5://127.0.0.1:1080
//   - socks4://127.0.0.1:1080
//   - http://127.0.0.1:8080
//   - https://127.0.0.1:8080
func (c *Context) WithProxy(proxy string) *Context {
	c.proxy = proxy
	c.buildDefaultClient() // 重建默认客户端以应用新配置
	return c
}

// WithHTTPSender 设置自定义HTTPSender
func (c *Context) WithHTTPSender(sender HTTPSender) *Context {
	c.httpSender = sender
	return c
}

// WithClient 设置自定义HTTP客户端
func (c *Context) WithClient(client *http.Client) *Context {
	c.client = client
	return c
}

// GetHTTPSender 获取HTTPSender，如果未设置则返回默认实现
func (c *Context) GetHTTPSender() HTTPSender {
	if c.httpSender == nil {
		return NewDefaultHTTPSender(time.Duration(c.timeout)*time.Second, c.proxy)
	}
	return c.httpSender
}

// GetClient 获取HTTP客户端，如果未设置则返回默认客户端
func (c *Context) GetClient() *http.Client {
	// 优先返回用户自定义的客户端
	if c.client != nil {
		return c.client
	}

	// 如果默认客户端未初始化，则构建它
	if c.defaultClient == nil {
		c.buildDefaultClient()
	}

	return c.defaultClient
}

// buildDefaultClient 构建默认HTTP客户端
// 根据当前的 timeout 和 proxy 配置创建客户端。代理经 SDK 统一 httpx 桥接生效，
// 因此主动指纹探测（GetClient）与 HTTPSender 行为一致，都会走代理。
func (c *Context) buildDefaultClient() {
	timeout := time.Duration(c.timeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	var proxies []string
	if c.proxy != "" {
		proxies = []string{c.proxy}
	}
	client, err := sdkhttpx.NewClient(sdkhttpx.Config{
		Timeout:            timeout,
		Proxy:              proxies,
		FollowRedirects:    true,
		InsecureSkipVerify: true,
	})
	if err != nil {
		// 代理解析失败时回退到无代理客户端，保持尽力而为语义。
		client, _ = sdkhttpx.NewClient(sdkhttpx.Config{
			Timeout:            timeout,
			FollowRedirects:    true,
			InsecureSkipVerify: true,
		})
	}
	c.defaultClient = client
}

// GetTimeout 获取超时时间（秒）
func (c *Context) GetTimeout() int {
	return c.timeout
}

// GetProxy 获取代理地址
func (c *Context) GetProxy() string {
	return c.proxy
}

// WithLevel 设置探测级别
// 参数:
//   - level: 探测级别（0=被动, 1=基础, 2=深度）
func (c *Context) WithLevel(level int) *Context {
	c.level = level
	return c
}

// GetLevel 获取探测级别
func (c *Context) GetLevel() int {
	return c.level
}

func (c *Context) clone() *Context {
	cp := *c
	return &cp
}

func (c *Context) withResolvedClient(client *http.Client) *Context {
	c.client = client
	return c
}

// ========================================
// Result 实现
// ========================================

// MatchResult 指纹匹配结果
type MatchResult struct {
	success    bool
	err        error
	frameworks types.Frameworks
}

func (r *MatchResult) Success() bool {
	return r.success
}

func (r *MatchResult) Error() error {
	return r.err
}

func (r *MatchResult) Data() interface{} {
	return r.frameworks
}

// Frameworks 获取匹配到的指纹
func (r *MatchResult) Frameworks() types.Frameworks {
	return r.frameworks
}

// HasMatch 是否匹配到指纹
func (r *MatchResult) HasMatch() bool {
	return len(r.frameworks) > 0
}

// Count 匹配到的指纹数量
func (r *MatchResult) Count() int {
	return len(r.frameworks)
}

// TargetResult 目标扫描结果（用于批量扫描）
type TargetResult struct {
	Target  string                 // 扫描的目标 URL 或 target
	Results []*types.ServiceResult // 指纹识别结果
	Err     error                  // 错误信息（如果有）
}

// Success 是否成功（无错误）
func (r *TargetResult) Success() bool {
	return r.Err == nil
}

// Error 返回错误（实现 types.Result 接口）
func (r *TargetResult) Error() error {
	return r.Err
}

// HasResults 是否有匹配结果
func (r *TargetResult) HasResults() bool {
	return len(r.Results) > 0
}

// Data 返回结果数据（实现 types.Result 接口）
func (r *TargetResult) Data() interface{} {
	return r.Results
}

// ========================================
// Task 实现
// ========================================

// MatchTask 指纹匹配任务
type MatchTask struct {
	Data []byte // HTTP 响应原始数据
}

// NewMatchTask 创建匹配任务
func NewMatchTask(data []byte) *MatchTask {
	return &MatchTask{Data: data}
}

// NewMatchTaskFromResponse 从 HTTP Response 创建任务
func NewMatchTaskFromResponse(resp *http.Response) *MatchTask {
	data := httputils.ReadRaw(resp)
	return &MatchTask{Data: data}
}

func (t *MatchTask) Type() string {
	return "match"
}

func (t *MatchTask) Validate() error {
	if len(t.Data) == 0 {
		return fmt.Errorf("data cannot be empty")
	}
	return nil
}

// HTTPMatchTask HTTP主动探测任务
type HTTPMatchTask struct {
	URLs []string // 目标URL列表
}

// NewHTTPMatchTask 创建HTTP匹配任务
func NewHTTPMatchTask(urls []string) *HTTPMatchTask {
	return &HTTPMatchTask{URLs: urls}
}

func (t *HTTPMatchTask) Type() string {
	return "http_match"
}

func (t *HTTPMatchTask) Validate() error {
	if len(t.URLs) == 0 {
		return fmt.Errorf("urls cannot be empty")
	}
	return nil
}

// ServiceMatchTask 服务主动探测任务
type ServiceMatchTask struct {
	Targets []string // 目标地址列表（格式：ip:port 或 host:port）
}

// NewServiceMatchTask 创建服务匹配任务
func NewServiceMatchTask(targets []string) *ServiceMatchTask {
	return &ServiceMatchTask{Targets: targets}
}

func (t *ServiceMatchTask) Type() string {
	return "service_match"
}

func (t *ServiceMatchTask) Validate() error {
	if len(t.Targets) == 0 {
		return fmt.Errorf("targets cannot be empty")
	}
	return nil
}

// FaviconMatchTask Favicon匹配任务
type FaviconMatchTask struct {
	Data []byte // favicon图标的原始字节数据
}

// NewFaviconMatchTask 创建Favicon匹配任务
func NewFaviconMatchTask(data []byte) *FaviconMatchTask {
	return &FaviconMatchTask{Data: data}
}

func (t *FaviconMatchTask) Type() string {
	return "favicon_match"
}

func (t *FaviconMatchTask) Validate() error {
	if len(t.Data) == 0 {
		return fmt.Errorf("data cannot be empty")
	}
	return nil
}

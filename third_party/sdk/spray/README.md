# Spray SDK

Spray SDK 提供了简洁的 Go API，用于 HTTP URL 检测和路径暴力破解。

## 核心概念

SDK 由两部分组成:

1. **SprayEngine**: 管理持久化状态（指纹库等）
2. **两个核心 API**:
   - `CheckStream`: URL 批量检测，返回 channel
   - `BruteStream`: 路径暴力破解，返回 channel

其他 API（`Check`、`Brute`）都是对 Stream API 的简单封装，你也可以根据需要自行封装。

## 快速开始

```go
import "github.com/chainreactors/sdk/spray"

// 1. 创建 SprayEngine
engine := spray.NewEngine(nil)

// 2. 初始化（加载指纹库等）
engine.Init()

// 3. 使用
ctx := spray.NewContext()

// URL 检测
urls := []string{"http://example.com", "http://httpbin.org"}
resultCh, _ := engine.CheckStream(ctx, urls)
for result := range resultCh {
    fmt.Printf("%s [%d]\n", result.UrlString, result.Status)
}

// 路径暴力破解
wordlist := []string{"admin", "api", "test", ".git"}
resultCh, _ := engine.BruteStream(ctx, "http://example.com", wordlist)
for result := range resultCh {
    fmt.Printf("%s [%d] %d bytes\n",
        result.UrlString, result.Status, result.BodyLength)
}

// 同步检测（等待所有结果）
results, _ := engine.Check(ctx, urls)
for _, result := range results {
    fmt.Printf("%s [%d]\n", result.UrlString, result.Status)
}
```

## 配置

### 使用默认配置

```go
engine := spray.NewEngine(nil)
```

默认配置针对 SDK 场景优化：20 线程、5 秒超时、GET 方法、静默模式。

### 自定义配置

```go
import "github.com/chainreactors/sdk/pkg/types"

opt := types.NewDefaultSprayOption()
opt.Threads = 200
opt.Timeout = 10
opt.Method = "POST"
opt.RandomUserAgent = true
opt.Headers = []string{"Authorization: Bearer token"}
opt.Filter = "current.Status == 404"  // 过滤 404

ctx := spray.NewContext().SetOption(opt)
engine := spray.NewEngine(nil)
```

### 运行时修改

```go
ctx := spray.NewContext().
    SetThreads(150).
    SetTimeout(15)
```

## API 参考

### SprayEngine

```go
// 创建实例
engine := spray.NewEngine(nil) // nil 时使用默认配置

// 初始化（必须调用）
engine.Init()

// 设置参数
ctx.SetThreads(threads)
ctx.SetTimeout(timeout)
```

### 核心 API

```go
// URL 检测（流式）
CheckStream(ctx, urls) -> channel

// 暴力破解（流式）
BruteStream(ctx, baseURL, wordlist) -> channel
```

### 便捷 API

```go
// URL 检测（同步）
results, err := engine.Check(ctx, urls)

// 暴力破解（同步）
results, err := engine.Brute(ctx, baseURL, wordlist)
```

## 使用统一 SDK 接口

Spray SDK 实现了 Chainreactors 统一 SDK 接口，可以与其他 SDK 多态使用：

```go
import (
    rootsdk "github.com/chainreactors/sdk"
    "github.com/chainreactors/sdk/spray"
)

// 使用工厂创建引擎
engine, err := rootsdk.NewEngine("spray", nil)

// 使用统一接口
ctx := spray.NewContext()
task := spray.NewCheckTask([]string{"http://example.com"})
resultCh, _ := engine.Execute(ctx, task)

for result := range resultCh {
    if result.Success() {
        fmt.Printf("Success: %v\n", result.Data())
    }
}
```

## 示例

### 基础 URL 检测

```go
engine := spray.NewEngine(nil)
engine.Init()

ctx := spray.NewContext().SetThreads(100)

urls := []string{
    "http://example.com",
    "https://httpbin.org/get",
    "http://github.com",
}

results, err := engine.Check(ctx, urls)
if err != nil {
    log.Fatal(err)
}

for _, result := range results {
    fmt.Printf("%s [%d] - %s\n",
        result.UrlString, result.Status, result.Title)
}
```

### 流式 URL 检测（推荐大批量）

```go
engine := spray.NewEngine(nil)
engine.Init()

ctx := spray.NewContext()

// 从文件读取 URL 列表
urls := readURLsFromFile("urls.txt")

resultCh, _ := engine.CheckStream(ctx, urls)
for result := range resultCh {
    if result.Status >= 200 && result.Status < 300 {
        fmt.Printf("✓ %s [%d]\n", result.UrlString, result.Status)
    }
}
```

### 路径暴力破解

```go
engine := spray.NewEngine(nil)
engine.Init()

ctx := spray.NewContext()

wordlist := []string{
    "admin",
    "api",
    "test",
    ".git",
    ".env",
    "config.json",
}

resultCh, _ := engine.BruteStream(ctx, "http://example.com", wordlist)
for result := range resultCh {
    fmt.Printf("%s [%d] %d bytes\n",
        result.UrlString, result.Status, result.BodyLength)
}
```

### 带超时和取消的检测

```go
engine := spray.NewEngine(nil)
engine.Init()

timeoutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
defer cancel()

ctx := spray.NewContext().WithContext(timeoutCtx)

urls := []string{"http://example.com", "http://github.com"}
results, err := engine.Check(ctx, urls)
if err != nil {
    log.Printf("Error: %v", err)
}

for _, result := range results {
    fmt.Printf("%s [%d]\n", result.UrlString, result.Status)
}
```

### 自定义请求头和方法

```go
opt := types.NewDefaultSprayOption()
opt.Method = "POST"
opt.Headers = []string{
    "Authorization: Bearer your-token",
    "X-Custom-Header: value",
}
opt.RandomUserAgent = true  // 随机 User-Agent

ctx := spray.NewContext().SetOption(opt)
engine := spray.NewEngine(nil)
engine.Init()
results, _ := engine.Check(ctx, urls)
```

### 自定义Host检测

在某些场景下需要自定义Host头进行检测：
- 虚拟主机探测：同一IP上的不同域名
- CDN绕过：直接访问源站IP但指定域名
- 负载均衡测试：指定后端服务器

```go
engine := spray.NewEngine(nil)
engine.Init()

// 方式1: 使用SetHost方法（推荐）
ctx := spray.NewContext().
    SetThreads(20).
    SetTimeout(10).
    SetHost("target.example.com")  // 设置自定义Host头

// 使用IP访问，但Host头指定域名
urls := []string{"http://1.2.3.4/admin"}
results, _ := engine.Check(ctx, urls)
// 实际请求: GET /admin HTTP/1.1
//          Host: target.example.com

// 方式2: Host碰撞检测（批量测试多个域名）
hosts := []string{"example.com", "admin.example.com", "test.example.com"}
for _, host := range hosts {
    ctx := spray.NewContext().SetHost(host)
    results, _ := engine.Check(ctx, []string{"http://1.2.3.4"})
    for _, result := range results {
        fmt.Printf("[%s] %s [%d]\n", host, result.UrlString, result.Status)
    }
}
```

### 使用过滤器

```go
opt := types.NewDefaultSprayOption()
opt.Filter = "current.Status != 404"  // 过滤掉 404

ctx := spray.NewContext().SetOption(opt)
engine := spray.NewEngine(nil)
engine.Init()
results, _ := engine.Check(ctx, urls)
// 结果中不会包含 404 状态码的响应
```

## 注意事项

1. **必须调用 Init()**：使用前必须调用 `Init()` 初始化引擎
2. **Context 支持**：所有 API 都支持 context 取消和超时
3. **线程数**：默认 100 线程，可根据目标服务器性能调整
4. **结果处理**：
   - `Check/Brute`: 返回所有结果（包括失败）
   - `CheckStream/BruteStream`: 返回所有结果（包括失败）
5. **过滤**：使用 `Filter` 字段可以过滤不需要的结果
6. **速率限制**：注意目标服务器的速率限制，避免被封禁

## 结果字段

```go
type SprayResult = types.SprayResult

// 常用字段:
// UrlString  完整 URL
// Status     HTTP 状态码
// Title      页面标题
// BodyLength 响应体长度
// Frameworks 识别的框架
// Extracts   提取的信息
```

## 常见问题

### Q: 如何加快检测速度？
A: 增加线程数 `ctx.SetThreads(200)`，但注意目标服务器的承受能力

### Q: 如何避免被 WAF 拦截？
A:
- 使用 `RandomUserAgent = true`
- 降低线程数
- 增加延迟 `opt.Delay`
- 使用代理

### Q: 如何只获取特定状态码的结果？
A: 使用过滤器：`opt.Filter = "current.Status == 200"`

### Q: Stream 和 Sync 有什么区别？
A:
- Stream: 返回 channel，实时处理结果，适合大批量检测，内存占用小
- Sync: 返回切片，等待所有结果完成，适合小批量检测，方便处理

### Q: 如何处理 HTTPS 证书错误？
A: SDK 默认会跳过证书验证

## 更多信息

- [Chainreactors SDK 主文档](../../README.md)
- [Spray 项目](https://github.com/chainreactors/spray)
- [测试示例](spray_test.go)

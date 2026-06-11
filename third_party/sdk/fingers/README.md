# Fingers SDK

基于 Cyberhub 的 Fingers 指纹管理 SDK，提供对 fingers 库的统一加载和匹配入口。

## 🌟 亮点

- **统一入口**: `NewEngine` 负责加载，`Match` 负责匹配（也可 `Get()` 取底层引擎）
- **本地/远程**: 支持从本地 YAML/目录或 Cyberhub 加载
- **零冗余**: Cyberhub 响应使用 `json:",inline"` 直接嵌入 `types.Finger`
- **无侵入集成**: gogo/spray 等通过注入引擎完成集成

## 📦 安装

```bash
go get github.com/chainreactors/sdk/fingers
```

## 🚀 快速开始

### API 设计

Fingers SDK 提供两类 API：

1. **被动匹配 API**：直接匹配已有的 HTTP 响应数据
   - `Match(data []byte)` - 匹配原始字节数据
   - `MatchFavicon(data []byte)` - 匹配 favicon 数据
   - `MatchHTTP(resp *http.Response)` - 匹配 HTTP 响应

2. **主动探测 API**：支持批量目标扫描
   - `HTTPMatch(ctx, urls []string)` - HTTP/HTTPS 批量扫描（同步）
   - `HTTPMatchStream(ctx, urls []string)` - HTTP/HTTPS 批量扫描（流式）
   - `ServiceMatch(ctx, targets []string)` - 通用服务批量扫描（同步）
   - `ServiceMatchStream(ctx, targets []string)` - 通用服务批量扫描（流式）

### 被动匹配示例

```go
config := fingers.NewConfig().
    WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))
engine, _ := fingers.NewEngine(config)

// 匹配原始字节数据
frameworks, _ := engine.Match(httpResponseBytes)

// 匹配 HTTP 响应
resp, _ := http.Get("http://example.com")
frameworks, _ := engine.MatchHTTP(resp)

// 匹配 favicon
faviconData, _ := os.ReadFile("favicon.ico")
frameworks, _ := engine.MatchFavicon(faviconData)
```

### 主动探测示例

#### 单目标扫描

```go
config := fingers.NewConfig().
    WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))
engine, _ := fingers.NewEngine(config)

// 创建上下文（配置 timeout、level 等）
ctx := fingers.NewContext().WithTimeout(10).WithLevel(1)

// HTTP 主动探测
results, _ := engine.HTTPMatch(ctx, []string{"https://example.com"})
for _, targetResult := range results {
    if targetResult.Err != nil {
        fmt.Printf("Error scanning %s: %v\n", targetResult.Target, targetResult.Err)
        continue
    }

    for _, result := range targetResult.Results {
        fmt.Printf("Found: %s\n", result.Framework.Name)
    }
}
```

#### 批量目标扫描

```go
// 批量扫描多个目标
urls := []string{
    "https://example1.com",
    "https://example2.com",
    "https://example3.com",
}

// 同步版本 - 等待所有结果
results, _ := engine.HTTPMatch(ctx, urls)
for _, targetResult := range results {
    fmt.Printf("Target: %s, Results: %d\n",
        targetResult.Target, len(targetResult.Results))
}

// 流式版本 - 边扫描边处理
resultCh, _ := engine.HTTPMatchStream(ctx, urls)
for targetResult := range resultCh {
    // 实时处理每个目标的结果
    if targetResult.Success() && targetResult.HasResults() {
        fmt.Printf("Found %d fingerprints on %s\n",
            len(targetResult.Results), targetResult.Target)
    }
}
```

#### Service 扫描示例

```go
// Service 扫描（支持 TCP/UDP 等协议）
targets := []string{
    "192.168.1.1:22",
    "192.168.1.1:80",
    "192.168.1.1:443",
}

ctx := fingers.NewContext().WithTimeout(5).WithLevel(2)
results, _ := engine.ServiceMatch(ctx, targets)
```

### Context 配置

```go
ctx := fingers.NewContext().
    WithTimeout(10).              // 设置超时（秒）
    WithLevel(1).                 // 设置探测级别（HTTP: 0-3, Service: 0-9）
    WithProxy("socks5://127.0.0.1:1080"). // 设置代理
    WithClient(customHTTPClient)  // 自定义 HTTP 客户端

// 探测级别说明：
// HTTP: 0=被动, 1=基础主动, 2=深度主动, 3=最深主动
// Service: 0-9 级别，数字越大探测越深入
```

### 示例 1：从 Cyberhub 加载

```go
config := fingers.NewConfig()
config.WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))

engine, _ := fingers.NewEngine(config)
frameworks, _ := engine.Match(httpResponseBytes)
```

### 示例 2：从本地文件/目录加载

```go
config := fingers.NewConfig()
config.WithProvider(provider.NewFileProvider("path", "")) // 文件或目录

engine, _ := fingers.NewEngine(config)
```

### 示例 3：集成到 gogo（自己组装）

```go
import (
    "github.com/chainreactors/sdk/fingers"
    "github.com/chainreactors/sdk/gogo"
    "github.com/chainreactors/sdk/pkg/cyberhub"
)

// 1. 加载完整引擎
config := fingers.NewConfig()
config.WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))

fingersEngine, _ := fingers.NewEngine(config)

// 2. 注入到 gogo
gogoConfig := gogo.NewConfig().WithFingersEngine(fingersEngine)
gogoEngine := gogo.NewEngine(gogoConfig)
gogoEngine.Init()
```

### 示例 4：集成到 spray（自己组装）

```go
import (
    "github.com/chainreactors/sdk/fingers"
    "github.com/chainreactors/sdk/pkg/cyberhub"
    "github.com/chainreactors/sdk/spray"
)

// 1. 加载完整引擎
config := fingers.NewConfig()
config.WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))

fingersEngine, _ := fingers.NewEngine(config)

// 2. 直接注入到 spray（spray 需要完整 Engine）
sprayConfig := spray.NewConfig().WithFingersEngine(fingersEngine)
sprayEngine := spray.NewEngine(sprayConfig)
sprayEngine.Init()
```

### 示例 5：SDK Engine（可选）

如果需要统一的 SDK 接口：

```go
import (
    "fmt"
    "net/http"

    rootsdk "github.com/chainreactors/sdk"
    "github.com/chainreactors/sdk/fingers"
    "github.com/chainreactors/sdk/pkg/cyberhub"
)

// 通过全局工厂创建
config := fingers.NewConfig()
config.WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))
engine, _ := rootsdk.NewEngine("fingers", config)
defer engine.Close()

// 使用 SDK 接口
resp, _ := http.Get("http://example.com")
defer resp.Body.Close()

ctx := fingers.NewContext()
task := fingers.NewMatchTaskFromResponse(resp)

resultCh, _ := engine.Execute(ctx, task)
for result := range resultCh {
    if result.Success() {
        matchResult := result.(*fingers.MatchResult)
        for _, fw := range matchResult.Frameworks() {
            fmt.Printf("指纹: %s\n", fw.Name)
        }
    }
}
```

## 🔧 配置选项

```go
config := fingers.NewConfig()
config.WithProvider(
    cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key").
        WithFilter(types.NewExportFilter().WithSources("github")).
        WithTimeout(30 * time.Second),
)

// 本地加载时使用 FileProvider
// config.WithProvider(provider.NewFileProvider("./fingers.yaml", ""))

engine, _ := fingers.NewEngine(config)
```

也可以直接注入内存数据：

```go
config := fingers.NewConfig()
config.WithFingers(fingersData)
config.WithAliases(aliases)

engine, _ := fingers.NewEngine(config)
```

## 🏗️ 架构设计

### 核心结构

```go
// pkg/cyberhub/types.go
type FingerprintResponse struct {
    *types.Finger `json:",inline" yaml:",inline"`
    Alias           *types.Alias `json:"alias,omitempty" yaml:"alias,omitempty"`
}
```

### 目录结构

```
sdk/
├── fingers/           # Fingers SDK
│   ├── config.go     # 配置管理
│   ├── engine.go     # Engine 封装
│   ├── types.go      # Context/Task/Result
│   ├── additions.go  # 动态扩展 (AddFingers/AddFingersFile)
│   └── init.go       # 全局注册
├── pkg/cyberhub/     # Cyberhub 客户端
│   ├── client.go
│   ├── config.go
│   └── types.go
├── gogo/             # gogo 集成
└── spray/            # spray 集成
```

## 🎯 特性

- [x] Cyberhub Export API 集成
- [x] 本地 YAML/目录加载
- [x] Alias 管理
- [x] SDK Engine 接口（可选）
- [x] 被动匹配：支持 `[]byte`、`http.Response`、Favicon
- [x] 主动探测：HTTP/HTTPS 批量扫描（同步/流式）
- [x] 主动探测：通用服务批量扫描（同步/流式）
- [x] Context 配置：timeout、level、proxy、自定义 HTTP 客户端
- [x] 批量目标扫描：支持多目标并发探测
- [x] 动态扩展（AddFingers / AddFingersFile）

## 📚 API 参考

### TargetResult 结构

```go
type TargetResult struct {
    Target  string                    // 扫描的目标 URL 或 target
    Results []*types.ServiceResult    // 指纹识别结果
    Err     error                     // 错误信息（如果有）
}

// 方法
func (r *TargetResult) Success() bool      // 是否成功（无错误）
func (r *TargetResult) HasResults() bool   // 是否有匹配结果
```

### Context 方法

```go
func NewContext() *Context
func (c *Context) WithTimeout(timeout int) *Context
func (c *Context) WithLevel(level int) *Context
func (c *Context) WithProxy(proxy string) *Context
func (c *Context) WithClient(client *http.Client) *Context
func (c *Context) WithHTTPSender(sender HTTPSender) *Context
```

## 📖 文档

- [SDK 主文档](../README.md)
- [CLI 示例](../examples/fingers/main.go)

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 License

MIT License

# GoGo SDK

GoGo SDK 提供了简洁的 Go API，用于端口扫描和服务识别。

## 核心概念

SDK 由两部分组成:

1. **GogoEngine**: 管理持久化状态（指纹库等）
2. **两个核心 API**:
   - `ScanStream`: 端口批量扫描，返回 channel
   - `WorkflowStream`: 工作流扫描，返回 channel

其他 API（`Scan`、`ScanOne`、`Workflow`）都是对 Stream API 的简单封装，你也可以根据需要自行封装。

## 快速开始

```go
import "github.com/chainreactors/sdk/gogo"

// 1. 创建 GogoEngine
engine := gogo.NewEngine(nil)

// 2. 初始化（加载指纹库等）
engine.Init()

// 3. 使用
ctx := gogo.NewContext()

// 单目标扫描
result := engine.ScanOne(ctx, "192.168.1.1", "80")
fmt.Printf("%s:%s - %s\n", result.Ip, result.Port, result.Status)

// 批量扫描
resultCh, _ := engine.ScanStream(ctx, "192.168.1.0/24", "top100")
for result := range resultCh {
    fmt.Printf("%s:%s - %s\n", result.Ip, result.Port, result.Title)
}

// 同步扫描（等待所有结果）
results, _ := engine.Scan(ctx, "192.168.1.0/24", "80,443,8080")
for _, result := range results {
    fmt.Printf("%s:%s - %s\n", result.Ip, result.Port, result.Status)
}
```

## 配置

### 使用默认配置

```go
engine := gogo.NewEngine(nil)
```

默认配置针对 SDK 场景优化：1000 线程。

### 运行时配置

```go
import "github.com/chainreactors/sdk/pkg/types"

opt := types.NewDefaultGogoOption()
opt.VersionLevel = 2      // 深度指纹识别
opt.Exploit = "auto"      // 启用漏洞检测
opt.Delay = 5             // 超时时间（秒）

ctx := gogo.NewContext().SetOption(opt)
engine := gogo.NewEngine(nil)
```

### 运行时修改

```go
ctx.SetThreads(500)  // 设置线程数
```

## API 参考

### GogoEngine

```go
// 创建实例
engine := gogo.NewEngine(nil)  // nil 时使用默认配置

// 初始化（必须调用）
engine.Init()

// 设置参数
ctx.SetThreads(threads)
```

### 核心 API

```go
// 端口扫描（流式）
ScanStream(ctx, ip, ports) -> channel

// 工作流扫描（流式）
WorkflowStream(ctx, workflow) -> channel
```

### 便捷 API

```go
// 单目标扫描
result := engine.ScanOne(ctx, ip, port)

// 批量扫描（同步）
results, err := engine.Scan(ctx, ip, ports)

// 工作流扫描（同步）
results, err := engine.Workflow(ctx, workflow)
```

## 端口格式

支持多种端口格式：

```go
// 单个端口
engine.Scan(ctx, "192.168.1.1", "80")

// 多个端口
engine.Scan(ctx, "192.168.1.1", "80,443,8080")

// 端口范围
engine.Scan(ctx, "192.168.1.1", "1-1000")

// Top 端口
engine.Scan(ctx, "192.168.1.1", "top100")
engine.Scan(ctx, "192.168.1.1", "top1000")
```

## IP 格式

支持多种 IP 格式：

```go
// 单个 IP
engine.Scan(ctx, "192.168.1.1", "80")

// CIDR 网段
engine.Scan(ctx, "192.168.1.0/24", "80")

// IP 范围
engine.Scan(ctx, "192.168.1.1-192.168.1.254", "80")
```

## 工作流

工作流提供了更复杂的扫描配置：

```go
import "github.com/chainreactors/sdk/pkg/types"

workflow := &types.Workflow{
    Name:        "security-scan",
    IP:          "example.com",
    Ports:       "top1000",
    Verbose:     2,           // 详细程度
    VersionLevel: 3,          // 深度指纹识别
    Exploit:     "auto",      // 启用漏洞检测
}

results, err := engine.Workflow(ctx, workflow)
```

## 使用统一 SDK 接口

GoGo SDK 实现了 Chainreactors 统一 SDK 接口，可以与其他 SDK 多态使用：

```go
import (
    rootsdk "github.com/chainreactors/sdk"
    "github.com/chainreactors/sdk/gogo"
)

// 使用工厂创建引擎
engine, err := rootsdk.NewEngine("gogo", nil)

// 使用统一接口
ctx := gogo.NewContext()
task := gogo.NewScanTask("192.168.1.0/24", "80,443")
resultCh, _ := engine.Execute(ctx, task)

for result := range resultCh {
    if result.Success() {
        fmt.Printf("Found: %v\n", result.Data())
    }
}
```

## 示例

### 基础扫描

```go
engine := gogo.NewEngine(nil)
engine.Init()

ctx := gogo.NewContext().SetThreads(500)

// 扫描单个目标
result := engine.ScanOne(ctx, "192.168.1.1", "80")
fmt.Printf("%s:%s - %s\n", result.Ip, result.Port, result.Status)
```

### 流式扫描（推荐大规模扫描）

```go
engine := gogo.NewEngine(nil)
engine.Init()

ctx := gogo.NewContext()

resultCh, _ := engine.ScanStream(ctx, "192.168.1.0/24", "top100")
for result := range resultCh {
    fmt.Printf("%s:%s - %s (Title: %s)\n",
        result.Ip, result.Port, result.Status, result.Title)
}
```

### 带超时的扫描

```go
engine := gogo.NewEngine(nil)
engine.Init()

timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
defer cancel()

ctx := gogo.NewContext().WithContext(timeoutCtx)

results, err := engine.Scan(ctx, "192.168.1.0/24", "top1000")
if err != nil {
    log.Fatal(err)
}

for _, result := range results {
    fmt.Printf("%s:%s - %s\n", result.Ip, result.Port, result.Status)
}
```

### 深度扫描（指纹识别 + 漏洞检测）

```go
import "github.com/chainreactors/sdk/pkg/types"

opt := types.NewDefaultGogoOption()
opt.VersionLevel = 3      // 最深度的指纹识别
opt.Exploit = "auto"      // 自动漏洞检测

ctx := gogo.NewContext().SetOption(opt)
engine := gogo.NewEngine(nil)
engine.Init()

results, _ := engine.Scan(ctx, "192.168.1.0/24", "top100")
for _, result := range results {
    if result.Extracts != nil && len(result.Extracts) > 0 {
        fmt.Printf("%s:%s - Found vulnerabilities: %v\n",
            result.Ip, result.Port, result.Extracts)
    }
}
```

## 注意事项

1. **必须调用 Init()**：使用前必须调用 `Init()` 加载指纹库等资源
2. **Context 支持**：所有扫描 API 都支持 context 取消和超时
3. **线程数**：默认 1000 线程，可根据网络环境调整
4. **结果处理**：
   - `ScanOne`: 返回单个结果，包含失败情况
   - `Scan/Workflow`: 只返回开放端口的结果
   - `ScanStream/WorkflowStream`: 只返回开放端口的结果
5. **指纹识别**：设置 `VersionLevel` 可以启用深度指纹识别，但会增加扫描时间

## 结果字段

```go
type GOGOResult = types.GOGOResult

// 常用字段:
// Ip         IP 地址
// Port       端口
// Status     状态 (open/closed)
// Title      HTTP 标题
// Uri        URI
// Frameworks 识别的框架
// Extracts   漏洞信息
```

## 常见问题

### Q: 如何加快扫描速度？
A: 增加线程数 `ctx.SetThreads(2000)`，但注意网络带宽限制

### Q: 如何减少误报？
A: 提高指纹识别级别 `opt.VersionLevel = 2` 或 `3`

### Q: 如何只扫描特定服务？
A: 使用 workflow 并配置 `Exploit` 字段

### Q: Stream 和 Sync 有什么区别？
A:
- Stream: 返回 channel，实时处理结果，适合大规模扫描，内存占用小
- Sync: 返回切片，等待所有结果完成，适合小规模扫描，方便处理

## 更多信息

- [Chainreactors SDK 主文档](../../README.md)
- [GoGo 项目](https://github.com/chainreactors/gogo)
- [测试示例](gogo_test.go)

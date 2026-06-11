# 核心概念

SDK 将所有扫描引擎统一为四个核心抽象：**Engine**、**Context**、**Task**、**Result**。

## 架构总览

```
              ┌──────────────────────────┐
              │         Client           │
              │  WithProvider / WithIndex │
              └────┬──────────┬──────────┘
                   │          │
         ┌─────────┤   ┌──────▼──────┐
         │         │   │ Index (可选) │
         │         │   └──────────────┘
    ┌────▼───┐ ┌───▼────┐ ┌───────┐ ┌────────┐
    │ Fingers │ │ Neutron │ │ GoGo  │ │ Spray  │ ...
    └────┬───┘ └───┬────┘ └───┬───┘ └───┬────┘
         │         │          │         │
    ┌────▼─────────▼──────────▼─────────▼────┐
    │         types.Engine 接口               │
    │   Execute(Context, Task) → chan Result   │
    └─────────────────────────────────────────┘
```

- Client 管理引擎生命周期和依赖注入
- Index 是可选的关联查询层，将各引擎产出的结果进行跨域关联

## 四个核心接口

接口定义在 [`pkg/types/types.go`](../pkg/types/types.go)：

### Engine

```go
type Engine interface {
    Name() string
    Execute(ctx Context, task Task) (<-chan Result, error)
    io.Closer
}
```

每个扫描引擎都实现此接口。`Execute` 返回一个 Result channel，结果以流式方式送出。

### Context

```go
type Context interface {
    Context() context.Context
}
```

携带运行时配置（超时、线程数、代理等）。每个引擎有自己的 Context 实现，提供引擎特有的配置方法：

```go
// Fingers
fingers.NewContext().WithTimeout(10).WithLevel(2).WithProxy("socks5://127.0.0.1:1080")

// GoGo
gogo.NewContext().SetThreads(2000).SetVersionLevel(2).SetExploit("all")

// Spray
spray.NewContext().SetThreads(50).SetTimeout(10).SetMethod("POST")
```

### Task

```go
type Task interface {
    Type() string
    Validate() error
}
```

描述「做什么」。不同引擎定义不同的 Task 类型：

| 引擎 | Task 类型 | 说明 |
|------|----------|------|
| Fingers | `MatchTask` | 被动匹配原始 HTTP 数据 |
| Fingers | `HTTPMatchTask` | 主动 HTTP 探测 |
| Fingers | `ServiceMatchTask` | 主动服务探测 |
| Neutron | `ExecuteTask` | 对目标执行 POC |
| GoGo | `ScanTask` | IP + 端口扫描 |
| GoGo | `WorkflowTask` | 工作流扫描 |
| Spray | `CheckTask` | URL 存活检测 |
| Spray | `BruteTask` | 路径爆破 |
| Zombie | `BruteTask` | 弱口令检测（Brute/Pitchfork/Sniper） |

### Result

```go
type Result interface {
    Success() bool
    Error() error
    Data() interface{}
}
```

SDK 通过泛型包装器 `TypedResult[T]` 实现类型安全（见 [`pkg/types/result.go`](../pkg/types/result.go)）：

```go
// 从 Result channel 中提取类型化数据
for result := range resultCh {
    if data, ok := types.ResultData[*types.GOGOResult](result); ok {
        fmt.Println(data.Ip, data.Port)
    }
}
```

## 同步 vs 流式

大多数引擎同时提供同步和流式两种 API：

```go
// 同步 —— 等待所有结果
results, err := engine.Scan(ctx, ip, ports)

// 流式 —— 逐个接收结果
resultCh, err := engine.ScanStream(ctx, ip, ports)
for result := range resultCh {
    // 每扫完一个目标就能拿到结果
}
```

流式 API 适合大规模扫描场景，可以边扫边处理、及时输出。

## 并发控制：Capacity

当多个 goroutine 同时调用同一引擎时，可以通过 `Capacity` 限制总并发（见 [`pkg/types/capacity.go`](../pkg/types/capacity.go)）：

```go
// 在 Config 中设置
config := gogo.NewConfig().WithCapacity(5000)

// 或在运行时设置
engine.SetCapacity(5000)
```

每次 `Execute` 会从共享池中申请线程配额，池满则阻塞等待。

## 统计回调：Stats

引擎执行完成后可以通过 Context 的 `SetStatsHandler` 回调获取执行统计：

```go
ctx := gogo.NewContext().SetStatsHandler(func(s types.Stats) {
    fmt.Printf("engine=%s targets=%d results=%d duration=%v\n",
        s.Engine, s.Targets, s.Results, s.Duration)
})
```

## 下一步

了解了核心抽象后，可以深入各引擎的具体用法：

- [Fingers - 指纹识别](fingers.md)
- [Neutron - POC 扫描](neutron.md)
- [GoGo - 端口扫描](gogo.md)
- [Spray - HTTP 检测](spray.md)
- [Zombie - 弱口令检测](zombie.md)

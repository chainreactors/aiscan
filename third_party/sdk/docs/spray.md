# Spray - HTTP 检测

Spray 引擎用于 HTTP 批量检测和路径爆破，支持存活检测、字典爆破、爬虫、备份文件发现等功能。

## 创建引擎

```go
// 基础创建
engine := spray.NewEngine(nil)

// 注入 Fingers 引擎（用于爬虫后深度指纹识别）
fingersEng, _ := fingers.NewEngine(fingersConfig)
config := spray.NewConfig().
    WithFingersEngine(fingersEng).
    WithMatchDetail()  // 开启匹配细节

engine := spray.NewEngine(config)
engine.Init()
```

Config 选项：

| 方法 | 说明 |
|------|------|
| `WithFingersEngine` | 注入自定义指纹引擎 |
| `WithMatchDetail()` | 开启匹配细节 |
| `WithCapacity(n)` | 限制总并发线程数 |
| `WithResourceProvider` | 自定义资源加载 |

> 源码：[`spray/types.go`](../spray/types.go)

## URL 存活检测

### 同步

```go
ctx := spray.NewContext().
    SetThreads(50).
    SetTimeout(10)

results, err := engine.Check(ctx, []string{
    "http://example.com",
    "http://192.168.1.1:8080",
    "https://target.com",
})

for _, r := range results {
    fmt.Printf("[%d] %s - %s\n", r.Status, r.UrlString, r.Title)
}
```

### 流式

```go
resultCh, err := engine.CheckStream(ctx, urls)
for r := range resultCh {
    fmt.Printf("[%d] %s\n", r.Status, r.UrlString)
}
```

> 示例：[`examples/spray/main.go`](../examples/spray/main.go)

## 路径爆破

### 单目标

```go
wordlist := []string{"admin", "login", "api", "config", ".git/config"}

results, err := engine.Brute(ctx, "http://target.com", wordlist)
for _, r := range results {
    fmt.Printf("[%d] %s\n", r.Status, r.UrlString)
}

// 流式版本
resultCh, err := engine.BruteStream(ctx, baseURL, wordlist)
```

### 多目标

```go
urls := []string{"http://a.com", "http://b.com", "http://c.com"}
results, err := engine.BruteMany(ctx, urls, wordlist)

// 流式版本
resultCh, err := engine.BruteManyStream(ctx, urls, wordlist)
```

## Context 配置

### 基础配置

```go
ctx := spray.NewContext().
    SetThreads(50).           // 并发线程数
    SetTimeout(10).           // 超时（秒）
    SetMethod("POST").        // HTTP 方法
    SetHeaders([]string{      // 自定义请求头
        "Authorization:Bearer token",
        "X-Custom:value",
    }).
    SetHost("internal.com").  // 自定义 Host 头
    SetFilter("--fc 404").    // 过滤规则
    SetMatch("--mc 200,301")  // 匹配规则
```

### 插件配置

```go
ctx := spray.NewContext().
    SetAdvance(true).          // 启用全部插件
    SetActivePlugin(true).     // 主动指纹路径探测
    SetReconPlugin(true).      // 信息提取
    SetBakPlugin(true).        // 备份文件发现
    SetCommonPlugin(true).     // 常见文件发现
    SetCrawlPlugin(true).      // 爬虫
    SetCrawlDepth(3).          // 爬虫深度
    SetFinger(true).           // 主动指纹检测
    SetFuzzuliPlugin(true).    // Fuzzuli 文件猜测
    SetRecursiveDepth(2).      // 递归深度
    SetExtracts([]string{"recon"})  // 信息提取规则
```

### 字典配置

```go
ctx := spray.NewContext().
    SetDictionaries([]string{"dict1.txt", "dict2.txt"}).
    SetRules([]string{"rule1.txt"}).
    SetWord("custom-word").
    SetDefaultDict(true)
```

## 爬虫 + 深度指纹

结合 Spray 的爬虫和 Fingers 的主动指纹识别：

```go
sprayEng := spray.NewEngine(spray.NewConfig().WithMatchDetail())
sprayEng.Init()

ctx := spray.NewContext().
    SetThreads(4).
    SetTimeout(5).
    SetCrawlPlugin(true).
    SetFinger(true).
    SetCrawlDepth(2)

results, err := sprayEng.Brute(ctx, "http://target.com", []string{"/"})
for _, r := range results {
    for _, fw := range r.Frameworks {
        fmt.Printf("%s: %s\n", r.UrlString, fw.Name)
        if fw.MatchDetail != nil {
            fmt.Printf("  rule=%d type=%s\n",
                fw.MatchDetail.RuleIndex, fw.MatchDetail.MatcherType)
        }
    }
}
```

> 示例：[`examples/cases/spray_crawl_finger/main.go`](../examples/cases/spray_crawl_finger/main.go)

## 扫描结果

`*types.SprayResult` 包含：

```go
r.UrlString    // 完整 URL
r.Status       // HTTP 状态码
r.Title        // 页面标题
r.Frameworks   // 识别到的指纹 map[string]*Framework
r.Source       // 结果来源（check/crawl/finger/word 等）
r.IsValid      // 是否为有效结果
```

## 通过统一接口使用

```go
task := spray.NewCheckTask(urls)
resultCh, err := engine.Execute(ctx, task)

for result := range resultCh {
    if data, ok := types.ResultData[*types.SprayResult](result); ok {
        fmt.Printf("[%d] %s\n", data.Status, data.UrlString)
    }
}
```

## 统计回调

```go
ctx := spray.NewContext().
    SetStatsHandler(func(s types.Stats) {
        fmt.Printf("targets=%d requests=%d results=%d duration=%v\n",
            s.Targets, s.Requests, s.Results, s.Duration)
    })
```

## Host 碰撞

Spray 支持 Host 碰撞检测，使用 `host` 模式对目标 IP 尝试不同的 Host 头：

```go
ctx := spray.NewContext().SetMod("host")
```

> 示例：[`examples/spray/host_spray_sdk.go`](../examples/spray/host_spray_sdk.go)（需要 `hostspray` build tag）

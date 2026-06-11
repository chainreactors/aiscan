# Fingers - 指纹识别

Fingers 引擎用于识别 Web 应用和网络服务的技术栈，支持被动匹配和主动探测两种模式。

## 创建引擎

```go
// 从 Cyberhub 加载
config := fingers.NewConfig().
    WithProvider(cyberhub.NewProvider("http://hub:8080", "api-key"))

// 从本地文件加载
config := fingers.NewConfig().
    WithProvider(provider.NewFileProvider("path", ""))

// 直接传入数据
config := fingers.NewConfig().
    WithFingers(myFingers).
    WithAliases(myAliases)

engine, err := fingers.NewEngine(config)
```

Config 的其他选项：

```go
config.WithMatchDetail()                        // 开启匹配细节（命中的规则索引、匹配值等）
config.WithFilter(func(f *fingers.FullFinger) bool {  // 过滤指纹
    return f.Finger.Protocol == "http"
})
```

> 源码：[`fingers/config.go`](../fingers/config.go)

## 被动匹配

对已有的 HTTP 响应数据进行指纹匹配，不发送额外请求：

```go
// 从原始字节匹配
frameworks, err := engine.Match(rawHTTPData)

// 从 http.Response 匹配
frameworks, err := engine.MatchHTTP(resp)

// 从 Favicon 图标匹配
frameworks, err := engine.MatchFavicon(faviconBytes)
```

返回的 `Frameworks` 是 `map[string]*Framework`，包含识别到的技术名称、版本等信息：

```go
for _, fw := range frameworks {
    fmt.Printf("name=%s version=%s\n", fw.Name, fw.Version)
    if cpe := fw.CPE(); cpe != "" {
        fmt.Printf("cpe=%s\n", cpe)
    }
}
```

## 主动探测

向目标发送额外请求来识别更多指纹。Level 越高，发送的探测请求越多。

### HTTP 主动探测

```go
ctx := fingers.NewContext().
    WithTimeout(10).  // 超时（秒）
    WithLevel(2).     // 探测级别 0-3
    WithProxy("socks5://127.0.0.1:1080")  // 可选代理

// 批量同步
results, err := engine.HTTPMatch(ctx, []string{
    "http://example.com",
    "https://192.168.1.1:8443",
})

// 流式
resultCh, err := engine.HTTPMatchStream(ctx, urls)
for result := range resultCh {
    fmt.Printf("target=%s results=%d\n", result.Target, len(result.Results))
    for _, sr := range result.Results {
        fmt.Println(" ", sr.Framework.Name)
    }
}
```

### 服务主动探测

针对 TCP 服务进行指纹识别：

```go
ctx := fingers.NewContext().
    WithTimeout(10).
    WithLevel(3)  // 服务探测级别 0-9

results, err := engine.ServiceMatch(ctx, []string{
    "192.168.1.1:22",
    "192.168.1.1:3306",
})
```

> 源码：[`fingers/engine.go`](../fingers/engine.go) | 示例：[`examples/fingers/main.go`](../examples/fingers/main.go)

## 匹配细节（MatchDetail）

开启后可以查看每个指纹的匹配细节，用于调试和分析：

```go
config := fingers.NewConfig().WithMatchDetail()
engine, _ := fingers.NewEngine(config)

frameworks, _ := engine.Match(data)
for _, fw := range frameworks {
    if fw.MatchDetail != nil {
        fmt.Printf("rule_index=%d matcher_type=%s value=%s\n",
            fw.MatchDetail.RuleIndex,
            fw.MatchDetail.MatcherType,
            fw.MatchDetail.MatcherValue)
    }
}
```

> 示例：[`examples/cases/match_detail/main.go`](../examples/cases/match_detail/main.go)

## Alias 关联

引擎加载时会同时加载 Alias 数据，Alias 将指纹名映射到关联的 POC ID：

```go
aliases := engine.Aliases()

// 底层 engine 的 Aliases 可用于查找关联 POC
libEngine := engine.Get()
for _, fw := range frameworks {
    if a, ok := libEngine.Aliases.FindFramework(fw); ok {
        fmt.Printf("%s -> POCs: %v\n", fw.Name, a.Pocs)
    }
}
```

## 通过统一接口使用

Fingers 也实现了 `types.Engine` 接口，可以通过 `Execute` 使用：

```go
task := fingers.NewMatchTask(rawData)
resultCh, err := engine.Execute(ctx, task)
for result := range resultCh {
    matchResult := result.(*fingers.MatchResult)
    fmt.Println(matchResult.Frameworks())
}
```

## TargetResult 结构

批量探测返回 `*TargetResult`：

```go
type TargetResult struct {
    Target  string                  // 目标地址
    Results []*types.ServiceResult  // 匹配到的指纹列表
    Err     error                   // 错误信息
}
```

> 源码：[`fingers/types.go`](../fingers/types.go)

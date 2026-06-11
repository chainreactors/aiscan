# Neutron - POC 扫描

Neutron 引擎用于加载和执行 POC 模板，对目标进行漏洞检测。SDK 负责加载和编译模板，用户负责选择和执行。

## 创建引擎

```go
// 从 Cyberhub 加载
config := neutron.NewConfig().
    WithProvider(cyberhub.NewProvider("http://hub:8080", "api-key"))

// 从本地目录加载
config := neutron.NewConfig().
    WithProvider(provider.NewFileProvider("path", ""))

// 直接传入模板
config := neutron.NewConfig().
    WithTemplates(myTemplates)

engine, err := neutron.NewEngine(config)
```

加载时会自动编译所有模板（设置超时、协议处理等），编译失败的模板会被静默跳过。

Config 的其他选项：

```go
config.WithCapacity(10)  // 限制同时执行的 POC 数量
config.WithFilter(func(t *types.Template) bool {  // 过滤模板
    return t.Info.Severity == "critical"
})
```

> 源码：[`neutron/config.go`](../neutron/config.go)

## 执行扫描

### 通过便捷 API

模板加载后，可以直接调用 `Execute` 执行：

```go
templates := engine.Get()  // 获取所有已编译模板
ctx := neutron.NewContext()

task := neutron.NewExecuteTask("http://target.com")
resultCh, err := engine.Execute(ctx, task)

for result := range resultCh {
    execResult := result.(*neutron.ExecuteResult)
    if execResult.Matched() {
        t := execResult.Template()
        fmt.Printf("matched: %s [%s] %s\n", t.Id, t.Info.Severity, t.Info.Name)
    }
}
```

### 执行特定模板

通过 Task 指定要执行的模板子集：

```go
task := &neutron.ExecuteTask{
    Target:    "http://target.com",
    Templates: selectedTemplates,  // 只执行这些模板
    Payload:   map[string]interface{}{  // 自定义变量（可选）
        "username": "admin",
    },
}
```

### 直接使用 Template.Execute

也可以跳过 Engine，直接调用模板的 Execute 方法：

```go
for _, t := range templates {
    result, err := t.Execute(target, nil)
    if err != nil {
        continue
    }
    if result != nil && result.Matched {
        fmt.Printf("matched: %s\n", t.Id)
    }
}
```

> 示例：[`examples/neutron/main.go`](../examples/neutron/main.go)

## 结果结构

`ExecuteResult` 包含完整的执行信息：

```go
execResult := result.(*neutron.ExecuteResult)

execResult.Success()     // 是否执行成功（不是匹配成功）
execResult.Matched()     // 是否命中漏洞
execResult.Template()    // 执行的模板 *types.Template
execResult.Result()      // *NeutronResult，包含 OperatorResult 和 Events
```

`NeutronResult` 的详细数据：

```go
nr := execResult.Result()
nr.Result   // *types.OperatorResult —— 匹配结果
nr.Events   // []*types.ResultEvent  —— 执行事件（每个请求/响应步骤）
```

通过 Events 可以获取 POC 执行的每一步请求和响应数据：

```go
result, events, err := template.ExecuteWithEvents(target, nil)
for _, event := range events {
    fmt.Printf("request: %s\n", event.Request)
    fmt.Printf("response: %s\n", event.Response)
}
```

> 示例：[`examples/cases/request_response/main.go`](../examples/cases/request_response/main.go)

## 模板过滤

加载后在内存中按需过滤：

```go
// 按严重程度
var critical []*types.Template
for _, t := range engine.Get() {
    if t.Info.Severity == "critical" {
        critical = append(critical, t)
    }
}

// 按标签
for _, t := range engine.Get() {
    for _, tag := range t.GetTags() {
        if tag == "rce" { ... }
    }
}

// 按 ID
for _, t := range engine.Get() {
    if strings.ToLower(t.Id) == "cve-2021-44228" { ... }
}
```

也可以在加载远程数据时通过 Filter 提前过滤，减少网络传输（见 [Cyberhub 数据源](cyberhub.md)）。

## 并发控制

```go
// 限制最多同时执行 10 个 POC
engine.SetCapacity(10)

// 多个 goroutine 同时调用 Execute 时，
// 超出容量的调用会阻塞等待
```

> 源码：[`neutron/engine.go`](../neutron/engine.go)

## 模板信息

每个模板包含以下元信息：

```go
t.Id                 // 模板 ID，如 "CVE-2021-44228"
t.Info.Name          // 名称
t.Info.Severity      // 严重程度：info/low/medium/high/critical
t.Info.Description   // 描述
t.Info.Tags          // 标签字符串，用逗号分隔
t.GetTags()          // 标签列表 []string
t.Fingers            // 关联的指纹名列表
```

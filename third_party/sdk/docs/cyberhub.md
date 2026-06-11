# Cyberhub 数据源

Cyberhub 是指纹和 POC 数据的远程管理平台。SDK 通过 `cyberhub.Provider` 从 Cyberhub 加载数据。

## 创建 Provider

```go
provider := cyberhub.NewProvider("http://hub:8080", "your-api-key")
```

可选配置：

```go
provider.WithTimeout(15 * time.Second)  // 请求超时
provider.WithFilter(filter)             // 过滤条件
```

> 源码：[`pkg/cyberhub/provider.go`](../pkg/cyberhub/provider.go)

## 加载数据

### 加载指纹 + 别名

```go
fingers, aliases, err := provider.Fingers(ctx)
// fingers: []*types.Finger —— 指纹规则
// aliases: []*types.Alias  —— 别名（包含关联 POC ID）
```

### 加载 POC 模板

```go
templates, err := provider.POCs(ctx)
// templates: []*types.Template
```

## 过滤条件

通过 `ExportFilter` 在请求时过滤数据，减少传输量：

```go
filter := types.NewExportFilter().
    WithNames("tomcat", "nginx").           // 按名称
    WithTags("cms", "middleware").           // 按标签
    WithSources("github", "custom").        // 按来源
    WithSeverities("high", "critical").     // 按严重程度
    WithPOCType("http").                    // POC 类型
    WithStatuses("active").                 // POC 状态
    WithReviewStatus("reviewed").           // 审核状态
    WithCreatedAfter(time.Now().AddDate(0, -1, 0)).  // 创建时间
    WithUpdatedAfter(time.Now().AddDate(0, 0, -7)).  // 更新时间
    WithLimit(100)                          // 最大数量

provider.WithFilter(filter)
```

> 源码：[`pkg/types/export_filter.go`](../pkg/types/export_filter.go)

## 配合引擎使用

Provider 是各引擎的数据加载方式之一：

```go
provider := cyberhub.NewProvider(url, key).
    WithFilter(types.NewExportFilter().WithSeverities("critical"))

// Fingers
fingersConfig := fingers.NewConfig().WithProvider(provider)
fingersEng, _ := fingers.NewEngine(fingersConfig)

// Neutron
neutronConfig := neutron.NewConfig().WithProvider(provider)
neutronEng, _ := neutron.NewEngine(neutronConfig)

// GoGo（一个 Provider 自动创建 fingers + neutron）
gogoConfig := gogo.NewConfig().WithProvider(provider)
gogoEng := gogo.NewEngine(gogoConfig)
gogoEng.Init()
```

> 示例：[`examples/cyberhub/main.go`](../examples/cyberhub/main.go)

## 远程过滤 vs 本地过滤

| | 远程过滤 | 本地过滤 |
|---|---------|---------|
| 时机 | 请求 Cyberhub API 时 | 数据加载到内存后 |
| 方式 | `ExportFilter` | Config 的 `WithFilter` 或手动遍历 |
| 优势 | 减少网络传输 | 更灵活的过滤逻辑 |

两种方式可以组合使用：先远程粗筛，再本地精选。

```go
// 远程：只下载 critical 级别的 POC
filter := types.NewExportFilter().WithSeverities("critical")
provider.WithFilter(filter)

// 本地：在 critical 中进一步按标签筛选
config := neutron.NewConfig().
    WithProvider(provider).
    WithFilter(func(t *types.Template) bool {
        for _, tag := range t.GetTags() {
            if tag == "rce" {
                return true
            }
        }
        return false
    })
```

> 示例：[`examples/filter/main.go`](../examples/filter/main.go)

# Association 关联查询

Association 模块提供指纹、别名、模板、CVE 之间的跨域关联查询。通过构建内存索引，实现"由指纹查 POC"、"由 CVE 查指纹"等关联查找。

## 核心概念

```
  Finger ←──→ Alias ←──→ Template
    ↑                        ↑
    └── Tags, Service,       └── CVE, Tags,
        CPE, Attributes          Severity
```

- **Finger**：指纹规则（如 "tomcat"）
- **Alias**：别名，将指纹名映射到 POC 列表和元数据
- **Template**：POC 模板（如 "CVE-2021-44228"）

Index 在内存中维护它们的双向关联，支持从任意维度查询。

## 构建索引

### 从 Cyberhub 加载

```go
provider := cyberhub.NewProvider(url, key)
idx, err := association.BuildFromProvider(ctx, provider)
```

### 从本地数据构建

```go
idx := association.NewIndex()
idx.BuildWithFingers(fingers, aliases, templates)
```

### 带选项构建

```go
idx, err := association.BuildFromProviderWithOptions(ctx, provider,
    association.IndexOptions{
        MetadataKeys: []string{"category", "service"},  // 额外索引的元数据键
    },
)
```

> 源码：[`pkg/association/index.go`](../pkg/association/index.go)

## 查询

### 基础查询

```go
q := association.NewQuery().WithFingers("tomcat")
result := idx.Lookup(q)

// result.Fingers   —— 匹配的指纹
// result.Aliases   —— 关联的别名
// result.Templates —— 关联的 POC 模板
```

### 多维查询

```go
q := association.NewQuery().
    WithFingers("tomcat", "nginx").  // 按指纹名
    WithAliases("apache").           // 按别名
    WithTemplates("CVE-2022-0001").  // 按模板 ID
    WithCVEs("CVE-2021-44228").      // 按 CVE
    WithTags("rce").                 // 按标签
    WithServices("http").            // 按服务类型
    WithCPEs("apache/tomcat").       // 按 CPE
    WithAttr("severity", "critical") // 按属性

result := idx.Lookup(q)
```

### 从扫描结果查询

扫描结果可以直接转为查询条件：

```go
// 方式一：通过 Client 便捷方法（推荐）
for result := range resultCh {
    related, _ := c.LookupResult(result)
    for _, t := range related.Templates {
        fmt.Printf("related POC: %s\n", t.Id)
    }
}

// 方式二：手动构建查询
q := association.QueryFromResult(result)
related := idx.Lookup(q)

// 从 Frameworks 构建查询
q := association.NewQuery().WithFrameworks(gogoResult.Frameworks)

// 从 Vulns 构建查询
q := association.NewQuery().WithVulns(gogoResult.Vulns)
```

> 源码：[`pkg/association/query.go`](../pkg/association/query.go)

## 单实体查找

```go
finger := idx.Finger("tomcat")       // 按名称查指纹
alias := idx.Alias("tomcat")         // 按名称查别名
template := idx.Template("CVE-xxx")  // 按 ID 查模板

// 批量查找
fingers := idx.Fingers("tomcat", "nginx")
aliases := idx.Aliases("tomcat", "nginx")
templates := idx.Templates("CVE-001", "CVE-002")
```

## 查询合并

```go
q1 := association.NewQuery().WithFingers("tomcat")
q2 := association.NewQuery().WithCVEs("CVE-2021-44228")
q1.Merge(q2)

result := idx.Lookup(q1)
```

## 通过 Client 使用

Client 的 `WithIndex` 开启关联索引，各引擎产出的结果可以直接关联查询：

```go
c := client.New(
    client.WithProvider(provider),
    client.WithIndex(nil),  // 开启关联索引
)
defer c.Close()

// 便捷查询
result, _ := c.LookupByFinger("tomcat")
result, _ := c.LookupByCVE("CVE-2021-44228")

// 扫描结果直接关联
gogoEng, _ := c.Gogo()
resultCh, _ := gogoEng.ScanStream(ctx, ip, ports)
for r := range resultCh {
    related, _ := c.LookupResult(r)
    // related.Templates — 关联的 POC
    // related.Aliases   — 关联的别名
}
```

## 完整示例

内联数据演示（不需要 Cyberhub）：

```go
idx := association.NewIndex(association.WithMetadataKeys("category"))
idx.BuildWithFingers(fingers, aliases, templates)

// finger → alias → template
result := idx.Lookup(association.NewQuery().WithFingers("apache tomcat"))
fmt.Println("templates:", result.Templates)

// template → alias → finger
result = idx.Lookup(association.NewQuery().WithTemplates("CVE-2022-0001"))
fmt.Println("fingers:", result.Fingers)

// CVE → template → finger
result = idx.Lookup(association.NewQuery().WithCVEs("CVE-2021-44228"))
fmt.Println("fingers:", result.Fingers)

// 属性查询
result = idx.Lookup(association.NewQuery().WithAttr("severity", "medium"))
fmt.Println("templates:", result.Templates)
```

> 示例：[`examples/association/main.go`](../examples/association/main.go)

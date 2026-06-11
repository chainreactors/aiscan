# Association Index

`pkg/association` 用一个 `Query` 入口完成 finger、alias、template、CVE、tag、service、CPE 和属性之间的关联查询。

## 最小用法

```go
idx := association.NewIndex()
idx.BuildWithFingers(fingers, aliases, templates)

result := idx.Lookup(
    association.NewQuery().WithFingers("tomcat"),
)

for _, tpl := range result.Templates {
    fmt.Println(tpl.Id)
}
```

`QueryResult` 返回完整实体：

```go
type QueryResult struct {
    Fingers   types.Fingers
    Aliases   []*types.Alias
    Templates []*types.Template
}
```

## 从 Cyberhub 构建

```go
hub := cyberhub.NewProvider(url, apiKey).
    WithFilter(types.NewExportFilter().WithSources("github"))

idx, err := association.BuildFromProvider(ctx, hub)
```

## Query 组合

```go
q := association.NewQuery().
    WithFingers("tomcat").
    WithTemplates("CVE-2022-0001").
    WithTags("rce").
    WithServices("http").
    WithCPEs("apache/tomcat").
    WithCVEs("CVE-2021-44228").
    WithAttr("severity", "critical")

result := idx.Lookup(q)
```

需要索引自定义 metadata 字段时显式声明：

```go
idx := association.NewIndex(association.WithMetadataKeys("category"))
```

## 从扫描结果查询

```go
for result := range resultCh {
    related := idx.Lookup(association.QueryFromResult(result))
    _ = related.Templates
}
```

## 示例

```bash
go run ./examples/association
go run ./examples/association -finger "apache tomcat"
go run ./examples/association -url http://127.0.0.1:8080 -key your-api-key -finger tomcat
```

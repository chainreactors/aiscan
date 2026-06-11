# Cyberhub Provider

`pkg/cyberhub` 是远程数据源适配层，负责从 Cyberhub 导出 `types.Fingers`、`types.Alias` 和 `types.Template`。

## 最小用法

```go
hub := cyberhub.NewProvider(url, apiKey).
    WithTimeout(15 * time.Second).
    WithFilter(types.NewExportFilter().
        WithSources("github").
        WithTags("cms").
        WithLimit(100))

fingers, aliases, err := hub.Fingers(ctx)
templates, err := hub.POCs(ctx)
```

## 接入引擎

```go
hub := cyberhub.NewProvider(url, apiKey)

fingersConfig := fingers.NewConfig().WithProvider(hub)
neutronConfig := neutron.NewConfig().WithProvider(hub)
gogoConfig := gogo.NewConfig().WithProvider(hub)
```

如果指纹和 POC 需要不同过滤条件，创建不同 Provider。

## 说明

- filter 从 `types.NewExportFilter()` 创建，避免用户感知多套类型入口。
- URL 会自动补齐 `/api/v1`。
- API Key 使用 `X-API-Key` 请求头。
- 客户端会请求并自动解压 gzip 响应。
- `POCs()` 在未指定状态时默认请求 active POC。

## 示例

```bash
go run ./examples/cyberhub -url http://127.0.0.1:8080 -key your-api-key -source github -limit 20
```

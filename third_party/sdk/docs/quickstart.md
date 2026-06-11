# 快速开始

## 安装

```bash
go get github.com/chainreactors/sdk@latest
```

## 最小示例：通过 Client 使用 SDK

SDK 提供统一的 `client.Client`，一行代码获取任意引擎：

```go
package main

import (
    "fmt"

    "github.com/chainreactors/sdk/client"
    "github.com/chainreactors/sdk/gogo"
    "github.com/chainreactors/sdk/spray"
)

func main() {
    c := client.New()
    defer c.Close()

    // 指纹识别
    fingersEngine, _ := c.Fingers()
    frameworks, _ := fingersEngine.Match([]byte("HTTP/1.1 200 OK\r\nServer: nginx\r\n\r\n"))
    for _, fw := range frameworks {
        fmt.Println("finger:", fw.Name)
    }

    // 端口扫描
    gogoEngine, _ := c.Gogo()
    results, _ := gogoEngine.Scan(gogo.NewContext(), "127.0.0.1", "80,443")
    for _, r := range results {
        fmt.Printf("%s:%s open\n", r.Ip, r.Port)
    }

    // URL 检测
    sprayEngine, _ := c.Spray()
    sprayResults, _ := sprayEngine.Check(spray.NewContext(), []string{"http://example.com"})
    for _, r := range sprayResults {
        fmt.Printf("[%d] %s\n", r.Status, r.UrlString)
    }
}
```

> 完整可运行代码见 [`examples/sdk_usage/main.go`](../examples/sdk_usage/main.go)

## 通过 Provider 共享数据源

配置一个 Cyberhub Provider，所有引擎自动共享：

```go
provider := cyberhub.NewProvider("http://hub:8080", "api-key")
c := client.New(client.WithProvider(provider))
defer c.Close()

// Fingers、Neutron 自动从 Provider 加载数据
// GoGo 自动注入 Fingers + Neutron 引擎
// Spray 自动注入 Fingers 引擎
gogoEngine, _ := c.Gogo()
results, _ := gogoEngine.Scan(gogo.NewContext(), "192.168.1.0/24", "80,443")
```

## Client 的三种用法

### 1. 零配置（本地默认数据）

```go
c := client.New()
```

### 2. 共享 Provider（推荐）

```go
c := client.New(client.WithProvider(provider))
```

### 3. 共享 Provider + 单引擎自定义

```go
c := client.New(
    client.WithProvider(provider),
    client.WithGogoConfig(gogo.NewConfig().WithCapacity(5000)),
    client.WithSprayConfig(spray.NewConfig().WithMatchDetail()),
)
```

自定义配置中的空字段会自动从共享 Provider 和依赖引擎补全。

## 依赖自动注入

Client 管理引擎间的依赖关系：

```
GoGo  → 自动注入 Fingers + Neutron
Spray → 自动注入 Fingers
```

调用 `c.Gogo()` 时，如果 Fingers 和 Neutron 尚未创建，Client 会先自动创建它们。

## 关联查询（可选）

通过 `WithIndex` 开启关联索引，将各引擎产出的数据进行关联：

```go
c := client.New(
    client.WithProvider(provider),
    client.WithIndex(nil),  // 开启关联索引
)

// 按指纹查关联 POC
result, _ := c.LookupByFinger("tomcat")
for _, t := range result.Templates {
    fmt.Println(t.Id)
}

// 扫描结果直接关联
gogoEng, _ := c.Gogo()
resultCh, _ := gogoEng.ScanStream(gogo.NewContext(), "192.168.1.1", "80")
for r := range resultCh {
    related, _ := c.LookupResult(r)
    // related.Templates, related.Aliases, related.Fingers
}
```

详见 [Association 关联查询](association.md)。

## 直接创建引擎

如果只需要单个引擎且需要完全控制配置，可以跳过 Client：

```go
config := fingers.NewConfig().
    WithProvider(cyberhub.NewProvider(url, key))

engine, err := fingers.NewEngine(config)
```

## 数据加载

每个引擎都需要加载数据（指纹库、POC 模板等），有三种来源：

```go
// 1. 从 Cyberhub 远程加载（通过 Provider 或 Client）
config.WithProvider(cyberhub.NewProvider("http://hub:8080", "api-key"))

// 2. 从本地文件加载
config.WithProvider(provider.NewFileProvider("path", ""))

// 3. 直接传入内存数据
config.WithFingers(myFingers)
```

## 下一步

- 理解 SDK 的设计抽象 → [核心概念](concepts.md)
- 深入某个引擎 → [Fingers](fingers.md) / [Neutron](neutron.md) / [GoGo](gogo.md) / [Spray](spray.md) / [Zombie](zombie.md)

# Chainreactors SDK

统一的安全扫描工具 Go SDK，提供一致的接口设计。

## 概述

Chainreactors SDK 为多个安全扫描工具提供统一接口：

- **Fingers**: Web 指纹识别（HTTP/Socket）
- **Neutron**: POC/漏洞扫描
- **GoGo**: 集成指纹识别和 POC 检测的端口扫描
- **Spray**: HTTP 批量检测和路径爆破
- **Zombie**: 弱口令检测和未授权访问检测
- **Cyberhub Provider**: 统一远程数据源，导出指纹、alias 和 POC
- **Association**: 统一关联查询，从 finger、alias、template、CVE 等条件互相查找

详细文档见 [docs/](docs/README.md)。

## 安装

```bash
go get github.com/chainreactors/sdk
```

## 快速开始

### 通过 Client 使用（推荐）

Client 是 SDK 的统一入口，管理引擎生命周期、依赖注入和关联查询：

```go
import (
    "github.com/chainreactors/sdk/client"
    "github.com/chainreactors/sdk/pkg/cyberhub"
    "github.com/chainreactors/sdk/gogo"
)

// 创建 Client，共享 Provider，开启关联索引
provider := cyberhub.NewProvider("http://127.0.0.1:8080", "your_key")
c := client.New(
    client.WithProvider(provider),
    client.WithIndex(nil),  // 可选：开启关联查询
)
defer c.Close()

// 获取引擎 — 依赖自动注入
// GoGo 自动获得 Fingers + Neutron 引擎
gogoEng, _ := c.Gogo()
results, _ := gogoEng.Scan(gogo.NewContext().SetThreads(1000), "192.168.1.0/24", "80,443")

// 关联查询 — 扫描结果直接查关联 POC
for _, r := range results {
    related, _ := c.LookupByFinger(r.Frameworks.Names()...)
    // related.Templates — 关联的 POC
}
```

Client 的依赖注入关系：

```
GoGo  → 自动注入 Fingers + Neutron
Spray → 自动注入 Fingers
```

### 数据源（Provider）

所有数据通过 `types.Provider` 接口显式加载，支持多个数据源合并：

```go
import (
    "github.com/chainreactors/sdk/pkg/cyberhub"
    "github.com/chainreactors/sdk/pkg/provider"
)

// 内置 embed 数据
provider.NewEmbedProvider()

// 本地文件或目录
provider.NewFileProvider("fingers.yaml", "pocs/")

// 远程 URL
provider.NewURLProvider("https://example.com/fingers.yaml", "")

// CyberHub API
cyberhub.NewProvider(url, key)

// 多源合并：embed + CyberHub
client.New(client.WithProvider(
    provider.NewEmbedProvider(),
    cyberhub.NewProvider(url, key),
))
```

### 直接使用引擎

如果只需要单个引擎，可以跳过 Client 直接创建：

```go
// Fingers - 指纹识别
config := fingers.NewConfig().
    WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your_key"))
engine, _ := fingers.NewEngine(config)
frameworks, _ := engine.Match(httpResponseBytes)

// Neutron - POC 扫描
config := neutron.NewConfig().
    WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your_key"))
engine, _ := neutron.NewEngine(config)
for _, t := range engine.Get() {
    result, _ := t.Execute("http://target.com", nil)
}

// GoGo - 端口扫描（Provider 自动加载 Fingers 和 Neutron）
gogoConfig := gogo.NewConfig().
    WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your_key"))
gogoEngine := gogo.NewEngine(gogoConfig)
results, _ := gogoEngine.Scan(gogo.NewContext(), "192.168.1.0/24", "80,443")

// Spray - HTTP 检测
sprayEngine := spray.NewEngine(nil)
results, _ := sprayEngine.Check(spray.NewContext(), []string{"http://example.com"})

// Zombie - 弱口令检测
zombieEngine := zombie.NewEngine(nil)
task := zombie.NewWeakpassTask([]zombie.Target{{IP: "192.168.1.1", Port: "22", Service: "ssh"}})
results, _ := zombieEngine.Weakpass(zombie.NewContext(), task)
```

## 架构设计

### 核心接口

SDK 采用四组件架构，定义在 `pkg/types/types.go`：

| 接口 | 职责 |
|------|------|
| **Engine** | `Execute(Context, Task) → chan Result`，实现具体扫描逻辑 |
| **Context** | 携带运行时配置（线程、超时、代理等） |
| **Task** | 定义扫描目标和参数 |
| **Result** | 返回扫描结果，通过 `TypedResult[T]` 实现类型安全 |

### Client 架构

```
              ┌──────────────────────────┐
              │         Client           │
              │  WithProvider / WithIndex │
              └────┬──────────┬──────────┘
                   │          │
         ┌─────────┤   ┌──────▼──────┐
         │         │   │ Index (可选) │
         │         │   └──────────────┘
    ┌────▼───┐ ┌───▼────┐ ┌───────┐ ┌────────┐ ┌────────┐
    │ Fingers │ │ Neutron │ │ GoGo  │ │ Spray  │ │ Zombie │
    └────────┘ └────────┘ └───────┘ └────────┘ └────────┘
```

- 引擎懒加载，首次访问时创建
- 依赖自动注入（GoGo ← Fingers + Neutron，Spray ← Fingers）
- Index 是可选的关联查询层，通过 `WithIndex` 开启

### 数据源

所有引擎支持双重加载模式：

- **本地模式**: 从嵌入数据或文件系统加载
- **远程模式**: 从 Cyberhub API 加载，支持过滤条件

## Client API

### Option

```go
client.WithProvider(provider)           // 共享 Cyberhub 数据源
client.WithResourceProvider(rp)         // 共享资源加载器
client.WithIndex(opts)                  // 开启关联索引（可选）
client.WithFingersConfig(cfg)           // 覆盖 Fingers 配置
client.WithNeutronConfig(cfg)           // 覆盖 Neutron 配置
client.WithGogoConfig(cfg)              // 覆盖 GoGo 配置
client.WithSprayConfig(cfg)             // 覆盖 Spray 配置
client.WithZombieConfig(cfg)            // 覆盖 Zombie 配置
```

### 引擎访问

```go
c.Fingers()  // *fingers.Engine
c.Neutron()  // *neutron.Engine
c.Gogo()     // *gogo.GogoEngine
c.Spray()    // *spray.SprayEngine
c.Zombie()   // *zombie.Engine
```

### 关联查询

```go
c.Index()                    // 获取关联索引
c.Lookup(query)              // 通用查询
c.LookupResult(result)      // 从引擎结果查询关联
c.LookupByFinger("tomcat")  // 按指纹名查询
c.LookupByCVE("CVE-...")    // 按 CVE 查询
c.BuildIndex(ctx, opts...)   // 构建独立索引
```

## 配置

### 引擎配置

```go
// Fingers
fingers.NewConfig().
    WithProvider(provider).           // 通过 Provider 加载（CyberHub/Embed/File/URL）
    WithMatchDetail()                 // 开启匹配细节

// Neutron
neutron.NewConfig().
    WithProvider(provider).
    WithCapacity(10)                  // 并发限制

// GoGo
gogo.NewConfig().
    WithProvider(provider).           // 自动加载 Fingers + Neutron
    WithFingersEngine(fingersEng).    // 或手动注入
    WithNeutronEngine(neutronEng).
    WithCapacity(5000)

// Spray
spray.NewConfig().
    WithFingersEngine(fingersEng).
    WithMatchDetail().
    WithCapacity(3000)

// Zombie
zombie.NewConfig().
    WithCapacity(500)
```

### 运行时 Context

```go
// GoGo
gogo.NewContext().
    SetThreads(1000).
    SetVersionLevel(2).         // 0=被动, 1=基础, 2=深度, 3=全量
    SetExploit("all").          // none/all/known
    SetDelay(5)

// Spray
spray.NewContext().
    SetThreads(100).
    SetTimeout(10).
    SetCrawlPlugin(true).
    SetFinger(true)

// Zombie
zombie.NewContext().
    SetThreads(100).
    SetTimeout(5).
    SetTop(10)                  // 使用 top N 字典
```

### 数据筛选

```go
// 远程筛选（减少传输量）
filter := types.NewExportFilter().
    WithTags("cms", "rce").
    WithSources("github").
    WithSeverities("critical", "high").
    WithLimit(100)

provider := cyberhub.NewProvider(url, key).WithFilter(filter)

// 本地筛选（加载后过滤）
config := fingers.NewConfig().
    WithProvider(provider).
    WithFilter(func(f *fingers.FullFinger) bool {
        return f.Finger.Protocol == "http"
    })
```

## 命令行工具

`examples/` 目录提供了预构建的命令行工具：

```bash
cd examples
go build -o bin/fingers ./fingers
go build -o bin/neutron ./neutron
go build -o bin/gogo ./gogo
go build -o bin/spray ./spray
go build -o bin/cyberhub ./cyberhub
go build -o bin/association ./association
```

详细使用方法参见 [examples/README.md](examples/README.md)。

## 项目结构

- `client/` — 统一客户端（依赖注入、关联查询）
- `fingers/` — 指纹识别引擎
- `neutron/` — POC 扫描引擎
- `gogo/` — 端口扫描引擎
- `spray/` — HTTP 检测引擎
- `zombie/` — 弱口令检测引擎
- `pkg/types/` — 核心接口（Engine / Provider / Context / Task / Result）
- `pkg/cyberhub/` — CyberHub 远程数据源
- `pkg/provider/` — 内置数据源（EmbedProvider / FileProvider / URLProvider）
- `pkg/association/` — 关联索引
- `examples/` — 示例程序
- `docs/` — 用户文档
    ├── quickstart.md    # 快速开始
    ├── concepts.md      # 核心概念
    ├── fingers.md       # Fingers 引擎
    ├── neutron.md       # Neutron 引擎
    ├── gogo.md          # GoGo 引擎
    ├── spray.md         # Spray 引擎
    ├── cyberhub.md      # Cyberhub 数据源
    └── association.md   # 关联查询
```

## 开发

### 运行测试

```bash
go test ./...
```

### 添加新引擎

1. 实现 `pkg/types` 中的 Engine / Context / Task / Result 接口
2. 创建引擎包（engine.go / types.go / init.go）
3. 在 `client/client.go` 中添加 ensure 方法和访问器
4. 在 `examples/` 中添加示例

## License

MIT License

## 相关项目

- [Fingers](https://github.com/chainreactors/fingers) - 指纹识别库
- [Neutron](https://github.com/chainreactors/neutron) - POC 框架
- [GoGo](https://github.com/chainreactors/gogo) - 端口扫描器
- [Spray](https://github.com/chainreactors/spray) - HTTP 扫描器
- [Zombie](https://github.com/chainreactors/zombie) - 弱口令检测

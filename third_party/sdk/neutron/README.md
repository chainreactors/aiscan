# Neutron SDK

Neutron SDK 为 [chainreactors/neutron](https://github.com/chainreactors/neutron) POC 引擎提供了简洁的加载接口，支持从本地文件和 Cyberhub 远程加载 POC。

## 设计理念

**SDK = Loader，用户 = Composer**

- 提供加载/编译入口，用户自行组装复杂功能
- 不过度封装，通过 `types.Template` 暴露模板数据
- 支持本地与 Cyberhub 数据源

## 快速开始

### 1. 从 Cyberhub 加载 POC

```go
import (
    "github.com/chainreactors/sdk/pkg/cyberhub"
    "github.com/chainreactors/sdk/neutron"
)

// 最简单的方式
config := neutron.NewConfig()
config.WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))
engine, err := neutron.NewEngine(config)
if err != nil {
    log.Fatal(err)
}
templates := engine.Get()

fmt.Printf("加载了 %d 个 POC\n", len(templates))
```

### 2. 从本地目录加载 POC

```go
// 加载指定目录的所有 YAML 文件
config := neutron.NewConfig()
config.WithProvider(provider.NewFileProvider("path", ""))
engine, err := neutron.NewEngine(config)
if err != nil {
    log.Fatal(err)
}
templates := engine.Get()
```

### 3. 高级配置

```go
config := neutron.NewConfig()
config.WithProvider(
    cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key").
        WithFilter(types.NewExportFilter().WithTags("cve", "rce")),
)
config.Timeout = 30 * time.Second

engine, err := neutron.NewEngine(config)
if err != nil {
    log.Fatal(err)
}
templates := engine.Get()
```

需要本地加载时使用 `FileProvider`：

```go
config := neutron.NewConfig()
config.WithProvider(provider.NewFileProvider("", "./pocs")) // 目录或单个 YAML 文件
engine, _ := neutron.NewEngine(config)
```

## API 参考

### `neutron.NewEngine(config *Config)`

初始化引擎时完成加载与编译：

```go
config := neutron.NewConfig()
config.WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))

engine, err := neutron.NewEngine(config)
if err != nil {
    log.Fatal(err)
}
templates := engine.Get()
```

## 配置选项

```go
type Config struct {
    Providers []types.Provider   // 数据源（CyberHub/Embed/File/URL），支持多源合并
    Templates neutron.Templates  // 已加载的 POC
    Timeout   time.Duration     // 模板执行超时
}
```

## 使用示例

### 示例 1: 从 Cyberhub 加载并执行

```go
package main

import (
    "fmt"
    "github.com/chainreactors/sdk/pkg/cyberhub"
    "github.com/chainreactors/sdk/neutron"
)

func main() {
    // 1. 加载 POC
    config := neutron.NewConfig()
    config.WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))
    engine, err := neutron.NewEngine(config)
    if err != nil {
        panic(err)
    }
    templates := engine.Get()
    fmt.Printf("✅ 加载了 %d 个 POC\n", len(templates))

    // 3. 执行 POC
    targetURL := "http://example.com"
    for _, t := range templates {
        result, err := t.Execute(targetURL, nil)
        if err != nil {
            continue
        }
        if result != nil && result.Matched {
            fmt.Printf("🎯 匹配: %s - %s\n", t.Id, t.Info.Name)
        }
    }
}
```

### 示例 2: 流式批量扫描（用户组装）

```go
package main

import (
    "fmt"
    "sync"
    "github.com/chainreactors/sdk/pkg/cyberhub"
    "github.com/chainreactors/sdk/neutron"
    "github.com/chainreactors/sdk/pkg/types"
)

func main() {
    // 1. 加载并编译 POC
    config := neutron.NewConfig()
    config.WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))
    engine, _ := neutron.NewEngine(config)
    compiledPOCs := engine.Get()

    // 2. 用户自己组装流式扫描
    type ScanTask struct {
        Target string
        POC    *types.Template
    }

    targets := []string{"http://example.com", "http://test.com"}

    inputCh := make(chan ScanTask, 100)
    outputCh := make(chan bool, 100)

    // 生产者
    go func() {
        defer close(inputCh)
        for _, target := range targets {
            for _, poc := range compiledPOCs {
                inputCh <- ScanTask{Target: target, POC: poc}
            }
        }
    }()

    // 处理器（20 并发）
    go func() {
        defer close(outputCh)

        var wg sync.WaitGroup
        semaphore := make(chan struct{}, 20)

        for task := range inputCh {
            wg.Add(1)
            semaphore <- struct{}{}

            go func(t ScanTask) {
                defer wg.Done()
                defer func() { <-semaphore }()

                result, _ := t.POC.Execute(t.Target, nil)
                matched := result != nil && result.Matched
                outputCh <- matched
            }(task)
        }

        wg.Wait()
    }()

    // 消费者
    matchCount := 0
    for matched := range outputCh {
        if matched {
            matchCount++
        }
    }

    fmt.Printf("✅ 共匹配 %d 个 POC\n", matchCount)
}
```

### 示例 3: 混合本地和远程数据源

```go
config := neutron.NewConfig()
config.WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "your-api-key"))

engine, err := neutron.NewEngine(config)
if err != nil {
    log.Fatal(err)
}

// 追加本地 POC
if err := engine.AddPocsFile("./my_custom_pocs"); err != nil {
    log.Fatal(err)
}

templates := engine.Get()
```

## 完整示例

SDK CLI 示例参考：`examples/neutron/main.go`

## 测试结果

```bash
✅ 成功加载 9444 个 POC
✅ 成功编译 9444 个 POC
⏱️  加载速度: ~1s
```

## 与 Fingers SDK 的一致性

Neutron SDK 和 Fingers SDK 遵循相同的设计理念：

| 特性 | Fingers SDK | Neutron SDK |
|------|-------------|-------------|
| **加载入口** | `NewEngine` | `NewEngine` |
| **返回类型** | `*fingers.Engine` | `*neutron.Engine` |
| **数据源** | 本地 YAML/目录 + Cyberhub | 本地目录/文件 + Cyberhub |
| **设计理念** | SDK = Loader | SDK = Loader |

## 架构设计

```
neutron/
├── config.go       # 配置结构
└── engine.go       # 引擎实现（初始化时加载）

pkg/cyberhub/
├── client.go       # ExportPOCs() API
└── types.go        # ExportFilter compatibility alias
```

## 依赖项

- `github.com/chainreactors/neutron` - Neutron POC 引擎
- `github.com/chainreactors/sdk/pkg/cyberhub` - Cyberhub API 客户端
- `gopkg.in/yaml.v3` - YAML 解析

## 注意事项

1. **Cyberhub 必须运行** - 使用远程配置前确保 Cyberhub 服务可访问
2. **编译 POC** - 初始化引擎时自动完成
3. **变量支持** - 某些 POC 需要 wordlist、BaseDNS 等变量，通过 `Execute(target, payload)` 的 payload 参数传递
4. **错误处理** - POC 执行可能返回 `types.OpsecError`，表示 opsec 模式跳过

## License

MIT





# ina-go

Go SDK for Ina recon engines (fofa / zoomeye / hunter). Phase 1: fofa only.

Designed as a sibling of `chainreactors/sdk/{gogo,spray,zombie}`. aiscan
links this via `go.mod` instead of `exec`-ing the Python implementation.

## Quick start

```go
import (
    ina "github.com/chainreactors/ina-go"
    _   "github.com/chainreactors/ina-go/fofa" // auto-registers fofa source
)

cfg := ina.NewConfig().
    WithFofa("you@example.com", "your-key").
    WithLimit(100).
    WithLogger(ina.StdLogger(os.Stderr))

engine := ina.NewEngine(cfg)
defer engine.Close()

ctx := context.Background()
status := engine.Status(ctx) // map[string]EngineStatus, optional

// 语义化查询: ina-go 自动按 source 映射 key 名 (domain → fofa 的 domain, hunter 的 domain.suffix, ...)
assets, err := engine.Query(ctx, "fofa", ina.NewCode().And("domain", "example.com"))

// 或者透传原生语法
assets, err = engine.QueryRaw(ctx, "fofa", `domain="example.com" && icp="京ICP123"`)
```

## Status

- [x] fofa client (login check, pagination, semaphore=1 rate limit)
- [x] Code/Pair query AST + key mapping (fofa / zoomeye / hunter)
- [ ] zoomeye client (Phase 2)
- [ ] hunter client (Phase 2)
- [ ] cross-source recursion (Phase 2)

## Example

```bash
FOFA_EMAIL=... FOFA_KEY=... FOFA_QUERY='domain="example.com"' go run ./examples/fofa
```

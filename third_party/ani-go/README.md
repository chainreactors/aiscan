# ani-go

Go SDK for Ani recon engines (corporate asset graph: subsidiaries → ICP → domains).

Phase 1: `aqc_unauth` source only (qiye.baidu.com, no cookies required).

## Quick start

```go
import (
    ani "github.com/chainreactors/ani-go"
    _   "github.com/chainreactors/ani-go/aqc" // auto-registers aqc_unauth
)

cfg := ani.NewConfig().
    WithDepth(2).        // recurse 2 layers of subsidiaries
    WithPercent(0.5).    // only follow >= 50% ownership
    WithLogger(ani.StdLogger(os.Stderr))

engine := ani.NewEngine(cfg)
defer engine.Close()

assets, err := engine.Query(context.Background(), "aqc_unauth", "默安科技")
// each CompanyAsset is one (company, ICP, domain) tuple, flattened across the tree
```

## Status

- [x] aqc_unauth (qiye.baidu.com search → ICP → invest tree)
- [ ] tyc_unauth (Phase 2)
- [ ] aqc / tyc / qcc (Phase 2, require cookies)

## Example

```bash
ANI_DEPTH=2 ANI_PERCENT=0.5 go run ./examples/aqc 默安科技
```

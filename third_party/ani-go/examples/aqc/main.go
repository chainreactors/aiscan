// 真实跑一次: go run ./examples/aqc 默安科技
//
// depth/percent 可通过 env 调:
//
//	ANI_DEPTH=2 ANI_PERCENT=0.5 go run ./examples/aqc 阿里巴巴
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	ani "github.com/chainreactors/ani-go"
	_ "github.com/chainreactors/ani-go/aqc"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: aqc <company name>")
		os.Exit(1)
	}
	depth := 1
	if v := os.Getenv("ANI_DEPTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			depth = n
		}
	}
	percent := 0.5
	if v := os.Getenv("ANI_PERCENT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			percent = f
		}
	}

	cfg := ani.NewConfig().
		WithDepth(depth).
		WithPercent(percent).
		WithLogger(ani.StdLogger(os.Stderr))

	engine := ani.NewEngine(cfg)
	defer engine.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	assets, err := engine.Query(ctx, "aqc_unauth", os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "query failed: %v\n", err)
		os.Exit(2)
	}
	enc := json.NewEncoder(os.Stdout)
	for _, a := range assets {
		_ = enc.Encode(a)
	}
}

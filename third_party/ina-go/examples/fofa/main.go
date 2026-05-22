// 最小可运行示例: 用环境变量 FOFA_EMAIL / FOFA_KEY 跑一次查询。
//
//	go run ./examples/fofa
//
// 默认查询 domain="example.com", 可用 FOFA_QUERY 覆盖。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	ina "github.com/chainreactors/ina-go"
	_ "github.com/chainreactors/ina-go/fofa"
)

func main() {
	email := os.Getenv("FOFA_EMAIL")
	key := os.Getenv("FOFA_KEY")
	if email == "" || key == "" {
		fmt.Fprintln(os.Stderr, "set FOFA_EMAIL and FOFA_KEY")
		os.Exit(1)
	}
	query := os.Getenv("FOFA_QUERY")
	if query == "" {
		query = `domain="example.com"`
	}

	cfg := ina.NewConfig().
		WithFofa(email, key).
		WithLimit(20).
		WithLogger(ina.StdLogger(os.Stderr))

	engine := ina.NewEngine(cfg)
	defer engine.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status := engine.Status(ctx)
	fmt.Fprintf(os.Stderr, "status: %+v\n", status)

	assets, err := engine.QueryRaw(ctx, "fofa", query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query failed: %v\n", err)
		os.Exit(2)
	}
	enc := json.NewEncoder(os.Stdout)
	for _, a := range assets {
		_ = enc.Encode(a)
	}
}

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/chainreactors/sdk/client"
	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/pkg/cyberhub"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: quickstart <cyberhub_url> <api_key> <target> [ports]")
		os.Exit(1)
	}

	url, key, target := os.Args[1], os.Args[2], os.Args[3]
	ports := "80"
	if len(os.Args) > 4 {
		ports = os.Args[4]
	}

	provider := cyberhub.NewProvider(url, key)
	c := client.New(
		client.WithProvider(provider),
		client.WithIndex(nil),
	)
	defer c.Close()

	gogoEngine, err := c.Gogo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
		os.Exit(1)
	}

	ctx := gogo.NewContext().SetThreads(500).SetVersionLevel(1)
	results, err := gogoEngine.Scan(ctx, target, ports)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan failed: %v\n", err)
		os.Exit(1)
	}

	for _, r := range results {
		fws := make([]string, 0, len(r.Frameworks))
		for _, fw := range r.Frameworks {
			fws = append(fws, fw.Name)
		}
		fmt.Printf("%s:%s [%s] %s\n", r.Ip, r.Port, r.Status, strings.Join(fws, ","))

		// 关联查询
		if result, err := c.LookupByFinger(fws...); err == nil {
			for _, t := range result.Templates {
				fmt.Printf("  -> %s [%s]\n", t.Id, t.Info.Severity)
			}
		}
	}
}

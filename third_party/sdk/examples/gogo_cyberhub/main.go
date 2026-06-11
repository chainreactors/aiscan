package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/chainreactors/sdk/gogo"
	"github.com/chainreactors/sdk/pkg/cyberhub"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: gogo_cyberhub <cyberhub_url> <api_key> <target> [ports]")
		fmt.Println("  gogo_cyberhub http://127.0.0.1:8080 your-key 192.168.1.1 80,443")
		os.Exit(1)
	}

	hubURL := os.Args[1]
	apiKey := os.Args[2]
	target := os.Args[3]
	ports := "80,443,8080"
	if len(os.Args) > 4 {
		ports = os.Args[4]
	}

	// 一行创建 Provider，gogo 内部自动加载 fingers + neutron
	hub := cyberhub.NewProvider(hubURL, apiKey)

	engine := gogo.NewEngine(gogo.NewConfig().WithProvider(hub))
	ctx := gogo.NewContext().SetThreads(500).SetVersionLevel(1)

	results, err := engine.Scan(ctx, target, ports)
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
	}

	fmt.Printf("\nTotal: %d alive\n", len(results))
}

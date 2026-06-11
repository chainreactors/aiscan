// request_response demonstrates capturing full HTTP request/response via SDK.
//
// Result().Events contains every step's request/response.
// Result().Request / Result().Response is the final matched step only.
//
// Usage:
//
//	go run ./examples/cases/request_response -path ./pocs -target http://example.com
//	go run ./examples/cases/request_response -url http://127.0.0.1:8080 -key <api_key> -target http://example.com
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/chainreactors/sdk/neutron"
	"github.com/chainreactors/sdk/pkg/cyberhub"
	"github.com/chainreactors/sdk/pkg/provider"
	"github.com/chainreactors/sdk/pkg/types"
)

func main() {
	cyberhubURL := flag.String("url", "", "Cyberhub URL")
	apiKey := flag.String("key", "", "Cyberhub API Key")
	localPath := flag.String("path", "", "Local POC directory or file")
	target := flag.String("target", "", "Target URL to scan (required)")
	pocID := flag.String("poc", "", "Specific POC ID to execute (optional)")
	flag.Parse()

	if *target == "" {
		flag.Usage()
		os.Exit(1)
	}
	if *cyberhubURL == "" && *localPath == "" {
		fmt.Println("Error: either -url or -path is required")
		os.Exit(1)
	}

	config := neutron.NewConfig()
	if *cyberhubURL != "" {
		if *apiKey == "" {
			fmt.Println("Error: -key is required when using -url")
			os.Exit(1)
		}
		config.WithProvider(cyberhub.NewProvider(*cyberhubURL, *apiKey))
	} else {
		config.WithProvider(provider.NewFileProvider("", *localPath))
	}

	engine, err := neutron.NewEngine(config)
	if err != nil {
		fmt.Printf("engine init failed: %v\n", err)
		os.Exit(1)
	}

	templates := engine.Get()
	if len(templates) == 0 {
		fmt.Println("no templates loaded")
		os.Exit(1)
	}
	fmt.Printf("Loaded %d template(s)\n\n", len(templates))

	for _, t := range templates {
		if *pocID != "" && !strings.EqualFold(t.Id, *pocID) {
			continue
		}

		result, events, err := t.ExecuteWithEvents(*target, nil)
		if err != nil {
			if err == types.OpsecError {
				continue
			}
			fmt.Printf("[%s] error: %v\n", t.Id, err)
			continue
		}
		if result == nil || !result.Matched {
			continue
		}

		fmt.Printf("===== %s =====\n", t.Id)
		fmt.Printf("Name: %s\n", t.Info.Name)
		fmt.Printf("Severity: %s\n", t.Info.Severity)

		for i, ev := range events {
			if ev.Request == "" && ev.Response == "" {
				continue
			}
			fmt.Printf("\n--- Step %d ---\n", i+1)
			if ev.Request != "" {
				fmt.Printf("[Request]\n%s\n", ev.Request)
			}
			if ev.Response != "" {
				fmt.Printf("[Response]\n%s\n", ev.Response)
			}
		}

		if len(result.Extracts) > 0 {
			fmt.Printf("\n--- Extracts ---\n")
			for name, values := range result.Extracts {
				fmt.Printf("  %s: %s\n", name, strings.Join(values, ", "))
			}
		}
		fmt.Println()
	}
}

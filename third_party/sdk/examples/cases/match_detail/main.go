// match_detail demonstrates the SDK-level MatchDetail config.
//
// Usage:
//
//	go run ./examples/cases/match_detail -url http://127.0.0.1:8080 -key <api_key> -target http://example.com
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/pkg/cyberhub"
)

func main() {
	cyberhubURL := flag.String("url", "", "Cyberhub URL (optional, local fingers used if empty)")
	apiKey := flag.String("key", "", "Cyberhub API Key")
	target := flag.String("target", "", "Target URL to match (required)")
	flag.Parse()
	if *target == "" {
		flag.Usage()
		os.Exit(1)
	}

	cfg := fingers.NewConfig().WithMatchDetail()
	if *cyberhubURL != "" {
		cfg.WithProvider(cyberhub.NewProvider(*cyberhubURL, *apiKey))
	}
	eng, err := fingers.NewEngine(cfg)
	if err != nil {
		fmt.Printf("engine init failed: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.Get(*target)
	if err != nil {
		fmt.Printf("http get failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	frameworks, err := eng.MatchHTTP(resp)
	if err != nil {
		fmt.Printf("match failed: %v\n", err)
		os.Exit(1)
	}
	if len(frameworks) == 0 {
		fmt.Println("no fingerprints matched")
		return
	}

	for _, fw := range frameworks {
		fmt.Printf("[%s]\n", fw.Name)
		if fw.MatchDetail == nil {
			continue
		}
		fmt.Printf("  matcher_type  : %s\n", fw.MatchDetail.MatcherType)
		fmt.Printf("  matcher_value : %s\n", fw.MatchDetail.MatcherValue)
		fmt.Printf("  matcher_index : %d\n", fw.MatchDetail.MatcherIndex)
		fmt.Printf("  rule_index    : %d\n", fw.MatchDetail.RuleIndex)
		if fw.MatchDetail.SendData != "" {
			fmt.Printf("  send_data     : %s\n", fw.MatchDetail.SendData)
		}
	}
}

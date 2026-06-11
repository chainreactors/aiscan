// spray_crawl_finger shows a single-URL workflow:
// crawl automatically, and use spray's finger engine for deep matching.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/chainreactors/sdk/pkg/types"
	"github.com/chainreactors/sdk/spray"
)

func main() {
	target := flag.String("target", "", "Target base URL (required)")
	flag.Parse()

	if *target == "" {
		flag.Usage()
		os.Exit(1)
	}

	sprayEng := spray.NewEngine(spray.NewConfig().WithMatchDetail())
	if err := sprayEng.Init(); err != nil {
		fmt.Printf("spray engine init failed: %v\n", err)
		os.Exit(1)
	}

	opt := types.NewDefaultSprayOption()
	opt.Fuzzy = true
	ctx := spray.NewContext().
		SetOption(opt).
		SetThreads(4).
		SetTimeout(5).
		SetCrawlPlugin(true).
		SetFinger(true).
		SetCrawlDepth(2)

	results, err := sprayEng.Brute(ctx, *target, []string{"/"})
	if err != nil {
		fmt.Printf("spray failed: %v\n", err)
		os.Exit(1)
	}

	matched := false
	for _, result := range results {
		if result == nil || len(result.Frameworks) == 0 {
			continue
		}

		matched = true
		fmt.Printf("[%s] %s [%d]\n", result.Source.Name(), result.UrlString, result.Status)

		names := make([]string, 0, len(result.Frameworks))
		for name := range result.Frameworks {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			fw := result.Frameworks[name]
			if fw == nil {
				continue
			}
			fmt.Printf("  - %s\n", fw.Name)
			if fw.MatchDetail == nil {
				continue
			}
			fmt.Printf("    rule_index    : %d\n", fw.MatchDetail.RuleIndex)
			fmt.Printf("    matcher_type  : %s\n", fw.MatchDetail.MatcherType)
			fmt.Printf("    matcher_value : %s\n", fw.MatchDetail.MatcherValue)
			if fw.MatchDetail.SendData != "" {
				fmt.Printf("    send_data     : %s\n", fw.MatchDetail.SendData)
			}
		}
	}
	if !matched {
		fmt.Println("no fingerprint matched")
	}
}

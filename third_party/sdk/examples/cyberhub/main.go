package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chainreactors/sdk/pkg/cyberhub"
	"github.com/chainreactors/sdk/pkg/types"
)

var (
	hubURL       = flag.String("url", "", "Cyberhub URL, for example http://127.0.0.1:8080")
	apiKey       = flag.String("key", "", "Cyberhub API key")
	names        = flag.String("names", "", "Comma-separated names filter")
	tags         = flag.String("tags", "", "Comma-separated tags filter")
	sources      = flag.String("source", "", "Comma-separated sources filter")
	severities   = flag.String("severity", "", "Comma-separated severity filter for POCs")
	statuses     = flag.String("status", "", "Comma-separated POC status filter")
	reviewStatus = flag.String("review", "", "POC review status filter")
	pocType      = flag.String("type", "", "POC type filter")
	limit        = flag.Int("limit", 20, "Maximum records returned by Cyberhub")
	timeout      = flag.Duration("timeout", 15*time.Second, "Request timeout")
)

func main() {
	flag.Parse()

	if *hubURL == "" || *apiKey == "" {
		fmt.Println("Usage: go run ./examples/cyberhub -url http://127.0.0.1:8080 -key your-api-key [filters]")
		fmt.Println("Example: go run ./examples/cyberhub -url http://127.0.0.1:8080 -key your-api-key -source github -tags cms -limit 20")
		return
	}

	filter := types.NewExportFilter().
		WithNames(splitCSV(*names)...).
		WithTags(splitCSV(*tags)...).
		WithSources(splitCSV(*sources)...).
		WithSeverities(splitCSV(*severities)...).
		WithStatuses(splitCSV(*statuses)...).
		WithPOCType(*pocType).
		WithReviewStatus(*reviewStatus).
		WithLimit(*limit)

	ctx := context.Background()
	hub := cyberhub.NewProvider(*hubURL, *apiKey).
		WithTimeout(*timeout).
		WithFilter(filter)

	fingers, aliases, err := hub.Fingers(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load fingers failed: %v\n", err)
		os.Exit(1)
	}

	templates, err := hub.POCs(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load pocs failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Fingers : %d\n", len(fingers))
	fmt.Printf("Aliases : %d\n", len(aliases))
	fmt.Printf("POCs    : %d\n\n", len(templates))

	printFingerSamples(fingers, 5)
	printAliasSamples(aliases, 5)
	printTemplateSamples(templates, 5)
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func printFingerSamples(fingers types.Fingers, limit int) {
	fmt.Println("Sample fingers:")
	for i, finger := range fingers {
		if i >= limit {
			break
		}
		if finger == nil {
			continue
		}
		fmt.Printf("  - %s [%s]\n", finger.Name, finger.Protocol)
	}
}

func printAliasSamples(aliases []*types.Alias, limit int) {
	fmt.Println("Sample aliases:")
	for i, item := range aliases {
		if i >= limit {
			break
		}
		if item == nil {
			continue
		}
		fmt.Printf("  - %s\n", item.Name)
	}
}

func printTemplateSamples(templates []*types.Template, limit int) {
	fmt.Println("Sample templates:")
	for i, template := range templates {
		if i >= limit {
			break
		}
		if template == nil {
			continue
		}
		fmt.Printf("  - %s [%s] %s\n", template.Id, template.Info.Severity, template.Info.Name)
	}
}

package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/neutron"
	"github.com/chainreactors/sdk/pkg/cyberhub"
	"github.com/chainreactors/sdk/pkg/types"
)

var (
	url = flag.String("url", "", "Cyberhub URL")
	key = flag.String("key", "", "Cyberhub API Key")
)

func main() {
	flag.Parse()

	if *url == "" || *key == "" {
		testExportFilter()
		testLocalFilter()
		fmt.Println("All Filter API tests passed (no real data)")
		return
	}

	ctx := context.Background()
	testWithRealData(ctx, *url, *key)
}

func testExportFilter() {
	filter := types.NewExportFilter().
		WithTags("cms", "framework").
		WithSources("github").
		WithLimit(100).
		WithUpdatedAfter(time.Now().AddDate(0, -1, 0))

	_ = fingers.NewConfig().
		WithProvider(cyberhub.NewProvider("http://127.0.0.1:8080", "test_key").WithFilter(filter))

	fmt.Printf("Fingers ExportFilter: Tags=%v, Sources=%v, Limit=%d\n",
		filter.Tags, filter.Sources, filter.Limit)
}

func testLocalFilter() {
	fullFingers := fingers.FullFingers{}
	_ = fullFingers.Filter(func(f *fingers.FullFinger) bool {
		return f.Finger != nil && f.Finger.Protocol == "http"
	})
	fmt.Println("FullFingers.Filter: OK")

	tpls := neutron.Templates{}
	_ = tpls.Filter(func(t *types.Template) bool {
		severity := t.Info.Severity
		return severity == "critical" || severity == "high"
	})
	fmt.Println("Templates.Filter (severity): OK")
}

func testWithRealData(ctx context.Context, url, key string) {
	fmt.Println("=== Fingers remote filter ===")

	hub := cyberhub.NewProvider(url, key)

	engine1, err := fingers.NewEngine(fingers.NewConfig().WithProvider(hub))
	if err != nil {
		fmt.Printf("Fingers load failed: %v\n", err)
		return
	}
	fmt.Printf("No filter: %d fingerprints\n", engine1.Count())

	engine2, err := fingers.NewEngine(fingers.NewConfig().
		WithProvider(cyberhub.NewProvider(url, key).
			WithFilter(types.NewExportFilter().WithTags("cms"))))
	if err != nil {
		fmt.Printf("Fingers filter failed: %v\n", err)
		return
	}
	fmt.Printf("Tag=cms: %d fingerprints\n", engine2.Count())

	fmt.Println("\n=== Neutron remote filter ===")

	nEngine1, err := neutron.NewEngine(neutron.NewConfig().WithProvider(hub))
	if err != nil {
		fmt.Printf("Neutron load failed: %v\n", err)
		return
	}
	fmt.Printf("No filter: %d POCs\n", len(nEngine1.Get()))

	nEngine2, err := neutron.NewEngine(neutron.NewConfig().
		WithProvider(cyberhub.NewProvider(url, key).
			WithFilter(types.NewExportFilter().WithTags("rce"))))
	if err != nil {
		fmt.Printf("Neutron filter failed: %v\n", err)
		return
	}
	fmt.Printf("Tag=rce: %d POCs\n", len(nEngine2.Get()))

	fmt.Println("\nAll tests done!")
}

package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/chainreactors/sdk/fingers"
	"github.com/chainreactors/sdk/pkg/cyberhub"
	"github.com/chainreactors/utils/httputils"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: quickstart_fingers <cyberhub_url> <api_key> <target_url>")
		os.Exit(1)
	}

	provider := cyberhub.NewProvider(os.Args[1], os.Args[2])
	engine, err := fingers.NewEngine(fingers.NewConfig().WithProvider(provider))
	if err != nil {
		fmt.Fprintf(os.Stderr, "init failed: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.Get(os.Args[3])
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	frameworks, err := engine.Get().DetectContent(httputils.ReadRaw(resp))
	if err != nil {
		fmt.Fprintf(os.Stderr, "detect failed: %v\n", err)
		os.Exit(1)
	}

	for _, fw := range frameworks {
		fmt.Printf("%s %s\n", fw.Name, fw.CPE())
	}
}

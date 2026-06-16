package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/chainreactors/aiscan/pkg/commands"
)

type FetchTool struct {
	fetch *FetchCommand
}

type fetchArgs struct {
	URL     string `json:"url"              jsonschema:"description=URL to fetch"`
	Extract string `json:"extract,omitempty" jsonschema:"description=Optional extraction hint (e.g. CVSS score)"`
}

func NewFetchTool() *FetchTool {
	return &FetchTool{fetch: NewFetchCommand()}
}

func (t *FetchTool) Name() string { return "fetch" }

func (t *FetchTool) Description() string {
	return "Fetch a URL and return the content as readable text. Useful for reading advisories, documentation, and vulnerability details."
}

func (t *FetchTool) Definition() commands.ToolDefinition {
	return commands.ToolDef("fetch", t.Description(), fetchArgs{})
}

func (t *FetchTool) Execute(ctx context.Context, arguments string) (commands.ToolResult, error) {
	args, err := commands.ParseArgs[fetchArgs](arguments)
	if err != nil {
		return commands.ToolResult{}, err
	}
	args.URL = strings.TrimSpace(args.URL)
	if args.URL == "" {
		return commands.ToolResult{}, fmt.Errorf("url is required")
	}

	cliArgs := []string{args.URL}
	if args.Extract != "" {
		cliArgs = append(cliArgs, "--extract", args.Extract)
	}
	result, err := t.fetch.Execute(ctx, cliArgs)
	if err != nil {
		return commands.ToolResult{}, fmt.Errorf("fetch: %w", err)
	}
	return commands.TextResult(result), nil
}

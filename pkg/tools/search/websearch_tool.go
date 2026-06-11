package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/command"
)

type WebSearchTool struct {
	provider provider.Provider
}

type webSearchArgs struct {
	Query string `json:"query"         jsonschema:"description=Search query (e.g. CVE-2024-1234 exploit)"`
	Num   int    `json:"num,omitempty"  jsonschema:"description=Max search round-trips 1-10 (default 5),minimum=1,maximum=10"`
}

func NewWebSearchTool(p provider.Provider) *WebSearchTool {
	return &WebSearchTool{provider: p}
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the web for CVEs, exploits, vulnerability details, and product documentation. Returns a list of results with titles, URLs, and an optional summary."
}

func (t *WebSearchTool) Definition() command.ToolDefinition {
	return command.ToolDef("web_search", t.Description(), webSearchArgs{})
}

func (t *WebSearchTool) Execute(ctx context.Context, arguments string) (command.ToolResult, error) {
	args, err := command.ParseStrictArgs[webSearchArgs](arguments)
	if err != nil {
		return command.ToolResult{}, err
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return command.ToolResult{}, fmt.Errorf("query is required")
	}
	if t.provider == nil {
		return command.ToolResult{}, fmt.Errorf("web_search: provider not configured")
	}

	num := args.Num
	if num == 0 {
		num = defaultMaxUses
	}
	if num < 1 || num > 10 {
		return command.ToolResult{}, fmt.Errorf("num must be between 1 and 10")
	}

	resp, err := t.provider.WebSearch(ctx, args.Query, num)
	if err != nil {
		return command.ToolResult{}, fmt.Errorf("web_search: %w", err)
	}

	return command.TextResult(formatWebSearchResponse(resp, args.Query)), nil
}

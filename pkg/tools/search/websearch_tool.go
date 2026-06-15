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
	tavily   *TavilySearch
}

type webSearchArgs struct {
	Query string `json:"query"         jsonschema:"description=Search query (e.g. CVE-2024-1234 exploit)"`
	Num   int    `json:"num,omitempty"  jsonschema:"description=Max results 1-10 (default 5),minimum=1,maximum=10"`
}

func NewWebSearchTool(p provider.Provider, tavily *TavilySearch) *WebSearchTool {
	return &WebSearchTool{provider: p, tavily: tavily}
}

func (t *WebSearchTool) Name() string { return "web_search" }

func (t *WebSearchTool) Description() string {
	return "Search the web for CVEs, exploits, vulnerability details, and product documentation."
}

func (t *WebSearchTool) Definition() command.ToolDefinition {
	return command.ToolDef("web_search", t.Description(), webSearchArgs{})
}

func (t *WebSearchTool) Execute(ctx context.Context, arguments string) (command.ToolResult, error) {
	args, err := command.ParseArgs[webSearchArgs](arguments)
	if err != nil {
		return command.ToolResult{}, err
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return command.ToolResult{}, fmt.Errorf("query is required")
	}

	num := args.Num
	if num <= 0 {
		num = 5
	}
	if num > 10 {
		num = 10
	}

	if ws, ok := t.provider.(provider.WebSearchProvider); ok {
		resp, err := ws.WebSearch(ctx, args.Query, num)
		if err == nil {
			return command.TextResult(formatWebSearchResponse(resp, args.Query)), nil
		}
	}

	if t.tavily != nil {
		result, err := t.tavily.Execute(ctx, []string{args.Query, "--num", fmt.Sprint(num)})
		if err == nil {
			return command.TextResult(result), nil
		}
	}

	return command.ToolResult{}, fmt.Errorf("web_search: no search backend available")
}

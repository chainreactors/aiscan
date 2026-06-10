package search

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
)

const defaultMaxUses = 5

type WebSearch struct {
	provider provider.Provider
}

func NewWebSearch(p provider.Provider) *WebSearch {
	return &WebSearch{provider: p}
}

func (ws *WebSearch) Execute(ctx context.Context, args []string) (string, error) {
	query, num, err := parseWebSearchArgs(args)
	if err != nil {
		return "", err
	}
	if ws.provider == nil {
		return "", fmt.Errorf("search web: provider not configured")
	}

	resp, err := ws.provider.WebSearch(ctx, query, num)
	if err != nil {
		return "", fmt.Errorf("search web: %w", err)
	}

	return formatWebSearchResponse(resp, query), nil
}

func formatWebSearchResponse(resp *provider.WebSearchResponse, query string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Web search results for: %s\n\n", query))

	for i, r := range resp.Results {
		sb.WriteString(fmt.Sprintf("[%d] %s\n    URL: %s\n\n", i+1, r.Title, r.URL))
	}

	if len(resp.Results) == 0 && resp.Summary == "" {
		sb.WriteString("No results found.\n")
		return sb.String()
	}

	if resp.Summary != "" {
		sb.WriteString("Summary:\n")
		sb.WriteString(resp.Summary)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func parseWebSearchArgs(args []string) (query string, maxUses int, err error) {
	maxUses = defaultMaxUses
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--num":
			i++
			if i >= len(args) {
				return "", 0, fmt.Errorf("search web: --num requires a value")
			}
			n, parseErr := strconv.Atoi(args[i])
			if parseErr != nil {
				return "", 0, fmt.Errorf("search web: invalid --num value: %s", args[i])
			}
			if n < 1 {
				n = 1
			}
			if n > 10 {
				n = 10
			}
			maxUses = n
		default:
			if strings.HasPrefix(args[i], "--") {
				return "", 0, fmt.Errorf("search web: unknown flag: %s", args[i])
			}
			if query == "" {
				query = args[i]
			} else {
				query += " " + args[i]
			}
		}
	}
	if query == "" {
		return "", 0, fmt.Errorf("search web: query is required\n\nUsage: search web <query> [--num <N>]")
	}
	return query, maxUses, nil
}

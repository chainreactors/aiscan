package search

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/chainreactors/aiscan/pkg/resources"
)

type Command struct {
	tavily   *TavilySearch
	fetch    *FetchCommand
	cyberhub *CyberhubSearch
}

type Opts struct {
	TavilyKeys   string
	ScannerProxy string
	SearchProxy  string // search-specific proxy; overrides ScannerProxy when set
	Resources    *resources.Set
}

func New(opts Opts) *Command {
	cmd := &Command{
		tavily: NewTavilySearch(opts.TavilyKeys),
		fetch:  NewFetchCommand(),
	}
	proxy := opts.ScannerProxy
	if opts.SearchProxy != "" {
		proxy = opts.SearchProxy
	}
	if proxy != "" {
		cmd.tavily.SetProxy(proxy)
	}
	if opts.Resources != nil {
		cmd.cyberhub = NewCyberhubSearch(opts.Resources)
	}
	return cmd
}

func (c *Command) SetProxy(proxyURLStr string) {
	c.tavily.SetProxy(proxyURLStr)
}

func (c *Command) Name() string { return "search" }

func (c *Command) Usage() string {
	return `search - Unified search across web and local resources
Usage:
  search tavily <query> [--num <N>]
  search fetch <url> [--extract <hint>]
  search cyberhub list|search [finger|poc|all] [options]

Subcommands:
  tavily     Search the web via Tavily API (fallback: DuckDuckGo)
  fetch      Fetch a URL and return as readable text
  cyberhub   Search loaded fingerprints and POC templates

Examples:
  search tavily "CVE-2024-1234 exploit"
  search tavily "nginx misconfiguration" --num 10
  search fetch https://example.com/advisory
  search fetch https://nvd.nist.gov/vuln/detail/CVE-2024-1234 --extract "CVSS score"
  search cyberhub list finger --limit 20
  search cyberhub search poc spring --severity critical`
}

func (c *Command) Execute(ctx context.Context, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("search: subcommand required\n\n%s", c.Usage())
	}

	var result string
	var err error
	switch strings.ToLower(args[0]) {
	case "tavily":
		result, err = c.tavily.Execute(ctx, args[1:])
	case "fetch":
		result, err = c.fetch.Execute(ctx, args[1:])
	case "cyberhub":
		if c.cyberhub == nil {
			return fmt.Errorf("search cyberhub: resources not loaded")
		}
		result, err = c.cyberhub.Execute(ctx, args[1:])
	default:
		return fmt.Errorf("search: unknown subcommand %q\n\n%s", args[0], c.Usage())
	}
	if result != "" {
		_, _ = io.WriteString(w, result)
	}
	return err
}

func (c *Command) ClearFetchCache() { c.fetch.ClearCache() }

// Package ina is the aiscan tool wrapper for ina-go (cyberspace asset recon).
// LLM agents call this with a source-native query string; the wrapper dispatches
// to the registered ina-go Engine and emits Python InaData.to_dict compatible
// JSON.
package ina

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	inago "github.com/chainreactors/ina-go"
)

const defaultTimeout = 600 * time.Second

// Command runs ina-go recon queries.
type Command struct {
	engine *inago.Engine
	logger telemetry.Logger
}

func New(engine *inago.Engine) *Command {
	return &Command{engine: engine, logger: telemetry.NopLogger()}
}

func (c *Command) WithLogger(logger telemetry.Logger) *Command {
	if logger != nil {
		c.logger = logger
	}
	return c
}

func (c *Command) Name() string { return "ina" }

func (c *Command) Usage() string {
	return `ina - cyberspace asset recon (FOFA + Hunter via ina-go SDK)
Usage: ina '<query>' [options]

Arguments:
  <query>            Source-native syntax. Examples:
                       fofa:    'domain="example.com" && icp="京ICP备1号"'
                       hunter:  'domain.suffix="example.com" && icp.number="京ICP备1号"'

Options:
  -s <source>        Engine: fofa (default) | hunter
  -h, --help         Show this help

Output:
  JSON array compatible with Python InaData.to_dict():
    fofa:    [{ip, port, url, domain, title, icp}]
    hunter:  [{ip, port, url, domain, status, company, frame, title, icp}]

Notes:
  - FOFA:   set recon.fofa_email/fofa_key or env FOFA_EMAIL/FOFA_KEY
  - Hunter: set recon.hunter_api_key or env HUNTER_API_KEY
            (Hunter 屏蔽境外 IP, 海外机器需 recon.proxy=socks5://...)
  - Source 未配置凭证时该 source 不可用; 全无凭证则 ina 子命令本身不挂载。`
}

func (c *Command) Execute(ctx context.Context, args []string) (string, error) {
	if c.engine == nil {
		return "", fmt.Errorf("ina: engine not initialized — set recon.fofa_key in config or FOFA_KEY env var")
	}
	query, source, helpRequested, err := parseArgs(args)
	if err != nil {
		return "", err
	}
	if helpRequested {
		return c.Usage(), nil
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}

	assets, err := c.engine.QueryRaw(ctx, source, query)
	if err != nil {
		return "", fmt.Errorf("ina: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(pythonAssets(source, assets)); err != nil {
		return buf.String(), fmt.Errorf("ina: encode assets: %w", err)
	}
	c.logger.Debugf("ina source=%s assets=%d", source, len(assets))
	return buf.String(), nil
}

type pythonFofaAsset struct {
	IP     string `json:"ip"`
	Port   string `json:"port"`
	URL    string `json:"url"`
	Domain string `json:"domain"`
	Title  string `json:"title"`
	ICP    string `json:"icp"`
}

type pythonHunterAsset struct {
	IP      string `json:"ip"`
	Port    string `json:"port"`
	URL     string `json:"url"`
	Domain  string `json:"domain"`
	Status  string `json:"status"`
	Company string `json:"company"`
	Frame   string `json:"frame"`
	Title   string `json:"title"`
	ICP     string `json:"icp"`
}

type pythonZoomeyeAsset struct {
	IP     string `json:"ip"`
	Port   string `json:"port"`
	URL    string `json:"url"`
	Domain string `json:"domain"`
	ICO    string `json:"ico"`
}

func pythonAssets(source string, assets []inago.Asset) any {
	effectiveSource := source
	if effectiveSource == "" && len(assets) > 0 {
		effectiveSource = assets[0].Source
	}
	switch effectiveSource {
	case "hunter":
		out := make([]pythonHunterAsset, 0, len(assets))
		for _, a := range assets {
			out = append(out, pythonHunterAsset{
				IP: a.IP, Port: a.Port, URL: a.URL, Domain: a.Domain,
				Status: a.Status, Company: a.Company, Frame: a.Frame,
				Title: a.Title, ICP: a.ICP,
			})
		}
		return out
	case "zoomeye":
		out := make([]pythonZoomeyeAsset, 0, len(assets))
		for _, a := range assets {
			out = append(out, pythonZoomeyeAsset{
				IP: a.IP, Port: a.Port, URL: a.URL, Domain: a.Domain, ICO: a.ICO,
			})
		}
		return out
	default:
		out := make([]pythonFofaAsset, 0, len(assets))
		for _, a := range assets {
			out = append(out, pythonFofaAsset{
				IP: a.IP, Port: a.Port, URL: a.URL, Domain: a.Domain,
				Title: a.Title, ICP: a.ICP,
			})
		}
		return out
	}
}

func parseArgs(args []string) (query, source string, helpRequested bool, err error) {
	source = "fofa"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			helpRequested = true
			return
		case a == "-s" || a == "--source":
			if i+1 >= len(args) {
				err = fmt.Errorf("ina: -s requires a value")
				return
			}
			source = args[i+1]
			i++
		case strings.HasPrefix(a, "-"):
			err = fmt.Errorf("ina: unknown flag %q (use -h for usage)", a)
			return
		default:
			if query != "" {
				err = fmt.Errorf("ina: multiple positional args; query must be a single quoted string")
				return
			}
			query = a
		}
	}
	if query == "" {
		err = fmt.Errorf("ina: missing query (e.g. ina 'domain=\"example.com\"')")
	}
	return
}

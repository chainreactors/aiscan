// Package ani is the aiscan tool wrapper for ani-go (enterprise asset graph).
// LLM agents call this with a Chinese company name; the wrapper dispatches to
// the registered ani-go Engine and emits Python ani -t json compatible JSON.
package ani

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	anigo "github.com/chainreactors/ani-go"
)

const defaultTimeout = 300 * time.Second
const defaultAniDepth = 1
const defaultAniPercent = 0.5

type Command struct {
	engine   *anigo.Engine
	logger   telemetry.Logger
	defaults Defaults
}

type Defaults struct {
	Depth      int
	DepthSet   bool
	Percent    float64
	PercentSet bool
	Proxy      string
	TycToken   string
	QccCookie  string
	AqcCookie  string
}

func New(engine *anigo.Engine) *Command {
	return &Command{
		engine: engine,
		logger: telemetry.NopLogger(),
		defaults: Defaults{
			Depth:      defaultAniDepth,
			DepthSet:   true,
			Percent:    defaultAniPercent,
			PercentSet: true,
		},
	}
}

func (c *Command) WithLogger(logger telemetry.Logger) *Command {
	if logger != nil {
		c.logger = logger
	}
	return c
}

func (c *Command) WithDefaults(defaults Defaults) *Command {
	if !defaults.DepthSet {
		defaults.Depth = defaultAniDepth
		defaults.DepthSet = true
	}
	if !defaults.PercentSet {
		defaults.Percent = defaultAniPercent
		defaults.PercentSet = true
	}
	c.defaults = defaults
	return c
}

func (c *Command) Name() string { return "ani" }

func (c *Command) Usage() string {
	return `ani - enterprise subsidiary tree + ICP domain recon (via ani-go SDK)
Usage: ani -n "<company name>" [options]

Options:
  -n <name>          Target Chinese company name (required)
  -d <int>           Recursion depth (default from config: ani_depth / 1)
  -p <float>         Min ownership ratio (0-1) to follow subsidiaries (default: ani_percent / 0.5)
  -s <source>        Data source. No-cred: aqc_unauth (default), tyc_unauth.
                     Cred-gated: tyc (auth_token JWT), qcc (QCCSESSID),
                     aqc (BAIDUID). See "Sources and credentials" below.
  -h, --help         Show this help

Output:
  JSON object compatible with Python ani -t json:
    {"公司名": {"name": "...", "perc": 1.0, "aqcid": "...", "icp": "...", "icps": [...], "parent": null}}

Sources and credentials:
  aqc_unauth   爱企查 (qiye.baidu.com)        no credentials
  tyc_unauth   天眼查 (api9.tianyancha.com)   no credentials
  tyc          天眼查 (www.tianyancha.com)    recon.ani_tyc_token   (auth_token JWT)
  qcc          企查查 (www.qcc.com)           recon.ani_qcc_cookie  (QCCSESSID cookie)
  aqc          爱企查 (aiqicha.baidu.com)     recon.ani_aqc_cookie  (BAIDUID cookie)

Depth/percent come from config.yaml recon.ani_depth / ani_percent unless
overridden by -d / -p flags. Credentials can also be supplied via env
ANI_TYC_TOKEN / ANI_QCC_COOKIE / ANI_AQC_COOKIE.`
}

func (c *Command) Execute(ctx context.Context, args []string) (string, error) {
	if c.engine == nil {
		return "", fmt.Errorf("ani: engine not initialized")
	}
	name, source, depth, percent, depthSet, percentSet, helpRequested, err := parseArgs(args)
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

	// Per-call depth/percent override: build a transient engine if either is set.
	// Otherwise reuse the global one (which already has config-supplied defaults).
	engine := c.engine
	if depthSet || percentSet {
		cfg := anigo.NewConfig().WithLogger(&aniLoggerAdapter{logger: c.logger})
		cfg.WithDepth(c.defaults.Depth)
		cfg.WithPercent(c.defaults.Percent)
		if c.defaults.Proxy != "" {
			cfg.WithProxy(c.defaults.Proxy)
		}
		if c.defaults.TycToken != "" {
			cfg.WithTycToken(c.defaults.TycToken)
		}
		if c.defaults.QccCookie != "" {
			cfg.WithQccCookie(c.defaults.QccCookie)
		}
		if c.defaults.AqcCookie != "" {
			cfg.WithAqcCookie(c.defaults.AqcCookie)
		}
		if depthSet {
			cfg.WithDepth(depth)
		}
		if percentSet {
			cfg.WithPercent(percent)
		}
		engine = anigo.NewEngine(cfg)
		defer engine.Close()
	}

	assets, err := engine.Query(ctx, source, name)
	if err != nil {
		return "", fmt.Errorf("ani: %w", err)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	if err := enc.Encode(pythonCompanies(assets)); err != nil {
		return buf.String(), fmt.Errorf("ani: encode assets: %w", err)
	}
	c.logger.Debugf("ani source=%s name=%s assets=%d", source, name, len(assets))
	return buf.String(), nil
}

type pythonICPItem struct {
	ICP    string `json:"icp"`
	Domain string `json:"domain"`
	Title  string `json:"title"`
}

type pythonCompany struct {
	Name   string          `json:"name"`
	Perc   float64         `json:"perc"`
	AqcID  string          `json:"aqcid,omitempty"`
	TycID  string          `json:"tycid,omitempty"`
	QccID  string          `json:"qccid,omitempty"`
	ICP    string          `json:"icp,omitempty"`
	ICPs   []pythonICPItem `json:"icps"`
	Parent *string         `json:"parent"`
}

func pythonCompanies(assets []anigo.CompanyAsset) map[string]pythonCompany {
	out := make(map[string]pythonCompany)
	for _, asset := range assets {
		// Python traverse_all() only emits companies that have ICP data unless
		// all=True. The CLI uses the default all=False path.
		if asset.ICP == "" {
			continue
		}
		company, ok := out[asset.Name]
		if !ok {
			company = pythonCompany{
				Name:   asset.Name,
				Perc:   asset.Percent,
				ICPs:   []pythonICPItem{},
				Parent: pythonParent(asset.Parent),
			}
			setPythonCompanyID(&company, asset.Source, asset.PID)
		}
		if company.ICP == "" {
			company.ICP = asset.ICP
		}
		company.ICPs = append(company.ICPs, pythonICPItem{
			ICP:    asset.ICP,
			Domain: asset.Domain,
			Title:  asset.Title,
		})
		out[asset.Name] = company
	}
	return out
}

func pythonParent(parent string) *string {
	if parent == "" {
		return nil
	}
	return &parent
}

func setPythonCompanyID(company *pythonCompany, source, pid string) {
	switch source {
	case "tyc", "tyc_unauth":
		company.TycID = pid
	case "qcc":
		company.QccID = pid
	default:
		company.AqcID = pid
	}
}

func parseArgs(args []string) (name, source string, depth int, percent float64, depthSet, percentSet, helpRequested bool, err error) {
	source = "aqc_unauth"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			helpRequested = true
			return
		case a == "-n" || a == "--name":
			if i+1 >= len(args) {
				err = fmt.Errorf("ani: -n requires a value")
				return
			}
			name = args[i+1]
			i++
		case a == "-s" || a == "--source":
			if i+1 >= len(args) {
				err = fmt.Errorf("ani: -s requires a value")
				return
			}
			source = args[i+1]
			i++
		case a == "-d" || a == "--depth":
			if i+1 >= len(args) {
				err = fmt.Errorf("ani: -d requires a value")
				return
			}
			d, perr := strconv.Atoi(args[i+1])
			if perr != nil {
				err = fmt.Errorf("ani: -d %q: %v", args[i+1], perr)
				return
			}
			depth = d
			depthSet = true
			i++
		case a == "-p" || a == "--percent":
			if i+1 >= len(args) {
				err = fmt.Errorf("ani: -p requires a value")
				return
			}
			f, perr := strconv.ParseFloat(args[i+1], 64)
			if perr != nil {
				err = fmt.Errorf("ani: -p %q: %v", args[i+1], perr)
				return
			}
			percent = f
			percentSet = true
			i++
		case strings.HasPrefix(a, "-"):
			err = fmt.Errorf("ani: unknown flag %q (use -h for usage)", a)
			return
		}
	}
	if name == "" {
		err = fmt.Errorf("ani: missing -n <name>")
	}
	return
}

type aniLoggerAdapter struct{ logger telemetry.Logger }

func (a *aniLoggerAdapter) Debugf(format string, args ...any) { a.logger.Debugf(format, args...) }
func (a *aniLoggerAdapter) Infof(format string, args ...any)  { a.logger.Infof(format, args...) }
func (a *aniLoggerAdapter) Warnf(format string, args ...any)  { a.logger.Warnf(format, args...) }
func (a *aniLoggerAdapter) Errorf(format string, args ...any) { a.logger.Errorf(format, args...) }

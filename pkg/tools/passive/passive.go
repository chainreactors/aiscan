// Package passive is a unified aiscan wrapper for company recon (ani-go) and
// cyberspace recon (ina-go). It replaces the old separate ani / ina tools with
// a single "passive" command that dispatches by -s <source>.
package passive

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
	inago "github.com/chainreactors/ina-go"
)

const (
	aniTimeout = 300 * time.Second
	inaTimeout = 600 * time.Second
)

// AniDefaults mirrors config-file values for company recon.
type AniDefaults struct {
	Depth      int
	DepthSet   bool
	Percent    float64
	PercentSet bool
	Proxy      string
	TycToken   string
	QccCookie  string
	AqcCookie  string
}

// Command dispatches passive recon to ani-go or ina-go by -s <source>.
type Command struct {
	ani        *anigo.Engine
	ina        *inago.Engine
	logger     telemetry.Logger
	defaults   AniDefaults
	aniSources map[string]bool
	inaSources map[string]bool
}

// New creates a passive command. Either engine may be nil (not configured).
func New(ani *anigo.Engine, ina *inago.Engine) *Command {
	c := &Command{
		ani: ani, ina: ina,
		logger:     telemetry.NopLogger(),
		defaults:   AniDefaults{Depth: 1, DepthSet: true, Percent: 0.5, PercentSet: true},
		aniSources: map[string]bool{},
		inaSources: map[string]bool{},
	}
	if ani != nil {
		for _, s := range ani.Sources() {
			c.aniSources[s] = true
		}
	}
	if ina != nil {
		for _, s := range ina.Sources() {
			c.inaSources[s] = true
		}
	}
	return c
}

func (c *Command) WithLogger(l telemetry.Logger) *Command {
	if l != nil {
		c.logger = l
	}
	return c
}

func (c *Command) WithDefaults(d AniDefaults) *Command {
	if !d.DepthSet {
		d.Depth = 1
		d.DepthSet = true
	}
	if !d.PercentSet {
		d.Percent = 0.5
		d.PercentSet = true
	}
	c.defaults = d
	return c
}

func (c *Command) Name() string { return "passive" }

func (c *Command) Usage() string {
	return `passive - unified passive recon (company graph + cyberspace search)

Company recon (ani-go):
  passive -s aqc_unauth -n "默安科技"
  passive -s tyc -n "深信服" -d 2 -p 0.5

Cyberspace recon (ina-go):
  passive -s fofa 'domain="example.com"'
  passive -s hunter 'domain.suffix="example.com"'

Options:
  -s <source>   Data source (required).
                Company:    aqc_unauth, tyc_unauth, tyc, qcc, aqc
                Cyberspace: fofa, hunter
  -n <name>     Target company name  (company sources only, required)
  -d <int>      Recursion depth       (company only, default from config / 1)
  -p <float>    Min ownership ratio   (company only, default from config / 0.5)
  -h            Show this help`
}

func (c *Command) Execute(ctx context.Context, args []string) (string, error) {
	src, rest, help, err := splitSource(args)
	if err != nil {
		return "", err
	}
	if help {
		return c.Usage(), nil
	}
	if c.aniSources[src] {
		return c.runAni(ctx, src, rest)
	}
	if c.inaSources[src] {
		return c.runIna(ctx, src, rest)
	}
	return "", fmt.Errorf("passive: unknown source %q (available: %v)", src, c.sourceList())
}

// --------------- ani dispatch ------------------------------------------------

func (c *Command) runAni(ctx context.Context, src string, args []string) (string, error) {
	if c.ani == nil {
		return "", fmt.Errorf("passive: ani engine not initialized")
	}
	name, depth, pct, dSet, pSet, err := parseAniArgs(args)
	if err != nil {
		return "", err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, aniTimeout)
		defer cancel()
	}
	eng := c.ani
	if dSet || pSet {
		cfg := anigo.NewConfig().WithLogger(&aniLog{c.logger})
		cfg.WithDepth(c.defaults.Depth).WithPercent(c.defaults.Percent)
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
		if dSet {
			cfg.WithDepth(depth)
		}
		if pSet {
			cfg.WithPercent(pct)
		}
		eng = anigo.NewEngine(cfg)
		defer eng.Close()
	}
	assets, err := eng.Query(ctx, src, name)
	if err != nil {
		return "", fmt.Errorf("passive: %w", err)
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(aniPython(assets)); err != nil {
		return buf.String(), fmt.Errorf("passive: encode: %w", err)
	}
	c.logger.Debugf("passive source=%s name=%s assets=%d", src, name, len(assets))
	return buf.String(), nil
}

// --------------- ina dispatch ------------------------------------------------

func (c *Command) runIna(ctx context.Context, src string, args []string) (string, error) {
	if c.ina == nil {
		return "", fmt.Errorf("passive: ina engine not initialized — set recon credentials")
	}
	query, err := parseInaArgs(args)
	if err != nil {
		return "", err
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, inaTimeout)
		defer cancel()
	}
	assets, err := c.ina.QueryRaw(ctx, src, query)
	if err != nil {
		return "", fmt.Errorf("passive: %w", err)
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(inaPython(src, assets)); err != nil {
		return buf.String(), fmt.Errorf("passive: encode: %w", err)
	}
	c.logger.Debugf("passive source=%s assets=%d", src, len(assets))
	return buf.String(), nil
}

// --------------- arg parsing -------------------------------------------------

// splitSource extracts -s <source> and -h from the full arg list, returning
// the remaining args untouched for the engine-specific parsers.
func splitSource(args []string) (source string, rest []string, help bool, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			help = true
			return
		case "-s", "--source":
			if i+1 >= len(args) {
				err = fmt.Errorf("passive: -s requires a value")
				return
			}
			source = args[i+1]
			i++ // skip value
		default:
			rest = append(rest, args[i])
		}
	}
	if source == "" && !help {
		err = fmt.Errorf("passive: -s <source> is required (use -h for help)")
	}
	return
}

func parseAniArgs(args []string) (name string, depth int, pct float64, dSet, pSet bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-n" || a == "--name":
			if i+1 >= len(args) {
				err = fmt.Errorf("passive: -n requires a value")
				return
			}
			i++
			name = args[i]
		case a == "-d" || a == "--depth":
			if i+1 >= len(args) {
				err = fmt.Errorf("passive: -d requires a value")
				return
			}
			i++
			depth, err = strconv.Atoi(args[i])
			if err != nil {
				err = fmt.Errorf("passive: -d %q: %v", args[i], err)
				return
			}
			dSet = true
		case a == "-p" || a == "--percent":
			if i+1 >= len(args) {
				err = fmt.Errorf("passive: -p requires a value")
				return
			}
			i++
			pct, err = strconv.ParseFloat(args[i], 64)
			if err != nil {
				err = fmt.Errorf("passive: -p %q: %v", args[i], err)
				return
			}
			pSet = true
		case strings.HasPrefix(a, "-"):
			err = fmt.Errorf("passive: unknown flag %q", a)
			return
		}
	}
	if name == "" {
		err = fmt.Errorf("passive: -n <company name> is required for company sources")
	}
	return
}

func parseInaArgs(args []string) (query string, err error) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			err = fmt.Errorf("passive: unknown flag %q for cyberspace source", a)
			return
		}
		if query != "" {
			err = fmt.Errorf("passive: multiple positional args; query must be a single quoted string")
			return
		}
		query = a
	}
	if query == "" {
		err = fmt.Errorf("passive: missing query (e.g. passive -s fofa 'domain=\"example.com\"')")
	}
	return
}

// --------------- Python-compatible JSON shapes --------------------------------

type pyICPItem struct {
	ICP    string `json:"icp"`
	Domain string `json:"domain"`
	Title  string `json:"title"`
}

type pyCompany struct {
	Name   string      `json:"name"`
	Perc   float64     `json:"perc"`
	AqcID  string      `json:"aqcid,omitempty"`
	TycID  string      `json:"tycid,omitempty"`
	QccID  string      `json:"qccid,omitempty"`
	ICP    string      `json:"icp,omitempty"`
	ICPs   []pyICPItem `json:"icps"`
	Parent *string     `json:"parent"`
}

// aniPython converts flat CompanyAsset list to the Python ani -t json shape.
func aniPython(assets []anigo.CompanyAsset) map[string]pyCompany {
	out := make(map[string]pyCompany)
	for _, a := range assets {
		if a.ICP == "" {
			continue
		}
		co, ok := out[a.Name]
		if !ok {
			co = pyCompany{Name: a.Name, Perc: a.Percent, ICPs: []pyICPItem{}, Parent: nilStr(a.Parent)}
			setCompanyID(&co, a.Source, a.PID)
		}
		if co.ICP == "" {
			co.ICP = a.ICP
		}
		co.ICPs = append(co.ICPs, pyICPItem{ICP: a.ICP, Domain: a.Domain, Title: a.Title})
		out[a.Name] = co
	}
	return out
}

func nilStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func setCompanyID(co *pyCompany, src, pid string) {
	switch src {
	case "tyc", "tyc_unauth":
		co.TycID = pid
	case "qcc":
		co.QccID = pid
	default:
		co.AqcID = pid
	}
}

type pyFofa struct {
	IP     string `json:"ip"`
	Port   string `json:"port"`
	URL    string `json:"url"`
	Domain string `json:"domain"`
	Title  string `json:"title"`
	ICP    string `json:"icp"`
}

type pyHunter struct {
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

type pyZoomeye struct {
	IP     string `json:"ip"`
	Port   string `json:"port"`
	URL    string `json:"url"`
	Domain string `json:"domain"`
	ICO    string `json:"ico"`
}

// inaPython converts Asset list to the Python InaData.to_dict() shape.
func inaPython(src string, assets []inago.Asset) any {
	eff := src
	if eff == "" && len(assets) > 0 {
		eff = assets[0].Source
	}
	switch eff {
	case "hunter":
		out := make([]pyHunter, 0, len(assets))
		for _, a := range assets {
			out = append(out, pyHunter{
				IP: a.IP, Port: a.Port, URL: a.URL, Domain: a.Domain,
				Status: a.Status, Company: a.Company, Frame: a.Frame,
				Title: a.Title, ICP: a.ICP,
			})
		}
		return out
	case "zoomeye":
		out := make([]pyZoomeye, 0, len(assets))
		for _, a := range assets {
			out = append(out, pyZoomeye{IP: a.IP, Port: a.Port, URL: a.URL, Domain: a.Domain, ICO: a.ICO})
		}
		return out
	default:
		out := make([]pyFofa, 0, len(assets))
		for _, a := range assets {
			out = append(out, pyFofa{
				IP: a.IP, Port: a.Port, URL: a.URL, Domain: a.Domain,
				Title: a.Title, ICP: a.ICP,
			})
		}
		return out
	}
}

// --------------- helpers -----------------------------------------------------

func (c *Command) sourceList() []string {
	out := make([]string, 0, len(c.aniSources)+len(c.inaSources))
	for s := range c.aniSources {
		out = append(out, s)
	}
	for s := range c.inaSources {
		out = append(out, s)
	}
	return out
}

type aniLog struct{ l telemetry.Logger }

func (a *aniLog) Debugf(f string, v ...any) { a.l.Debugf(f, v...) }
func (a *aniLog) Infof(f string, v ...any)  { a.l.Infof(f, v...) }
func (a *aniLog) Warnf(f string, v ...any)  { a.l.Warnf(f, v...) }
func (a *aniLog) Errorf(f string, v ...any) { a.l.Errorf(f, v...) }

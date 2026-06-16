package katana

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/toolargs"
	"github.com/projectdiscovery/goflags"
	"github.com/projectdiscovery/gologger"
	"github.com/projectdiscovery/gologger/levels"
	"github.com/projectdiscovery/katana/pkg/engine/standard"
	katanaoutput "github.com/projectdiscovery/katana/pkg/output"
	katanatypes "github.com/projectdiscovery/katana/pkg/types"
	urlutil "github.com/projectdiscovery/utils/url"
)

const defaultTimeout = 120 * time.Second
const defaultBodyReadSize = 4 * 1024 * 1024

type Command struct {
	proxy   string
	logger  telemetry.Logger
	workDir string
}

func New() *Command { return &Command{logger: telemetry.NopLogger()} }

func (c *Command) WithLogger(logger telemetry.Logger) *Command {
	if logger != nil {
		c.logger = logger
	}
	return c
}

func (c *Command) WithProxy(proxy string) *Command {
	c.proxy = proxy
	return c
}

func (c *Command) SetProxy(proxy string) { c.proxy = proxy }

func (c *Command) SetWorkDir(dir string) { c.workDir = dir }

func (c *Command) Name() string { return "katana" }

func (c *Command) Usage() string {
	return `katana - deep web crawling with full parameter discovery
Usage: katana -u <url> [options]

Input:
  -u, -list          Target URL or file with URLs
  -e, -exclude       Exclude host matching filter (cdn, private-ips, cidr, ip, regex)

Configuration:
  -d, -depth         Crawl depth (default: 3)
  -jc, -js-crawl     Enable JavaScript crawling
  -jsl, -jsluice     Enable jsluice parsing in JS files
  -timeout           Timeout in seconds (default: 10)
  -ct, -crawl-duration  Crawl duration limit (s, m, h, d)
  -s, -strategy      Visit strategy: depth-first, breadth-first (default: depth-first)
  -proxy             HTTP/SOCKS5 proxy to use
  -H, -headers       Custom headers (header:value format)
  -iqp               Ignore crawling same path with different query params
  -fsu               Filter crawling of similar looking URLs
  -dr                Disable following redirects
  -pc                Enable path climb (auto crawl parent paths)

Scope:
  -fs, -field-scope  Field scope (rdn, fqdn, dn) or custom regex (default: rdn)
  -cs, -crawl-scope  In-scope URL regex
  -cos, -crawl-out-scope  Out-of-scope URL regex
  -ns, -no-scope     Disable host based default scope
  -do, -display-out-scope  Display external endpoints

Filter:
  -f, -field         Field to display in output (url, path, fqdn, rdn, rurl, qurl, qpath, file, ufile, key, value, kv, dir, udir)
  -sf, -store-field  Field to store in per-host output
  -em, -extension-match   Match output for given extensions
  -ef, -extension-filter  Filter output for given extensions
  -mr, -match-regex  Regex to match output URL
  -fr, -filter-regex Regex to filter output URL

Rate-Limit:
  -c, -concurrency   Number of concurrent fetchers (default: 10)
  -p, -parallelism   Number of concurrent inputs to process (default: 10)
  -rl, -rate-limit   Maximum requests per second (default: 150)
  -rd, -delay        Request delay in seconds

Output:
  -o, -output        File to write output to
  -j, -jsonl         JSON Lines output
  -silent            Silent mode
  -nc, -no-color     Disable output coloring

Examples:
  katana -u https://target.com -d 3 -jc
  katana -u https://target.com -d 2 -silent -jsonl
  katana -u https://target.com -f qurl
  katana -list urls.txt -d 2 -jc -timeout 60`
}

func (c *Command) Execute(ctx context.Context, args []string, w io.Writer) (err error) {
	defer telemetry.SDKRecover("katana", &err)
	args = c.resolveRelativePaths(args)

	if toolargs.BoolFlagEnabled(args, "--debug") {
		restoreDebug := telemetry.ActivateDebug(c.logger)
		defer restoreDebug()
		c.logger.Debugf("katana debug enabled")
	}

	options, err := readFlags(args)
	if err != nil {
		return fmt.Errorf("katana: %w", err)
	}

	// Force agent-friendly defaults.
	options.Silent = true
	options.NoColors = true
	options.DisableUpdateCheck = true

	// Inject proxy.
	if options.Proxy == "" && c.proxy != "" {
		options.Proxy = c.proxy
	}

	if err := validateOptions(options); err != nil {
		return fmt.Errorf("katana: %w", err)
	}

	// Context timeout.
	if _, ok := ctx.Deadline(); !ok {
		timeout := defaultTimeout
		if options.CrawlDuration > 0 {
			timeout = options.CrawlDuration
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Collect results via OnResult callback.
	var mu sync.Mutex
	var lines [][]byte

	options.OnResult = func(r katanaoutput.Result) {
		if r.Request == nil || r.Request.URL == "" {
			return
		}
		var line []byte
		if options.JSON {
			data, jsonErr := json.Marshal(r)
			if jsonErr != nil {
				return
			}
			line = data
		} else {
			line = []byte(r.Request.URL)
		}
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	}

	// Suppress gologger during crawl.
	gologger.DefaultLogger.SetMaxLevel(levels.LevelSilent)
	crawlerOptions, err := katanatypes.NewCrawlerOptions(options)
	if err != nil {
		gologger.DefaultLogger.SetMaxLevel(levels.LevelWarning)
		return fmt.Errorf("katana: init: %w", err)
	}
	crawlerOptions.OutputWriter = &silentWriter{}
	defer func() {
		crawlerOptions.Close()
		gologger.DefaultLogger.SetMaxLevel(levels.LevelWarning)
	}()

	crawler, err := standard.New(crawlerOptions)
	if err != nil {
		return fmt.Errorf("katana: create crawler: %w", err)
	}
	defer crawler.Close()

	// Crawl each URL.
	for _, u := range options.URLs {
		if ctx.Err() != nil {
			break
		}
		u = addSchemeIfNotExists(u)
		if crawlErr := crawler.Crawl(u); crawlErr != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("katana: timed out")
			}
			c.logger.Warnf("katana: crawl %s: %v", u, crawlErr)
		}
	}

	// Write collected results.
	mu.Lock()
	defer mu.Unlock()
	for _, line := range lines {
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
	}
	return nil
}

// readFlags replicates katana's cmd/katana/main.go readFlags() using goflags,
// keeping CLI arguments 100% compatible with the upstream katana binary.
func readFlags(args []string) (*katanatypes.Options, error) {
	options := &katanatypes.Options{}

	flagSet := goflags.NewFlagSet()
	flagSet.CommandLine = flag.NewFlagSet("katana", flag.ContinueOnError)
	flagSet.SetDescription("Katana is a fast crawler focused on execution in automation pipelines.")

	// Input
	flagSet.CreateGroup("input", "Input",
		flagSet.StringSliceVarP(&options.URLs, "list", "u", nil, "target url / list to crawl", goflags.FileCommaSeparatedStringSliceOptions),
		flagSet.StringSliceVarP(&options.Exclude, "exclude", "e", nil, "exclude host matching specified filter", goflags.CommaSeparatedStringSliceOptions),
	)

	// Configuration
	flagSet.CreateGroup("config", "Configuration",
		flagSet.StringSliceVarP(&options.Resolvers, "resolvers", "r", nil, "list of custom resolver", goflags.FileCommaSeparatedStringSliceOptions),
		flagSet.IntVarP(&options.MaxDepth, "depth", "d", 3, "maximum depth to crawl"),
		flagSet.BoolVarP(&options.ScrapeJSResponses, "js-crawl", "jc", false, "enable endpoint parsing / crawling in javascript file"),
		flagSet.BoolVarP(&options.ScrapeJSLuiceResponses, "jsluice", "jsl", false, "enable jsluice parsing in javascript file"),
		flagSet.DurationVarP(&options.CrawlDuration, "crawl-duration", "ct", 0, "maximum duration to crawl the target for"),
		flagSet.IntVarP(&options.BodyReadSize, "max-response-size", "mrs", defaultBodyReadSize, "maximum response size to read"),
		flagSet.IntVar(&options.Timeout, "timeout", 10, "time to wait for request in seconds"),
		flagSet.BoolVarP(&options.AutomaticFormFill, "automatic-form-fill", "aff", false, "enable automatic form filling"),
		flagSet.BoolVarP(&options.FormExtraction, "form-extraction", "fx", false, "extract form elements in jsonl output"),
		flagSet.IntVar(&options.Retries, "retry", 1, "number of times to retry the request"),
		flagSet.StringVar(&options.Proxy, "proxy", "", "http/socks5 proxy to use"),
		flagSet.BoolVarP(&options.TechDetect, "tech-detect", "td", false, "enable technology detection"),
		flagSet.StringSliceVarP(&options.CustomHeaders, "headers", "H", nil, "custom header/cookie in header:value format", goflags.FileStringSliceOptions),
		flagSet.StringVarP(&options.Strategy, "strategy", "s", "depth-first", "Visit strategy (depth-first, breadth-first)"),
		flagSet.BoolVarP(&options.IgnoreQueryParams, "ignore-query-params", "iqp", false, "ignore crawling same path with different query-param values"),
		flagSet.BoolVarP(&options.FilterSimilar, "filter-similar", "fsu", false, "filter crawling of similar looking URLs"),
		flagSet.IntVarP(&options.FilterSimilarThreshold, "filter-similar-threshold", "fst", 10, "filter similar threshold"),
		flagSet.BoolVarP(&options.TlsImpersonate, "tls-impersonate", "tlsi", false, "enable tls randomization"),
		flagSet.BoolVarP(&options.DisableRedirects, "disable-redirects", "dr", false, "disable following redirects"),
		flagSet.BoolVarP(&options.PathClimb, "path-climb", "pc", false, "enable path climb"),
		flagSet.BoolVarP(&options.KnowledgeBase, "knowledge-base", "kb", false, "enable knowledge base classification"),
		flagSet.IntVarP(&options.MaxDomainPages, "max-domain-pages", "mdp", 0, "max pages per domain"),
	)

	// Scope
	flagSet.CreateGroup("scope", "Scope",
		flagSet.StringSliceVarP(&options.Scope, "crawl-scope", "cs", nil, "in scope url regex", goflags.FileCommaSeparatedStringSliceOptions),
		flagSet.StringSliceVarP(&options.OutOfScope, "crawl-out-scope", "cos", nil, "out of scope url regex", goflags.FileCommaSeparatedStringSliceOptions),
		flagSet.StringVarP(&options.FieldScope, "field-scope", "fs", "rdn", "pre-defined scope field (dn,rdn,fqdn) or custom regex"),
		flagSet.BoolVarP(&options.NoScope, "no-scope", "ns", false, "disables host based default scope"),
		flagSet.BoolVarP(&options.DisplayOutScope, "display-out-scope", "do", false, "display external endpoint from scoped crawling"),
	)

	// Filter
	flagSet.CreateGroup("filter", "Filter",
		flagSet.StringSliceVarP(&options.OutputMatchRegex, "match-regex", "mr", nil, "regex to match output url", goflags.FileStringSliceOptions),
		flagSet.StringSliceVarP(&options.OutputFilterRegex, "filter-regex", "fr", nil, "regex to filter output url", goflags.FileStringSliceOptions),
		flagSet.StringVarP(&options.Fields, "field", "f", "", "field to display in output"),
		flagSet.StringVarP(&options.StoreFields, "store-field", "sf", "", "field to store in per-host output"),
		flagSet.StringSliceVarP(&options.ExtensionsMatch, "extension-match", "em", nil, "match output for given extension", goflags.CommaSeparatedStringSliceOptions),
		flagSet.StringSliceVarP(&options.ExtensionFilter, "extension-filter", "ef", nil, "filter output for given extension", goflags.CommaSeparatedStringSliceOptions),
		flagSet.BoolVarP(&options.NoDefaultExtFilter, "no-default-ext-filter", "ndef", false, "remove default extensions from filter list"),
		flagSet.StringVarP(&options.OutputMatchCondition, "match-condition", "mdc", "", "match response with dsl condition"),
		flagSet.StringVarP(&options.OutputFilterCondition, "filter-condition", "fdc", "", "filter response with dsl condition"),
		flagSet.BoolVarP(&options.DisableUniqueFilter, "disable-unique-filter", "duf", false, "disable duplicate content filtering"),
		flagSet.StringSliceVarP(&options.FilterPageType, "filter-page-type", "fpt", nil, "filter by page type", goflags.CommaSeparatedStringSliceOptions),
	)

	// Rate-Limit
	flagSet.CreateGroup("ratelimit", "Rate-Limit",
		flagSet.IntVarP(&options.Concurrency, "concurrency", "c", 10, "number of concurrent fetchers"),
		flagSet.IntVarP(&options.Parallelism, "parallelism", "p", 10, "number of concurrent inputs to process"),
		flagSet.IntVarP(&options.Delay, "delay", "rd", 0, "request delay in seconds"),
		flagSet.IntVarP(&options.RateLimit, "rate-limit", "rl", 150, "maximum requests per second"),
		flagSet.IntVarP(&options.RateLimitMinute, "rate-limit-minute", "rlm", 0, "maximum requests per minute"),
		flagSet.IntVarP(&options.HostRateLimit, "host-rate-limit", "hrl", 0, "maximum requests per second per host"),
		flagSet.IntVarP(&options.HostRateLimitMinute, "host-rate-limit-minute", "hrlm", 0, "maximum requests per minute per host"),
	)

	// Output
	flagSet.CreateGroup("output", "Output",
		flagSet.StringVarP(&options.OutputFile, "output", "o", "", "file to write output to"),
		flagSet.StringVarP(&options.OutputTemplate, "output-template", "ot", "", "custom output template"),
		flagSet.BoolVarP(&options.StoreResponse, "store-response", "sr", false, "store http requests/responses"),
		flagSet.StringVarP(&options.StoreResponseDir, "store-response-dir", "srd", "", "store responses to custom directory"),
		flagSet.BoolVarP(&options.OmitRaw, "omit-raw", "or", false, "omit raw requests/responses from jsonl output"),
		flagSet.BoolVarP(&options.OmitBody, "omit-body", "ob", false, "omit response body from jsonl output"),
		flagSet.StringSliceVarP(&options.ExcludeOutputFields, "exclude-output-fields", "eof", nil, "exclude fields from jsonl output", goflags.CommaSeparatedStringSliceOptions),
		flagSet.BoolVarP(&options.JSON, "jsonl", "j", false, "write output in jsonl format"),
		flagSet.BoolVarP(&options.NoColors, "no-color", "nc", false, "disable output coloring"),
		flagSet.BoolVar(&options.Silent, "silent", false, "display output only"),
		flagSet.BoolVarP(&options.Verbose, "verbose", "v", false, "display verbose output"),
		flagSet.BoolVar(&options.Debug, "debug", false, "display debug output"),
	)

	if err := flagSet.Parse(args...); err != nil {
		return nil, err
	}
	return options, nil
}

// validateOptions replicates essential checks from katana's internal/runner/options.go.
func validateOptions(options *katanatypes.Options) error {
	if options.MaxDepth <= 0 && options.CrawlDuration.Seconds() <= 0 {
		return fmt.Errorf("either max-depth or crawl-duration must be specified")
	}
	if len(options.URLs) == 0 {
		return fmt.Errorf("no input URLs specified")
	}
	for _, mr := range options.OutputMatchRegex {
		cr, err := regexp.Compile(mr)
		if err != nil {
			return fmt.Errorf("invalid match regex: %w", err)
		}
		options.MatchRegex = append(options.MatchRegex, cr)
	}
	for _, fr := range options.OutputFilterRegex {
		cr, err := regexp.Compile(fr)
		if err != nil {
			return fmt.Errorf("invalid filter regex: %w", err)
		}
		options.FilterRegex = append(options.FilterRegex, cr)
	}
	if options.KnownFiles != "" && options.MaxDepth < 3 {
		options.MaxDepth = 3
	}
	if options.StoreResponseDir != "" && !options.StoreResponse {
		options.StoreResponse = true
	}
	return nil
}

func (c *Command) resolveRelativePaths(args []string) []string {
	if c.workDir == "" {
		return args
	}
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if key, value, ok := strings.Cut(arg, "="); ok && isFileFlag(key) {
			out = append(out, key+"="+c.resolvePathArg(value))
			continue
		}
		if isFileFlag(arg) {
			out = append(out, arg)
			if i+1 < len(args) {
				i++
				out = append(out, c.resolvePathArg(args[i]))
			}
			continue
		}
		out = append(out, arg)
	}
	return out
}

func (c *Command) resolvePathArg(value string) string {
	if c.workDir == "" || value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "-") {
		return value
	}
	return filepath.Join(c.workDir, value)
}

func isFileFlag(flag string) bool {
	switch flag {
	case "-list", "--list", "-o", "--output",
		"-resume", "--resume",
		"-fc", "--form-config",
		"-flc", "--field-config",
		"-elog", "--error-log":
		return true
	}
	return false
}

// addSchemeIfNotExists replicates katana's internal/runner/executer.go helper.
func addSchemeIfNotExists(inputURL string) string {
	if strings.HasPrefix(inputURL, "http://") || strings.HasPrefix(inputURL, "https://") {
		return inputURL
	}
	parsed, err := urlutil.Parse(inputURL)
	if err != nil {
		return inputURL
	}
	if parsed.Port() != "" && (parsed.Port() == "80" || parsed.Port() == "8080") {
		return "http://" + inputURL
	}
	return "https://" + inputURL
}

type silentWriter struct{}

func (w *silentWriter) Close() error                               { return nil }
func (w *silentWriter) Write(_ *katanaoutput.Result) error         { return nil }
func (w *silentWriter) WriteErr(_ *katanaoutput.Error) error       { return nil }

package types

import "github.com/chainreactors/spray/core"

// SprayOption wraps spray/core.Option with SDK-friendly defaults and builders.
type SprayOption struct {
	*core.Option
}

func NewDefaultSprayOption() *SprayOption {
	opt := &SprayOption{Option: &core.Option{}}

	// Request defaults.
	opt.Method = "GET"
	opt.PortRange = "80,443"
	opt.MaxBodyLength = 100
	opt.RandomUserAgent = false

	// Status defaults.
	opt.BlackStatus = "400,410"
	opt.WhiteStatus = "200"
	opt.FuzzyStatus = "500,501,502,503,301,302,404"
	opt.UniqueStatus = "403,200,404"

	// Check defaults.
	opt.CheckPeriod = 200
	opt.ErrPeriod = 10
	opt.BreakThreshold = 20

	// Recursion defaults.
	opt.Recursive = "current.IsDir()"
	opt.Depth = 0
	opt.Index = "/"
	opt.Random = ""

	// Retry defaults.
	opt.RetryCount = 0
	opt.SimhashDistance = 8

	// Runtime defaults.
	opt.Mod = "path"
	opt.Client = "auto"
	opt.Timeout = 5
	opt.Threads = 20
	opt.PoolSize = 5
	opt.Deadline = 999999

	// SDK output defaults.
	opt.Quiet = true
	opt.NoBar = true
	opt.NoStat = true
	opt.NoColor = false
	opt.Json = false
	opt.FileOutput = "json"

	// Plugin defaults.
	opt.Advance = false
	opt.Finger = false
	opt.CrawlPlugin = false
	opt.BakPlugin = false
	opt.FuzzuliPlugin = false
	opt.CommonPlugin = false
	opt.ActivePlugin = false
	opt.ReconPlugin = false
	opt.CrawlDepth = 3
	opt.AppendDepth = 2

	opt.FingerEngines = "all"

	return opt
}

func CloneSprayOption(opt *SprayOption) *SprayOption {
	if opt == nil || opt.Option == nil {
		return NewDefaultSprayOption()
	}
	coreOpt := *opt.Option
	clone := &SprayOption{Option: &coreOpt}
	clone.URL = cloneStrings(opt.URL)
	clone.CIDRs = cloneStrings(opt.CIDRs)
	clone.Dictionaries = cloneStrings(opt.Dictionaries)
	clone.Rules = cloneStrings(opt.Rules)
	clone.AppendRule = cloneStrings(opt.AppendRule)
	clone.AppendFile = cloneStrings(opt.AppendFile)
	clone.Prefixes = cloneStrings(opt.Prefixes)
	clone.Suffixes = cloneStrings(opt.Suffixes)
	clone.Replaces = cloneStringMap(opt.Replaces)
	clone.Skips = cloneStrings(opt.Skips)
	clone.Headers = cloneStrings(opt.Headers)
	clone.Cookie = cloneStrings(opt.Cookie)
	clone.Extracts = cloneStrings(opt.Extracts)
	clone.Scope = cloneStrings(opt.Scope)
	clone.Verbose = append([]bool(nil), opt.Verbose...)
	clone.Proxies = cloneStrings(opt.Proxies)
	clone.FingerFiles = cloneStrings(opt.FingerFiles)
	return clone
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func (opt *SprayOption) WithThreads(n int) *SprayOption {
	opt.Threads = n
	return opt
}

func (opt *SprayOption) WithTimeout(n int) *SprayOption {
	opt.Timeout = n
	return opt
}

func (opt *SprayOption) WithMethod(method string) *SprayOption {
	opt.Method = method
	return opt
}

func (opt *SprayOption) WithHeaders(headers []string) *SprayOption {
	opt.Headers = headers
	return opt
}

func (opt *SprayOption) WithProxy(proxy string) *SprayOption {
	opt.Proxies = []string{proxy}
	return opt
}

func (opt *SprayOption) WithFinger(enable bool) *SprayOption {
	opt.Finger = enable
	return opt
}

func (opt *SprayOption) WithCrawl(enable bool) *SprayOption {
	opt.CrawlPlugin = enable
	return opt
}

func (opt *SprayOption) WithDepth(depth int) *SprayOption {
	opt.Depth = depth
	return opt
}

func (opt *SprayOption) WithMod(mod string) *SprayOption {
	opt.Mod = mod
	return opt
}

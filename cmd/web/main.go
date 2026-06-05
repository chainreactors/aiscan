package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/app"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/web"

	_ "github.com/chainreactors/aiscan/pkg/tools"
)

//go:embed static
var staticFS embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address")
	dbPath := flag.String("db", "aiscan-web.db", "SQLite database path")
	configFile := flag.String("config", "", "Path to aiscan config.yaml")
	debug := flag.Bool("debug", false, "Enable debug logging")
	maxScans := flag.Int("max-scans", 3, "Maximum concurrent scans")
	scanTimeout := flag.Int("scan-timeout", 600, "Maximum scan runtime in seconds")
	flag.Parse()

	logger := telemetry.GlobalLogger(telemetry.LogConfig{
		Debug:  *debug,
		Output: os.Stderr,
		Color:  true,
	})

	store, err := web.NewSQLiteStore(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open database: %s\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	application, err := initApp(ctx, *configFile, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: init aiscan: %s\n", err)
		os.Exit(1)
	}

	if application.Provider != nil {
		logger.Infof("LLM provider ready, AI features enabled")
	} else {
		logger.Warnf("no LLM provider configured, AI features disabled (set api_key in config.yaml or env)")
	}

	service := web.NewService(web.ServiceConfig{
		Store:         store,
		App:           application,
		ConfigStore:   &llmConfigFileStore{explicit: *configFile},
		AppFactory:    func(ctx context.Context) (*app.App, error) { return initApp(ctx, *configFile, logger) },
		MaxConcurrent: *maxScans,
		ScanTimeout:   time.Duration(*scanTimeout) * time.Second,
	})
	defer service.Close()

	staticSub, _ := fs.Sub(staticFS, "static")
	handler := web.NewHandler(service, http.FileServer(http.FS(staticSub)))

	srv := &http.Server{
		Addr:    *addr,
		Handler: handler,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Infof("shutting down...")
		cancel()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		srv.Shutdown(shutCtx)
	}()

	logger.Infof("aiscan web server listening on http://%s", *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

type yamlConfig struct {
	LLM struct {
		Provider string `yaml:"provider" config:"provider"`
		BaseURL  string `yaml:"base_url" config:"base_url"`
		APIKey   string `yaml:"api_key" config:"api_key"`
		Model    string `yaml:"model" config:"model"`
		Proxy    string `yaml:"proxy" config:"proxy"`
	} `yaml:"llm" config:"llm"`
	Cyberhub struct {
		URL   string `yaml:"url" config:"url"`
		Key   string `yaml:"key" config:"key"`
		Mode  string `yaml:"mode" config:"mode"`
		Proxy string `yaml:"proxy" config:"proxy"`
	} `yaml:"cyberhub" config:"cyberhub"`
	Scan struct {
		Verify        string `yaml:"verify" config:"verify"`
		VerifyTimeout int    `yaml:"verify_timeout" config:"verify_timeout"`
	} `yaml:"scan" config:"scan"`
	Search struct {
		TavilyKeys string `yaml:"tavily_keys" config:"tavily_keys"`
	} `yaml:"search" config:"search"`
	WebSearch struct {
		Proxy string `yaml:"proxy" config:"proxy"`
	} `yaml:"websearch" config:"websearch"`
}

type llmConfigFileStore struct {
	explicit string
	mu       sync.Mutex
}

func (s *llmConfigFileStore) GetLLMConfig(ctx context.Context) (web.LLMConfig, error) {
	if err := ctx.Err(); err != nil {
		return web.LLMConfig{}, err
	}
	path, loaded := s.resolvePath()
	cfg := yamlConfig{}
	if loaded {
		cfg = loadYAMLConfig(path)
	}
	return web.LLMConfig{
		ConfigPath:       path,
		ConfigLoaded:     loaded,
		Provider:         cfg.LLM.Provider,
		BaseURL:          cfg.LLM.BaseURL,
		APIKeyConfigured: strings.TrimSpace(cfg.LLM.APIKey) != "",
		Model:            cfg.LLM.Model,
		Proxy:            cfg.LLM.Proxy,
	}, nil
}

func (s *llmConfigFileStore) SaveLLMConfig(ctx context.Context, cfg web.LLMConfig) (web.LLMConfig, error) {
	if err := ctx.Err(); err != nil {
		return web.LLMConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	path, loaded := s.resolvePath()
	var data []byte
	if loaded {
		current, err := os.ReadFile(path)
		if err != nil {
			return web.LLMConfig{}, err
		}
		data = current
	}

	current := yamlConfig{}
	if len(data) > 0 {
		parseSimpleYAML(data, &current)
	}
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		apiKey = current.LLM.APIKey
	}

	values := map[string]string{
		"provider": strings.TrimSpace(cfg.Provider),
		"base_url": strings.TrimSpace(cfg.BaseURL),
		"api_key":  apiKey,
		"model":    strings.TrimSpace(cfg.Model),
		"proxy":    strings.TrimSpace(cfg.Proxy),
	}
	next := replaceYAMLSection(data, "llm", values)
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return web.LLMConfig{}, err
		}
	}
	if err := os.WriteFile(path, []byte(next), 0600); err != nil {
		return web.LLMConfig{}, err
	}
	saved := loadYAMLConfig(path)
	return web.LLMConfig{
		ConfigPath:       path,
		ConfigLoaded:     true,
		Provider:         saved.LLM.Provider,
		BaseURL:          saved.LLM.BaseURL,
		APIKeyConfigured: strings.TrimSpace(saved.LLM.APIKey) != "",
		Model:            saved.LLM.Model,
		Proxy:            saved.LLM.Proxy,
	}, nil
}

func (s *llmConfigFileStore) resolvePath() (string, bool) {
	path := findConfigFile(s.explicit)
	if path != "" {
		return path, true
	}
	if s.explicit != "" {
		return s.explicit, false
	}
	return "config.yaml", false
}

func replaceYAMLSection(data []byte, section string, values map[string]string) string {
	replacement := []string{section + ":"}
	keys := []string{"provider", "base_url", "api_key", "model", "proxy"}
	for _, key := range keys {
		replacement = append(replacement, "  "+key+": "+yamlString(values[key]))
	}

	lines := splitLines(data)
	out := make([]string, 0, len(lines)+len(replacement)+1)
	inSection := false
	replaced := false
	for _, line := range lines {
		trimmed := trimString(line)
		if !inSection && countLeadingSpaces(line) == 0 {
			key, _ := splitKV(trimmed)
			if key == section {
				out = append(out, replacement...)
				inSection = true
				replaced = true
				continue
			}
		}
		if inSection {
			if trimmed == "" || trimmed[0] == '#' || countLeadingSpaces(line) > 0 {
				continue
			}
			inSection = false
		}
		out = append(out, line)
	}
	if !replaced {
		if len(out) > 0 && trimString(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, replacement...)
	}
	return strings.Join(out, "\n") + "\n"
}

func yamlString(value string) string {
	return strconv.Quote(value)
}

func findConfigFile(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if _, err := os.Stat("config.yaml"); err == nil {
		return "config.yaml"
	}
	if dir, err := os.UserConfigDir(); err == nil {
		p := filepath.Join(dir, "aiscan", "config.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func loadYAMLConfig(path string) yamlConfig {
	var cfg yamlConfig
	if path == "" {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	parseSimpleYAML(data, &cfg)
	return cfg
}

func initApp(ctx context.Context, configFile string, logger telemetry.Logger) (*app.App, error) {
	cfgPath := findConfigFile(configFile)
	ycfg := loadYAMLConfig(cfgPath)
	if cfgPath != "" {
		logger.Infof("loaded config: %s", cfgPath)
	}

	providerCfg := provider.ProviderConfig{
		Provider: ycfg.LLM.Provider,
		BaseURL:  ycfg.LLM.BaseURL,
		APIKey:   ycfg.LLM.APIKey,
		Model:    ycfg.LLM.Model,
		Proxy:    ycfg.LLM.Proxy,
		Timeout:  120,
	}

	// Env vars override config file
	if v := os.Getenv("AISCAN_API_KEY"); v != "" {
		providerCfg.APIKey = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		providerCfg.APIKey = v
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		providerCfg.APIKey = v
		providerCfg.Provider = "anthropic"
	}
	if v := os.Getenv("AISCAN_PROVIDER"); v != "" {
		providerCfg.Provider = v
	}
	if v := os.Getenv("AISCAN_MODEL"); v != "" {
		providerCfg.Model = v
	}
	if v := os.Getenv("AISCAN_BASE_URL"); v != "" {
		providerCfg.BaseURL = v
	}

	providerName := strings.ToLower(strings.TrimSpace(providerCfg.Provider))
	if providerName == "" {
		providerName = provider.InferFromBaseURL(providerCfg.BaseURL)
	}
	hasProvider := strings.TrimSpace(providerCfg.APIKey) != "" || providerName == "ollama"

	cyberhubURL := ycfg.Cyberhub.URL
	if v := os.Getenv("CYBERHUB_URL"); v != "" {
		cyberhubURL = v
	}
	cyberhubKey := ycfg.Cyberhub.Key
	if v := os.Getenv("CYBERHUB_KEY"); v != "" {
		cyberhubKey = v
	}

	cfg := app.Config{
		Provider: app.ProviderConfig{
			Enabled:  hasProvider,
			Optional: true,
			Config:   providerCfg,
		},
		Scanner: app.ScannerConfig{
			AIEnabled:         hasProvider,
			EnableAllAISkills: false,
			AITimeout:         120,
			VerifyMode:        "off",
			CyberhubURL:       cyberhubURL,
			CyberhubKey:       cyberhubKey,
			CyberhubMode:      firstNonEmpty(ycfg.Cyberhub.Mode, "merge"),
			Proxy:             firstNonEmpty(os.Getenv("AISCAN_PROXY"), ycfg.Cyberhub.Proxy),
		},
		Tools: app.ToolConfig{
			Enabled:        true,
			BashTimeout:    300,
			TavilyKeys:     ycfg.Search.TavilyKeys,
			WebSearchProxy: firstNonEmpty(os.Getenv("AISCAN_SEARCH_PROXY"), ycfg.WebSearch.Proxy),
		},
		Logger: logger,
	}

	return app.New(ctx, cfg)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parseSimpleYAML is a minimal YAML parser for flat/two-level config.
// It avoids importing a YAML library in the web entry point.
func parseSimpleYAML(data []byte, cfg *yamlConfig) {
	lines := splitLines(data)
	var section string
	for _, line := range lines {
		trimmed := trimString(line)
		if trimmed == "" || trimmed[0] == '#' {
			continue
		}
		indent := countLeadingSpaces(line)
		key, value := splitKV(trimmed)
		if key == "" {
			continue
		}
		if indent == 0 {
			section = key
			continue
		}
		value = unquote(value)
		switch section {
		case "llm":
			switch key {
			case "provider":
				cfg.LLM.Provider = value
			case "base_url":
				cfg.LLM.BaseURL = value
			case "api_key":
				cfg.LLM.APIKey = value
			case "model":
				cfg.LLM.Model = value
			case "proxy":
				cfg.LLM.Proxy = value
			}
		case "cyberhub":
			switch key {
			case "url":
				cfg.Cyberhub.URL = value
			case "key":
				cfg.Cyberhub.Key = value
			case "mode":
				cfg.Cyberhub.Mode = value
			case "proxy":
				cfg.Cyberhub.Proxy = value
			}
		case "scan":
			switch key {
			case "verify":
				cfg.Scan.Verify = value
			}
		case "search":
			switch key {
			case "tavily_keys":
				cfg.Search.TavilyKeys = value
			}
		case "websearch":
			switch key {
			case "proxy":
				cfg.WebSearch.Proxy = value
			}
		}
	}
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, string(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}

func trimString(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

func countLeadingSpaces(s string) int {
	n := 0
	for _, c := range s {
		if c == ' ' {
			n++
		} else if c == '\t' {
			n += 2
		} else {
			break
		}
	}
	return n
}

func splitKV(s string) (string, string) {
	idx := -1
	for i, c := range s {
		if c == ':' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return s, ""
	}
	return trimString(s[:idx]), trimString(s[idx+1:])
}

func unquote(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

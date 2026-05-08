package servercmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	acpserver "github.com/chainreactors/aiscan/pkg/acp/server"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	goflags "github.com/jessevdk/go-flags"
)

type Options struct {
	URL     string `long:"acp-url" description:"ACP server listen URL" default:"http://127.0.0.1:8765"`
	DB      string `long:"acp-db" description:"ACP SQLite database path" default:"./acp.db"`
	Timeout int    `long:"timeout" description:"Overall timeout in seconds" default:"3600"`
	Debug   bool   `long:"debug" description:"Enable debug logging"`
	Quiet   bool   `short:"q" long:"quiet" description:"Quiet mode"`
	Logger  telemetry.Logger
}

func Main() {
	var opts Options
	parser := newParser(&opts)
	if _, err := parser.Parse(); err != nil {
		if flagsErr, ok := err.(*goflags.Error); ok && flagsErr.Type == goflags.ErrHelp {
			printHelp(parser)
			return
		}
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	logger := telemetry.GlobalLogger(telemetry.LogConfig{Debug: opts.Debug, Quiet: opts.Quiet, Output: os.Stderr})

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.Timeout)*time.Second)
	defer cancel()
	setupSignalHandler(cancel, logger)

	opts.Logger = logger
	if err := Run(ctx, opts); err != nil {
		logger.Errorf("acp server failed: %s", err)
		os.Exit(1)
	}
}

func Run(ctx context.Context, opts Options) error {
	logger := opts.Logger
	if logger == nil {
		logger = telemetry.NopLogger()
	}
	dbPath := opts.DB
	if dbPath == "" {
		dbPath = "./acp.db"
	}
	if !filepath.IsAbs(dbPath) {
		if wd, err := os.Getwd(); err == nil {
			dbPath = filepath.Join(wd, dbPath)
		}
	}
	store, err := acpserver.NewSQLiteStore(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	listenURL := opts.URL
	if listenURL == "" {
		listenURL = "http://127.0.0.1:8765"
	}
	addr, err := listenAddrFromURL(listenURL)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:    addr,
		Handler: acpserver.NewHandler(acpserver.NewService(store)),
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	logger.Importantf("acp_server status=starting url=%s db=%s", publicACPURL(listenURL), dbPath)
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func newParser(opts *Options) *goflags.Parser {
	parser := goflags.NewParser(opts, goflags.Default&^goflags.PrintErrors)
	parser.Usage = `[OPTIONS]

acp - ACP HTTP server

Examples:
  acp --acp-url http://127.0.0.1:8765 --acp-db ./acp.db`
	return parser
}

func listenAddrFromURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "127.0.0.1:8765", nil
	}
	if !strings.Contains(raw, "://") {
		return "", fmt.Errorf("invalid acp url %q: expected URL", raw)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid acp url %q: %w", raw, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("invalid acp url %q: expected http or https", raw)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("invalid acp url %q: missing host", raw)
	}
	return parsed.Host, nil
}

func publicACPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	return raw
}

func setupSignalHandler(cancel context.CancelFunc, logger telemetry.Logger) {
	if logger == nil {
		logger = telemetry.NopLogger()
	}
	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sigCount := 0
		for range sigChan {
			sigCount++
			if sigCount == 1 {
				logger.Warnf("signal=shutdown action=finish_current_turn")
				cancel()
			} else {
				logger.Warnf("signal=shutdown action=force_exit")
				os.Exit(1)
			}
		}
	}()
}

func printHelp(parser *goflags.Parser) {
	parser.WriteHelp(os.Stdout)
}

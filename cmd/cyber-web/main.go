package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"syscall"

	"github.com/chainreactors/cyber/core/telemetry"
	"github.com/chainreactors/cyber/pkg/cli/configuration"
	cfg "github.com/chainreactors/cyber/pkg/config"
	webext "github.com/chainreactors/cyber/pkg/exts/web"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	managementapi "github.com/chainreactors/cyber/pkg/web/api"
	webhost "github.com/chainreactors/cyber/pkg/web/host"
	webstatic "github.com/chainreactors/cyber/web"
	flags "github.com/jessevdk/go-flags"
)

type options struct {
	Addr            string `long:"addr" default:"127.0.0.1:8080" description:"HTTP listen address"`
	DB              string `long:"db" default:"cyber-web.db" description:"SQLite database path"`
	Token           string `long:"token" description:"Access key (auto-generated if empty)"`
	cfg.LLMOptions  `group:"Shared model settings"`
	cfg.MiscOptions `group:"Options"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "cyber-web: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	codec := webhost.SharedConfigCodec()
	if handled, err := configuration.Run(ctx, args, configuration.Host{Name: "cyber-web", Sections: codec.Sections, Out: stdout, Err: stderr}); handled {
		return err
	}
	var opts options
	parser := flags.NewParser(&opts, flags.Default&^flags.PrintErrors)
	parser.Name = "cyber-web"
	parser.Usage = "[OPTIONS]\n\nProfile-neutral Web Hub. Connect cyber-scan, cyber-audit or custom AOP nodes."
	configuration.RegisterHelp(parser)
	parser.SubcommandsOptional = true
	rest, err := parser.ParseArgs(args)
	if err != nil {
		var flagErr *flags.Error
		if errors.As(err, &flagErr) && flagErr.Type == flags.ErrHelp {
			parser.WriteHelp(stdout)
			return nil
		}
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected arguments: %v", rest)
	}
	if opts.Version {
		_, err := fmt.Fprintf(stdout, "cyber-web v%s\n", cfg.Version)
		return err
	}
	explicit := cfg.Option{LLMOptions: opts.LLMOptions, MiscOptions: opts.MiscOptions, Sections: codec.Sections}
	cfg.CaptureExplicitFlags(&explicit, parser)
	option := explicit
	if _, err := cfg.ResolveRuntimeConfig(&option); err != nil {
		return err
	}
	if option.Snapshot != nil {
		for _, message := range option.Snapshot.Diagnostics {
			fmt.Fprintln(stderr, message)
		}
	}
	store := &webhost.FileConfigStore{Explicit: opts.ConfigFile, Runtime: &option, Overrides: &explicit, Codec: codec}
	key := opts.Token
	if key == "" {
		key = rand.Text()
	}
	static, err := fs.Sub(webstatic.FS, "static")
	if err != nil {
		return err
	}
	logger := telemetry.GlobalLogger(telemetry.LogConfig{Debug: opts.Debug, Quiet: opts.Quiet, Output: stderr, Color: !opts.NoColor})
	return webhost.Serve(ctx, webhost.Config{
		Addr: opts.Addr, Static: static, Logger: logger,
		Management: webext.Config{
			Product: "cyber-harness", Profiles: []webpkg.Profile{{ID: "cyber-scan", Title: "Cyber Scan"}, {ID: "cyber-audit", Title: "Cyber Audit"}}, Database: opts.DB, AccessKey: key, ConfigStore: store,
			ConfigAPI: managementapi.ConfigOptions{Sections: codec.Sections}, RuntimeLLM: store.RuntimeLLM,
		},
	})
}

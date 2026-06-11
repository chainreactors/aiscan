package search

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/chainreactors/aiscan/pkg/resources"
)

type Command struct {
	cyberhub *CyberhubSearch
}

type Opts struct {
	Resources *resources.Set
}

func New(opts Opts) *Command {
	cmd := &Command{}
	if opts.Resources != nil {
		cmd.cyberhub = NewCyberhubSearch(opts.Resources)
	}
	return cmd
}

func (c *Command) Name() string { return "search" }

func (c *Command) Usage() string {
	return `search - Search loaded fingerprints and POC templates
Usage:
  search cyberhub list|search [finger|poc|all] [options]

Subcommands:
  cyberhub   Search loaded fingerprints and POC templates (same as standalone cyberhub command)

Examples:
  search cyberhub search poc seeyon
  search cyberhub list poc --severity critical,high`
}

func (c *Command) Execute(ctx context.Context, args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("search: subcommand required\n\n%s", c.Usage())
	}

	var result string
	var err error
	switch strings.ToLower(args[0]) {
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

type CyberhubCommand struct {
	cyberhub *CyberhubSearch
}

func NewCyberhubCommand(resources *resources.Set) *CyberhubCommand {
	return &CyberhubCommand{cyberhub: NewCyberhubSearch(resources)}
}

func (c *CyberhubCommand) Name() string { return "cyberhub" }

func (c *CyberhubCommand) Usage() string { return cyberhubUsage() }

func (c *CyberhubCommand) Execute(ctx context.Context, args []string, w io.Writer) error {
	if c == nil || c.cyberhub == nil {
		return fmt.Errorf("cyberhub: resources not loaded")
	}
	result, err := c.cyberhub.Execute(ctx, args)
	if result != "" {
		_, _ = io.WriteString(w, result)
	}
	return err
}

package scan

import (
	"context"
	"strings"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

type Option func(*Command)

type DeepBrowserFunc func(ctx context.Context, targetURL string) (string, error)

type AISkillConfig struct {
	Model      string
	Timeout    int
	Workers    int
	Enable     bool
	VerifyMode string
}

func WithParent(a *agent.Agent) Option {
	return func(c *Command) { c.parent = a }
}

func WithAISkillConfig(cfg AISkillConfig) Option {
	return func(c *Command) { c.aiConfig = cfg }
}

func WithProxy(proxy string) Option {
	return func(c *Command) { c.proxy = proxy }
}

func WithLogger(logger telemetry.Logger) Option {
	return func(c *Command) {
		if logger != nil {
			c.logger = logger
		}
	}
}

func (c *Command) Configure(opts ...Option) {
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}
}

func WithDeepBrowserFunc(fn DeepBrowserFunc) Option {
	return func(c *Command) { c.deepBrowser = fn }
}

func WithCheckpointSink(fn command.CheckpointSink) Option {
	return func(c *Command) { c.checkpointSink = fn }
}

func verificationEnabled(mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return mode != "" && mode != "off"
}

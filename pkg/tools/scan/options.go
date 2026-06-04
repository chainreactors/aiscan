package scan

import (
	"context"
	"strings"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

type Option func(*Command)

type ReportFunc func(ctx context.Context, prompt, systemPrompt, model string, maxTokens int) (string, error)

type DeepBrowserFunc func(ctx context.Context, targetURL string) (string, error)

type AISkillConfig struct {
	Model      string
	Timeout    int
	Workers    int
	Enable     bool
	VerifyMode string
}

func WithReportFunc(fn ReportFunc) Option {
	return func(c *Command) { c.reportFunc = fn }
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

// AgentRunResult carries raw LLM output (for recording) and the checkpoint submitted by the agent.
type AgentRunResult struct {
	Raw        string
	Checkpoint *command.CheckpointResult
	Messages   []provider.ChatMessage
	Usage      *provider.Usage
}

// AgentFunc runs a multi-turn agent with tool access and returns both raw output and parsed result.
type AgentFunc func(ctx context.Context, prompt, systemPrompt, model string, maxTokens int) (*AgentRunResult, error)

func WithAgentFunc(fn AgentFunc) Option {
	return func(c *Command) { c.agentFunc = fn }
}

func WithDeepBrowserFunc(fn DeepBrowserFunc) Option {
	return func(c *Command) { c.deepBrowser = fn }
}

func verificationEnabled(mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return mode != "" && mode != "off"
}

package agent

import (
	"context"

	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/agent/inbox"
	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)


type EventType string

const (
	EventAgentStart         EventType = "agent_start"
	EventAgentEnd           EventType = "agent_end"
	EventTurnStart          EventType = "turn_start"
	EventTurnEnd            EventType = "turn_end"
	EventMessageStart       EventType = "message_start"
	EventMessageUpdate      EventType = "message_update"
	EventMessageEnd         EventType = "message_end"
	EventToolExecutionStart EventType = "tool_execution_start"
	EventToolExecutionEnd   EventType = "tool_execution_end"
	EventTokenBudgetWarning EventType = "token_budget_warning"
)

type StopReason string

const (
	StopReasonCompleted  StopReason = "completed"
	StopReasonTerminated StopReason = "terminated"
	StopReasonStopped    StopReason = "stopped"
	StopReasonBudget     StopReason = "budget"
	StopReasonError      StopReason = "error"
	StopReasonCanceled StopReason = "canceled"
)

type Event struct {
	Type          EventType
	Turn          int
	Message       provider.ChatMessage
	Messages      []provider.ChatMessage
	NewMessages   []provider.ChatMessage
	ToolResults   []provider.ChatMessage
	ToolCallID    string
	ToolName      string
	Arguments     string
	Result        string
	IsError       bool
	Err           error
	Stop          StopReason
	Usage         *provider.Usage
	ContextTokens int
}

type EventHandler func(context.Context, Event) error

type TransformContextFunc func([]provider.ChatMessage) []provider.ChatMessage

type BeforeToolCallContext struct {
	AssistantMessage provider.ChatMessage
	ToolCall         provider.ToolCall
	SystemPrompt     string
	Messages         []provider.ChatMessage
}

type BeforeToolCallResult struct {
	Block  bool
	Reason string
}

type AfterToolCallContext struct {
	AssistantMessage provider.ChatMessage
	ToolCall         provider.ToolCall
	Result           string
	IsError          bool
	SystemPrompt     string
	Messages         []provider.ChatMessage
}

type ToolFlowDecision int

const (
	ToolFlowContinue  ToolFlowDecision = iota
	ToolFlowTerminate
)

type AfterToolCallResult struct {
	Result  *string
	IsError *bool
	Flow    ToolFlowDecision
}

type Config struct {
	Provider            provider.Provider
	Tools               *command.CommandRegistry
	Model               string
	SystemPrompt        string
	Messages            []provider.ChatMessage
	MaxTokens           int
	Temperature         *float64
	Stream              bool
	MaxRetries          int
	TokenBudget         int
	ResponseFormat      *provider.ResponseFormat
	Logger              telemetry.Logger
	TransformContext    TransformContextFunc
	Emit                EventHandler
	BeforeToolCall func(context.Context, BeforeToolCallContext) (*BeforeToolCallResult, error)
	AfterToolCall  func(context.Context, AfterToolCallContext) (*AfterToolCallResult, error)
	MaxTurns       int
	LoopScheduler  *LoopScheduler
	Inbox          inbox.Inbox
	Expander       *inbox.Expander
	MaxResultSize  int
	CacheRetention provider.CacheRetention
	SessionID      string
}

// Builder methods — each returns a modified copy (Config is a value type).

func (c Config) WithProvider(p provider.Provider) Config             { c.Provider = p; return c }
func (c Config) WithTools(t *command.CommandRegistry) Config         { c.Tools = t; return c }
func (c Config) WithModel(m string) Config                           { c.Model = m; return c }
func (c Config) WithSystemPrompt(s string) Config                    { c.SystemPrompt = s; return c }
func (c Config) WithMessages(msgs []provider.ChatMessage) Config     { c.Messages = msgs; return c }
func (c Config) WithStream(s bool) Config                            { c.Stream = s; return c }
func (c Config) WithInbox(ib inbox.Inbox) Config                     { c.Inbox = ib; return c }
func (c Config) WithLogger(l telemetry.Logger) Config                { c.Logger = l; return c }
func (c Config) WithEventHandler(h EventHandler) Config              { c.Emit = h; return c }
func (c Config) WithMaxTokens(n int) Config                          { c.MaxTokens = n; return c }
func (c Config) WithTemperature(t float64) Config                    { c.Temperature = &t; return c }
func (c Config) WithMaxRetries(n int) Config                         { c.MaxRetries = n; return c }
func (c Config) WithTokenBudget(n int) Config                        { c.TokenBudget = n; return c }
func (c Config) WithExpander(e *inbox.Expander) Config               { c.Expander = e; return c }
func (c Config) WithTransformContext(fn TransformContextFunc) Config { c.TransformContext = fn; return c }
func (c Config) WithCacheRetention(r provider.CacheRetention) Config { c.CacheRetention = r; return c }
func (c Config) WithSessionID(id string) Config                      { c.SessionID = id; return c }
func (c Config) WithResponseFormat(rf *provider.ResponseFormat) Config {
	c.ResponseFormat = rf
	return c
}
func (c Config) WithLoopScheduler(s *LoopScheduler) Config {
	c.LoopScheduler = s
	return c
}

func prepareConfig(c Config) Config {
	cfg := normalizeConfig(c)
	if cfg.Tools == nil {
		cfg.Tools = command.NewRegistry()
	}
	if cfg.Inbox == nil {
		cfg.Inbox = inbox.NewBuffered(SubInboxCapacity)
	}
	return cfg
}

// NewAgent creates an Agent from a Config.
func NewAgent(cfg Config) *Agent {
	cfg = prepareConfig(cfg)
	return &Agent{
		Cfg: cfg,
		state: State{
			SystemPrompt: cfg.SystemPrompt,
			Tools:        cfg.Tools,
		},
	}
}

type TurnUsage struct {
	Turn             int `json:"turn"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

type Result struct {
	Output        string
	NewMessages   []provider.ChatMessage
	Messages      []provider.ChatMessage
	Turns         int
	TotalUsage    provider.Usage
	TurnUsages    []TurnUsage
	ContextTokens int
	Err           error
}

type State struct {
	SystemPrompt string
	Messages     []provider.ChatMessage
	Tools        *command.CommandRegistry
	ErrorMessage string
	LastError    error
}

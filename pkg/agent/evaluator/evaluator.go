package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	agentpkg "github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/agent/truncate"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

const (
	defaultMaxRetries = 3
	maxResultPreview  = 200
	maxOutputPreview  = 3000
	maxTraceSize      = 16000
)

type Config struct {
	Provider      provider.Provider
	Model         string
	MaxRetries    int
	ContextWindow int
	Logger        telemetry.Logger
	// WorkDir roots file-based acceptance checks (count/exists/judge evidence).
	// Empty falls back to the process working directory, which is where the
	// agent's file tools write. RunWithEval populates it from the agent.
	WorkDir string
}

type Verdict struct {
	Pass           bool   `json:"pass"`
	Reason         string `json:"reason"`
	Feedback       string `json:"feedback"`
	InheritContext bool   `json:"inherit_context"`
}

type Evaluator struct {
	cfg Config

	mu           sync.Mutex
	compileCache map[string][]Assertion
}

func New(cfg Config) *Evaluator {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaultMaxRetries
	}
	if cfg.ContextWindow <= 0 {
		cfg.ContextWindow = agentpkg.ModelContextWindow(cfg.Model)
	}
	if cfg.Logger == nil {
		cfg.Logger = telemetry.NopLogger()
	}
	return &Evaluator{cfg: cfg, compileCache: make(map[string][]Assertion)}
}

func (e *Evaluator) Evaluate(ctx context.Context, goal, criteria string, messages []provider.ChatMessage, output string, turns, contextTokens int) (*Verdict, error) {
	trace := buildTrace(messages, output, turns, contextTokens, e.cfg.ContextWindow)

	// Structured path: compile the criteria into machine-checkable assertions and
	// verify them against the artifacts the agent actually produced. Countable
	// and existence checks resolve deterministically (no LLM guessing); only
	// genuinely qualitative clauses reach an LLM, and then with real evidence in
	// front of it. Falls through to the trace judge if compilation yields nothing.
	if criteria != "" && e.cfg.Provider != nil {
		if assertions, err := e.compileAssertions(ctx, goal, criteria); err != nil {
			e.cfg.Logger.Warnf("criteria compile failed, falling back to trace judge: %s", err)
		} else if len(assertions) > 0 {
			v := e.checkAssertions(ctx, goal, trace, output, assertions)
			v.InheritContext = shouldInherit(contextTokens, e.cfg.ContextWindow)
			return v, nil
		}
	}

	prompt := buildPrompt(goal, criteria, trace)

	var lastErr error
	for attempt := 0; attempt < e.cfg.MaxRetries; attempt++ {
		v, err := e.call(ctx, prompt)
		if err == nil {
			return v, nil
		}
		lastErr = err
		e.cfg.Logger.Warnf("evaluate attempt %d failed: %s", attempt+1, err)
		if attempt < e.cfg.MaxRetries-1 {
			select {
			case <-time.After(time.Duration(attempt+1) * time.Second):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("evaluate failed after %d attempts: %w", e.cfg.MaxRetries, lastErr)
}

const systemPrompt = `You are an evaluator. Call the "verdict" tool with your result. No text replies.

Rules:
- pass=true only if the task was fully achieved per criteria
- feedback: actionable next step when pass=false
- inherit_context decision based on context_usage% shown in trace:
  - >80%: MUST set inherit_context=false (context nearly full, fresh start required)
  - >50%: SHOULD set inherit_context=false unless critical intermediate state exists
  - <=50%: default inherit_context=true
- When inherit_context=false, feedback must be fully self-contained (include file paths, findings, variable names, prior progress)`

var verdictTool = provider.ToolDefinition{
	Type: "function",
	Function: provider.FunctionDefinition{
		Name:        "verdict",
		Description: "Submit evaluation verdict",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pass":            map[string]interface{}{"type": "boolean", "description": "task fully achieved"},
				"reason":          map[string]interface{}{"type": "string", "description": "one-sentence summary"},
				"feedback":        map[string]interface{}{"type": "string", "description": "next step if not pass; self-contained when inherit_context=false"},
				"inherit_context": map[string]interface{}{"type": "boolean", "description": "false to discard conversation history for next round"},
			},
			"required": []string{"pass", "reason", "feedback", "inherit_context"},
		},
	},
}

func (e *Evaluator) call(ctx context.Context, userPrompt string) (*Verdict, error) {
	temp := float64(0)
	resp, err := e.cfg.Provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model: e.cfg.Model,
		Messages: []provider.ChatMessage{
			provider.NewTextMessage("system", systemPrompt),
			provider.NewTextMessage("user", userPrompt),
		},
		Tools:       []provider.ToolDefinition{verdictTool},
		MaxTokens:   2048,
		Temperature: &temp,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no choices returned")
	}

	for _, tc := range resp.Choices[0].Message.ToolCalls {
		if tc.Function.Name == "verdict" {
			var v Verdict
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &v); err != nil {
				return nil, fmt.Errorf("unmarshal verdict: %w", err)
			}
			return &v, nil
		}
	}
	return nil, fmt.Errorf("model did not call verdict tool")
}

// shouldInherit applies the context-usage policy deterministically for the
// structured path: discard history only when the window is nearly full.
func shouldInherit(contextTokens, contextWindow int) bool {
	if contextWindow <= 0 {
		return true
	}
	return float64(contextTokens)/float64(contextWindow) <= 0.8
}

func buildPrompt(goal, criteria, trace string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Goal\n%s\n\n", goal)
	if criteria != "" {
		fmt.Fprintf(&sb, "## Acceptance Criteria\n%s\n\n", criteria)
	}
	fmt.Fprintf(&sb, "## Execution Trace\n%s", trace)
	return sb.String()
}

func buildTrace(messages []provider.ChatMessage, output string, turns, contextTokens, contextWindow int) string {
	var sb strings.Builder
	usagePct := float64(contextTokens) / float64(contextWindow) * 100
	fmt.Fprintf(&sb, "Turns: %d | Messages: %d | Context tokens: %d/%d (%.0f%%)\n", turns, len(messages), contextTokens, contextWindow, usagePct)

	toolCallCount := 0
	for _, msg := range messages {
		toolCallCount += len(msg.ToolCalls)
	}
	fmt.Fprintf(&sb, "Tool calls: %d\n", toolCallCount)

	sb.WriteString("\nTool call sequence:\n")
	seq := 0
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			seq++
			fmt.Fprintf(&sb, "  [%d] %s\n", seq, tc.Function.Name)
		}
	}

	sb.WriteString("\nAssistant summaries:\n")
	for _, msg := range messages {
		if msg.Role == "assistant" && msg.Content != nil && *msg.Content != "" {
			fmt.Fprintf(&sb, "- %s\n", truncate.Clip(*msg.Content, maxResultPreview))
		}
	}

	if output = strings.TrimSpace(output); output != "" {
		fmt.Fprintf(&sb, "\nFinal output:\n%s\n", truncate.Clip(output, maxOutputPreview))
	}
	return truncate.Clip(sb.String(), maxTraceSize)
}

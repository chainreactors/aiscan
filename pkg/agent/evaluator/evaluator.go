package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/agent/truncate"
)

const (
	defaultMaxRetries = 3
	maxResultPreview  = 200
	maxOutputPreview  = 3000
	maxTraceSize      = 16000
)

type Config struct {
	Provider   provider.Provider
	Model      string
	MaxRetries int
	Logger     telemetry.Logger
}

type Verdict struct {
	Pass     bool   `json:"pass"`
	Reason   string `json:"reason"`
	Feedback string `json:"feedback"`
}

type Evaluator struct {
	cfg Config
}

func New(cfg Config) *Evaluator {
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = defaultMaxRetries
	}
	if cfg.Logger == nil {
		cfg.Logger = telemetry.NopLogger()
	}
	return &Evaluator{cfg: cfg}
}

func (e *Evaluator) Evaluate(ctx context.Context, goal, criteria string, messages []provider.ChatMessage, output string, turns int) (*Verdict, error) {
	trace := buildTrace(messages, output, turns)
	prompt := buildPrompt(goal, criteria, trace)

	var lastErr error
	for attempt := 0; attempt < e.cfg.MaxRetries; attempt++ {
		v, err := e.call(ctx, prompt)
		if err == nil {
			return v, nil
		}
		lastErr = err
		e.cfg.Logger.Warnf("goal eval attempt %d failed: %s", attempt+1, err)
		if attempt < e.cfg.MaxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
	}
	return nil, fmt.Errorf("goal eval failed after %d attempts: %w", e.cfg.MaxRetries, lastErr)
}

const systemPrompt = `You are a goal completion evaluator. Given the original goal, acceptance criteria, and execution trace, determine whether the goal was fully achieved.

You MUST call the "verdict" tool with your evaluation. Do not respond with text.

Rules:
- pass=true only if the goal was fully and correctly completed per the criteria
- feedback: if pass=false, provide a specific, actionable instruction for what the agent should do next
- Be strict: "ran without errors" is NOT the same as "fulfilled the goal"`

var verdictTool = provider.ToolDefinition{
	Type: "function",
	Function: provider.FunctionDefinition{
		Name:        "verdict",
		Description: "Submit the goal evaluation result",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pass":     map[string]interface{}{"type": "boolean", "description": "true only if goal was fully achieved per criteria"},
				"reason":   map[string]interface{}{"type": "string", "description": "one sentence summary of the evaluation"},
				"feedback": map[string]interface{}{"type": "string", "description": "actionable next step if not pass, empty string if pass"},
			},
			"required": []string{"pass", "reason", "feedback"},
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

func buildPrompt(goal, criteria, trace string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Goal\n%s\n\n", goal)
	if criteria != "" {
		fmt.Fprintf(&sb, "## Acceptance Criteria\n%s\n\n", criteria)
	}
	fmt.Fprintf(&sb, "## Execution Trace\n%s", trace)
	return sb.String()
}

func buildTrace(messages []provider.ChatMessage, output string, turns int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Turns: %d\n", turns)

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

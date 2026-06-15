package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

const (
	defaultMaxRetries = 3
	maxArgsPreview    = 200
	maxResultPreview  = 300
	maxOutputPreview  = 1000
	maxTraceSize      = 60000
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

const systemPrompt = `You are a goal completion evaluator for an AI agent system. Given the original goal, acceptance criteria, and execution trace, determine whether the goal was fully achieved.

Respond with ONLY a JSON object:
{"pass": true/false, "reason": "one sentence summary", "feedback": "actionable next step if not pass, empty string if pass"}

Rules:
- pass=true only if the goal was fully and correctly completed per the criteria
- feedback: if pass=false, provide a specific, actionable instruction for what the agent should do next to complete the goal
- Be strict: "ran without errors" is NOT the same as "fulfilled the goal"
- Check that results contain expected data, not just that tools were called`

var verdictResponseFormat = &provider.ResponseFormat{
	Type: "json_object",
}

func (e *Evaluator) call(ctx context.Context, userPrompt string) (*Verdict, error) {
	temp := float64(0)
	req := &provider.ChatCompletionRequest{
		Model: e.cfg.Model,
		Messages: []provider.ChatMessage{
			provider.NewTextMessage("system", systemPrompt),
			provider.NewTextMessage("user", userPrompt),
		},
		MaxTokens:      512,
		Temperature:    &temp,
		ResponseFormat: verdictResponseFormat,
	}

	resp, err := e.cfg.Provider.ChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("evaluator LLM call: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("evaluator LLM error: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("evaluator returned no choices")
	}

	content := ""
	if resp.Choices[0].Message.Content != nil {
		content = *resp.Choices[0].Message.Content
	}
	v, err := parseVerdict(content)
	if err != nil {
		e.cfg.Logger.Warnf("eval verdict parse failed, raw response (%d chars): %s", len(content), clip(content, 500))
	}
	return v, err
}

func parseVerdict(raw string) (*Verdict, error) {
	raw = strings.TrimSpace(raw)

	// Strip markdown fences
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	// Try direct parse first
	var v Verdict
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return &v, nil
	}

	// Fallback: extract first JSON object from the response
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			candidate := raw[start : end+1]
			if err := json.Unmarshal([]byte(candidate), &v); err == nil {
				return &v, nil
			}
		}
	}

	return nil, fmt.Errorf("parse verdict: no valid JSON found in response (%d chars): %s", len(raw), clip(raw, 300))
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

	sb.WriteString("\nTool call trace:\n")
	seq := 0
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			seq++
			fmt.Fprintf(&sb, "  [%d] %s\n", seq, tc.Function.Name)
			if tc.Function.Arguments != "" {
				fmt.Fprintf(&sb, "      args: %s\n", clip(tc.Function.Arguments, maxArgsPreview))
			}
		}
		if msg.Role == "tool" && msg.Content != nil {
			fmt.Fprintf(&sb, "      result: %s\n", clip(*msg.Content, maxResultPreview))
		}
	}

	if output = strings.TrimSpace(output); output != "" {
		fmt.Fprintf(&sb, "\nFinal output:\n%s\n", clip(output, maxOutputPreview))
	}
	return clip(sb.String(), maxTraceSize)
}

func clip(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

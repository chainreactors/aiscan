package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/agent/truncate"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

const (
	defaultMaxRetries    = 3
	maxResultPreview     = 200
	maxOutputPreview     = 3000
	maxToolArgsPreview   = 1200
	maxToolResultPreview = 5000
	maxTraceSize         = 64000
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

// Evaluate judges goal completion against a CUMULATIVE progress summary rather
// than a single round's raw trace. summary carries forward all prior rounds'
// confirmed work so a round where the agent only replied without tool calls
// cannot erase earlier progress from the evaluator's view.
func (e *Evaluator) Evaluate(ctx context.Context, goal, criteria, summary string, messages []provider.ChatMessage, output string, turns int) (*Verdict, error) {
	trace := buildTrace(messages, output, turns)
	prompt := buildPrompt(goal, criteria, summary, trace)

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

const systemPrompt = `You are a goal completion evaluator. You are given the original goal, acceptance criteria, a CUMULATIVE progress summary of everything accomplished across all rounds so far, and the latest round's execution trace.

You MUST call the "verdict" tool with your evaluation. Do not respond with text.

Rules:
- Judge against the CUMULATIVE progress, not just the latest round. If the latest round shows no tool calls but the cumulative summary already shows substantial work, that earlier work still counts.
- pass=true only if the goal was fully and correctly completed per the criteria
- feedback: if pass=false, provide a specific, actionable instruction for what the agent should do NEXT — name the concrete remaining work (e.g. untested subdomains, unverified vulnerability classes), and require the agent to perform actual tool calls, not just describe a plan.
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

func buildPrompt(goal, criteria, summary, trace string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Goal\n%s\n\n", goal)
	if criteria != "" {
		fmt.Fprintf(&sb, "## Acceptance Criteria\n%s\n\n", criteria)
	}
	if strings.TrimSpace(summary) != "" {
		fmt.Fprintf(&sb, "## Cumulative Progress (all rounds so far)\n%s\n\n", summary)
	}
	fmt.Fprintf(&sb, "## Latest Round Execution Trace\n%s", trace)
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

	sb.WriteString("\nTool calls and results:\n")
	seq := 0
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			seq++
			fmt.Fprintf(&sb, "  [%d] %s id=%s\n", seq, tc.Function.Name, tc.ID)
			if strings.TrimSpace(tc.Function.Arguments) != "" {
				fmt.Fprintf(&sb, "      args: %s\n", truncate.Clip(tc.Function.Arguments, maxToolArgsPreview))
			}
		}
		if msg.Role == "tool" {
			body := toolMessageText(msg)
			if strings.TrimSpace(body) == "" {
				continue
			}
			fmt.Fprintf(&sb, "  result for %s:\n%s\n", msg.ToolCallID, indent(clipTraceText(body, maxToolResultPreview), "      "))
		}
	}

	sb.WriteString("\nAssistant summaries:\n")
	for _, msg := range messages {
		if msg.Role == "assistant" && msg.Content != nil && *msg.Content != "" {
			fmt.Fprintf(&sb, "- %s\n", truncate.Clip(*msg.Content, maxResultPreview))
		}
	}

	if output = strings.TrimSpace(output); output != "" {
		fmt.Fprintf(&sb, "\nFinal output:\n%s\n", clipTraceText(output, maxOutputPreview))
	}
	return clipTraceText(sb.String(), maxTraceSize)
}

func toolMessageText(msg provider.ChatMessage) string {
	if msg.Content != nil {
		return *msg.Content
	}
	if len(msg.ContentParts) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, part := range msg.ContentParts {
		switch part.Type {
		case "text":
			sb.WriteString(part.Text)
		case "image_url":
			if part.ImageURL != nil {
				fmt.Fprintf(&sb, "[image: %s]\n", part.ImageURL.Detail)
			} else {
				sb.WriteString("[image]\n")
			}
		default:
			fmt.Fprintf(&sb, "[%s]\n", part.Type)
		}
	}
	return sb.String()
}

func indent(value, prefix string) string {
	value = strings.TrimRight(value, "\n")
	if value == "" {
		return prefix
	}
	return prefix + strings.ReplaceAll(value, "\n", "\n"+prefix)
}

func clipTraceText(value string, maxBytes int) string {
	tr := truncate.Head(value, truncate.Options{MaxLines: 400, MaxBytes: maxBytes})
	if !tr.Truncated {
		return tr.Content
	}
	if tr.FirstLineExceedsLimit {
		return truncate.Clip(value, maxBytes)
	}
	return tr.Content + fmt.Sprintf(
		"\n[truncated: showing %d/%d lines (%s of %s)]",
		tr.OutputLines, tr.TotalLines, truncate.FormatSize(tr.OutputBytes), truncate.FormatSize(tr.TotalBytes))
}

const summarizeSystemPrompt = `You are a progress-tracking assistant for a security assessment agent. You maintain a single cumulative findings summary across many rounds of work.

Do NOT continue the work or evaluate it. ONLY output the updated summary text, nothing else.`

const summarizeInitialPrompt = `Below is the execution trace of the FIRST round of work toward the goal. Produce a structured progress summary that a later round and an evaluator will rely on.

Use this EXACT format:

## Goal
[restate the goal in one or two lines]

## Coverage
- [each target / subdomain / component and whether it was tested, partially tested, or untouched]

## Confirmed Findings
- [each confirmed vulnerability: TYPE — exact URL/endpoint — the PoC request/command verbatim — the evidence observed. Preserve exact URLs, parameters, payloads, and response markers. Do NOT paraphrase PoCs.]

## In Progress / Leads
- [endpoints or ideas identified but not yet confirmed]

## Next Steps
1. [ordered concrete remaining work]

Keep it concise but NEVER drop a confirmed finding or its PoC.`

const summarizeUpdatePrompt = `The text in <previous-summary> is the cumulative summary so far. Below it is the execution trace of the LATEST round. Merge the new round into the summary.

RULES:
- PRESERVE every Confirmed Finding from the previous summary verbatim (type, URL, PoC, evidence). Never delete or shorten an existing PoC.
- ADD newly confirmed findings from the latest round.
- UPDATE Coverage: mark newly tested targets/subdomains.
- UPDATE Next Steps based on what is now done.
- If the latest round shows no new tool calls, keep the previous summary unchanged except possibly refining Next Steps.

Output the FULL updated summary using the same EXACT format (## Goal / ## Coverage / ## Confirmed Findings / ## In Progress / Leads / ## Next Steps).`

// Summarize folds the latest round's trace into a cumulative progress summary.
// When previousSummary is empty it produces an initial summary; otherwise it
// merges the new round into the existing one (pi-mono UPDATE pattern). This is
// what lets the evaluator judge against everything accomplished so far rather
// than a single round that may have produced no tool calls.
func (e *Evaluator) Summarize(ctx context.Context, goal, previousSummary string, messages []provider.ChatMessage, output string, turns int) (string, error) {
	trace := buildTrace(messages, output, turns)

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Goal\n%s\n\n", goal)
	if strings.TrimSpace(previousSummary) != "" {
		fmt.Fprintf(&sb, "<previous-summary>\n%s\n</previous-summary>\n\n", previousSummary)
		fmt.Fprintf(&sb, "## Latest Round Execution Trace\n%s\n\n%s", trace, summarizeUpdatePrompt)
	} else {
		fmt.Fprintf(&sb, "## Execution Trace\n%s\n\n%s", trace, summarizeInitialPrompt)
	}

	resp, err := e.cfg.Provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model: e.cfg.Model,
		Messages: []provider.ChatMessage{
			provider.NewTextMessage("system", summarizeSystemPrompt),
			provider.NewTextMessage("user", sb.String()),
		},
		MaxTokens: 4096,
	})
	if err != nil {
		return previousSummary, fmt.Errorf("summarize LLM call: %w", err)
	}
	if len(resp.Choices) == 0 {
		return previousSummary, fmt.Errorf("summarize: no choices returned")
	}
	content := resp.Choices[0].Message.Content
	if content == nil || strings.TrimSpace(*content) == "" {
		return previousSummary, fmt.Errorf("summarize: empty content")
	}
	return strings.TrimSpace(*content), nil
}

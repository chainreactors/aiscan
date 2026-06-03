package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
)

type CheckpointArgs struct {
	Kind    string   `json:"kind"              jsonschema:"description=Checkpoint kind: verify, sniper, or deep,enum=verify,enum=sniper,enum=deep"`
	Title   string   `json:"title"             jsonschema:"description=Short title summarizing the finding"`
	Content string   `json:"content"           jsonschema:"description=Markdown body with evidence and analysis details"`
	Target  string   `json:"target,omitempty"  jsonschema:"description=Target host:port or URL being analyzed"`
	Status  string   `json:"status,omitempty"  jsonschema:"description=Verification status,enum=confirmed,enum=not_confirmed,enum=info,enum=inconclusive"`
	Options []string `json:"options,omitempty"  jsonschema:"description=Optional reviewer action labels"`
	Labels  []string `json:"labels,omitempty"   jsonschema:"description=Semantic labels for severity and classification (e.g. high or critical)"`
}

type CheckpointResult struct {
	Kind    string
	Title   string
	Content string
	Target  string
	Status  string
	Options []string
	Labels  []string
}

// IOAContent returns the checkpoint as IOA message content.
func (r *CheckpointResult) IOAContent() map[string]interface{} {
	m := map[string]interface{}{
		"type":    "checkpoint",
		"kind":    r.Kind,
		"title":   r.Title,
		"content": r.Content,
	}
	if r.Target != "" {
		m["target"] = r.Target
	}
	if r.Status != "" {
		m["status"] = r.Status
	}
	if len(r.Options) > 0 {
		m["options"] = r.Options
	}
	return m
}

// IOAMeta returns labels as IOA message metadata.
func (r *CheckpointResult) IOAMeta() map[string]interface{} {
	if len(r.Labels) == 0 {
		return nil
	}
	return map[string]interface{}{
		"labels": r.Labels,
	}
}

type CheckpointTool struct {
	result *CheckpointResult
}

func NewCheckpointTool() *CheckpointTool {
	return &CheckpointTool{}
}

func (t *CheckpointTool) Result() *CheckpointResult {
	return t.result
}

func (t *CheckpointTool) Name() string { return "checkpoint" }

func (t *CheckpointTool) Description() string {
	return "Submit the final finding after completing verification or analysis. This terminates the current session. Call exactly once with your conclusion."
}

func (t *CheckpointTool) Definition() provider.ToolDefinition {
	return ToolDef("checkpoint", t.Description(), CheckpointArgs{})
}

func (t *CheckpointTool) Execute(_ context.Context, arguments string) (ToolResult, error) {
	args, err := ParseArgs[CheckpointArgs](arguments)
	if err != nil {
		return ToolResult{}, err
	}
	status := NormalizeStatus(args.Status)
	if status == "" {
		status = args.Status
	}
	t.result = &CheckpointResult{
		Kind:    args.Kind,
		Title:   args.Title,
		Content: args.Content,
		Target:  args.Target,
		Status:  status,
		Options: args.Options,
		Labels:  args.Labels,
	}
	return TerminateResult(fmt.Sprintf("checkpoint submitted: kind=%s title=%s", t.result.Kind, t.result.Title)), nil
}

func NormalizeStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "confirmed":
		return "confirmed"
	case "not_confirmed", "not confirmed", "false_positive":
		return "not_confirmed"
	case "info", "informational":
		return "info"
	case "inconclusive":
		return "inconclusive"
	default:
		return ""
	}
}

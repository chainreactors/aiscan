package command

import (
	"context"
	"fmt"
	"strings"

)

type CheckpointArgs struct {
	Kind    string   `json:"kind"              jsonschema:"description=Checkpoint kind (e.g. verify, sniper, deep),enum=verify,enum=sniper,enum=deep"`
	Title   string   `json:"title"             jsonschema:"description=Short checkpoint title summarizing the result"`
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
	return "Record a structured checkpoint with evidence and analysis details. Can be called multiple times; each call overwrites the previous result. Does NOT terminate the session — call the finish tool when done."
}

func (t *CheckpointTool) Definition() ToolDefinition {
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
	return TextResult(fmt.Sprintf("checkpoint recorded: kind=%s title=%s", t.result.Kind, t.result.Title)), nil
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

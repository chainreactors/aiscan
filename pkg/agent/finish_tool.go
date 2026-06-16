package agent

import (
	"context"
	"strings"

	"github.com/chainreactors/aiscan/pkg/commands"
)

type FinishTool struct{}

func NewFinishTool() *FinishTool { return &FinishTool{} }

func (t *FinishTool) Name() string { return "finish" }

func (t *FinishTool) Description() string {
	return "Signal that you have completed the current task. You MUST call this tool when you are done — the session does not end automatically."
}

type finishArgs struct {
	Summary string `json:"summary" jsonschema:"description=Brief summary of what was accomplished"`
}

func (t *FinishTool) Definition() ToolDefinition {
	return commands.ToolDef("finish", t.Description(), finishArgs{})
}

func (t *FinishTool) Execute(_ context.Context, arguments string) (commands.ToolResult, error) {
	args, _ := commands.ParseArgs[finishArgs](arguments)
	summary := strings.TrimSpace(args.Summary)
	if summary == "" {
		summary = "Task completed."
	}
	return commands.TerminateResult(summary), nil
}

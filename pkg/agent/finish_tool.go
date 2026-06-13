package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/chainreactors/aiscan/pkg/command"
)

// FinishTool gives a finite agent run an explicit, deterministic way to
// terminate. Its Execute returns a terminating ToolResult, which flows through
// the existing ToolFlowTerminate path in runLoop (loop.go:387 -> 357 -> 141)
// and returns StopReasonTerminated.
//
// Without it, finite tasks running in loop mode have no "done" signal: the
// parent re-summarizes on every subagent completion trickle and, with
// --heartbeat>0, never exits on its own.
type FinishTool struct{}

// NewFinishTool constructs a FinishTool.
func NewFinishTool() *FinishTool { return &FinishTool{} }

func (t *FinishTool) Name() string { return "finish" }

func (t *FinishTool) Description() string {
	return "Signal that the current task is complete and terminate the agent loop cleanly. " +
		"Call this exactly once, after the objective is fully achieved AND every spawned subagent " +
		"has reported its result. Do NOT call while subagents are still running."
}

type finishArgs struct {
	Reason string `json:"reason,omitempty" jsonschema:"description=Optional one-line summary of what was accomplished"`
}

func (t *FinishTool) Definition() ToolDefinition {
	return command.ToolDef(t.Name(), t.Description(), finishArgs{})
}

func (t *FinishTool) Execute(_ context.Context, arguments string) (command.ToolResult, error) {
	args, err := command.ParseArgs[finishArgs](arguments)
	if err != nil {
		return command.ToolResult{}, err
	}
	msg := "task complete, terminating agent loop"
	if r := strings.TrimSpace(args.Reason); r != "" {
		msg = fmt.Sprintf("task complete: %s", r)
	}
	return command.TerminateResult(msg), nil
}

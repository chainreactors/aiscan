package command

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/task"
)

// TaskTool exposes the background-task manager to the agent. Pairs with
// the bash tool's background:true mode. Action discriminator pattern
// mirrors pi-mono's tmux tool.
type TaskTool struct {
	manager *task.Manager
}

func NewTaskTool(manager *task.Manager) *TaskTool { return &TaskTool{manager: manager} }

func (t *TaskTool) Name() string { return "task" }

func (t *TaskTool) Description() string {
	return "Inspect and control background tasks started by `bash` with background:true. Actions: list (show running/done tasks), peek (last N lines of stdout), wait (block until done or timeout), kill (terminate)."
}

func (t *TaskTool) Definition() provider.ToolDefinition {
	return provider.ToolDefinition{
		Type: "function",
		Function: provider.FunctionDefinition{
			Name:        "task",
			Description: t.Description(),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"list", "peek", "wait", "kill"},
						"description": "Which subcommand to invoke.",
					},
					"id": map[string]any{
						"type":        "string",
						"description": "Task id (required for peek/wait/kill).",
					},
					"lines": map[string]any{
						"type":        "integer",
						"description": "Lines to return from peek. Default 30.",
					},
					"timeout_seconds": map[string]any{
						"type":        "integer",
						"description": "Seconds to block in wait before returning the task's current state. Default 60.",
					},
				},
				"required": []string{"action"},
			},
		},
	}
}

func (t *TaskTool) Execute(ctx context.Context, arguments string) (string, error) {
	var args struct {
		Action         string `json:"action"`
		ID             string `json:"id"`
		Lines          int    `json:"lines"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(args.Action)) {
	case "list":
		return t.actionList(), nil
	case "peek":
		if args.ID == "" {
			return "", fmt.Errorf("peek requires id")
		}
		return t.manager.Peek(args.ID, args.Lines)
	case "wait":
		if args.ID == "" {
			return "", fmt.Errorf("wait requires id")
		}
		timeout := time.Duration(args.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
		info, err := t.manager.Wait(ctx, args.ID, timeout)
		if err != nil {
			return "", err
		}
		return formatInfo(info), nil
	case "kill":
		if args.ID == "" {
			return "", fmt.Errorf("kill requires id")
		}
		if err := t.manager.Kill(args.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Sent SIGTERM to task %s.", args.ID), nil
	default:
		return "", fmt.Errorf("unknown action: %q (expected list/peek/wait/kill)", args.Action)
	}
}

func (t *TaskTool) actionList() string {
	items := t.manager.List()
	if len(items) == 0 {
		return "No background tasks."
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt.Before(items[j].StartedAt) })

	var sb strings.Builder
	fmt.Fprintf(&sb, "%-10s %-20s %-10s %-10s %s\n", "ID", "NAME", "STATE", "ELAPSED", "COMMAND")
	for _, it := range items {
		var elapsed time.Duration
		if it.State == task.StateRunning {
			elapsed = time.Since(it.StartedAt).Round(time.Second)
		} else {
			elapsed = it.EndedAt.Sub(it.StartedAt).Round(time.Second)
		}
		cmdPreview := it.Command
		if len(cmdPreview) > 60 {
			cmdPreview = cmdPreview[:57] + "..."
		}
		fmt.Fprintf(&sb, "%-10s %-20s %-10s %-10s %s\n", it.ID, truncName(it.Name, 20), it.State, elapsed, cmdPreview)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func formatInfo(info task.Info) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "task id=%s name=%s state=%s exit_code=%d\n",
		info.ID, info.Name, info.State, info.ExitCode)
	if info.State == task.StateRunning {
		fmt.Fprintf(&sb, "elapsed=%s (still running; call again or `task peek %s` for progress)\n",
			time.Since(info.StartedAt).Round(time.Second), info.ID)
	} else {
		fmt.Fprintf(&sb, "duration=%s ended_at=%s\n",
			info.EndedAt.Sub(info.StartedAt).Round(time.Second), info.EndedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(&sb, "stdout file: %s", info.StdoutFile)
	return sb.String()
}

func truncName(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

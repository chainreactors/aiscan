package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/agent/truncate"
)

type LoopTool struct {
	scheduler *LoopScheduler
}

func NewLoopTool(scheduler *LoopScheduler) *LoopTool {
	return &LoopTool{scheduler: scheduler}
}

func (t *LoopTool) Name() string { return "loop" }

func (t *LoopTool) Description() string {
	return "Manage recurring scheduled tasks. Actions: create (schedule a periodic task), list (show active loops), delete (remove a loop by name). Loop prompts fire at fixed intervals and are processed as system messages."
}

type loopArgs struct {
	Action    string `json:"action"              jsonschema:"description=Action to perform,enum=create,enum=list,enum=delete"`
	Name      string `json:"name,omitempty"       jsonschema:"description=Loop name (required for create/delete)"`
	Interval  string `json:"interval,omitempty"   jsonschema:"description=Fire interval e.g. 30s 5m 1h (required for create)"`
	Prompt    string `json:"prompt,omitempty"     jsonschema:"description=Prompt to execute on each fire (required for create)"`
	Immediate bool   `json:"immediate,omitempty"  jsonschema:"description=Fire once immediately on creation"`
}

func (t *LoopTool) Definition() ToolDefinition {
	return commands.ToolDef("loop", t.Description(), loopArgs{})
}

func (t *LoopTool) Execute(ctx context.Context, arguments string) (commands.ToolResult, error) {
	args, err := commands.ParseArgs[loopArgs](arguments)
	if err != nil {
		return commands.ToolResult{}, err
	}

	switch args.Action {
	case "create":
		return t.create(ctx, args)
	case "list":
		return t.list()
	case "delete":
		return t.delete(args)
	default:
		return commands.ErrorResult(fmt.Sprintf("unknown action: %s", args.Action)), nil
	}
}

func (t *LoopTool) create(ctx context.Context, args loopArgs) (commands.ToolResult, error) {
	if strings.TrimSpace(args.Name) == "" {
		return commands.ToolResult{}, fmt.Errorf("name is required for create")
	}
	if strings.TrimSpace(args.Interval) == "" {
		return commands.ToolResult{}, fmt.Errorf("interval is required for create")
	}
	if strings.TrimSpace(args.Prompt) == "" {
		return commands.ToolResult{}, fmt.Errorf("prompt is required for create")
	}

	interval, err := time.ParseDuration(args.Interval)
	if err != nil {
		return commands.ToolResult{}, fmt.Errorf("invalid interval %q: %w", args.Interval, err)
	}

	entry := LoopEntry{
		Name:      args.Name,
		Interval:  interval,
		Prompt:    args.Prompt,
		Mode:      ModeInbox,
		Immediate: args.Immediate,
	}
	if err := t.scheduler.Add(ctx, entry); err != nil {
		return commands.ToolResult{}, err
	}

	msg := fmt.Sprintf("Loop %q created: fires every %s", args.Name, interval)
	if args.Immediate {
		msg += " (first fire: now)"
	}
	return commands.TextResult(msg), nil
}

func (t *LoopTool) list() (commands.ToolResult, error) {
	loops := t.scheduler.List()
	if len(loops) == 0 {
		return commands.TextResult("No active loops."), nil
	}
	var sb strings.Builder
	sb.WriteString("Active loops:\n")
	for _, l := range loops {
		sb.WriteString(fmt.Sprintf("  - %s: interval=%s fires=%d", l.Name, l.Interval, l.FireCount))
		if !l.LastFired.IsZero() {
			sb.WriteString(fmt.Sprintf(" last=%s", time.Since(l.LastFired).Round(time.Second)))
		}
		sb.WriteString(fmt.Sprintf(" prompt=%q\n", truncate.Clip(l.Prompt, 60)))
	}
	return commands.TextResult(sb.String()), nil
}

func (t *LoopTool) delete(args loopArgs) (commands.ToolResult, error) {
	if strings.TrimSpace(args.Name) == "" {
		return commands.ToolResult{}, fmt.Errorf("name is required for delete")
	}
	if err := t.scheduler.Remove(args.Name); err != nil {
		return commands.ToolResult{}, err
	}
	return commands.TextResult(fmt.Sprintf("Loop %q deleted.", args.Name)), nil
}


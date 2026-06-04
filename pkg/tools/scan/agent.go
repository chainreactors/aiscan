package scan

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/command"
)

func verifySystemPrompt() string {
	return "You are aiscan's active verification agent. Probe the target to confirm or deny the finding. " +
		"When you have reached your conclusion, call the checkpoint tool with target, status, title, and content. " +
		"Use labels for severity tags only (e.g. high, critical). " +
		"Do not output raw JSON directly."
}

func reportSystemPrompt() string {
	return "You are aiscan's scan AI skill agent. Analyze the provided scan finding using your knowledge. Do not call any tools. Return only the requested JSON output."
}

func verifyBeforeToolCall(_ context.Context, call agent.BeforeToolCallContext) (*agent.BeforeToolCallResult, error) {
	if call.ToolCall.Function.Name != "bash" {
		return nil, nil
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(call.ToolCall.Function.Arguments), &args); err != nil {
		return nil, err
	}
	if !verifyBlocksCommand(args.Command) {
		return nil, nil
	}
	return &agent.BeforeToolCallResult{
		Block:  true,
		Reason: "scan verification may use search (tavily/fetch), but scanner pseudo-commands are blocked to avoid recursive or active scanning",
	}, nil
}

func verifyBlocksCommand(commandLine string) bool {
	tokens, err := command.SplitCommandLine(commandLine)
	if err != nil {
		tokens = strings.Fields(commandLine)
	}
	if len(tokens) == 0 {
		return false
	}
	if isVerifyBlockedCommand(tokens[0]) {
		return true
	}
	return strings.EqualFold(tokens[0], "aiscan") && len(tokens) > 1 && isVerifyBlockedCommand(tokens[1])
}

func isVerifyBlockedCommand(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "scan", "passive", "gogo", "spray", "zombie", "neutron":
		return true
	default:
		return false
	}
}

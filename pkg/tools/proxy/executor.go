package proxy

import (
	"context"

	"github.com/chainreactors/aiscan/pkg/command"
)

func passthroughExecutor(reg *command.CommandRegistry) CommandExecutor {
	return func(ctx context.Context, tokens []string) (string, error) {
		if bt, ok := reg.GetTool("bash"); ok {
			if bash, ok := bt.(*command.BashTool); ok {
				res, err := bash.ExecuteTokens(ctx, tokens)
				if err != nil {
					return "", err
				}
				return res.Text(), nil
			}
		}
		return reg.ExecuteArgs(ctx, tokens)
	}
}

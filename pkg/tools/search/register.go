package search

import (
	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/resources"
)

func init() {
	command.RegisterFactory(command.Factory{
		Group: "tools",
		Build: func(deps *command.Deps, reg *command.CommandRegistry) {
			var res *resources.Set
			if deps.Resources != nil {
				res, _ = deps.Resources.(*resources.Set)
			}
			var p provider.Provider
			if deps.Provider != nil {
				p, _ = deps.Provider.(provider.Provider)
			}

			if p != nil {
				reg.RegisterTool(NewWebSearchTool(p))
			}
			reg.RegisterTool(NewFetchTool())

			cmd := New(Opts{Resources: res})
			reg.Register(cmd, "tools")
			if res != nil {
				reg.Register(NewCyberhubCommand(res), "tools")
			}
		},
	})
}

package search

import (
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
			cmd := New(Opts{
				TavilyKeys:   deps.TavilyKeys,
				ScannerProxy: deps.ScannerProxy,
				Resources:    res,
			})
			reg.Register(cmd, "tools")
		},
	})
}

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
			searchProxy := deps.WebSearchProxy
			if searchProxy == "" {
				searchProxy = deps.ScannerProxy
			}
			cmd := New(Opts{
				TavilyKeys:   deps.TavilyKeys,
				ScannerProxy: searchProxy,
				Resources:    res,
			})
			reg.Register(cmd, "tools")
		},
	})
}

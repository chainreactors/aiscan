package search

import (
	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	"github.com/chainreactors/sdk/pkg/association"
)

func init() {
	command.RegisterFactory(command.Factory{
		Group: "tools",
		Build: func(deps *command.Deps, reg *command.CommandRegistry) {
			var p provider.Provider
			if deps.Provider != nil {
				p, _ = deps.Provider.(provider.Provider)
			}

			tavily := NewTavilySearch(deps.TavilyKeys)
			if deps.ScannerProxy != "" {
				tavily.SetProxy(deps.ScannerProxy)
			}

			if p != nil {
				reg.RegisterTool(NewWebSearchTool(p, tavily))
			}
			reg.RegisterTool(NewFetchTool())

			var idx *association.Index
			if es, ok := deps.EngineSet.(*engine.Set); ok && es != nil {
				idx = es.Index
			}

			cmd := New(Opts{
				TavilyKeys:   deps.TavilyKeys,
				ScannerProxy: deps.ScannerProxy,
				Index:        idx,
			})
			reg.Register(cmd, "tools")
			if cmd.cyberhub != nil {
				reg.Register(cmd.cyberhub, "tools")
			}
		},
	})
}

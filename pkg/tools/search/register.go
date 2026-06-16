package search

import (
	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
	"github.com/chainreactors/sdk/pkg/association"
)

func init() {
	commands.RegisterFactory(commands.Factory{
		Group: "tools",
		Build: func(deps *commands.Deps, reg *commands.CommandRegistry) {
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
			if idx != nil {
				reg.Register(NewCyberhubSearch(idx), "tools")
			}
		},
	})
}

//go:build full

package passive

import (
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
)

func init() {
	commands.RegisterFactory(commands.Factory{
		Group: "scanner",
		Build: func(deps *commands.Deps, reg *commands.CommandRegistry) {
			es, _ := deps.EngineSet.(*engine.Set)
			if es == nil || es.Uncover == nil {
				return
			}
			logger, _ := deps.Logger.(telemetry.Logger)
			if logger == nil {
				logger = telemetry.NopLogger()
			}
			reg.Register(New(es.Uncover).WithLogger(logger), "scanner")
		},
	})
}

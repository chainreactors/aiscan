//go:build full

package passive

import (
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tools/scan/engine"
)

func init() {
	command.RegisterFactory(command.Factory{
		Group: "scanner",
		Build: func(deps *command.Deps, reg *command.CommandRegistry) {
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

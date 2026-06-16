//go:build full

package katana

import (
	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

func init() {
	commands.RegisterFactory(commands.Factory{
		Group: "scanner",
		Build: func(deps *commands.Deps, reg *commands.CommandRegistry) {
			logger, _ := deps.Logger.(telemetry.Logger)
			if logger == nil {
				logger = telemetry.NopLogger()
			}
			reg.Register(New().WithLogger(logger).WithProxy(deps.ScannerProxy), "scanner")
		},
	})
}

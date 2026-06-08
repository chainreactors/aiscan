//go:build full

package katana

import "github.com/chainreactors/aiscan/pkg/command"

func init() {
	command.RegisterFactory(command.Factory{
		Group: "scanner",
		Build: func(_ *command.Deps, reg *command.CommandRegistry) {
			reg.Register(New(), "scanner")
		},
	})
}

//go:build full

package katana

import "github.com/chainreactors/aiscan/pkg/commands"

func init() {
	commands.RegisterFactory(commands.Factory{
		Group: "scanner",
		Build: func(_ *commands.Deps, reg *commands.CommandRegistry) {
			reg.Register(New(), "scanner")
		},
	})
}

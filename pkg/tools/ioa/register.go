package ioa

import (
	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/ioa/protocols"

	_ "github.com/chainreactors/ioa/protocols/checkpoint"
	_ "github.com/chainreactors/ioa/protocols/swarm"
)

func init() {
	command.RegisterFactory(command.Factory{
		Group: "ioa",
		Build: func(deps *command.Deps, reg *command.CommandRegistry) {
			client, _ := deps.IOAClient.(protocols.ClientAPI)
			if client == nil {
				return
			}
			for _, cmd := range NewCommands(client, deps.NodeName, deps.NodeMeta) {
				reg.Register(cmd, "ioa")
			}
		},
	})
}

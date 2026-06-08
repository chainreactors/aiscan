package ioa

import (
	"github.com/chainreactors/aiscan/pkg/command"
	ioaclient "github.com/chainreactors/ioa/client"

	_ "github.com/chainreactors/ioa/protocols/checkpoint"
)

func init() {
	command.RegisterFactory(command.Factory{
		Group: "ioa",
		Build: func(deps *command.Deps, reg *command.CommandRegistry) {
			client, _ := deps.IOAClient.(ioaclient.API)
			if client == nil {
				return
			}
			for _, cmd := range NewCommands(client, deps.NodeName, deps.NodeMeta) {
				reg.Register(cmd, "ioa")
			}
		},
	})
}

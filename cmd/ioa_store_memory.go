package cmd

import (
	"github.com/chainreactors/aiscan/pkg/telemetry"
	ioaserver "github.com/chainreactors/ioa/server"
)

func openIOAStore(option *Option, logger telemetry.Logger) (ioaserver.Store, func() error, error) {
	return nil, nil, nil
}

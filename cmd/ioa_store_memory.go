//go:build !sqlite

package cmd

import (
	"github.com/chainreactors/aiscan/pkg/telemetry"
	ioaserver "github.com/chainreactors/ioa/server"
)

func openIOAStore(option *Option, logger telemetry.Logger) (ioaserver.Store, func() error, error) {
	logger.Warnf("ioa_server store=memory: --ioa-db=%q ignored, all state will be lost on restart (rebuild with -tags sqlite to enable persistence)", option.IOADB)
	return nil, nil, nil
}

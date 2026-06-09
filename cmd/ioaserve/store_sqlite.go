package ioaserve

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	ioaserver "github.com/chainreactors/ioa/server"
)

func openStore(dbPath string, logger telemetry.Logger) (ioaserver.Store, func() error, error) {
	if dbPath == "" || dbPath == ":memory:" {
		store := ioaserver.NewMemoryStore()
		logger.Importantf("ioa_server store=memory")
		return store, store.Close, nil
	}
	if !filepath.IsAbs(dbPath) {
		if wd, err := os.Getwd(); err == nil {
			dbPath = filepath.Join(wd, dbPath)
		}
	}
	store, err := ioaserver.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open ioa sqlite store at %s: %w", dbPath, err)
	}
	logger.Importantf("ioa_server store=sqlite path=%s", dbPath)
	return store, store.Close, nil
}

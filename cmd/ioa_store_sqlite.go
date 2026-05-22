//go:build sqlite

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chainreactors/aiscan/pkg/telemetry"
	ioaserver "github.com/chainreactors/ioa/server"
	ioasqlite "github.com/chainreactors/ioa/sqlite"
)

func openIOAStore(option *Option, logger telemetry.Logger) (ioaserver.Store, func() error, error) {
	dbPath := option.IOADB
	if dbPath == "" {
		dbPath = "./ioa.db"
	}
	if !filepath.IsAbs(dbPath) {
		if wd, err := os.Getwd(); err == nil {
			dbPath = filepath.Join(wd, dbPath)
		}
	}
	store, err := ioasqlite.NewSQLiteStore(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open ioa sqlite store at %s: %w", dbPath, err)
	}
	logger.Importantf("ioa_server store=sqlite path=%s", dbPath)
	return store, store.Close, nil
}

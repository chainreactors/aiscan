// Package arsenal registers the package manager in an extension scope.
package arsenal

import (
	crtm "github.com/chainreactors/crtm/pkg"
	"github.com/chainreactors/cyber/core/extension"
	tool "github.com/chainreactors/cyber/tools/arsenal"
)

type Extension struct{ manager *crtm.Manager }

// New opens catalog metadata only. Load prepares executables and publishes the
// manager and command together, so hosts never assemble a partial installation.
func New(directory string, options crtm.ManagerOption) (*Extension, error) {
	manager, err := tool.NewManager(directory, options)
	if err != nil {
		return nil, err
	}
	return &Extension{manager: manager}, nil
}

// BinPath is available before Load so the host can configure child processes.
func (e *Extension) BinPath() string { return e.manager.BinPath() }

func (e *Extension) Load(scope *extension.Scope) error {
	if err := e.manager.Prepare(scope.Init()); err != nil {
		return err
	}
	if err := extension.Provide[*crtm.Manager](scope, e.manager); err != nil {
		return err
	}
	return extension.Add(scope, tool.NewCommand(e.manager))
}

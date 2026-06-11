package neutron

import (
	"context"
	"fmt"

	"github.com/chainreactors/sdk/pkg/provider"
	"github.com/chainreactors/sdk/pkg/types"
)

// AddPocs adds templates to the current engine and compiles them.
func (e *Engine) AddPocs(pocs []*types.Template) error {
	if len(pocs) == 0 {
		return fmt.Errorf("pocs cannot be empty")
	}
	if e.config == nil {
		e.config = NewConfig()
	}

	if e.templates == nil && e.config.Templates.Len() > 0 {
		e.templates = e.compileTemplates(e.config.Templates.Templates())
	}

	compiled := e.compileTemplates(pocs)
	if len(compiled) == 0 {
		return fmt.Errorf("no templates compiled")
	}

	e.templates = append(e.templates, compiled...)
	e.config.Templates = e.config.Templates.Merge(pocs)
	return nil
}

// AddPocsFile loads templates from a yaml file or directory and adds them.
func (e *Engine) AddPocsFile(path string) error {
	p := provider.NewFileProvider("", path)
	pocs, err := p.POCs(context.Background())
	if err != nil {
		return err
	}
	return e.AddPocs(pocs)
}

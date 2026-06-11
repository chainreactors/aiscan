package fingers

import (
	"context"
	"fmt"

	"github.com/chainreactors/sdk/pkg/provider"
	"github.com/chainreactors/sdk/pkg/types"
)

// AddFingers adds fingerprints to the current engine and rebuilds it.
func (e *Engine) AddFingers(fingers types.Fingers) error {
	if len(fingers) == 0 {
		return fmt.Errorf("fingers cannot be empty")
	}
	if e.config == nil {
		e.config = NewConfig()
	}

	if e.engine == nil {
		engine, err := buildEngineFromFingers(e.config.FullFingers.Fingers(), e.config.FullFingers.Aliases(), e.config.MatchDetail)
		if err != nil {
			return err
		}
		e.aliases = e.config.FullFingers.Aliases()
		e.engine = engine
	}

	fingersEngineImpl, err := e.GetFingersEngine()
	if err != nil {
		return err
	}
	if fingersEngineImpl == nil {
		return fmt.Errorf("fingers engine is not initialized")
	}

	if err := fingersEngineImpl.Append(fingers); err != nil {
		return err
	}
	e.config.FullFingers = e.config.FullFingers.Merge(fingers, nil)
	return nil
}

// AddFingersFile loads fingerprints from a yaml file or directory and adds them.
func (e *Engine) AddFingersFile(path string) error {
	p := provider.NewFileProvider(path, "")
	fingers, _, err := p.Fingers(context.Background())
	if err != nil {
		return err
	}
	return e.AddFingers(fingers)
}

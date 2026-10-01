// Package recap installs the task recap worker without user configuration.
package recap

import (
	"context"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/agent/recap"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/eventbus"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/telemetry"
)

type Extension struct {
	cancel context.CancelFunc
	sub    *eventbus.Subscription[*aop.Event]
	done   chan struct{}
}

func New() *Extension { return &Extension{} }

func (e *Extension) Load(scope *extension.Scope) error {
	stream, err := extension.Use[*events.Stream](scope)
	if err != nil {
		return err
	}
	providers, err := extension.Use[*provider.State](scope)
	if err != nil {
		return err
	}
	logger, err := extension.Use[telemetry.Logger](scope)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(scope.Lifetime())
	e.cancel, e.done = cancel, make(chan struct{})
	worker := recap.New(ctx, providers, stream, logger)
	e.sub = stream.Observe(worker.Observe)
	go func() {
		defer close(e.done)
		worker.Run()
	}()
	return nil
}

func (e *Extension) Close(ctx context.Context) error {
	if e.cancel == nil {
		return nil
	}
	e.cancel()
	if err := e.sub.Close(ctx); err != nil {
		return err
	}
	select {
	case <-e.done:
		return nil
	default:
	}
	select {
	case <-e.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var _ extension.Extension = (*Extension)(nil)

// Package apptest exposes shared extension fixtures to packages outside pkg.
package apptest

import (
	"context"
	"testing"

	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/telemetry"
	internal "github.com/chainreactors/cyber/pkg/internal/testutil/apptest"
)

type Fixture = internal.Fixture

func NewFixture(t testing.TB, logger telemetry.Logger, stream *events.Stream) *Fixture {
	t.Helper()
	return internal.NewFixture(t, logger, stream)
}

func Entries(t testing.TB, fixture *Fixture) []extension.Extension {
	t.Helper()
	return internal.Entries(t, fixture)
}

func Load(t testing.TB, ctx context.Context, fixture *Fixture) *extension.Set {
	t.Helper()
	return internal.Load(t, ctx, fixture)
}

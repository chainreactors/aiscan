// Package hosttest exposes shared test hosts to packages outside pkg.
package hosttest

import (
	"context"
	"testing"

	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/hooks"
	coretool "github.com/chainreactors/cyber/core/tool"
	internal "github.com/chainreactors/cyber/pkg/internal/testutil/hosttest"
)

func Set(t testing.TB, values ...extension.Extension) *extension.Set {
	t.Helper()
	return internal.Set(t, values...)
}

func Load(t testing.TB, ctx context.Context, values ...extension.Extension) *extension.Set {
	t.Helper()
	return internal.Load(t, ctx, values...)
}

func Commands(t testing.TB, values ...coretool.Command) *coretool.CommandRegistry {
	t.Helper()
	return internal.Commands(t, values...)
}

func Tools(t testing.TB, values ...coretool.Tool) coretool.Executor {
	t.Helper()
	return internal.Tools(t, values...)
}

func ToolsWithHooks(t testing.TB, registry *hooks.Registry, values ...coretool.Tool) coretool.Executor {
	t.Helper()
	return internal.ToolsWithHooks(t, registry, values...)
}

func Capabilities() extension.Extension {
	return internal.Capabilities()
}

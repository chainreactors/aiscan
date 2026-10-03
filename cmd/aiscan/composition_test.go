package main

import (
	"context"
	"testing"

	"github.com/chainreactors/cyber/agent"
	agentsession "github.com/chainreactors/cyber/agent/session"
	aop "github.com/chainreactors/cyber/aop"
	filepb "github.com/chainreactors/cyber/aop/file"
	operationpb "github.com/chainreactors/cyber/aop/operation"
	"github.com/chainreactors/cyber/core/operation"
	"github.com/chainreactors/cyber/core/telemetry"
	observeext "github.com/chainreactors/cyber/exts/observe"
	cfg "github.com/chainreactors/cyber/pkg/config"
)

// Every capability a profile publishes is resolved by type at load, not by the
// compiler. A shape that nobody loads in a test is a shape whose wiring nobody
// checks -- so each one CI cares about is loaded and closed here.
func TestEveryProfileShapeLoadsAndCloses(t *testing.T) {
	for _, test := range []struct {
		name  string
		build func(t *testing.T) config
	}{
		{
			name:  "application only",
			build: func(*testing.T) config { return minimalConfig(nil) },
		},
		{
			name:  "with a session runtime",
			build: func(*testing.T) config { return minimalConfig(&agentsession.Config{}) },
		},
		{
			name: "with the scan engines",
			build: func(*testing.T) config {
				config := minimalConfig(&agentsession.Config{})
				config.Base.SkipEngines = false
				return config
			},
		},
		{
			name: "with event output",
			build: func(t *testing.T) config {
				config := minimalConfig(&agentsession.Config{})
				config.Output = t.TempDir() + "/events.jsonl"
				return config
			},
		},
		{
			name: "with observation",
			build: func(*testing.T) config {
				config := minimalConfig(&agentsession.Config{})
				config.Observe = []observeext.Kind{observeext.Tools}
				return config
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile, err := buildAIScanProfile(test.build(t))
			if err != nil {
				t.Fatalf("construct: %v", err)
			}
			if err := profile.Load(t.Context()); err != nil {
				t.Fatalf("load: %v", err)
			}
			if err := profile.Close(context.Background()); err != nil {
				t.Fatalf("close: %v", err)
			}
		})
	}
}

// The composition root reads what the graph assembled, rather than holding the
// parts it threaded in.
func TestLoadedProfilePublishesItsApplicationAndRuntime(t *testing.T) {
	profile, err := buildAIScanProfile(minimalConfig(&agentsession.Config{Loop: agent.StandardLoop{}}))
	if err != nil {
		t.Fatal(err)
	}
	if application, err := profile.Providers(); err == nil && application != nil {
		t.Fatal("the application existed before the graph loaded")
	}
	if err := profile.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = profile.Close(context.Background()) })

	if _, err := profile.Providers(); err != nil {
		t.Fatal(err)
	}
	runtime, err := profile.Runtime()
	if err != nil {
		t.Fatal(err)
	}
	if runtime == nil {
		t.Fatal("the session runtime was not published")
	}
	// The runtime borrowed these; nothing threaded them in.
	if runtime.CommandRegistry() == nil || runtime.Tools() == nil || runtime.Skills() == nil {
		t.Error("the session runtime is missing capabilities its extensions published")
	}
}

var _ = cfg.Option{}
var _ = telemetry.NopLogger

func TestSessionProfileObservesToolAndFileActivityByDefault(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "explicit selection"}[explicit], func(t *testing.T) {
			config := minimalConfig(&agentsession.Config{})
			if explicit {
				config.Observe = []observeext.Kind{observeext.Tools}
			}
			profile, err := buildAIScanProfile(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := profile.Load(t.Context()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = profile.Close(context.Background()) })
			runtime, err := profile.Runtime()
			if err != nil {
				t.Fatal(err)
			}
			stream, err := profile.Events()
			if err != nil {
				t.Fatal(err)
			}
			var events []*aop.Event
			sub := stream.Observe(func(event *aop.Event) { events = append(events, event) })
			defer sub.Cancel()
			ctx := operation.ContextWithInvocation(t.Context(), operation.Invocation{SessionID: "observed-session", TurnID: "observed-turn", CallID: "observed-call", Emitter: "node"})
			if _, err := runtime.Tools().ExecuteTool(ctx, "read", `{"path":"profile.go"}`); err != nil {
				t.Fatal(err)
			}
			var started, completed, accesses int
			for _, event := range events {
				if event.SessionId != "observed-session" || event.TurnId != "observed-turn" || event.Emitter != "node" {
					t.Fatalf("lost invocation: %v", event)
				}
				payload := event.GetExtension()
				if payload.MessageIs(new(operationpb.Started)) {
					started++
				}
				if payload.MessageIs(new(operationpb.Completed)) {
					completed++
				}
				if payload.MessageIs(new(filepb.Access)) {
					accesses++
				}
			}
			if started != 1 || completed != 1 || (!explicit && accesses != 1) || (explicit && accesses != 0) {
				t.Fatalf("observations: started=%d completed=%d files=%d", started, completed, accesses)
			}
		})
	}
}

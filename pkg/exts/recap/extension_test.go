package recap_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/agent/session"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/events"
	eventjsonl "github.com/chainreactors/cyber/core/events/jsonl"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/telemetry"
	"github.com/chainreactors/cyber/core/types"
	recapext "github.com/chainreactors/cyber/pkg/exts/recap"
	telemetryext "github.com/chainreactors/cyber/pkg/exts/telemetry"
)

type model struct {
	call func(context.Context) (*provider.ChatCompletionResponse, error)
}

func (*model) Name() string { return "test" }
func (p *model) ChatCompletion(ctx context.Context, _ *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	return p.call(ctx)
}

func setup(t *testing.T, p *model, extra ...extension.Extension) (*extension.Set, *events.Stream) {
	t.Helper()
	stream := events.New()
	providers := &provider.State{}
	providers.Set(p, provider.ProviderConfig{Model: "default"})
	values := []extension.Extension{extension.Provided[*events.Stream](stream), extension.Provided[*provider.State](providers), extension.Provided[telemetry.Logger](telemetry.NopLogger())}
	values = append(values, extra...)
	values = append(values, recapext.New())
	set, err := extension.New(values...)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close(context.Background()) })
	return set, stream
}

func publishTask(t *testing.T, stream *events.Stream) {
	t.Helper()
	start := &aop.Event{SessionId: "s", Payload: &aop.Event_SessionStarted{SessionStarted: &aop.SessionStarted{Model: "default"}}}
	if err := types.SetSessionHistory(start, &types.SessionHistory{Mode: types.SessionHistory_MODE_INHERIT}); err != nil {
		t.Fatal(err)
	}
	stream.Publish(start)
	stream.Publish(&aop.Event{SessionId: "s", TurnId: "t", Payload: &aop.Event_TurnStarted{TurnStarted: &aop.TurnStarted{}}})
	msg := provider.TextMessage("assistant", "Task answer")
	msg.Id = "m-1"
	stream.Publish(&aop.Event{SessionId: "s", TurnId: "t", Payload: &aop.Event_Message{Message: msg}})
	stream.Publish(&aop.Event{SessionId: "s", TurnId: "t", Payload: &aop.Event_TurnEnded{TurnEnded: &aop.TurnEnded{StopReason: "completed"}}})
}

func TestRecapPersistsButDoesNotEnterResumedConversation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := telemetryext.New(telemetryext.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	set, stream := setup(t, &model{call: func(context.Context) (*provider.ChatCompletionResponse, error) {
		return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: provider.TextMessage("assistant", "Recap annotation")}}}, nil
	}}, writer)
	received := make(chan struct{}, 1)
	sub := stream.Observe(func(e *aop.Event) {
		if value := e.GetExtension(); value != nil && value.MessageIs(new(types.Recap)) {
			received <- struct{}{}
		}
	})
	defer sub.Cancel()
	publishTask(t, stream)
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("no recap")
	}
	if err := set.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	log, err := eventjsonl.ReadJSONL(path)
	if err != nil || len(log) != 5 || log[4].GetExtension() == nil {
		t.Fatalf("events=%d err=%v", len(log), err)
	}
	history, err := session.ReadHistory(path)
	if err != nil || len(history.Messages) != 1 || provider.MessageText(history.Messages[0]) != "Task answer" {
		t.Fatalf("recap contaminated history: history=%v err=%v", history, err)
	}
}

func TestCloseCancelsWorkerAndSupportsDrainRetry(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	set, stream := setup(t, &model{call: func(ctx context.Context) (*provider.ChatCompletionResponse, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release
		return nil, ctx.Err()
	}})
	// Release even if an assertion fails, before setup's draining cleanup.
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	publishTask(t, stream)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider was not called")
	}
	deadline, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := set.Close(deadline); !errors.Is(err, extension.ErrCloseIncomplete) {
		t.Fatalf("close did not retain dependencies for drain retry: %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("provider was not canceled")
	}
	close(release)
	if err := set.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

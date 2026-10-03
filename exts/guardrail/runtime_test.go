package guardrail

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/aop"
	operationpb "github.com/chainreactors/cyber/aop/operation"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	"google.golang.org/protobuf/encoding/protojson"
)

type testObserver func(*aop.Event)

func (f testObserver) ObserveEvent(event *aop.Event) { f(event) }

func fixture(t *testing.T, timeout time.Duration, modes ...Mode) (*Runtime, *hooks.Registry, chan *Review) {
	t.Helper()
	stream := events.New()
	reviews := make(chan *Review, 64)
	stream.Observe(testObserver(func(event *aop.Event) {
		var review Review
		if extension := event.GetExtension(); extension != nil && extension.MessageIs(&review) && extension.UnmarshalTo(&review) == nil {
			reviews <- &review
		}
		// A payload is required by the Web event broker and durable transcript.
		if event.GetExtension() == nil {
			t.Error("guardrail event is missing its durable extension payload")
		}
		encoded, err := protojson.Marshal(event)
		if err != nil {
			t.Errorf("serialize guardrail event: %v", err)
			return
		}
		var restored aop.Event
		if err := protojson.Unmarshal(encoded, &restored); err != nil || restored.GetExtension() == nil {
			t.Errorf("restore guardrail event: %v", err)
		}
	}))
	mode := ModeSafe
	if len(modes) > 0 {
		mode = modes[0]
	}
	runtime := newRuntime(t.Context(), stream, timeout, mode, nil, nil)
	registry := hooks.New()
	toolhooks.Before.On(registry, "guardrail", runtime.Admit)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return runtime, registry, reviews
}

func register(t *testing.T, r *Runtime, action Action) {
	t.Helper()
	r.check = func(context.Context, toolhooks.CallEvent) (*Decision, error) {
		return &Decision{Action: action, Reason: "policy"}, nil
	}
}

func callContext() context.Context {
	return operation.ContextWithInvocation(context.Background(), operation.Invocation{SessionID: "session", CallID: "same-call"})
}
func nextReview(t *testing.T, c <-chan *Review) *Review {
	t.Helper()
	select {
	case r := <-c:
		return r
	case <-time.After(time.Second):
		t.Fatal("review event missing")
		return nil
	}
}

func TestAdmissionFailuresNeverExecute(t *testing.T) {
	for _, test := range []struct {
		name  string
		check checkFunc
	}{
		{"block", func(context.Context, toolhooks.CallEvent) (*Decision, error) {
			return &Decision{Action: Action_ACTION_BLOCK}, nil
		}},
		{"error", func(context.Context, toolhooks.CallEvent) (*Decision, error) {
			return nil, errors.New("private secret")
		}},
		{"panic", func(context.Context, toolhooks.CallEvent) (*Decision, error) { panic("private secret") }},
		{"nil", func(context.Context, toolhooks.CallEvent) (*Decision, error) { return nil, nil }},
		{"invalid", func(context.Context, toolhooks.CallEvent) (*Decision, error) { return &Decision{Action: 99}, nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, registry, _ := fixture(t, time.Second, ModeAuto)
			r.check, r.confirm = test.check, nil
			_, err := toolhooks.Execute(callContext(), registry, "shell", "{}", func(context.Context, string) (*aop.ToolResult, error) {
				t.Fatal("denied invocation executed")
				return nil, nil
			})
			if !errors.Is(err, operation.ErrDenied) {
				t.Fatalf("got %v", err)
			}
			if strings.Contains(err.Error(), "private secret") {
				t.Fatal("check error leaked")
			}
		})
	}
}

func TestDisabledAndClosedRuntime(t *testing.T) {
	r, registry, _ := fixture(t, time.Second)
	calls := 0
	run := func(context.Context, string) (*aop.ToolResult, error) { calls++; return &aop.ToolResult{}, nil }
	if _, err := toolhooks.Execute(callContext(), registry, "read", "{}", run); err != nil {
		t.Fatal(err)
	}
	register(t, r, Action_ACTION_RECORD)
	if _, err := toolhooks.Execute(callContext(), registry, "read", "{}", run); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := toolhooks.Execute(callContext(), registry, "read", "{}", run); !errors.Is(err, operation.ErrDenied) {
		t.Fatalf("closed hook allowed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("executed %d times", calls)
	}
}

func TestPolicySnapshotIsolation(t *testing.T) {
	r, _, _ := fixture(t, time.Second, ModeAuto)
	r.check = func(_ context.Context, ev toolhooks.CallEvent) (*Decision, error) {
		ev.Call.Name = "mutated"
		ev.Operation.OperationId = "mutated"
		return &Decision{Action: Action_ACTION_BLOCK}, nil
	}
	ev := toolhooks.CallEvent{Call: &aop.ToolCall{Name: "shell"}, Operation: &operationpb.Ref{OperationId: "op"}}
	result, err := r.Admit(callContext(), ev)
	if err != nil || !errors.Is(result.Deny, operation.ErrDenied) {
		t.Fatalf("result=%v err=%v", result, err)
	}
	if ev.Call.Name != "shell" || ev.Operation.OperationId != "op" {
		t.Fatal("original call mutated")
	}
}

func TestApprovalResumesExactlyOneInvocation(t *testing.T) {
	r, registry, reviews := fixture(t, time.Second)
	var judged, executed atomic.Int32
	r.check, r.confirm = func(context.Context, toolhooks.CallEvent) (*Decision, error) {
		judged.Add(1)
		return &Decision{Action: Action_ACTION_REVIEW}, nil
	}, nil
	run := func(_ context.Context, args string) (*aop.ToolResult, error) {
		if args != `{"token":"dummy","command":"read"}` {
			t.Error("executable arguments changed")
		}
		executed.Add(1)
		return &aop.ToolResult{}, nil
	}
	start := func() chan error {
		done := make(chan error, 1)
		go func() {
			_, err := toolhooks.Execute(callContext(), registry, "shell", `{"token":"dummy","command":"read"}`, run)
			done <- err
		}()
		return done
	}
	done := start()
	review := nextReview(t, reviews)
	if executed.Load() != 0 || review.State != ReviewState_REVIEW_STATE_PENDING {
		t.Fatal("executed before approval")
	}
	if strings.Contains(string(review.Call.Arguments.Data), "dummy") {
		t.Fatal("secret exposed")
	}
	wrong := operation.ContextWithInvocation(context.Background(), operation.Invocation{SessionID: "other"})
	if r.Resolve(wrong, review.Operation.OperationId, true) == nil {
		t.Fatal("cross-session approval accepted")
	}
	snapshot := r.Pending("session")
	snapshot[0].Call.Name = "mutated"
	if r.Pending("session")[0].Call.Name != "shell" {
		t.Fatal("pending query shared state")
	}
	if err := r.Resolve(callContext(), review.Operation.OperationId, true); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if executed.Load() != 1 || judged.Load() != 1 {
		t.Fatal("approval replayed execution or check")
	}
	if r.Resolve(callContext(), review.Operation.OperationId, true) == nil {
		t.Fatal("duplicate approval accepted")
	}
	nextReview(t, reviews)
	done = start()
	second := nextReview(t, reviews)
	if second.Operation.OperationId == review.Operation.OperationId {
		t.Fatal("operation IDs reused")
	}
	if err := r.Resolve(callContext(), second.Operation.OperationId, false); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, operation.ErrDenied) {
		t.Fatal(err)
	}
	if executed.Load() != 1 || judged.Load() != 2 {
		t.Fatal("allow decision cached")
	}
}

func TestReviewCancellationExpiryAndClose(t *testing.T) {
	for _, mode := range []string{"cancel", "expire", "close"} {
		t.Run(mode, func(t *testing.T) {
			timeout := time.Second
			if mode == "expire" {
				timeout = 20 * time.Millisecond
			}
			r, registry, reviews := fixture(t, timeout)
			register(t, r, Action_ACTION_REVIEW)
			ctx, cancel := context.WithCancel(callContext())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := toolhooks.Execute(ctx, registry, "shell", "{}", func(context.Context, string) (*aop.ToolResult, error) {
					t.Error("expired/canceled call executed")
					return nil, nil
				})
				done <- err
			}()
			review := nextReview(t, reviews)
			if mode == "cancel" {
				cancel()
			}
			if mode == "close" {
				if err := r.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				if !errors.Is(err, operation.ErrDenied) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("waiter did not unblock")
			}
			if len(r.Pending("session")) != 0 || r.Resolve(callContext(), review.Operation.OperationId, true) == nil {
				t.Fatal("stale review remained runnable")
			}
		})
	}
}

func TestCloseCancelsActiveCheck(t *testing.T) {
	r, registry, _ := fixture(t, time.Second)
	started := make(chan struct{})
	r.check, r.confirm = func(ctx context.Context, _ toolhooks.CallEvent) (*Decision, error) {
		close(started)
		<-ctx.Done()
		return &Decision{Action: Action_ACTION_RECORD}, nil
	}, nil
	done := make(chan error, 1)
	go func() {
		_, err := toolhooks.Execute(callContext(), registry, "shell", "{}", func(context.Context, string) (*aop.ToolResult, error) {
			t.Error("executed after close")
			return nil, nil
		})
		done <- err
	}()
	<-started
	if err := r.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, operation.ErrDenied) {
		t.Fatal(err)
	}
}

func TestRedactionPreservesCommandAndNumbers(t *testing.T) {
	original := &aop.ToolCall{Arguments: &aop.EncodedValue{Data: []byte(`{"command":"curl -H 'Authorization: Bearer abcdef' --password 'dummy pass' https://target/","api_key":"dummykey","count":9007199254740993}`)}}
	value := string(SanitizeCall(original).Arguments.Data)
	for _, secret := range []string{"abcdef", "dummy pass", "dummykey"} {
		if strings.Contains(value, secret) {
			t.Errorf("leaked %s", secret)
		}
	}
	for _, preserved := range []string{"curl", "https://target/", "9007199254740993"} {
		if !strings.Contains(value, preserved) {
			t.Errorf("lost %s: %s", preserved, value)
		}
	}
	if !strings.Contains(string(original.Arguments.Data), "dummykey") {
		t.Fatal("original modified")
	}
}

func TestCompetingApprovalsHaveOneWinner(t *testing.T) {
	r, registry, reviews := fixture(t, time.Second)
	register(t, r, Action_ACTION_REVIEW)
	for range 20 {
		var executed atomic.Int32
		done := make(chan error, 1)
		go func() {
			_, err := toolhooks.Execute(callContext(), registry, "shell", "{}", func(context.Context, string) (*aop.ToolResult, error) { executed.Add(1); return &aop.ToolResult{}, nil })
			done <- err
		}()
		review := nextReview(t, reviews)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var winners atomic.Int32
		var approved atomic.Bool
		for _, allow := range []bool{true, false} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if r.Resolve(callContext(), review.Operation.OperationId, allow) == nil {
					winners.Add(1)
					approved.Store(allow)
				}
			}()
		}
		close(start)
		wg.Wait()
		<-done
		terminal := nextReview(t, reviews)
		if winners.Load() != 1 {
			t.Fatal("multiple or missing winning transitions")
		}
		if approved.Load() {
			if executed.Load() != 1 || terminal.State != ReviewState_REVIEW_STATE_APPROVED {
				t.Fatal("approval did not release exactly one call")
			}
		} else if executed.Load() != 0 || terminal.State != ReviewState_REVIEW_STATE_REJECTED {
			t.Fatal("rejected call executed")
		}
	}
}

func TestSynchronousObserverCanResolve(t *testing.T) {
	stream := events.New()
	r := newRuntime(t.Context(), stream, time.Second, ModeSafe, nil, nil)
	defer r.Close(t.Context())
	registry := hooks.New()
	toolhooks.Before.On(registry, "guardrail", r.Admit)
	r.check = func(context.Context, toolhooks.CallEvent) (*Decision, error) {
		return &Decision{Action: Action_ACTION_REVIEW, Reason: "policy"}, nil
	}
	stream.Observe(testObserver(func(event *aop.Event) {
		var review Review
		if extension := event.GetExtension(); extension == nil || !extension.MessageIs(&review) || extension.UnmarshalTo(&review) != nil || review.State != ReviewState_REVIEW_STATE_PENDING {
			return
		}
		if review.Decision.Reason != "policy" {
			t.Error("risk reason was not preserved")
		}
		if len(r.Pending("session")) != 1 {
			t.Error("review not published after insertion")
		}
		if err := r.Resolve(callContext(), review.Operation.OperationId, true); err != nil {
			t.Error(err)
		}
	}))
	done := make(chan error, 1)
	go func() {
		_, err := toolhooks.Execute(callContext(), registry, "shell", "{}", func(context.Context, string) (*aop.ToolResult, error) { return &aop.ToolResult{}, nil })
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("synchronous observer deadlocked")
	}
}

func TestModeChangesPreserveWaitingInvocation(t *testing.T) {
	r, registry, reviews := fixture(t, time.Second*5)
	register(t, r, Action_ACTION_REVIEW)
	var executions atomic.Int32
	run := func(context.Context, string) (*aop.ToolResult, error) {
		executions.Add(1)
		return &aop.ToolResult{}, nil
	}
	done := make(chan error, 1)
	go func() { _, err := toolhooks.Execute(callContext(), registry, "bash", "{}", run); done <- err }()
	pending := nextReview(t, reviews)
	for _, mode := range []Mode{ModeAuto, ModeSafe} {
		if err := r.SetMode(mode); err != nil {
			t.Fatal(err)
		}
		if len(r.Pending("session")) != 1 || executions.Load() != 0 {
			t.Fatal("mode switch released or canceled pending call")
		}
		select {
		case <-done:
			t.Fatal("pending invocation ended")
		default:
		}
	}
	if err := r.SetMode(ModeAuto); err != nil {
		t.Fatal(err)
	}
	if _, err := toolhooks.Execute(callContext(), registry, "bash", "{}", run); !errors.Is(err, operation.ErrDenied) {
		t.Fatalf("new auto call = %v", err)
	}
	_ = nextReview(t, reviews) // automatic denial is an audit record, not a pending review
	resolveContext := operation.ContextWithInvocation(callContext(), operation.Invocation{SessionID: "session", Emitter: "control"})
	if err := r.Resolve(resolveContext, pending.Operation.OperationId, true); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil || executions.Load() != 1 {
		t.Fatalf("approved call: %v, executions=%d", err, executions.Load())
	}
	if terminal := nextReview(t, reviews); terminal.ResolutionSource != "control" {
		t.Fatal("resolution source missing")
	}
	if err := r.SetMode("off"); err == nil {
		t.Fatal("removed off mode accepted")
	}
	if err := r.SetMode("invalid"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestRepeatedAutoInterceptionsAreScopedAndRechecked(t *testing.T) {
	r, registry, _ := fixture(t, time.Second, ModeAuto)
	var checks atomic.Int32
	r.check, r.confirm = func(context.Context, toolhooks.CallEvent) (*Decision, error) {
		checks.Add(1)
		return &Decision{Action: Action_ACTION_BLOCK, Reason: "policy"}, nil
	}, nil
	run := func(context.Context, string) (*aop.ToolResult, error) { t.Fatal("denied call ran"); return nil, nil }
	for _, tc := range []struct{ session, turn, args, count string }{
		{"a", "one", `{"cmd":"echo x","x":1}`, "1 identical"},
		{"a", "one", `{ "x":1, "cmd":"echo x" }`, "2 identical"},
		{"a", "two", `{"cmd":"echo x","x":1}`, "1 identical"},
		{"b", "one", `{"cmd":"echo x","x":1}`, "1 identical"},
	} {
		ctx := operation.ContextWithInvocation(t.Context(), operation.Invocation{SessionID: tc.session, TurnID: tc.turn, CallID: "same"})
		_, err := toolhooks.Execute(ctx, registry, "bash", tc.args, run)
		if !errors.Is(err, operation.ErrDenied) || !strings.Contains(err.Error(), tc.count) {
			t.Fatalf("feedback = %v", err)
		}
		if ctx.Err() != nil {
			t.Fatal("interception canceled the agent context")
		}
	}
	if checks.Load() != 4 {
		t.Fatal("risk checks were cached")
	}
}

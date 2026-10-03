package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	"github.com/chainreactors/cyber/exts/guardrail"
	"google.golang.org/protobuf/proto"
)

type guardrailHistoryObserver func(*aop.Event)

func (f guardrailHistoryObserver) ObserveEvent(event *aop.Event) { f(event) }

func TestGuardrailDecisionsSurviveTimelineRestart(t *testing.T) {
	for _, tc := range []struct {
		name        string
		mode        guardrail.Mode
		consequence guardrail.Action
		state       guardrail.ReviewState
		source      string
	}{
		{"safe_approved", guardrail.ModeSafe, guardrail.Action_ACTION_RECORD, guardrail.ReviewState_REVIEW_STATE_APPROVED, "control"},
		{"safe_rejected", guardrail.ModeSafe, guardrail.Action_ACTION_RECORD, guardrail.ReviewState_REVIEW_STATE_REJECTED, "control"},
		{"safe_expired", guardrail.ModeSafe, guardrail.Action_ACTION_RECORD, guardrail.ReviewState_REVIEW_STATE_EXPIRED, ""},
		{"safe_canceled", guardrail.ModeSafe, guardrail.Action_ACTION_RECORD, guardrail.ReviewState_REVIEW_STATE_CANCELED, ""},
		{"auto_allowed", guardrail.ModeAuto, guardrail.Action_ACTION_RECORD, guardrail.ReviewState_REVIEW_STATE_APPROVED, "auto"},
		{"auto_harmful", guardrail.ModeAuto, guardrail.Action_ACTION_BLOCK, guardrail.ReviewState_REVIEW_STATE_REJECTED, "auto"},
		{"auto_uncertain", guardrail.ModeAuto, guardrail.Action_ACTION_REVIEW, guardrail.ReviewState_REVIEW_STATE_REJECTED, "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			approve := tc.state == guardrail.ReviewState_REVIEW_STATE_APPROVED
			path := filepath.Join(t.TempDir(), "history.db")
			store, err := NewSQLiteStore(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			createStoredSession(t, store, "session")
			service := NewService(ServiceConfig{Store: store})
			defer service.Close(context.Background())
			stream := events.New()
			timeout := time.Second
			if tc.state == guardrail.ReviewState_REVIEW_STATE_EXPIRED {
				timeout = 30 * time.Millisecond
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body jevapi.Request
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				choice := "review"
				instructions, _ := body.Questions["action"].Instructions.(string)
				if strings.Contains(instructions, "Stage 2:") {
					if tc.mode != guardrail.ModeAuto {
						t.Error("safe mode called automatic confirmation")
					}
					choice = map[guardrail.Action]string{
						guardrail.Action_ACTION_RECORD: "record",
						guardrail.Action_ACTION_REVIEW: "review",
						guardrail.Action_ACTION_BLOCK:  "block",
					}[tc.consequence]
				}
				fmt.Fprintf(w, `{"answers":{"action":{"type":"choice","choice":%q}}}`, choice)
			}))
			defer server.Close()
			client := jevapi.New("fixture-key", "", time.Second)
			client.Endpoint = server.URL
			defer client.Close()
			registry := hooks.New()
			var runtime *guardrail.Runtime
			set, err := extension.New(
				extension.Provided[*events.Stream](stream),
				extension.Provided[*hooks.Registry](registry),
				extension.Provided[*jevapi.Client](client),
				guardrail.New(guardrail.Config{Provider: "jev", Mode: tc.mode, ReviewTimeout: timeout.String()}),
				extension.Func{LoadFunc: func(scope *extension.Scope) error {
					var err error
					runtime, err = extension.Use[*guardrail.Runtime](scope)
					return err
				}},
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := set.Load(t.Context()); err != nil {
				t.Fatal(err)
			}
			defer set.Close(context.Background())
			ctx, cancel := context.WithCancel(operation.ContextWithInvocation(t.Context(), operation.Invocation{SessionID: "session", TurnID: "turn", CallID: "call", Emitter: "control"}))
			defer cancel()
			var emitted []*aop.Event
			stream.Observe(guardrailHistoryObserver(func(event *aop.Event) {
				emitted = append(emitted, proto.CloneOf(event))
				service.BroadcastAOPEvent("session", event)
				var review guardrail.Review
				if payload := event.GetExtension(); payload != nil && payload.MessageIs(&review) && payload.UnmarshalTo(&review) == nil && review.State == guardrail.ReviewState_REVIEW_STATE_PENDING {
					switch tc.state {
					case guardrail.ReviewState_REVIEW_STATE_CANCELED:
						cancel()
					case guardrail.ReviewState_REVIEW_STATE_EXPIRED:
						// Allow the runtime's own approval timer to expire.
					default:
						if err := runtime.Resolve(ctx, review.Operation.OperationId, approve); err != nil {
							t.Error(err)
						}
					}
				}
			}))
			executed := false
			args, _ := aop.JSONValue(map[string]string{"command": "echo safe"})
			_, err = toolhooks.Execute(ctx, registry, "bash", string(args.Data), func(context.Context, string) (*aop.ToolResult, error) {
				executed = true
				return &aop.ToolResult{}, nil
			})
			if executed != approve || (approve && err != nil) || (!approve && !errors.Is(err, operation.ErrDenied)) {
				t.Fatalf("execution=%v error=%v", executed, err)
			}
			// Re-delivery of an existing event must not duplicate its audit entry.
			for _, event := range emitted {
				service.BroadcastAOPEvent("session", event)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewSQLiteStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			history, err := reopened.ListAOPEvents(t.Context(), "session", 100)
			if err != nil {
				t.Fatal(err)
			}
			var reviews []*guardrail.Review
			if len(history) != len(emitted) {
				t.Fatalf("stored events=%d emitted=%d", len(history), len(emitted))
			}
			for i, event := range history {
				if !proto.Equal(event, emitted[i]) {
					t.Fatal("database roundtrip changed the event payload or identity")
				}
			}
			for _, event := range history {
				var review guardrail.Review
				if payload := event.GetExtension(); payload != nil && payload.MessageIs(&review) && payload.UnmarshalTo(&review) == nil {
					if event.EmittedAt == nil || event.Id == "" || event.TurnId != "turn" {
						t.Fatal("review lost its durable identity, time or turn")
					}
					reviews = append(reviews, &review)
				}
			}
			wantReviews := 1
			if tc.mode == guardrail.ModeSafe {
				wantReviews = 2
			}
			if len(reviews) != wantReviews || reviews[len(reviews)-1].State != tc.state {
				t.Fatalf("review history=%v", reviews)
			}
			terminal := reviews[len(reviews)-1]
			if tc.mode == guardrail.ModeSafe && reviews[0].State != guardrail.ReviewState_REVIEW_STATE_PENDING {
				t.Fatal("pending record missing")
			}
			if terminal.ResolutionSource != tc.source || terminal.SessionId != "session" || terminal.Decision.Action != guardrail.Action_ACTION_REVIEW {
				t.Fatal("stored review lost its source, session or initial risk")
			}
			if !strings.Contains(terminal.Decision.Reason, " / risk / policy ") ||
				(tc.mode == guardrail.ModeAuto && (!strings.Contains(terminal.Decision.Reason, "Consequence assessment: JEV ") || !strings.Contains(terminal.Decision.Reason, " / consequence / policy "))) {
				t.Fatal("stored review lost a judgment stage or policy version")
			}
			if reviews[0].Operation.OperationId != terminal.Operation.OperationId || terminal.Call.Id != "call" || string(terminal.Call.Arguments.Data) != string(args.Data) {
				t.Fatal("review history lost invocation correlation")
			}
		})
	}
}

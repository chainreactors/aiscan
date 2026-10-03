package guardrail

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
)

func TestSafeModeAuthorizesInterceptedInvocation(t *testing.T) {
	for _, mode := range []Mode{ModeSafe} {
		for _, action := range []Action{Action_ACTION_REVIEW, Action_ACTION_BLOCK} {
			for _, approve := range []bool{true, false} {
				t.Run(string(mode)+action.String()+map[bool]string{true: "approve", false: "reject"}[approve], func(t *testing.T) {
					r, registry, reviews := fixture(t, time.Second, mode)
					register(t, r, action)
					var executed atomic.Int32
					done := make(chan error, 1)
					go func() {
						_, err := toolhooks.Execute(callContext(), registry, "echo", "original arguments", func(_ context.Context, args string) (*aop.ToolResult, error) {
							if args != "original arguments" {
								t.Error("authorized invocation changed arguments")
							}
							executed.Add(1)
							return &aop.ToolResult{}, nil
						})
						done <- err
					}()
					review := nextReview(t, reviews)
					if executed.Load() != 0 || review.Decision.Action != action {
						t.Fatal("ran before authorization or lost original risk classification")
					}
					if err := r.Resolve(callContext(), review.Operation.OperationId, approve); err != nil {
						t.Fatal(err)
					}
					err := <-done
					if approve {
						if err != nil || executed.Load() != 1 {
							t.Fatalf("authorization: calls=%d err=%v", executed.Load(), err)
						}
					} else if !errors.Is(err, operation.ErrDenied) || executed.Load() != 0 {
						t.Fatal("rejection executed tool")
					}
					if len(r.Pending("session")) != 0 || r.Resolve(callContext(), review.Operation.OperationId, true) == nil {
						t.Fatal("stale approval remained usable")
					}
				})
			}
		}
	}
}

func TestAutoModeReturnsInterceptionWithoutReviewOrTermination(t *testing.T) {
	for _, action := range []Action{Action_ACTION_REVIEW, Action_ACTION_BLOCK} {
		t.Run(action.String(), func(t *testing.T) {
			r, registry, reviews := fixture(t, time.Second, ModeAuto)
			register(t, r, action)
			ctx := callContext()
			result, err := toolhooks.Execute(ctx, registry, "echo", "{}", func(context.Context, string) (*aop.ToolResult, error) {
				t.Fatal("intercepted tool executed")
				return nil, nil
			})
			if !errors.Is(err, operation.ErrDenied) || !result.IsError || result.Terminate || ctx.Err() != nil {
				t.Fatalf("interception did not produce a nonterminal tool error: %v, %v", result, err)
			}
			if !strings.Contains(err.Error(), "was not executed") || !strings.Contains(err.Error(), "policy") {
				t.Fatal("LLM feedback missing risk or execution status")
			}
			if len(r.Pending("session")) != 0 {
				t.Fatal("automatic mode created a pending human review")
			}
			audit := nextReview(t, reviews)
			if audit.State != ReviewState_REVIEW_STATE_REJECTED || audit.ResolutionSource != "auto" {
				t.Fatal("automatic rejection audit missing")
			}
		})
	}
}

func TestSafeModeCannotAuthorizeBrokenPolicy(t *testing.T) {
	r, registry, reviews := fixture(t, time.Second, ModeSafe)
	r.check, r.confirm = func(context.Context, toolhooks.CallEvent) (*Decision, error) { return nil, errors.New("broken policy") }, nil
	_, err := toolhooks.Execute(callContext(), registry, "echo", "{}", func(context.Context, string) (*aop.ToolResult, error) {
		t.Fatal("broken policy executed")
		return nil, nil
	})
	if !errors.Is(err, operation.ErrDenied) || len(reviews) != 0 {
		t.Fatal("broken policy became authorizable")
	}
}

package guardrail

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
)

func TestTwoStageAdmission(t *testing.T) {
	for _, mode := range []Mode{"", ModeAuto, ModeSafe} {
		for _, risk := range []Action{Action_ACTION_RECORD, Action_ACTION_REVIEW, Action_ACTION_BLOCK} {
			for _, consequence := range []Action{Action_ACTION_RECORD, Action_ACTION_REVIEW, Action_ACTION_BLOCK} {
				t.Run(string(mode)+risk.String()+consequence.String(), func(t *testing.T) {
					r, registry, reviews := fixture(t, time.Second, mode)
					var screens, confirmations, executions atomic.Int32
					r.check, r.confirm = func(_ context.Context, ev toolhooks.CallEvent) (*Decision, error) {
						screens.Add(1)
						ev.Call.Arguments.Data = []byte("mutated screening copy")
						return &Decision{Action: risk, Reason: "risk"}, nil
					}, func(_ context.Context, ev toolhooks.CallEvent) (*Decision, error) {
						confirmations.Add(1)
						if string(ev.Call.Arguments.Data) != "original" {
							t.Error("confirmation did not receive original arguments")
						}
						return &Decision{Action: consequence, Reason: "consequences"}, nil
					}
					done := make(chan error, 1)
					go func() {
						_, err := toolhooks.Execute(callContext(), registry, "echo", "original", func(_ context.Context, args string) (*aop.ToolResult, error) {
							if args != "original" {
								t.Error("executed mutated arguments")
							}
							executions.Add(1)
							return &aop.ToolResult{}, nil
						})
						done <- err
					}()
					flagged := risk != Action_ACTION_RECORD
					if mode == ModeSafe && flagged {
						pending := nextReview(t, reviews)
						if pending.State != ReviewState_REVIEW_STATE_PENDING || confirmations.Load() != 0 || executions.Load() != 0 {
							t.Fatal("safe mode did not wait for human consequence assessment")
						}
						if err := r.Resolve(callContext(), pending.Operation.OperationId, true); err != nil {
							t.Fatal(err)
						}
					}
					err := <-done
					allowed := !flagged || mode == ModeSafe || consequence == Action_ACTION_RECORD
					if allowed && (err != nil || executions.Load() != 1) {
						t.Fatalf("allowed invocation: %v executions=%d", err, executions.Load())
					}
					if !allowed && (!errors.Is(err, operation.ErrDenied) || executions.Load() != 0) {
						t.Fatal("harmful or uncertain consequences executed")
					}
					wantConfirm := int32(0)
					if flagged && mode != ModeSafe {
						wantConfirm = 1
					}
					if screens.Load() != 1 || confirmations.Load() != wantConfirm {
						t.Fatal("incorrect stage count")
					}
					if flagged {
						audit := nextReview(t, reviews)
						if audit.Decision.Action != risk || audit.State == ReviewState_REVIEW_STATE_PENDING {
							t.Fatal("terminal audit lost screening result")
						}
						if mode != ModeSafe && audit.ResolutionSource != "auto" {
							t.Fatal("automatic audit missing source")
						}
					}
					if len(r.Pending("session")) != 0 {
						t.Fatal("pending approval left behind")
					}
				})
			}
		}
	}
}

func TestConsequenceFailuresCannotExecute(t *testing.T) {
	for name, confirm := range map[string]checkFunc{
		"error": func(context.Context, toolhooks.CallEvent) (*Decision, error) {
			return nil, errors.New("private secret")
		},
		"nil":     func(context.Context, toolhooks.CallEvent) (*Decision, error) { return nil, nil },
		"invalid": func(context.Context, toolhooks.CallEvent) (*Decision, error) { return &Decision{Action: 99}, nil },
		"panic":   func(context.Context, toolhooks.CallEvent) (*Decision, error) { panic("private secret") },
	} {
		t.Run(name, func(t *testing.T) {
			r, registry, _ := fixture(t, time.Second, ModeAuto)
			r.check, r.confirm = func(context.Context, toolhooks.CallEvent) (*Decision, error) {
				return &Decision{Action: Action_ACTION_REVIEW}, nil
			}, confirm
			ctx := callContext()
			result, err := toolhooks.Execute(ctx, registry, "bash", "{}", func(context.Context, string) (*aop.ToolResult, error) {
				t.Fatal("failed confirmation executed")
				return nil, nil
			})
			if !errors.Is(err, operation.ErrDenied) || !result.IsError || result.Terminate || ctx.Err() != nil {
				t.Fatal("failure did not return a nonterminal denial")
			}
		})
	}
}

func TestIndependentToolHookDenialCannotBeOverridden(t *testing.T) {
	r, registry, _ := fixture(t, time.Second, ModeAuto)
	r.check = func(context.Context, toolhooks.CallEvent) (*Decision, error) {
		return &Decision{Action: Action_ACTION_BLOCK}, nil
	}
	r.confirm = func(context.Context, toolhooks.CallEvent) (*Decision, error) {
		return &Decision{Action: Action_ACTION_RECORD}, nil
	}
	toolhooks.Before.On(registry, "independent-policy", func(context.Context, toolhooks.CallEvent) (toolhooks.Admission, error) {
		return denied("independent policy"), nil
	})
	_, err := toolhooks.Execute(callContext(), registry, "bash", "{}", func(context.Context, string) (*aop.ToolResult, error) {
		t.Fatal("another hook's denial was erased")
		return nil, nil
	})
	if !errors.Is(err, operation.ErrDenied) {
		t.Fatal(err)
	}
}

func TestConsequenceAssessmentSurvivesModeChangeButNotCancellation(t *testing.T) {
	for _, cancelCall := range []bool{false, true} {
		t.Run(map[bool]string{false: "mode_snapshot", true: "cancellation"}[cancelCall], func(t *testing.T) {
			r, registry, reviews := fixture(t, time.Second, ModeAuto)
			entered, release := make(chan struct{}), make(chan struct{})
			r.check, r.confirm = func(context.Context, toolhooks.CallEvent) (*Decision, error) {
				return &Decision{Action: Action_ACTION_REVIEW}, nil
			}, func(ctx context.Context, _ toolhooks.CallEvent) (*Decision, error) {
				close(entered)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return &Decision{Action: Action_ACTION_RECORD}, nil
			}
			ctx, cancel := context.WithCancel(callContext())
			defer cancel()
			var executions atomic.Int32
			done := make(chan error, 1)
			go func() {
				_, err := toolhooks.Execute(ctx, registry, "bash", "{}", func(context.Context, string) (*aop.ToolResult, error) {
					executions.Add(1)
					return &aop.ToolResult{}, nil
				})
				done <- err
			}()
			<-entered
			if err := r.SetMode(ModeSafe); err != nil {
				t.Fatal(err)
			}
			if cancelCall {
				cancel()
			}
			close(release)
			err := <-done
			if cancelCall {
				if err == nil || executions.Load() != 0 || len(reviews) != 0 {
					t.Fatal("canceled assessment authorized execution")
				}
			} else if err != nil || executions.Load() != 1 || nextReview(t, reviews).ResolutionSource != "auto" {
				t.Fatal("mode switch changed an in-flight assessment")
			}
			if len(r.Pending("session")) != 0 {
				t.Fatal("mode switch introduced a pending human review")
			}
		})
	}
}

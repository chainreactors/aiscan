package api

import (
	"context"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
)

type terminalSessionStore struct {
	*sessionTestStore
	ended bool
}

func (s *terminalSessionStore) HasTurnEnded(context.Context, string, string) (bool, error) {
	return s.ended, nil
}

type finishedSessionRuntime struct {
	*sessionTestRuntime
	err error
}

func (r *finishedSessionRuntime) CancelTurn(context.Context, string, string) error { return r.err }

func TestCancelFinishedTurnConvergesAfterProviderFailureOrDisconnect(t *testing.T) {
	for _, runtimeErr := range []error{ErrTurnNotFound, Errorf(CodeUnavailable, "node is not connected")} {
		for _, terminalTurn := range []string{"requested", "another-turn", ""} {
			t.Run(runtimeErr.Error()+"/"+terminalTurn, func(t *testing.T) {
				store := &terminalSessionStore{sessionTestStore: &sessionTestStore{session: &types.SessionRecord{Session: &aop.Session{Id: "session", NodeId: "node"}}}}
				store.ended = terminalTurn == "requested"
				sessions := NewSessions(store, &finishedSessionRuntime{sessionTestRuntime: &sessionTestRuntime{}, err: runtimeErr}, nil)
				response, err := sessions.CancelTurn(t.Context(), "cancel", &aop.CancelTurnRequest{SessionId: "session", TurnId: "requested"})
				if terminalTurn == "requested" {
					if err != nil || response.GetAccepted().GetState() != "completed" {
						t.Fatalf("finished turn remains stuck: response=%v err=%v", response, err)
					}
				} else if err == nil && response.GetAccepted() != nil {
					t.Fatal("unrelated terminal incorrectly completed requested turn")
				}
			})
		}
	}
}

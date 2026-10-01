package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/types"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestDeletedSessionDiscardsLateRecapAndCloseEvents(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "recap.db"), ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(ServiceConfig{Store: store})
	defer svc.Close(context.Background())
	session := createTestSession(t, svc, "agent", "deleted task")
	id := session.GetSession().GetId()
	value, err := anypb.New(&types.Recap{Text: "Finished the local check."})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteSession(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	for _, event := range []*aop.Event{
		{SessionId: id, TurnId: "turn", Payload: &aop.Event_Extension{Extension: value}},
		{SessionId: id, Payload: &aop.Event_SessionEnded{SessionEnded: &aop.SessionEnded{Reason: "completed"}}},
	} {
		if recreated, err := svc.acceptAOPEvent(id, event); err != nil || recreated {
			t.Fatalf("late event was not discarded: recreated=%v err=%v", recreated, err)
		}
	}
	if seq, err := store.MaxAOPEventSeq(t.Context(), id); err != nil || seq != 0 {
		t.Fatalf("deleted history was recreated: seq=%d err=%v", seq, err)
	}
}

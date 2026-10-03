package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/exts/guardrail"
	"google.golang.org/protobuf/proto"
)

func TestGuardrailRoutesToSessionNodeWhileTurnIsBusy(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "guardrail.db"), ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewService(ServiceConfig{Store: store})
	defer service.Close(t.Context())
	pool := NewAgentPool(service.Hub(), nil)
	service.SetAgentPool(pool)
	agent, sent := newFakeAgent("owner", 4)
	pool.register(agent)
	other, otherSent := newFakeAgent("other", 4)
	pool.register(other)
	session := createTestSession(t, service, "owner", "guardrail")
	// A live turn waiter remains installed while a second control request flows.
	active := make(chan proto.Message, 1)
	agent.tasks["running-turn"] = active
	for _, resolve := range []bool{false, true} {
		request := &guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_Pending{Pending: &guardrail.PendingRequest{SessionId: session.Session.Id}}}
		if resolve {
			request.Message = &guardrail.ProtocolMessage_Resolve{Resolve: &guardrail.ResolveRequest{SessionId: session.Session.Id, OperationId: "operation", Approve: true}}
		}
		result := make(chan *guardrail.ProtocolMessage, 1)
		failures := make(chan error, 1)
		go func() {
			response, err := service.forwardGuardrail(t.Context(), request)
			result <- response
			failures <- err
		}()
		var envelope *aop.Envelope
		select {
		case envelope = <-sent:
		case <-time.After(time.Second):
			t.Fatal("guardrail queued behind running turn")
		}
		msg, err := aop.Unwrap(envelope)
		if err != nil || !proto.Equal(msg, request) {
			t.Fatalf("forwarded wrong request: %v", err)
		}
		response := &guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_PendingResult{PendingResult: &guardrail.PendingResponse{}}}
		if resolve {
			response.Message = &guardrail.ProtocolMessage_Resolved{Resolved: &guardrail.ResolveResponse{}}
		}
		pool.handleAgentEnvelope(agent, aop.Reply(envelope.Id, response))
		if got := <-result; !proto.Equal(got, response) {
			t.Fatalf("response=%v", got)
		}
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
		select {
		case <-otherSent:
			t.Fatal("request sent to another session's node")
		default:
		}
		select {
		case <-active:
			t.Fatal("approval completed the running turn")
		default:
		}
	}
	request := &guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_Pending{Pending: &guardrail.PendingRequest{SessionId: "absent"}}}
	if _, err := service.forwardGuardrail(t.Context(), request); err == nil {
		t.Fatal("unknown session accepted")
	}
}

func TestGuardrailRequestCancellationDropsOnlyControlWaiter(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "guardrail.db"), ScanSchema)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewService(ServiceConfig{Store: store})
	defer service.Close(t.Context())
	pool := NewAgentPool(service.Hub(), nil)
	service.SetAgentPool(pool)
	agent, sent := newFakeAgent("owner", 4)
	pool.register(agent)
	session := createTestSession(t, service, "owner", "guardrail")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.forwardGuardrail(ctx, &guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_Pending{Pending: &guardrail.PendingRequest{SessionId: session.Session.Id}}})
		done <- err
	}()
	request := <-sent
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	agent.mu.Lock()
	_, exists := agent.tasks[request.Id]
	agent.mu.Unlock()
	if exists {
		t.Fatal("canceled transport waiter retained")
	}
	select {
	case <-sent:
		t.Fatal("UI cancellation submitted a tool cancellation")
	default:
	}
}

package guardrail

import (
	"context"
	"fmt"
	agentsession "github.com/chainreactors/cyber/agent/session"

	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"

	"github.com/chainreactors/cyber/core/operation"
	"google.golang.org/protobuf/proto"
)

type ProtocolExtension struct{}

func NewProtocol() *ProtocolExtension { return &ProtocolExtension{} }
func (*ProtocolExtension) Load(scope *extension.Scope) error {
	runtime, err := extension.Use[*Runtime](scope)
	if err != nil {
		return err
	}
	sessions, err := extension.Use[*agentsession.Runtime](scope)
	if err != nil {
		return err
	}
	return extension.Add(scope, aop.Binding{Prototype: &ProtocolMessage{}, Open: func() aop.NamespaceHandler { return protocolHandler(runtime, sessions) }})
}

func protocolHandler(runtime *Runtime, sessions *agentsession.Runtime) aop.NamespaceHandler {
	return func(ctx context.Context, envelope *aop.Envelope, message proto.Message, send aop.SendFunc) error {
		value, ok := message.(*ProtocolMessage)
		if !ok {
			return fmt.Errorf("unexpected guardrail namespace message")
		}
		reply := func(payload proto.Message) error { return send(aop.Reply(envelope.GetId(), payload)) }
		if pending := value.GetPending(); pending != nil {
			if bound := operation.InvocationFromContext(ctx).SessionID; bound != "" && bound != pending.SessionId {
				return reply(aop.NewProtocolError("GUARDRAIL_DENIED", "session scope mismatch"))
			}
			return reply(&ProtocolMessage{Message: &ProtocolMessage_PendingResult{PendingResult: &PendingResponse{Reviews: sessionReviews(runtime, sessions, pending.SessionId)}}})
		}
		if resolve := value.GetResolve(); resolve != nil {
			if resolve.OperationId == "" {
				return reply(aop.NewProtocolError("INVALID_ARGUMENT", "operation_id is required"))
			}
			// The authenticated control channel supplies a session selector. Runtime
			// checks that the operation actually belongs to that exact session.
			invocation := operation.InvocationFromContext(ctx)
			if invocation.SessionID != "" && invocation.SessionID != resolve.SessionId {
				return reply(aop.NewProtocolError("GUARDRAIL_DENIED", "session scope mismatch"))
			}
			invocation.SessionID = reviewSession(runtime, sessions, resolve.SessionId, resolve.OperationId)
			if invocation.SessionID == "" {
				return reply(aop.NewProtocolError("GUARDRAIL_DENIED", "pending review not found in this session"))
			}
			invocation.Emitter = "control"
			if err := runtime.Resolve(operation.ContextWithInvocation(ctx, invocation), resolve.OperationId, resolve.Approve); err != nil {
				return reply(aop.NewProtocolError("GUARDRAIL_DENIED", err.Error()))
			}
			return reply(&ProtocolMessage{Message: &ProtocolMessage_Resolved{Resolved: &ResolveResponse{}}})
		}
		return reply(aop.NewProtocolError("INVALID_ARGUMENT", "pending or resolve request required"))
	}
}

func sessionReviews(runtime *Runtime, sessions *agentsession.Runtime, root string) []*Review {
	ids := []string{root}
	if sessions != nil {
		ids = sessions.SessionIDs(root)
	}
	var reviews []*Review
	for _, id := range ids {
		reviews = append(reviews, runtime.Pending(id)...)
	}
	return reviews
}

func reviewSession(runtime *Runtime, sessions *agentsession.Runtime, root, operationID string) string {
	for _, review := range sessionReviews(runtime, sessions, root) {
		if review.Operation.GetOperationId() == operationID {
			return review.SessionId
		}
	}
	return ""
}

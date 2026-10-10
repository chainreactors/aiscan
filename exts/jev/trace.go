package jev

import (
	"context"
	"encoding/json"
	"maps"
	"sort"

	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/decision"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type traceKey struct{}
type runtimeTrace struct {
	session, turn, task, segment, previous, reflex, call string
	boundary, claim                                      string
	attempt                                              uint32
	step                                                 uint32
	background, started                                  bool
}

func traceContext(ctx context.Context, trace *runtimeTrace) context.Context {
	return context.WithValue(ctx, traceKey{}, trace)
}
func traceFrom(ctx context.Context) *runtimeTrace {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(traceKey{}).(*runtimeTrace)
	return t
}
func (job declaration) trace() *runtimeTrace {
	return &runtimeTrace{session: job.session, turn: job.turn, task: job.task, boundary: job.boundary, background: true}
}

// JEV evidence is an extension payload, never a root tool event. Resumed model
// history must continue to contain only the agent's own tool protocol messages.
func (e *Extension) emit(ctx context.Context, payload proto.Message) {
	t := traceFrom(ctx)
	if e.stream == nil || t == nil || t.session == "" {
		return
	}
	v := &RuntimeEvent{TaskId: t.task, SegmentId: t.segment, PreviousSegmentId: t.previous,
		Step: t.step, ReflexId: t.reflex, CallId: t.call, Background: t.background, BoundaryId: t.boundary, ClaimId: t.claim}
	switch p := payload.(type) {
	case *Boundary:
		v.Payload = &RuntimeEvent_Boundary{Boundary: p}
	case *Observation:
		v.Payload = &RuntimeEvent_Observation{Observation: p}
	case *DecisionRequest:
		v.Payload = &RuntimeEvent_DecisionRequest{DecisionRequest: p}
	case *DecisionResult:
		v.Payload = &RuntimeEvent_DecisionResult{DecisionResult: p}
	case *Takeover:
		v.Payload = &RuntimeEvent_Takeover{Takeover: p}
	case *Dispatch:
		v.Payload = &RuntimeEvent_Dispatch{Dispatch: p}
	case *Result:
		v.Payload = &RuntimeEvent_Result{Result: p}
	case *Handoff:
		v.Payload = &RuntimeEvent_Handoff{Handoff: p}
	case *Generation:
		v.Payload = &RuntimeEvent_Generation{Generation: p}
	case *LibraryChange:
		v.Payload = &RuntimeEvent_LibraryChange{LibraryChange: p}
	default:
		return
	}
	packed, err := anypb.New(v)
	if err != nil {
		return
	}
	e.stream.Publish(&aop.Event{SessionId: t.session, TurnId: t.turn, Emitter: "jev",
		Payload: &aop.Event_Extension{Extension: packed}})
}

func jsonText(value any) string { data, _ := json.Marshal(value); return string(data) }
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func claimDefinition(id string, c claimRecord) *ClaimDefinition {
	return &ClaimDefinition{Id: id, Type: c.Type, Context: c.Context, Options: append([]string(nil), c.Options...), SourceTaskId: c.Task}
}
func reflexDefinition(id string, r reflexRecord) *ReflexDefinition {
	return &ReflexDefinition{Id: id, When: r.When, Decide: r.Decide, Observe: r.Observe, Readers: maps.Clone(r.Readers), Contracts: maps.Clone(r.Contracts), ClaimIds: append([]string(nil), r.Claims...), ApiVersion: uint32(r.APIVersion), QualificationJson: jsonText(r.Proof), Blocker: r.Blocker, ManifestJson: jsonText(map[string]any{"steps": r.Steps, "parameters_schema": r.Parameters})}
}
func (e *Extension) libraryView() *GetLibraryResponse {
	lib := e.snapshot()
	status := "ready"
	if e.config.Mode == "off" {
		status = "off"
	} else if e.client == nil {
		status = "unavailable"
	}
	out := &GetLibraryResponse{Mode: e.config.Mode, Status: status, Revision: digest(lib), Learning: e.config.Learning}
	ids := make([]string, 0, len(lib.Claims))
	for id := range lib.Claims {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out.Claims = append(out.Claims, claimDefinition(id, lib.Claims[id]))
	}
	ids = ids[:0]
	for id := range lib.Reflexes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out.Reflexes = append(out.Reflexes, reflexDefinition(id, lib.Reflexes[id]))
	}
	ids = ids[:0]
	for id := range lib.Candidates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out.Candidates = append(out.Candidates, reflexDefinition(id, lib.Candidates[id]))
	}
	return out
}

func traceClaims(claims map[string]Claim) map[string]*decision.Claim {
	out := map[string]*decision.Claim{}
	for id, c := range claims {
		out[id] = c.Proto()
	}
	return out
}
func traceEvaluations(response *jevapi.Evaluations) map[string]*decision.Evaluation {
	out := map[string]*decision.Evaluation{}
	if response != nil {
		for id, value := range response.Values {
			out[id] = proto.CloneOf(value)
		}
	}
	return out
}

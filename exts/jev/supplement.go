package jev

import (
	"context"
	"encoding/json"

	"github.com/chainreactors/cyber/core/operation"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
)

func (e *Extension) admitSupplement(ctx context.Context, event toolhooks.CallEvent) (toolhooks.Admission, error) {
	inv := operation.InvocationFromContext(ctx)
	if inv.Emitter == "jev" {
		return toolhooks.Admission{}, nil
	}
	run := digest([]string{inv.SessionID, inv.TurnID})
	e.mu.Lock()
	record := e.tasks[run]
	record.NativeEvidence = cloneEvidence(record.NativeEvidence)
	e.mu.Unlock()
	if record.NativeEpoch == "" {
		return toolhooks.Admission{}, nil
	}
	s := e.nativeSnapshot()
	call := NativeCall{Name: event.Call.Name, Arguments: event.Call.GetArguments().GetData()}
	call, err := prepareBinding(call)
	if err != nil {
		return toolhooks.Admission{Deny: err}, nil //nolint:nilerr // Denial is an admission result, not a hook failure.
	}
	if record.NativeEpoch != digest(e.contracts.Catalog()) {
		return toolhooks.Admission{Deny: handoffError{"qualified task contract unavailable"}}, nil
	}
	access, err := s.access(call)
	if err != nil || access != ReadAccess {
		return toolhooks.Admission{Deny: handoffError{"effect_unknown: supplementation must use trusted reads; mutations return to the Reflex"}}, nil //nolint:nilerr // The executor handles Deny.
	}
	call.Read = true
	raw, _ := json.Marshal(record.Input)
	capabilities := map[string]any{"tools": record.Input["tools"], "commands": record.Input["commands"], "native_contracts": e.contracts.Catalog()}
	if err := e.judgeRuntime(ctx, "binding", raw, map[string]any{"arguments": record.Arguments, "call": call, "capabilities": bindingCapabilities(call, capabilities), "effects": record.Ledger.summary()}); err != nil {
		return toolhooks.Admission{Deny: err}, nil //nolint:nilerr // The executor handles Deny.
	}
	return toolhooks.Admission{}, nil
}
func (e *Extension) observeSupplement(ctx context.Context, event toolhooks.Completion) (struct{}, error) {
	inv := operation.InvocationFromContext(ctx)
	if inv.Emitter == "jev" {
		return struct{}{}, nil
	}
	run := digest([]string{inv.SessionID, inv.TurnID})
	e.mu.Lock()
	record := e.tasks[run]
	e.mu.Unlock()
	if record.NativeEpoch == "" || event.Result == nil {
		return struct{}{}, nil
	}
	call := NativeCall{Name: event.Call.Name, Arguments: event.Call.GetArguments().GetData(), Read: true}
	call, err := prepareBinding(call)
	if err != nil {
		return struct{}{}, nil //nolint:nilerr // Malformed bindings have no evidence to reconcile.
	}
	value := runtimeResult(event.Call, event.Result)
	e.updateTask(run, record.Key, func(r *taskRecord) { r.NativeEvidence[event.Result.CallId] = cloneJSONMap(value) })
	if s := e.nativeSnapshot(); record.NativeEpoch == digest(e.contracts.Catalog()) && record.Ledger != nil {
		record.Ledger.reconcile(s, call, value)
	}
	return struct{}{}, nil
}

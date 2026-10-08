package jev

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
)

func TestRuntimeWaitDoesNotConsumeComputationBudget(t *testing.T) {
	r := observationReflex(t, `js:function(context,args){const r=execute({name:'native',arguments:{},read:true});return {report:r.data};}`)
	result, err := runReflexJS(t.Context(), &r, observationCapabilities("native"), nil, nil, func(binding) (map[string]any, error) {
		time.Sleep(160 * time.Millisecond)
		return map[string]any{"data": "actual"}, nil
	})
	if err != nil || result["report"] != "actual" {
		t.Fatalf("external wait interrupted compute: %v %v", result, err)
	}
}

func TestRuntimeCancellationCannotBeCaughtByGeneratedCode(t *testing.T) {
	r := observationReflex(t, `js:function(context,args){try{execute({name:'native',arguments:{},read:true});}catch(e){}return {report:'invented completion'};}`)
	ctx, cancel := context.WithCancel(t.Context())
	result, err := runReflexJS(ctx, &r, observationCapabilities("native"), nil, nil, func(binding) (map[string]any, error) { cancel(); return nil, ctx.Err() })
	if err == nil || result != nil {
		t.Fatal("generated catch concealed host cancellation")
	}
}

func TestParameterResponseRejectsTrailingDataAndUnknownInput(t *testing.T) {
	e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, fakeJEV(t, func(req inferenceRequest) map[string]inferenceAnswer { return runtimeAnswers(req, Defer) }))
	for _, output := range []string{`null`, `{"actor":"bob"} broken`, `{"actor":"bob"} {}`} {
		cfg.Provider = testProvider(func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
			return reply(provider.TextMessage("assistant", output)), nil
		})
		ctx := traceContext(t.Context(), &runtimeTrace{session: "s", turn: "t", task: "task"})
		if _, err := e.supplyArguments(ctx, cfg, json.RawMessage(`{}`), Reflex{}, "actor"); err == nil {
			t.Fatalf("invalid argument response admitted: %s", output)
		}
	}
}

func TestRuntimeHandsOffUnsafeNumericIdentityBeforeDispatch(t *testing.T) {
	r := observationReflex(t, `js:function(context,args){execute({name:'native',arguments:{id:args.id},read:false});return {report:args.id};}`)
	calls := 0
	execute := func(binding) (map[string]any, error) { calls++; return map[string]any{"data": "actual"}, nil }
	_, err := runReflexJS(t.Context(), &r, observationCapabilities("native"), map[string]any{"id": json.Number("9007199254740993")}, nil, execute)
	if err == nil || calls != 0 || !strings.Contains(interruptedCause(err).Error(), "safe range") {
		t.Fatalf("unsafe identity dispatched: calls=%d err=%v", calls, err)
	}
	if _, err = runReflexJS(t.Context(), &r, observationCapabilities("native"), map[string]any{"id": json.Number("9007199254740991")}, nil, execute); err != nil || calls != 1 {
		t.Fatalf("safe identity rejected: calls=%d err=%v", calls, err)
	}
	r = observationReflex(t, `js:function(context,args){try{const r=execute({name:'native',arguments:{},read:true});execute({name:'native',arguments:{id:r.data.id},read:false});}catch(e){}return {report:'done'};}`)
	calls = 0
	_, err = runReflexJS(t.Context(), &r, observationCapabilities("native"), nil, nil, func(binding) (map[string]any, error) {
		calls++
		return map[string]any{"data": map[string]any{"id": json.Number("9007199254740993")}}, nil
	})
	if err == nil || calls != 1 || !strings.Contains(interruptedCause(err).Error(), "safe range") {
		t.Fatalf("unsafe result identity was rounded or swallowed: calls=%d err=%v", calls, err)
	}
}

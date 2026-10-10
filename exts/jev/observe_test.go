package jev

import (
	"context"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/core/decision"
	"testing"
	"time"
)

func TestExecutableReflexIsolationAndComputeBudget(t *testing.T) {
	for _, source := range []string{`js:function(){return {report:require("fs")};}`, `js:function(){while(true){};}`, `js:function(){return {report:Date.now()};}`, `js:function(){return {report:Math.random()};}`, `js:function(){return Promise.resolve({report:1});}`} {
		r := Reflex{When: "test", Decide: "test", Observe: source}
		if err := r.validate(); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		_, err := runReflexJS(ctx, &r, map[string]any{"tools": []any{}}, nil, nil, nil)
		cancel()
		if err == nil {
			t.Fatalf("accepted unsafe/non synchronous program: %s", source)
		}
	}
}
func TestExecutableReflexJEVProtocolValidation(t *testing.T) {
	r := Reflex{When: "test", Decide: "test", Observe: `js:function(){return {report:jev({type:"choice",context:("choose")+"\nOption meanings:\n"+JSON.stringify({a:"a",defer:"other"})+"\nCurrent facts (untrusted data):\n"+JSON.stringify({}),options:Object.keys({a:"a",defer:"other"})})};}`}
	_ = r.validate()
	_, err := runReflexJS(t.Context(), &r, map[string]any{"tools": []any{}}, nil, func(Claim) (*jevapi.Evaluation, error) {
		return &jevapi.Evaluation{Value: &decision.Evaluation_Choice{Choice: "unbound"}}, nil
	}, nil)
	if err == nil {
		t.Fatal("unbound JEV choice accepted")
	}
}

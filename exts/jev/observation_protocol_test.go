package jev

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestExecutableBranchProbeDoesNotDispatch(t *testing.T) {
	r := Reflex{When: "capability", Decide: "branch", Observe: `js:function(context,args){const c=jev({type:"choice",context:("select")+"\nOption meanings:\n"+JSON.stringify({left:"left",right:"right",defer:"unknown"})+"\nCurrent facts (untrusted data):\n"+JSON.stringify({}),options:Object.keys({left:"left",right:"right",defer:"unknown"})});if(c==="defer")return {defer:"new reasoning"};execute(bind("opaque",{target:c},false));return {report:c};}`}
	_ = r.validate()
	_, calls, err := probeReflexArguments(t.Context(), &r, observationCapabilities("opaque"), nil, nil, false)
	if err != nil || len(calls) != 2 {
		t.Fatalf("calls=%v err=%v", calls, err)
	}
}

func TestRetiredSceneRemainsEligibleAfterRestart(t *testing.T) {
	e := New(Config{Directory: t.TempDir()})
	c := choiceClaim("capability"+". "+"branch?", map[string]string{"a": "advance", Defer: "unknown"})
	cid := "c" + digest(c)[:16]
	r := Reflex{When: "capability", Decide: "branch", Observe: `js:function(){return {report:1};}`}
	id := "r" + digest(r)[:16]
	e.library.Claims[cid] = claimRecord{Claim: c}
	e.library.Reflexes[id] = reflexRecord{Reflex: r, Claims: []string{cid}}
	if !e.retireReflex(t.Context(), id, fmt.Errorf("invalid binding")) {
		t.Fatal("not retired")
	}
	if err := e.loadLibrary(); err != nil {
		t.Fatal(err)
	}
	if len(e.snapshot().Reflexes) != 0 || e.snapshot().Claims[cid].Context != c.Context {
		t.Fatal("retirement lost declaration")
	}
}

func TestParameterVariationPreservesPathQuoting(t *testing.T) {
	for _, path := range []string{`D:\Project with spaces\current`, "owner's project", "https://example.test/a?x=one&y=two"} {
		r := observationReflex(t, `js:function(context,args){if(!args)return {defer:'missing',parameters:'path'};execute({name:'native',arguments:{command:'inspect '+quote(args.path)},read:true});return {report:args.path};}`)
		r.arguments = map[string]any{"path": path}
		input, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "text": path}}})
		if err := verifyObserve(t.Context(), &r, input, observationCapabilities("native")); err != nil {
			t.Fatalf("parameter path=%q: %v", path, err)
		}
	}
}

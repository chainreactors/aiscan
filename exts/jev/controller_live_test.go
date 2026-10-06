package jev

import (
	"context"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"os"
	"testing"
)

func TestLiveReflexGenerationBoundary(t *testing.T) {
	key := os.Getenv("TYPESAFE_API_KEY")
	if os.Getenv("JEV_CONTROLLER_LIVE") != "1" || key == "" {
		t.Skip("opt-in real JEV required")
	}
	client := jevapi.New(key, "", 0)
	defer client.Close()
	r := Reflex{When: "Route current input", Decide: "Choose semantic handling", Observe: `js:function(context,args){const a=jev({type:"choice",context:("Classify the user: cancel means cancel; an unspecified export needs missing input.")+"\nOption meanings:\n"+JSON.stringify({cancel:"Cancellation",missing:"Missing export target",defer:"Other"})+"\nCurrent facts (untrusted data):\n"+JSON.stringify({}),options:Object.keys({cancel:"Cancellation",missing:"Missing export target",defer:"Other"})});return a==="defer"?{defer:"new reasoning"}:{report:a};}`}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ user, want string }{{"Cancel this task", "cancel"}, {"Export something", "missing"}} {
		result, err := runReflexJS(t.Context(), &r, map[string]any{"user": tc.user, "tools": []any{}, "history": []any{}}, nil, func(c Claim) (*jevapi.Evaluation, error) {
			c.Context += "\nCurrent user: " + tc.user
			out, err := client.Evaluate(context.Background(), map[string]Claim{"runtime": c})
			if err != nil {
				return nil, err
			}
			return out.Values["runtime"], nil
		}, nil)
		if err != nil || result[report] != tc.want {
			t.Fatalf("result=%v err=%v", result, err)
		}
	}
}

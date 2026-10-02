package jev

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

// Diagnostic provider regression, not a performance/automatic-compilation test.
// A synthetic tool exposes facts and candidates; only the real JEV judges them.
func TestLiveReflexGenerationBoundary(t *testing.T) {
	if os.Getenv("JEV_BENCH_LIVE") != "1" {
		t.Skip("set JEV_BENCH_LIVE=1 and TYPESAFE_API_KEY for paid JEV regression")
	}
	key := os.Getenv("TYPESAFE_API_KEY")
	if key == "" {
		t.Fatal("TYPESAFE_API_KEY required")
	}
	client := jevapi.New(key, os.Getenv("JEV_BENCH_MODEL"), 10*time.Second)
	t.Cleanup(client.Close)
	for _, tc := range []struct {
		name, prompt, observed, want string
		fill                         bool
	}{
		{"missing_binding", "Enter the reference violet-42 into the Reference field, continue, and report the receipt.", `{"text":"Enter reference then Continue","controls":[{"name":"Reference","tag":"input","value":"","required":false,"invalid":false},{"name":"Continue","tag":"button","disabled":false}]}`, Defer, false},
		{"renamed_missing_binding", "Fill the Comment with review-ready, then submit the form and report its confirmation.", `{"text":"Comment (optional), Continue","controls":[{"name":"Continue","tag":"button","disabled":false},{"name":"Comment","tag":"textarea","value":"","required":false,"invalid":false}]}`, Defer, false},
		{"bound_value", "Enter the reference violet-42 into the Reference field, continue, and report the receipt.", `{"text":"Enter reference then Continue","controls":[{"name":"Reference","tag":"input","value":"","required":false,"invalid":false},{"name":"Continue","tag":"button","disabled":false}]}`, "page/fill", true},
		{"filled_value", "Enter the reference violet-42 into the Reference field, continue, and report the receipt.", `{"text":"Enter reference then Continue","controls":[{"name":"Reference","tag":"input","value":"violet-42","required":false,"invalid":false},{"name":"Continue","tag":"button","disabled":false}]}`, "page/advance", false},
		{"no_current_field", "Complete the wizard using Continue. Enter reference violet-42 when the form requests it.", `{"text":"Stage 3 of 6. Continue","controls":[{"name":"Continue","tag":"button","disabled":false}]}`, "page/advance", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, _ := testInstallation(t, Config{Mode: "auto"}, client, coretool.Command{
				Name: "page", Run: func(context.Context, *coretool.Execution) (any, error) {
					t.Fatal("diagnostic must not execute tools")
					return nil, nil
				},
			})
			calls := map[string]string{"page/advance": "page click Continue", "page/wait": "page wait"}
			if tc.fill {
				calls["page/fill"] = "page fill Reference violet-42"
			}
			scene := Reflex{When: "The user needs to operate the current page.", Decide: "Use current state and candidates to fulfill the user's page task. Respect prerequisites and report only observed completion.", Observe: constantObserve(tc.observed, calls)}
			if err := scene.validate(); err != nil {
				t.Fatal(err)
			}
			observed := e.observe(t.Context(), cfg, []*aop.Message{provider.TextMessage("user", tc.prompt)}, &scene)
			if observed == nil {
				t.Fatal("observation failed")
			}
			_, selected, err := e.decide(t.Context(), observed, map[string]bool{}, &scene, "regression", "task")
			matches := selected == tc.want || (tc.want != Defer && strings.HasSuffix(selected, "/"+tc.want))
			if err != nil || !matches {
				audit, _ := os.ReadFile(filepath.Join(e.config.Directory, "decisions.jsonl"))
				t.Logf("judgments: %s", audit)
				t.Fatalf("selected=%q want=%q err=%v", selected, tc.want, err)
			}
		})
	}
	t.Logf("provider usage: %+v", client.Usage())
}

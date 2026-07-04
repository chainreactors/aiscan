package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
)

// fakeProvider returns canned tool calls keyed by the tool offered in the
// request, so one instance can stand in for the compile, judge, and count calls.
type fakeProvider struct {
	assertions  []Assertion
	judgePass   bool
	judgeReason string
	count       int
	calls       int
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) ChatCompletion(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	f.calls++
	tool := ""
	if len(req.Tools) > 0 {
		tool = req.Tools[0].Function.Name
	}
	var name, args string
	switch tool {
	case "emit_assertions":
		name = tool
		b, _ := json.Marshal(map[string]any{"assertions": f.assertions})
		args = string(b)
	case "judge":
		name = tool
		b, _ := json.Marshal(map[string]any{"pass": f.judgePass, "reason": f.judgeReason})
		args = string(b)
	case "report_count":
		name = tool
		b, _ := json.Marshal(map[string]any{"count": f.count})
		args = string(b)
	default:
		return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: provider.ChatMessage{}}}}, nil
	}
	return &provider.ChatCompletionResponse{Choices: []provider.Choice{{
		Message: provider.ChatMessage{ToolCalls: []provider.ToolCall{{
			ID: "1", Type: "function",
			Function: provider.FunctionCall{Name: name, Arguments: args},
		}}},
	}}}, nil
}

func findingsDoc(n int) string {
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&sb, "## Finding %d\nSeverity: high\nDetails here.\n\n", i)
	}
	return sb.String()
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestCheckCountDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "redhaze_findings.md", findingsDoc(72))
	e := &Evaluator{cfg: Config{WorkDir: dir}} // no provider: purely deterministic

	pass := []Assertion{{Kind: AssertCount, Label: "findings", Path: "redhaze_findings.md", Pattern: "^## ", Op: ">=", Value: 70}}
	if v := e.checkAssertions(context.Background(), "goal", "", "", pass); !v.Pass {
		t.Fatalf("expected pass with 72>=70, got fail: %+v", v)
	}

	fail := []Assertion{{Kind: AssertCount, Label: "findings", Path: "redhaze_findings.md", Pattern: "^## ", Op: ">=", Value: 80}}
	v := e.checkAssertions(context.Background(), "goal", "", "", fail)
	if v.Pass {
		t.Fatalf("expected fail with 72<80")
	}
	if !strings.Contains(v.Feedback, "found 72") || !strings.Contains(v.Feedback, "need >= 80") {
		t.Fatalf("feedback should pinpoint the shortfall, got: %q", v.Feedback)
	}
}

func TestCheckExists(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "report.md", "hello")
	e := &Evaluator{cfg: Config{WorkDir: dir}}

	if v := e.checkExists(Assertion{Kind: AssertExists, Path: "report.md"}); !v.pass {
		t.Fatalf("expected report.md to exist")
	}
	if v := e.checkExists(Assertion{Kind: AssertExists, Path: "missing.md"}); v.pass {
		t.Fatalf("expected missing.md to be absent")
	}
	// glob support
	if v := e.checkExists(Assertion{Kind: AssertExists, Path: "*.md"}); !v.pass {
		t.Fatalf("expected *.md glob to match report.md")
	}
}

func TestResolvePathsRejectsOutsideWorkDir(t *testing.T) {
	parent := t.TempDir()
	workDir := filepath.Join(parent, "work")
	if err := os.Mkdir(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir: %v", err)
	}
	writeFile(t, parent, "secret.txt", "outside")
	writeFile(t, workDir, "inside.txt", "inside")

	e := &Evaluator{cfg: Config{WorkDir: workDir}}
	if got := e.resolvePaths("inside.txt"); len(got) != 1 {
		t.Fatalf("expected inside file to resolve, got %v", got)
	}
	if got := e.resolvePaths("../secret.txt"); len(got) != 0 {
		t.Fatalf("relative escape should not resolve, got %v", got)
	}
	if got := e.resolvePaths(filepath.Join(parent, "secret.txt")); len(got) != 0 {
		t.Fatalf("absolute outside path should not resolve, got %v", got)
	}
}

func TestCheckCountJudgeFallbackOnWrongPattern(t *testing.T) {
	dir := t.TempDir()
	// File is non-empty but uses a format the pattern won't match.
	writeFile(t, dir, "findings.txt", "1) alpha\n2) beta\n3) gamma\n")
	fp := &fakeProvider{count: 50}
	e := &Evaluator{cfg: Config{WorkDir: dir, Provider: fp}}

	a := Assertion{Kind: AssertCount, Label: "findings", Path: "findings.txt", Pattern: "^## ", Op: ">=", Value: 70}
	v := e.checkAssertions(context.Background(), "goal", "", "", []Assertion{a})
	if v.Pass {
		t.Fatalf("expected fail: judge counted 50 < 70")
	}
	if !strings.Contains(v.Feedback, "counted by judge") {
		t.Fatalf("expected judge-fallback marker in feedback, got: %q", v.Feedback)
	}
	if fp.calls != 1 {
		t.Fatalf("expected exactly one fallback LLM call, got %d", fp.calls)
	}
}

func TestCheckCountNoFallbackWhenPatternMatches(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f.md", findingsDoc(5))
	fp := &fakeProvider{count: 999}
	e := &Evaluator{cfg: Config{WorkDir: dir, Provider: fp}}

	a := Assertion{Kind: AssertCount, Path: "f.md", Pattern: "^## ", Op: ">=", Value: 5}
	if v := e.checkAssertions(context.Background(), "goal", "", "", []Assertion{a}); !v.Pass {
		t.Fatalf("expected deterministic pass")
	}
	if fp.calls != 0 {
		t.Fatalf("deterministic count must not call the LLM, got %d calls", fp.calls)
	}
}

func TestEvaluateStructuredCountPasses(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "redhaze_findings.md", findingsDoc(72))
	fp := &fakeProvider{assertions: []Assertion{
		{Kind: AssertCount, Label: "findings", Path: "redhaze_findings.md", Pattern: "^## ", Op: ">=", Value: 70},
	}}
	e := New(Config{Provider: fp, Model: "test", WorkDir: dir})

	v, err := e.Evaluate(context.Background(), "pentest the target", "find at least 70 vulnerabilities", nil, "done", 1, 10)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !v.Pass {
		t.Fatalf("72 findings should satisfy >=70; got fail: %+v", v)
	}
	if !v.InheritContext {
		t.Fatalf("low context usage should inherit context")
	}
	if fp.calls != 1 {
		t.Fatalf("expected 1 LLM call (compile only; count is deterministic), got %d", fp.calls)
	}
}

func TestEvaluateStructuredJudgeFails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "report.md", "auth looks weak")
	fp := &fakeProvider{
		assertions:  []Assertion{{Kind: AssertJudge, Label: "coverage", Question: "Does the report cover authz?", Evidence: []string{"report.md"}}},
		judgePass:   false,
		judgeReason: "report omits the authorization section",
	}
	e := New(Config{Provider: fp, Model: "test", WorkDir: dir})

	v, err := e.Evaluate(context.Background(), "audit", "report must cover authz", nil, "done", 1, 10)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if v.Pass {
		t.Fatalf("judge said fail; verdict should not pass")
	}
	if !strings.Contains(v.Feedback, "omits the authorization section") {
		t.Fatalf("feedback should carry the judge reason, got: %q", v.Feedback)
	}
	if fp.calls != 2 {
		t.Fatalf("expected 2 LLM calls (compile + judge), got %d", fp.calls)
	}
}

func TestCompileAssertionsCached(t *testing.T) {
	dir := t.TempDir()
	fp := &fakeProvider{assertions: []Assertion{{Kind: AssertExists, Label: "x", Path: "a.md"}}}
	e := New(Config{Provider: fp, Model: "test", WorkDir: dir})

	for i := 0; i < 3; i++ {
		if _, err := e.compileAssertions(context.Background(), "g", "c"); err != nil {
			t.Fatalf("compile: %v", err)
		}
	}
	if fp.calls != 1 {
		t.Fatalf("expected compilation cached after first call, got %d calls", fp.calls)
	}
}

func TestNormalizeAssertionsDropsInvalid(t *testing.T) {
	in := []Assertion{
		{Kind: AssertCount, Path: "", Value: 1},     // no path -> dropped
		{Kind: AssertCount, Path: "zero.md"},        // no positive threshold -> dropped
		{Kind: AssertCount, Path: "a.md", Value: 1}, // ok, op defaulted
		{Kind: "bogus", Label: "x"},                 // unknown kind -> dropped
		{Kind: AssertJudge, Question: ""},           // no question -> dropped
		{Kind: AssertExists, Path: "b.md"},          // ok
	}
	out := normalizeAssertions(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 valid assertions, got %d: %+v", len(out), out)
	}
	if out[0].Op != ">=" {
		t.Fatalf("count op should default to >=, got %q", out[0].Op)
	}
}

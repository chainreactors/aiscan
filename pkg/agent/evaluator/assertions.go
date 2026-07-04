package evaluator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
	"github.com/chainreactors/aiscan/pkg/agent/truncate"
)

// AssertionKind enumerates the machine-checkable clause types the criteria
// compiler emits. count/exists resolve deterministically against files in the
// agent's working directory — no LLM involved; judge defers a genuinely
// qualitative question to the LLM, but always grounded in real evidence.
type AssertionKind string

const (
	AssertCount  AssertionKind = "count"
	AssertExists AssertionKind = "exists"
	AssertJudge  AssertionKind = "judge"
)

// Assertion is one checkable clause compiled from free-text acceptance criteria.
type Assertion struct {
	Kind  AssertionKind `json:"kind"`
	Label string        `json:"label,omitempty"`

	// count, exists: a file path or glob, relative to the agent working dir.
	Path string `json:"path,omitempty"`

	// count: each line matching Pattern (a Go regexp) is one item; Op/Value form
	// the threshold, e.g. Op=">=" Value=70.
	Pattern string `json:"pattern,omitempty"`
	Op      string `json:"op,omitempty"`
	Value   int    `json:"value,omitempty"`

	// judge: a yes/no question plus the files to read as evidence.
	Question string   `json:"question,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

const (
	maxDigestBytes    = 6000
	maxEvidenceBytes  = 12000
	maxFileHeadBytes  = 1500
	maxJudgeInputSize = 16000
	maxDigestFileSize = 5 << 20
)

// digestTextExt lists extensions worth sampling into the compiler digest.
var digestTextExt = map[string]bool{
	".md": true, ".txt": true, ".json": true, ".jsonl": true, ".csv": true,
	".log": true, ".yaml": true, ".yml": true, ".html": true, ".xml": true,
}

type assertionOutcome struct {
	pass   bool
	detail string
}

// workDir is the root that file-based assertions resolve against, falling back
// to the process working directory (which is where the agent's tools write).
func (e *Evaluator) workDir() string {
	if e.cfg.WorkDir != "" {
		return e.cfg.WorkDir
	}
	wd, _ := os.Getwd()
	return wd
}

// compileAssertions turns free-text criteria into checkable assertions, caching
// the result per (goal, criteria) so repeated eval rounds don't recompile.
func (e *Evaluator) compileAssertions(ctx context.Context, goal, criteria string) ([]Assertion, error) {
	key := goal + "\x00" + criteria

	e.mu.Lock()
	if cached, ok := e.compileCache[key]; ok {
		e.mu.Unlock()
		return cached, nil
	}
	e.mu.Unlock()

	assertions, err := e.compileAssertionsLLM(ctx, goal, criteria)
	if err != nil {
		return nil, err
	}

	e.mu.Lock()
	if e.compileCache == nil {
		e.compileCache = make(map[string][]Assertion)
	}
	e.compileCache[key] = assertions
	e.mu.Unlock()
	return assertions, nil
}

const compileSystemPrompt = `You convert a free-text acceptance criteria into a small set of machine-checkable assertions, then call the "emit_assertions" tool. No prose.

An autonomous agent works in a working directory and writes its results to files there. Your assertions are checked against those files. A digest of the current directory (file names + heads) is provided — use it to pick real paths and accurate patterns.

Assertion kinds:
	- "count": a countable threshold. Fields: path (a real file or glob from the digest, e.g. "redhaze_findings.md"), pattern (a Go regexp; each LINE matching it counts as one item — infer it from the file head, e.g. "^#{1,6} " for markdown headings or "^\\s*\\d+[.)]" for numbered lists), op (one of ">=", ">", "==", "<=", "<"), value (required positive integer threshold). Use this for "at least N findings / vulnerabilities / items".
- "exists": a required artifact. Fields: path (file or glob).
- "judge": a genuinely qualitative requirement that cannot be counted or checked mechanically. Fields: question (a yes/no question), evidence (list of files to read).

Rules:
- STRONGLY prefer count/exists over judge whenever the criteria is about quantity or the presence of an artifact. Reserve judge for quality/coverage that truly needs reading.
- Give every assertion a short human-readable "label".
- Emit 1-4 assertions total; each must be independently checkable.
- If the criteria is purely qualitative and references no artifact, emit a single judge assertion with empty evidence.`

var emitAssertionsTool = provider.ToolDefinition{
	Type: "function",
	Function: provider.FunctionDefinition{
		Name:        "emit_assertions",
		Description: "Submit the compiled assertion list",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"assertions": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"kind":     map[string]interface{}{"type": "string", "enum": []string{"count", "exists", "judge"}},
							"label":    map[string]interface{}{"type": "string", "description": "short human-readable name"},
							"path":     map[string]interface{}{"type": "string", "description": "file or glob for count/exists"},
							"pattern":  map[string]interface{}{"type": "string", "description": "per-line Go regexp for count"},
							"op":       map[string]interface{}{"type": "string", "enum": []string{">=", ">", "==", "<=", "<"}},
							"value":    map[string]interface{}{"type": "integer"},
							"question": map[string]interface{}{"type": "string", "description": "yes/no question for judge"},
							"evidence": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "files to read for judge"},
						},
						"required": []string{"kind", "label"},
					},
				},
			},
			"required": []string{"assertions"},
		},
	},
}

func (e *Evaluator) compileAssertionsLLM(ctx context.Context, goal, criteria string) ([]Assertion, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Goal\n%s\n\n## Acceptance Criteria\n%s\n\n## Working directory digest\n%s\n",
		goal, criteria, e.workdirDigest())

	temp := float64(0)
	resp, err := e.cfg.Provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model: e.cfg.Model,
		Messages: []provider.ChatMessage{
			provider.NewTextMessage("system", compileSystemPrompt),
			provider.NewTextMessage("user", sb.String()),
		},
		Tools:       []provider.ToolDefinition{emitAssertionsTool},
		MaxTokens:   2048,
		Temperature: &temp,
	})
	if err != nil {
		return nil, fmt.Errorf("compile criteria: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("compile criteria: no choices returned")
	}
	for _, tc := range resp.Choices[0].Message.ToolCalls {
		if tc.Function.Name != "emit_assertions" {
			continue
		}
		var payload struct {
			Assertions []Assertion `json:"assertions"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &payload); err != nil {
			return nil, fmt.Errorf("compile criteria: unmarshal: %w", err)
		}
		return normalizeAssertions(payload.Assertions), nil
	}
	return nil, fmt.Errorf("compile criteria: model did not call emit_assertions")
}

// normalizeAssertions defaults and drops structurally invalid assertions so the
// checker can trust every clause it receives.
func normalizeAssertions(in []Assertion) []Assertion {
	out := make([]Assertion, 0, len(in))
	for _, a := range in {
		a.Kind = AssertionKind(strings.ToLower(strings.TrimSpace(string(a.Kind))))
		switch a.Kind {
		case AssertCount:
			if a.Op == "" {
				a.Op = ">="
			}
			if strings.TrimSpace(a.Path) == "" || a.Value <= 0 {
				continue
			}
		case AssertExists:
			if strings.TrimSpace(a.Path) == "" {
				continue
			}
		case AssertJudge:
			if strings.TrimSpace(a.Question) == "" {
				continue
			}
		default:
			continue
		}
		out = append(out, a)
	}
	return out
}

// workdirDigest returns a compact listing of the working directory plus heads of
// small text result files, to ground the compiler's path/pattern choices. Best
// effort: returns a placeholder on any error, never blocks evaluation.
func (e *Evaluator) workdirDigest() string {
	dir := e.workDir()
	if dir == "" {
		return "(unknown)"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "(unreadable)"
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var list, heads strings.Builder
	for _, ent := range entries {
		if ent.IsDir() || strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		fmt.Fprintf(&list, "- %s (%d bytes)\n", ent.Name(), info.Size())

		ext := strings.ToLower(filepath.Ext(ent.Name()))
		if !digestTextExt[ext] || info.Size() == 0 || info.Size() > maxDigestFileSize || heads.Len() >= maxDigestBytes {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			continue
		}
		fmt.Fprintf(&heads, "\n### %s (head)\n%s\n", ent.Name(), truncate.Clip(string(data), maxFileHeadBytes))
	}

	digest := list.String()
	if heads.Len() > 0 {
		digest += "\nFile heads:" + heads.String()
	}
	if strings.TrimSpace(digest) == "" {
		return "(empty directory)"
	}
	return truncate.Clip(digest, maxDigestBytes)
}

// checkAssertions evaluates every assertion and composes a Verdict. Deterministic
// assertions (count/exists) never call the LLM; judge assertions defer to a
// grounded yes/no sub-call. The verdict passes only if every assertion holds.
func (e *Evaluator) checkAssertions(ctx context.Context, goal, trace, output string, assertions []Assertion) *Verdict {
	outcomes := make([]assertionOutcome, 0, len(assertions))
	for _, a := range assertions {
		switch a.Kind {
		case AssertCount:
			outcomes = append(outcomes, e.checkCount(ctx, a))
		case AssertExists:
			outcomes = append(outcomes, e.checkExists(a))
		case AssertJudge:
			outcomes = append(outcomes, e.checkJudge(ctx, goal, trace, output, a))
		}
	}

	var passLines, failLines []string
	for _, o := range outcomes {
		if o.pass {
			passLines = append(passLines, "✓ "+o.detail)
		} else {
			failLines = append(failLines, "✗ "+o.detail)
		}
	}

	v := &Verdict{Pass: len(failLines) == 0}
	if v.Pass {
		v.Reason = fmt.Sprintf("all %d acceptance checks passed", len(outcomes))
		return v
	}

	v.Reason = fmt.Sprintf("%d of %d acceptance checks failed", len(failLines), len(outcomes))
	var fb strings.Builder
	fb.WriteString("Not all acceptance checks passed. Address these, then continue:\n")
	for _, l := range failLines {
		fb.WriteString("  " + l + "\n")
	}
	if len(passLines) > 0 {
		fb.WriteString("Already satisfied:\n")
		for _, l := range passLines {
			fb.WriteString("  " + l + "\n")
		}
	}
	v.Feedback = strings.TrimRight(fb.String(), "\n")
	return v
}

func (e *Evaluator) checkCount(ctx context.Context, a Assertion) assertionOutcome {
	label := a.Label
	if label == "" {
		label = "count " + a.Path
	}
	op := a.Op
	if op == "" {
		op = ">="
	}

	files := e.resolvePaths(a.Path)
	if len(files) == 0 {
		return assertionOutcome{false, fmt.Sprintf("%s: no file matches %q", label, a.Path)}
	}

	var re *regexp.Regexp
	if a.Pattern != "" {
		if compiled, err := regexp.Compile(a.Pattern); err == nil {
			re = compiled
		}
	}

	total := 0
	nonEmpty := false
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if len(bytes.TrimSpace(data)) > 0 {
			nonEmpty = true
		}
		if re != nil {
			for _, line := range strings.Split(string(data), "\n") {
				if re.MatchString(line) {
					total++
				}
			}
		}
	}

	// A missing/invalid pattern, or a pattern that matched nothing in a non-empty
	// file (a wrong guess), would let a mere formatting mismatch masquerade as
	// "not done". Fall back to an LLM count so real work still passes.
	estimated := false
	if (re == nil || (total == 0 && nonEmpty)) && e.cfg.Provider != nil {
		if n, err := e.countByJudge(ctx, files); err == nil {
			total = n
			estimated = true
		}
	}

	detail := fmt.Sprintf("%s: found %d (need %s %d)", label, total, op, a.Value)
	if estimated {
		detail += " [counted by judge]"
	}
	return assertionOutcome{compareInt(total, op, a.Value), detail}
}

func (e *Evaluator) checkExists(a Assertion) assertionOutcome {
	label := a.Label
	if label == "" {
		label = "exists " + a.Path
	}
	files := e.resolvePaths(a.Path)
	if len(files) > 0 {
		return assertionOutcome{true, fmt.Sprintf("%s: present (%s)", label, filepath.Base(files[0]))}
	}
	return assertionOutcome{false, fmt.Sprintf("%s: missing (%s)", label, a.Path)}
}

func (e *Evaluator) checkJudge(ctx context.Context, goal, trace, output string, a Assertion) assertionOutcome {
	label := a.Label
	if label == "" {
		label = "judge"
	}
	if e.cfg.Provider == nil {
		return assertionOutcome{false, fmt.Sprintf("%s: no evaluator provider available", label)}
	}
	pass, reason, err := e.judgeYesNo(ctx, goal, trace, output, a)
	if err != nil {
		return assertionOutcome{false, fmt.Sprintf("%s: could not evaluate (%s)", label, err)}
	}
	return assertionOutcome{pass, fmt.Sprintf("%s: %s", label, reason)}
}

// resolvePaths turns a path or glob (relative to the work dir) into the set of
// existing regular files it matches. Model-compiled paths are untrusted, so every
// resolved file must stay inside the evaluator work dir after symlink resolution.
func (e *Evaluator) resolvePaths(pathOrGlob string) []string {
	p := strings.TrimSpace(pathOrGlob)
	if p == "" {
		return nil
	}
	root := safeRoot(e.workDir())
	if root == "" {
		return nil
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	matches, err := filepath.Glob(p)
	if err != nil || len(matches) == 0 {
		matches = []string{p}
	}

	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if safe, ok := safeResolvedFile(root, match); ok {
			out = append(out, safe)
		}
	}
	sort.Strings(out)
	return out
}

func safeRoot(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return filepath.Clean(abs)
}

func safeResolvedFile(root, path string) (string, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return "", false
	}
	real := abs
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		real = resolved
	}
	real = filepath.Clean(real)
	if !pathInside(root, real) {
		return "", false
	}
	return real, true
}

func pathInside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func (e *Evaluator) readEvidence(refs []string) string {
	var sb strings.Builder
	budget := maxEvidenceBytes
	for _, ref := range refs {
		if budget <= 0 {
			break
		}
		for _, f := range e.resolvePaths(ref) {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			chunk := truncate.Clip(string(data), budget)
			fmt.Fprintf(&sb, "\n--- %s ---\n%s\n", filepath.Base(f), chunk)
			budget -= len(chunk)
			if budget <= 0 {
				break
			}
		}
	}
	return sb.String()
}

const judgeSystemPrompt = `You verify ONE sub-requirement of a task by reading the provided evidence. Call the "judge" tool. No prose.
- pass=true only if the evidence clearly supports the requirement.
- If the evidence is missing or insufficient, pass=false.
- reason: one concise sentence citing what you saw.`

var judgeTool = provider.ToolDefinition{
	Type: "function",
	Function: provider.FunctionDefinition{
		Name:        "judge",
		Description: "Submit the yes/no judgement for one requirement",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pass":   map[string]interface{}{"type": "boolean"},
				"reason": map[string]interface{}{"type": "string"},
			},
			"required": []string{"pass", "reason"},
		},
	},
}

func (e *Evaluator) judgeYesNo(ctx context.Context, goal, trace, output string, a Assertion) (bool, string, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Task goal\n%s\n\n## Requirement to verify\n%s\n", goal, a.Question)
	if ev := e.readEvidence(a.Evidence); ev != "" {
		fmt.Fprintf(&sb, "\n## Evidence files\n%s\n", ev)
	}
	if trace != "" {
		fmt.Fprintf(&sb, "\n## Execution summary\n%s\n", truncate.Clip(trace, 4000))
	}
	if output = strings.TrimSpace(output); output != "" {
		fmt.Fprintf(&sb, "\n## Final output\n%s\n", truncate.Clip(output, 3000))
	}

	temp := float64(0)
	resp, err := e.cfg.Provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model: e.cfg.Model,
		Messages: []provider.ChatMessage{
			provider.NewTextMessage("system", judgeSystemPrompt),
			provider.NewTextMessage("user", sb.String()),
		},
		Tools:       []provider.ToolDefinition{judgeTool},
		MaxTokens:   1024,
		Temperature: &temp,
	})
	if err != nil {
		return false, "", err
	}
	if len(resp.Choices) == 0 {
		return false, "", fmt.Errorf("no choices returned")
	}
	for _, tc := range resp.Choices[0].Message.ToolCalls {
		if tc.Function.Name == "judge" {
			var out struct {
				Pass   bool   `json:"pass"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &out); err != nil {
				return false, "", err
			}
			return out.Pass, out.Reason, nil
		}
	}
	return false, "", fmt.Errorf("model did not call judge tool")
}

const countSystemPrompt = `You count the number of distinct items (findings, vulnerabilities, entries) described in the content. Call the "report_count" tool with your best integer count. No prose.`

var countTool = provider.ToolDefinition{
	Type: "function",
	Function: provider.FunctionDefinition{
		Name:        "report_count",
		Description: "Report how many distinct items the content contains",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"count": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"count"},
		},
	},
}

// countByJudge estimates item count via the LLM — a robustness net used only when
// a deterministic pattern is unavailable or clearly wrong.
func (e *Evaluator) countByJudge(ctx context.Context, files []string) (int, error) {
	var sb strings.Builder
	budget := maxJudgeInputSize
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		chunk := truncate.Clip(string(data), budget)
		sb.WriteString(chunk)
		budget -= len(chunk)
		if budget <= 0 {
			break
		}
	}
	if strings.TrimSpace(sb.String()) == "" {
		return 0, fmt.Errorf("no content to count")
	}

	temp := float64(0)
	resp, err := e.cfg.Provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model: e.cfg.Model,
		Messages: []provider.ChatMessage{
			provider.NewTextMessage("system", countSystemPrompt),
			provider.NewTextMessage("user", sb.String()),
		},
		Tools:       []provider.ToolDefinition{countTool},
		MaxTokens:   256,
		Temperature: &temp,
	})
	if err != nil {
		return 0, err
	}
	if len(resp.Choices) == 0 {
		return 0, fmt.Errorf("no choices returned")
	}
	for _, tc := range resp.Choices[0].Message.ToolCalls {
		if tc.Function.Name == "report_count" {
			var out struct {
				Count int `json:"count"`
			}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &out); err != nil {
				return 0, err
			}
			return out.Count, nil
		}
	}
	return 0, fmt.Errorf("model did not call report_count")
}

func compareInt(got int, op string, want int) bool {
	switch op {
	case ">":
		return got > want
	case "==":
		return got == want
	case "<=":
		return got <= want
	case "<":
		return got < want
	default: // ">="
		return got >= want
	}
}

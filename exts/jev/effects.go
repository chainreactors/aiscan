package jev

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

type effectRecord struct {
	Call     NativeCall     `json:"call"`
	Result   map[string]any `json:"result,omitempty"`
	State    string         `json:"state"`
	Contract string         `json:"contract,omitempty"`
}
type effectLedger struct {
	mu      sync.Mutex
	records map[string]*effectRecord
}

func newEffectLedger() *effectLedger            { return &effectLedger{records: map[string]*effectRecord{}} }
func effectID(task string, c NativeCall) string { return digest([]any{task, c.Step, c.Occurrence}) }
func logicalBinding(c NativeCall) string {
	if len(c.Argv) > 0 {
		var args map[string]any
		d := json.NewDecoder(bytes.NewReader(c.Arguments))
		d.UseNumber()
		_ = d.Decode(&args)
		delete(args, "command")
		return digest([]any{c.Name, args, c.Argv})
	}
	return c.canonical()
}
func (l *effectLedger) reserve(task string, c NativeCall, contracts ...string) (map[string]any, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	id := effectID(task, c)
	contract := ""
	if len(contracts) > 0 {
		contract = contracts[0]
	}
	if old := l.records[id]; old != nil {
		if logicalBinding(old.Call) != logicalBinding(c) || (contract != "" && old.Contract != "" && contract != old.Contract) {
			return nil, false, handoffError{"effect_binding_conflict: logical effect parameters changed"}
		}
		if old.State != "returned" && old.Result == nil {
			return nil, false, handoffError{"effect_unknown: prior effect outcome unknown; inspect it before continuing"}
		}
		return cloneJSONMap(old.Result), true, nil
	}
	for _, old := range l.records {
		if old.State == "unknown" || old.State == "executing" {
			return nil, false, handoffError{"effect_unknown: unresolved prior effect blocks new mutations"}
		}
	}
	l.records[id] = &effectRecord{Call: c, State: "executing", Contract: contract}
	return nil, false, nil
}
func (l *effectLedger) complete(task string, c NativeCall, result map[string]any, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	record := l.records[effectID(task, c)]
	if record == nil {
		return
	}
	record.Result = cloneJSONMap(result)
	record.Call.ID = c.ID
	record.State = "unknown"
	if err == nil && result != nil && result["is_error"] != true {
		record.State = "returned"
	}
}
func (l *effectLedger) classifyOutcome(task string, c NativeCall, s nativeSnapshot, result map[string]any, contractID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.records[effectID(task, c)]
	if r == nil {
		return
	}
	r.Contract = contractID
	known := false
	if contract, ok := s.Contracts[contractID]; ok && result != nil {
		a, err := contract.Classify(coretool.NativeCall(c))
		if err == nil && a == EffectAccess && contract.Outcome != nil {
			outcome := contract.Outcome(coretool.NativeCall(c), cloneJSONMap(result))
			if outcome == "applied" || outcome == "not_applied" || outcome == "pending" {
				known = true
			}
		}
	}
	if known {
		r.State = "returned"
	} else {
		r.State = "unknown"
	}
}

func (l *effectLedger) reconcile(s nativeSnapshot, read NativeCall, result map[string]any) {
	if result == nil || result["is_error"] == true {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.records {
		if record.State != "unknown" {
			continue
		}
		for id, c := range s.Contracts {
			if record.Contract != "" && record.Contract != id {
				continue
			}
			if c.Resolve != nil && c.Resolve(coretool.NativeCall(record.Call), coretool.NativeCall(read), cloneJSONMap(result)) {
				record.Result = cloneJSONMap(result)
				record.State = "returned"
				break
			}
		}
	}
}
func (l *effectLedger) summary() map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]any{}
	for id, r := range l.records {
		out[id] = map[string]any{"step": r.Call.Step, "occurrence": r.Call.Occurrence, "state": r.State}
	}
	return out
}

func (l *effectLedger) unresolved() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.records {
		if r.State == "unknown" || r.State == "executing" {
			return true
		}
	}
	return false
}

// command(name, argv) is a structured value until the host encodes it. It is
// only valid in bash.arguments.command; arbitrary command strings stay opaque.
func prepareBinding(c NativeCall) (NativeCall, error) {
	c.ID = ""    // Invocation identity is assigned by the host, never generated code.
	c.Argv = nil // Generated metadata must never override the actual native call.
	if c.Name != "bash" {
		return c, nil
	}
	var args map[string]any
	d := json.NewDecoder(bytes.NewReader(c.Arguments))
	d.UseNumber()
	if err := d.Decode(&args); err != nil {
		return c, err
	}
	obj, structured := args["command"].(map[string]any)
	if !structured {
		if script, ok := args["command"].(string); ok {
			c.Argv = literalCommand(script)
		}
		return c, nil
	}
	name, ok := obj["name"].(string)
	if !ok || strings.TrimSpace(name) == "" || strings.ContainsRune(name, 0) {
		return c, errors.New("command needs a program name")
	}
	items, ok := obj["argv"].([]any)
	if !ok {
		return c, errors.New("command argv must be an array")
	}
	argv := []string{name}
	parts := []string{}
	for _, v := range items {
		s, ok := v.(string)
		if !ok || strings.ContainsRune(s, 0) {
			return c, errors.New("command argv must contain strings without NUL")
		}
		argv = append(argv, s)
	}
	for _, v := range argv {
		quoted, err := syntax.Quote(v, syntax.LangBash)
		if err != nil {
			return c, err
		}
		parts = append(parts, quoted)
	}
	args["command"] = strings.Join(parts, " ")
	c.Argv = argv
	c.Arguments, _ = json.Marshal(args)
	return c, nil
}

func literalCommand(text string) []string {
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "")
	if err != nil || len(f.Stmts) != 1 {
		return nil
	}
	s := f.Stmts[0]
	if s.Background || s.Negated || len(s.Redirs) > 0 {
		return nil
	}
	call, ok := s.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) > 0 {
		return nil
	}
	for _, word := range call.Args {
		for _, part := range word.Parts {
			if lit, ok := part.(*syntax.Lit); ok && strings.ContainsAny(lit.Value, "~*?[{") {
				return nil
			}
		}
	}
	safe := true
	syntax.Walk(call, func(n syntax.Node) bool {
		switch n.(type) {
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ExtGlob:
			safe = false
		}
		return safe
	})
	if !safe {
		return nil
	}
	// Match the native Bash interpreter's argument decoding. Literal keeps
	// escape bytes in concatenated quoted words, so e.g. 'owner'\''s' would
	// replay with a different target. Unsafe expansions were rejected above;
	// Fields only removes transport quoting from these literal arguments.
	argv, err := expand.Fields(&expand.Config{}, call.Args...)
	if err != nil {
		return nil
	}
	return argv
}

func validateParameters(r *Reflex, args map[string]any) error {
	if len(r.Parameters) == 0 {
		return nil
	}
	var doc any
	if err := json.Unmarshal(r.Parameters, &doc); err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(localSchemas{})
	if err := compiler.AddResource("urn:jev:parameters", doc); err != nil {
		return err
	}
	schema, err := compiler.Compile("urn:jev:parameters")
	if err != nil {
		return err
	}
	return schema.Validate(cloneJSONMap(args))
}

// Evidence references are resolved by the host, rather than accepted as
// generated claims. Current task semantics are checked by the runtime judge.
func resolveReport(value any, evidence map[string]map[string]any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		if id, ref := v["evidence"].(string); ref {
			if len(v) != 2 {
				return nil, errors.New("evidence reference needs exactly evidence and path")
			}
			result, ok := evidence[id]
			if !ok {
				return nil, errors.New("report references unavailable task evidence")
			}
			path, ok := v["path"].([]any)
			if !ok {
				return nil, errors.New("evidence reference path must be an array")
			}
			var out any = cloneJSONMap(result)
			for _, part := range path {
				switch current := out.(type) {
				case map[string]any:
					key, ok := part.(string)
					if !ok {
						return nil, errors.New("evidence object path requires a string field name")
					}
					out, ok = current[key]
					if !ok {
						return nil, fmt.Errorf("missing evidence field %q", key)
					}
				case []any:
					index := -1
					switch value := part.(type) {
					case int:
						index = value
					case int64:
						if value >= 0 && value < int64(len(current)) {
							index = int(value)
						}
					case float64:
						if value >= 0 && value < float64(len(current)) && value == float64(int(value)) {
							index = int(value)
						}
					case json.Number:
						if value, err := value.Int64(); err == nil && value >= 0 && value < int64(len(current)) {
							index = int(value)
						}
					}
					if index < 0 || index >= len(current) {
						return nil, fmt.Errorf("evidence array path requires an integer index within [0,%d); got %v", len(current), part)
					}
					out = current[index]
				default:
					return nil, errors.New("evidence path cannot traverse a scalar value")
				}
			}
			return out, nil
		}
		out := map[string]any{}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			item, err := resolveReport(v[k], evidence)
			if err != nil {
				return nil, err
			}
			out[k] = item
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			resolved, err := resolveReport(item, evidence)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	default:
		return value, nil
	}
}

func handoffCode(reason, detail string) string {
	if reason == report {
		return report
	}
	for _, pair := range []struct{ match, code string }{{"effect_binding_conflict", "effect_binding_conflict"}, {"effect_unknown", "effect_unknown"}, {"contract_violation", "contract_violation"}, {"new input", "input_updated"}, {"parameters", "missing_input"}, {"budget", "call_limit"}, {"deadline", "task_timeout"}, {"canceled", "canceled"}, {"unavailable", "capability_unavailable"}} {
		if strings.Contains(detail, pair.match) {
			return pair.code
		}
	}
	return reason
}

func argumentError(err error) error { return handoffError{fmt.Sprintf("parameters invalid: %v", err)} }

func cloneEvidence(in map[string]map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for id, value := range in {
		out[id] = cloneJSONMap(value)
	}
	return out
}

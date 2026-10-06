package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	"github.com/chainreactors/cyber/core/decision"
)

type replayResult struct {
	facts      json.RawMessage
	candidates map[string]binding
}
type observationReplay struct {
	reflex       *Reflex
	capabilities map[string]any
	messages     []map[string]any
	omitted      int
	start        int
	cache        map[string]replayResult
}

func newObservationReplay(reflex *Reflex, state json.RawMessage, capabilities map[string]any) (*observationReplay, error) {
	var projection struct {
		Messages []map[string]any `json:"messages"`
		Omitted  int              `json:"omitted_evidence"`
	}
	if err := json.Unmarshal(state, &projection); err != nil {
		return nil, err
	}
	replay := &observationReplay{reflex: reflex, capabilities: capabilities, messages: projection.Messages, omitted: projection.Omitted, cache: map[string]replayResult{}}
	for i, m := range replay.messages {
		if m["role"] == "user" && m["name"] == nil {
			replay.start = i
		}
	}
	return replay, nil
}
func (r *observationReplay) boundaries(recent bool) []int {
	if len(r.messages) == 0 {
		return []int{0}
	}
	result := []int{r.start + 1}
	for i := r.start + 1; i < len(r.messages); i++ {
		if r.messages[i]["call_id"] != nil {
			result = append(result, i+1)
		}
	}
	if len(result) > 16 {
		if recent {
			return append(result[:1], result[len(result)-15:]...)
		}
		return result[:16]
	}
	return result
}
func (r *observationReplay) input(end int, includeOmitted bool) json.RawMessage {
	value := map[string]any{"messages": r.messages[:end]}
	if includeOmitted {
		value["omitted_evidence"] = r.omitted
	}
	data, _ := json.Marshal(value)
	return data
}

var errProbeStop = errors.New("pure probe reached native dispatch")

// Explore finite semantic branches, replaying only exact recorded call/result
// pairs. No provider or native tool is invoked during publication checks.
// The exact same program and bridges are used in foreground execution.
func probeReflexArguments(ctx context.Context, reflex *Reflex, input, args map[string]any, results []map[string]any, allowMissing bool) (json.RawMessage, map[string]binding, error) {
	candidates := map[string]binding{}
	states := []any{}
	queue := []map[string]string{{}}
	visited := map[string]bool{}
	for len(queue) > 0 {
		if len(visited) >= 64 {
			return nil, nil, fmt.Errorf("branch probe exceeds finite budget")
		}
		schedule := queue[0]
		queue = queue[1:]
		key := digest(schedule)
		if visited[key] {
			continue
		}
		visited[key] = true
		prefix := map[string]string{}
		calls, dispatched, cursor := 0, 0, 0
		gap := ""
		var diagnostic *CompilerDiagnostic
		effects := map[string]map[string]any{}
		bindings := map[string]string{}
		evidence := map[string]map[string]any{}
		history, _ := input["history"].([]map[string]any)
		for _, row := range history {
			evidence[fmt.Sprint(row["call_id"])] = row
		}
		alternatives := func(node, chosen string, options []string) {
			for _, option := range options {
				if option == chosen {
					continue
				}
				next := map[string]string{}
				for k, v := range prefix {
					next[k] = v
				}
				next[node] = option
				if !visited[digest(next)] {
					queue = append(queue, next)
				}
			}
			prefix[node] = chosen
		}
		judge := func(claim Claim) (*jevapi.Evaluation, error) {
			calls++
			if calls > maxDecisions {
				return nil, fmt.Errorf("nonterminating decision loop")
			}
			states = append(states, claim)
			node := fmt.Sprint(calls)
			chosen := schedule[node]
			switch claim.Type {
			case jevapi.ClaimChoice:
				if chosen == "" {
					for _, option := range claim.Options {
						if option != Defer {
							chosen = option
							break
						}
					}
				}
				alternatives(node, chosen, claim.Options)
				return &jevapi.Evaluation{Value: &decision.Evaluation_Choice{Choice: chosen}}, nil
			case jevapi.ClaimScore, jevapi.ClaimNoul:
				if chosen == "" {
					chosen = "middle"
				}
				alternatives(node, chosen, []string{"low", "middle", "high"})
				upper := 1.0
				if claim.Type == jevapi.ClaimScore {
					upper = float64(len(claim.Options) - 1)
				}
				value := upper / 2
				if chosen == "low" {
					value = 0
				}
				if chosen == "high" {
					value = upper
				}
				if claim.Type == jevapi.ClaimScore {
					return &jevapi.Evaluation{Value: &decision.Evaluation_Score{Score: value}}, nil
				}
				return &jevapi.Evaluation{Value: &decision.Evaluation_Noul{Noul: value}}, nil
			}
			return nil, fmt.Errorf("unsupported Claim type")
		}
		execute := func(candidate binding) (map[string]any, error) {
			dispatched++
			if dispatched > maxCandidates {
				return nil, fmt.Errorf("native replay exceeds finite budget")
			}
			candidates["b"+digest(candidate)[:16]] = candidate
			id := effectID("probe", candidate)
			if !candidate.Read {
				if previous, ok := bindings[id]; ok && previous != logicalBinding(candidate) {
					return nil, fmt.Errorf("effect binding changed within replay")
				}
				if old := effects[id]; old != nil {
					return cloneJSONMap(old), nil
				}
				bindings[id] = logicalBinding(candidate)
			}
			if cursor < len(results) {
				result := results[cursor]
				args, _ := json.Marshal(result["arguments"])
				if (binding{Name: fmt.Sprint(result["name"]), Arguments: args}).replayKey() == candidate.replayKey() {
					cursor++
					evidence[fmt.Sprint(result["call_id"])] = result
					if !candidate.Read {
						effects[id] = cloneJSONMap(result)
					}
					return result, nil
				}
				gap = fmt.Sprintf("native call %d differs: generated=%s recorded=%s", cursor, clip(candidate.replayKey(), 512), clip((binding{Name: fmt.Sprint(result["name"]), Arguments: args}).replayKey(), 512))
				index := cursor
				diagnostic = &CompilerDiagnostic{Code: "native_call_mismatch", Stage: "replay", Status: "repair", Message: gap, Call: &index, Expected: json.RawMessage((binding{Name: fmt.Sprint(result["name"]), Arguments: args}).replayKey()), Actual: json.RawMessage(candidate.replayKey()), Replayed: cursor, Recorded: len(results), Action: "Compare the decoded argv and every native option. Recover exact current example values with inspect_evidence. Replay can supply only this recorded call's result; a different operation needs its own actual evidence."}
			} else {
				gap = "generated call has no recorded result"
				index := cursor
				diagnostic = &CompilerDiagnostic{Code: "unrecorded_native_call", Stage: "replay", Status: "repair", Message: gap, Call: &index, Actual: json.RawMessage(candidate.replayKey()), Replayed: cursor, Recorded: len(results), Action: "Remove redundant work if current evidence already satisfies the request. If this new operation is necessary, obtain its actual result in an isolated task before qualifying this path; never fabricate the result."}
			}
			return nil, errProbeStop
		}
		output, err := runReflexJS(ctx, reflex, input, args, judge, execute)
		if err != nil && !errors.Is(interruptedCause(err), errProbeStop) {
			return nil, nil, err
		}
		if gap == "" && cursor < len(results) {
			gap = fmt.Sprintf("function returned after %d/%d recorded native results", cursor, len(results))
			index := cursor
			diagnostic = &CompilerDiagnostic{Code: "trajectory_incomplete", Stage: "replay", Status: "repair", Message: gap, Call: &index, Replayed: cursor, Recorded: len(results), Expected: results[cursor], Actual: output, Action: "Continue the capability's required work within the synchronous function. A pending result is not completion: poll the same operation with fresh reads and inspect the required final count/receipt. Returning defer hands control to the main model; it does not schedule another invocation."}
		}
		if gap == "" && err == nil && output != nil && output[Defer] != nil && output["parameters"] == nil {
			gap = "function consumed recorded results but handed off without a completed report"
			diagnostic = &CompilerDiagnostic{Code: "completion_missing", Stage: "completion", Status: "repair", Message: gap, Actual: output, Replayed: cursor, Recorded: len(results), Action: "Use each execute return value to process fresh evidence and finish the promised work within this invocation. context.history is the entry snapshot; it does not gain results during the running function. Returning defer immediately hands control to the main model and cannot qualify as a complete entry replay."}
		}
		states = append(states, map[string]any{"replayed": cursor, "stopped": errors.Is(interruptedCause(err), errProbeStop), "complete": err == nil && cursor == len(results) && output != nil && output[report] != nil && output["parameters"] == nil, "gap": gap, "diagnostic": diagnostic})
		if output != nil {
			if output[report] != nil {
				grounded, err := resolveReport(output[report], evidence)
				if err != nil {
					return nil, nil, fmt.Errorf("report provenance: %w", err)
				}
				// Semantic review receives the same actual value as runtime
				// composition, rather than an unresolved evidence pointer.
				output[report] = grounded
			}
			if output["parameters"] != nil && !allowMissing {
				return nil, nil, fmt.Errorf("compiler requires current example arguments to probe the generated parameterized function")
			}
			states = append(states, map[string]any{"output": output})
		}
	}
	data, _ := json.Marshal(map[string]any{"branches": states})
	return data, candidates, nil
}
func (r *observationReplay) evaluate(ctx context.Context, input json.RawMessage, cache bool) (json.RawMessage, map[string]binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	env, err := observeInput(input, r.capabilities)
	if err != nil {
		return nil, nil, err
	}
	key := digest(env)
	if value, ok := r.cache[key]; ok && cache {
		return value.facts, value.candidates, nil
	}
	results, err := r.resultsAfter(input)
	if err != nil {
		return nil, nil, err
	}
	facts, candidates, err := probeReflexArguments(ctx, r.reflex, env, r.reflex.arguments, results, false)
	if err == nil && cache {
		r.cache[key] = replayResult{facts, candidates}
	}
	return facts, candidates, err
}
func observationWitnesses(ctx context.Context, reflex *Reflex, state json.RawMessage, capabilities map[string]any) ([]map[string]any, error) {
	r, err := newObservationReplay(reflex, state, capabilities)
	if err != nil {
		return nil, err
	}
	return r.witnesses(ctx)
}
func verifyObserve(ctx context.Context, reflex *Reflex, state json.RawMessage, capabilities map[string]any) error {
	r, err := newObservationReplay(reflex, state, capabilities)
	if err != nil {
		return err
	}
	return r.verify(ctx)
}
func (r *observationReplay) witnesses(ctx context.Context) ([]map[string]any, error) {
	rows := []map[string]any{}
	for _, end := range r.boundaries(false) {
		if end == 0 {
			continue
		}
		facts, candidates, err := r.evaluate(ctx, r.input(end, false), true)
		if err != nil {
			return nil, err
		}
		row := map[string]any{"boundary": end, "latest": r.messages[end-1], "state": facts, "candidates": candidates}
		for _, next := range r.messages[end:] {
			if next["calls"] != nil {
				row["next_calls"] = next["calls"]
				break
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func compactWitnesses(witnesses []map[string]any) ([]map[string]any, map[string]binding) {
	rows := []map[string]any{}
	bindings := map[string]binding{}
	for _, witness := range witnesses {
		row := map[string]any{}
		for k, v := range witness {
			row[k] = v
		}
		refs := map[string]string{}
		for id, c := range witness["candidates"].(map[string]binding) {
			ref := "b" + digest(c)
			bindings[ref] = c
			refs[id] = ref
		}
		row["candidates"] = refs
		rows = append(rows, row)
	}
	return rows, bindings
}

func (r *observationReplay) verify(ctx context.Context) error {
	for _, end := range r.boundaries(true) {
		raw := r.input(end, true)
		_, _, err := r.evaluate(ctx, raw, true)
		if err != nil {
			return fmt.Errorf("program at boundary %d: %w", end, err)
		}
		// Test both supplied and absent args. A copied fallback such as
		// args.actor || 'alice' is invisible when full example args are supplied.
		argumentSets := []map[string]any{r.reflex.arguments}
		if len(r.reflex.arguments) > 0 {
			argumentSets = append(argumentSets, nil)
		}
		for _, supplied := range argumentSets {
			if supplied == nil && len(r.reflex.arguments) > 0 {
				env, err := observeInput(raw, r.capabilities)
				if err != nil {
					return err
				}
				results, err := r.resultsAfter(raw)
				if err != nil {
					return err
				}
				_, _, err = probeReflexArguments(ctx, r.reflex, env, nil, results, true)
				if err != nil {
					return fmt.Errorf("program without arguments at boundary %d: %w", end, err)
				}
			}
			// Parameter variants belong to independent tests. Encoded
			// historical commands/results cannot be varied by string replacement.

		}
	}
	return nil
}

// Replay future recorded results, never fabricated success values. A branch
// without matching evidence stops and still receives semantic source review.
func (r *observationReplay) resultsAfter(input json.RawMessage) ([]map[string]any, error) {
	var prefix struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(input, &prefix); err != nil {
		return nil, err
	}
	env, err := observeInput(r.input(len(r.messages), true), r.capabilities)
	if err != nil {
		return nil, err
	}
	completed := map[string]bool{}
	for _, m := range prefix.Messages {
		if id, ok := m["call_id"].(string); ok {
			completed[id] = true
		}
	}
	results := []map[string]any{}
	for _, result := range env["history"].([]map[string]any) {
		if !completed[fmt.Sprint(result["call_id"])] {
			results = append(results, result)
		}
	}
	return results, nil
}

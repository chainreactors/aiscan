package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type replayResult struct {
	facts      json.RawMessage
	candidates map[string]binding
}

// A draft shares successful pure evaluations only for identical observation environments.
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
	r := &observationReplay{reflex: reflex, capabilities: capabilities, messages: projection.Messages, omitted: projection.Omitted, cache: map[string]replayResult{}}
	for i, message := range r.messages {
		if message["role"] == "user" && message["name"] == nil {
			r.start = i
		}
	}
	return r, nil
}

func (r *observationReplay) boundaries(recent bool) []int {
	if len(r.messages) == 0 {
		return []int{0}
	}
	boundaries := []int{r.start + 1}
	for i := r.start + 1; i < len(r.messages); i++ {
		if r.messages[i]["call_id"] != nil {
			boundaries = append(boundaries, i+1)
		}
	}
	if len(boundaries) > 16 {
		if recent {
			return append(boundaries[:1], boundaries[len(boundaries)-15:]...)
		}
		return boundaries[:16]
	}
	return boundaries
}

func (r *observationReplay) input(end int, includeOmitted bool) json.RawMessage {
	input := map[string]any{"messages": r.messages[:end]}
	if includeOmitted {
		input["omitted_evidence"] = r.omitted
	}
	data, _ := json.Marshal(input)
	return data
}

func (r *observationReplay) evaluate(ctx context.Context, input json.RawMessage, cache bool) (json.RawMessage, map[string]binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	env, err := observeInput(input, r.capabilities)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		return nil, nil, err
	}
	key := string(encoded)
	if cached, ok := r.cache[key]; cache && ok {
		return cached.facts, cached.candidates, nil
	}
	if r.reflex.program == nil {
		return nil, nil, fmt.Errorf("scene has no compiled observation")
	}
	facts, candidates, err := r.reflex.observeData(ctx, env, r.capabilities)
	if cache && err == nil {
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
	var witnesses []map[string]any
	for _, end := range r.boundaries(false) {
		if end == 0 {
			continue
		}
		input := r.input(end, false)
		facts, candidates, err := r.evaluate(ctx, input, true)
		if err != nil {
			return nil, err
		}
		witness := map[string]any{"boundary": end, "latest": r.messages[end-1], "state": facts, "candidates": candidates}
		for _, following := range r.messages[end:] {
			if following["calls"] != nil {
				witness["next_calls"] = following["calls"]
				break
			}
		}
		witnesses = append(witnesses, witness)
	}
	return witnesses, nil
}

// Share identical native bindings across actual boundaries. Serialized readers
// can be large; repeating their source adds no review evidence.
func compactWitnesses(witnesses []map[string]any) ([]map[string]any, map[string]binding) {
	rows := make([]map[string]any, 0, len(witnesses))
	bindings := map[string]binding{}
	for _, witness := range witnesses {
		row := make(map[string]any, len(witness))
		for key, value := range witness {
			row[key] = value
		}
		refs := map[string]string{}
		for id, candidate := range witness["candidates"].(map[string]binding) {
			ref := "b" + digest(candidate)
			bindings[ref], refs[id] = candidate, ref
		}
		row["candidates"] = refs
		rows = append(rows, row)
	}
	return rows, bindings
}

func (r *observationReplay) verify(ctx context.Context) error {
	// Task resources are runtime parameters. Reject copied resource/path
	// constants, including regex escapes, unless ordinary documentation defines
	// them as protocol syntax. This check knows no tool or selector format.
	documented, _ := json.Marshal(r.capabilities)
	code := strings.NewReplacer(`\\/`, `/`, `\/`, `/`, `\.`, `.`, `\-`, `-`).Replace(r.reflex.When + r.reflex.Decide + r.reflex.Observe)
	// Opaque identifiers in actual evidence are runtime values too. This
	// covers copied handles/selectors/tokens without knowing any tool syntax.
	opaque := regexp.MustCompile(`[0-9a-fA-F]{12,}`)
	for _, message := range r.messages {
		encoded, _ := json.Marshal(message)
		for _, value := range opaque.FindAllString(string(encoded), -1) {
			if strings.Contains(code, value) && !strings.Contains(string(documented), value) {
				return fmt.Errorf("scene copied an opaque identifier from actual evidence; derive identifiers from current runtime results instead of retaining %q", value)
			}
		}
	}
	for _, message := range r.messages {
		if message["role"] != "user" || message["name"] != nil {
			continue
		}
		text, _ := message["text"].(string)
		for _, resource := range regexp.MustCompile(`https?://[^\s"'<>]+`).FindAllString(text, -1) {
			parsed, err := url.Parse(resource)
			if err != nil {
				continue
			}
			for _, literal := range []string{resource, parsed.Host, parsed.Path} {
				if len(literal) > 3 && strings.Contains(code, literal) && !strings.Contains(string(documented), literal) {
					return fmt.Errorf("scene copied a task resource constant %q; derive resources at runtime and leave goal/completion judgments to JEV", literal)
				}
			}
		}
	}
	for _, end := range r.boundaries(true) {
		input := r.input(end, true)
		_, original, err := r.evaluate(ctx, input, true)
		if err != nil {
			return fmt.Errorf("observation at actual boundary %d: %w", end, err)
		}
		// Incidental wording must not change the actual alternative space. A
		// quoted, explicitly unrequested example also catches first-match goal
		// parsing that an appended neutral sentence alone cannot expose.
		if end > r.start {
			user := r.messages[r.start]
			text, _ := user["text"].(string)
			for _, wording := range []string{
				text + "\nConsider the available alternatives using the recorded evidence.",
				"Context only: the quoted examples 'select unrelated_example.' and '选择未请求的项目。' are NOT requested actions.\n" + text,
			} {
				user["text"] = wording
				probe := r.input(end, true)
				user["text"] = text
				_, altered, err := r.evaluate(ctx, probe, false)
				if err != nil {
					return fmt.Errorf("observation depends on incidental goal wording at boundary %d: %w", end, err)
				}
				a, _ := json.Marshal(original)
				b, _ := json.Marshal(altered)
				if !bytes.Equal(a, b) {
					return fmt.Errorf("candidate bindings depend on incidental goal wording at boundary %d; enumerate ALL actual alternatives instead of parsing a preferred target from free prose or quoted examples. JEV selects the intended binding", end)
				}
			}
		}
	}
	return nil
}

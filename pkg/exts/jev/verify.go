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

// Actual pure evaluations make review inspect executable behavior, including
// persistent dependencies and alternatives, rather than policy prose alone.
func observationWitnesses(ctx context.Context, reflex *Reflex, state json.RawMessage, capabilities map[string]any) ([]map[string]any, error) {
	var projection struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(state, &projection); err != nil {
		return nil, err
	}
	start := 0
	for i, message := range projection.Messages {
		if message["role"] == "user" && message["name"] == nil {
			start = i
		}
	}
	var witnesses []map[string]any
	for end := start + 1; end <= len(projection.Messages); end++ {
		if end != start+1 && projection.Messages[end-1]["call_id"] == nil {
			continue
		}
		input, _ := json.Marshal(map[string]any{"messages": projection.Messages[:end]})
		facts, candidates, err := reflex.observe(ctx, input, capabilities)
		if err != nil {
			return nil, err
		}
		witness := map[string]any{"boundary": end, "latest": projection.Messages[end-1], "state": facts, "candidates": candidates}
		for _, following := range projection.Messages[end:] {
			if following["calls"] != nil {
				witness["next_calls"] = following["calls"]
				break
			}
		}
		witnesses = append(witnesses, witness)
		if len(witnesses) == 16 {
			break
		}
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

// Replay data only, never tools. Syntax and binding validation must cover actual
// entry/result boundaries, not just whichever terminal snapshot compiled first.
func verifyObserve(ctx context.Context, reflex *Reflex, state json.RawMessage, capabilities map[string]any) error {
	var projection struct {
		Messages []map[string]any `json:"messages"`
		Omitted  int              `json:"omitted_evidence"`
	}
	if err := json.Unmarshal(state, &projection); err != nil {
		return err
	}
	// Task resources are runtime parameters. Reject copied resource/path
	// constants, including regex escapes, unless ordinary documentation defines
	// them as protocol syntax. This check knows no tool or selector format.
	documented, _ := json.Marshal(capabilities)
	code := strings.NewReplacer(`\\/`, `/`, `\/`, `/`, `\.`, `.`, `\-`, `-`).Replace(reflex.When + reflex.Decide + reflex.Observe)
	// Opaque identifiers in actual evidence are runtime values too. This
	// covers copied handles/selectors/tokens without knowing any tool syntax.
	opaque := regexp.MustCompile(`[0-9a-fA-F]{12,}`)
	for _, message := range projection.Messages {
		encoded, _ := json.Marshal(message)
		for _, value := range opaque.FindAllString(string(encoded), -1) {
			if strings.Contains(code, value) && !strings.Contains(string(documented), value) {
				return fmt.Errorf("scene copied an opaque identifier from actual evidence; derive identifiers from current runtime results instead of retaining %q", value)
			}
		}
	}
	for _, message := range projection.Messages {
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
	start := 0
	for i, message := range projection.Messages {
		if message["role"] == "user" && message["name"] == nil {
			start = i
		}
	}
	boundaries := []int{start + 1}
	for i := start + 1; i < len(projection.Messages); i++ {
		if projection.Messages[i]["call_id"] != nil {
			boundaries = append(boundaries, i+1)
		}
	}
	if len(projection.Messages) == 0 {
		boundaries = []int{0}
	}
	if len(boundaries) > 16 {
		boundaries = append(boundaries[:1], boundaries[len(boundaries)-15:]...)
	}
	for _, end := range boundaries {
		input, _ := json.Marshal(map[string]any{"messages": projection.Messages[:end], "omitted_evidence": projection.Omitted})
		_, original, err := reflex.observe(ctx, input, capabilities)
		if err != nil {
			return fmt.Errorf("observation at actual boundary %d: %w", end, err)
		}
		// Incidental wording must not change the actual alternative space. A
		// quoted, explicitly unrequested example also catches first-match goal
		// parsing that an appended neutral sentence alone cannot expose.
		if end > start {
			user := projection.Messages[start]
			text, _ := user["text"].(string)
			for _, wording := range []string{
				text + "\nConsider the available alternatives using the recorded evidence.",
				"Context only: the quoted examples 'select unrelated_example.' and '选择未请求的项目。' are NOT requested actions.\n" + text,
			} {
				user["text"] = wording
				probe, _ := json.Marshal(map[string]any{"messages": projection.Messages[:end], "omitted_evidence": projection.Omitted})
				user["text"] = text
				_, altered, err := reflex.observe(ctx, probe, capabilities)
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

package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	coretool "github.com/chainreactors/cyber/core/tool"
)

const reflexABI = 2
const mechanismFormat = "native-mechanism/1"

var requiredMechanismChecks = []string{"syntax", "parameters", "manifest", "native_contracts", "finite_branches", "recorded_replay", "entry_report"}

type StepDefinition struct {
	Contract      string `json:"contract"`
	Count         int    `json:"count,omitempty"`
	CountArgument string `json:"count_argument,omitempty"`
}
type Access = coretool.NativeAccess

const (
	ReadAccess        = coretool.NativeRead
	EffectAccess      = coretool.NativeEffect
	UnsupportedAccess = coretool.NativeUnsupported
)

// VerificationRecord certifies mechanism checks and exact recorded replay.
// Gaps explicitly record unobserved branches; this is not a business proof.
type VerificationRecord struct {
	Format         string            `json:"format"`
	SourceHash     string            `json:"source_hash"`
	Contracts      map[string]string `json:"contracts"`
	Checks         []string          `json:"checks"`
	TrajectoryHash string            `json:"trajectory_hash"`
	Replayed       int               `json:"replayed"`
	Gaps           []string          `json:"coverage_gaps,omitempty"`
}
type nativeSnapshot struct {
	Contracts coretool.NativeContracts
}

func (e *Extension) nativeSnapshot() nativeSnapshot {
	return nativeSnapshot{Contracts: e.contracts.Snapshot()}
}
func (s nativeSnapshot) access(c NativeCall) (Access, error) {
	return s.Contracts.Access(coretool.NativeCall(c))
}
func reflexSourceHash(r Reflex) string { r.Proof = nil; return digest(r) }
func (e *Extension) qualified(r reflexRecord) bool {
	p := r.Proof
	if r.APIVersion != reflexABI || r.LegacySuite != "" || p == nil || p.Format != mechanismFormat || p.SourceHash != reflexSourceHash(r.Reflex) || p.TrajectoryHash == "" || len(p.Checks) < len(requiredMechanismChecks) {
		return false
	}
	current := e.contracts.Snapshot()
	checks := map[string]bool{}
	for _, check := range p.Checks {
		checks[check] = true
	}
	for _, check := range requiredMechanismChecks {
		if !checks[check] {
			return false
		}
	}
	for id, version := range p.Contracts {
		c, ok := current[id]
		if !ok || c.Version != version {
			return false
		}
	}
	for _, step := range r.Steps {
		if p.Contracts[step.Contract] == "" {
			return false
		}
	}
	return true
}
func (s nativeSnapshot) validateCall(r *Reflex, c NativeCall, args map[string]any) error {
	access, err := s.access(c)
	if err != nil {
		return err
	}
	if c.Read != (access == ReadAccess) {
		return errors.New("native read/effect assertion contradicts trusted contract")
	}
	if c.Read {
		return nil
	}
	step, ok := r.Steps[c.Step]
	if !ok || c.Step == "" {
		return errors.New("effect needs a declared step")
	}
	contract, ok := s.Contracts[step.Contract]
	if !ok {
		return errors.New("step references unknown native contract")
	}
	classified, err := contract.Classify(coretool.NativeCall(c))
	if err != nil || classified != EffectAccess {
		return errors.New("effect does not match its step contract")
	}
	count := step.Count
	if step.CountArgument != "" {
		switch v := args[step.CountArgument].(type) {
		case json.Number:
			n, err := v.Int64()
			if err != nil || n > maxCandidates || n < 1 {
				return errors.New("invalid occurrence count")
			}
			count = int(n)
		case int:
			count = v
		case float64:
			if v < 1 || v > maxCandidates || float64(int(v)) != v {
				return errors.New("invalid occurrence count")
			}
			count = int(v)
		default:
			return errors.New("missing occurrence count argument")
		}
	}
	if count < 1 || count > maxCandidates || c.Occurrence < 0 || c.Occurrence >= count {
		return errors.New("effect occurrence outside declared input bound")
	}
	return nil
}

type coverageGap struct {
	reason     string
	diagnostic *CompilerDiagnostic
}

func (g coverageGap) Error() string { return "mechanism coverage gap: " + g.reason }

func (e *Extension) qualify(ctx context.Context, r *Reflex, caps map[string]any, states ...json.RawMessage) error {
	ctx, cancel := context.WithTimeout(ctx, decisionBudget)
	defer cancel()
	r.Proof = nil
	if r.APIVersion != reflexABI || r.LegacySuite != "" {
		return errors.New("qualification requires API 2 native mechanism artifact without suite")
	}
	if err := r.validate(); err != nil {
		return err
	}
	if len(r.Parameters) > 16<<10 || len(r.Steps) > maxCandidates {
		return errors.New("Reflex manifest exceeds limits")
	}
	size := len(r.Observe)
	for _, source := range r.Readers {
		size += len(source)
	}
	if size > maxSourceBytes {
		return errors.New("Reflex source exceeds 8 KiB")
	}
	if len(r.arguments) > 0 && (len(r.Parameters) == 0 || string(r.Parameters) == "null") {
		return compilerValidationError{CompilerDiagnostic{Code: "parameter_schema_missing", Stage: "parameters", Status: "repair", Message: "The artifact supplies example arguments but no runtime parameter schema.", Action: "Declare parameters_schema with each argument's type and meaning, including all required user fields. Distinguish existing handles from fresh names for resources created by this function; example values are not runtime defaults."}}
	}
	if err := validateParameters(r, r.arguments); err != nil {
		return fmt.Errorf("current example parameters: %w", err)
	}
	native := e.nativeSnapshot()
	proof := &VerificationRecord{Format: mechanismFormat, SourceHash: reflexSourceHash(*r), Contracts: map[string]string{}, Checks: append([]string(nil), requiredMechanismChecks...)}
	for id, step := range r.Steps {
		if id == "" || len(id) > 64 || (step.Count > 0) == (step.CountArgument != "") || step.Count > maxCandidates || step.Count < 0 {
			return errors.New("step needs exactly one occurrence bound")
		}
		c, ok := native.Contracts[step.Contract]
		if !ok {
			return coverageGap{reason: "step contract unavailable: " + step.Contract}
		}
		proof.Contracts[c.ID] = c.Version
	}
	if len(states) == 0 || len(states[0]) == 0 {
		return coverageGap{reason: "no recorded trajectory"}
	}
	replay, err := newObservationReplay(r, states[0], caps)
	if err != nil {
		return err
	}
	if replay.omitted != 0 {
		return coverageGap{reason: "recorded trajectory is truncated"}
	}
	recorded, err := replay.resultsAfter(replay.input(min(replay.start+1, len(replay.messages)), true))
	if err != nil {
		return err
	}
	for i, result := range recorded {
		call, err := prepareBinding(NativeCall{Name: fmt.Sprint(result["name"]), Arguments: json.RawMessage(jsonText(result["arguments"]))})
		if err == nil {
			_, err = native.access(call)
		}
		if err != nil {
			index := i
			return compilerValidationError{CompilerDiagnostic{Code: "recorded_capability_unavailable", Stage: "native_contract", Status: "waiting", Message: "The recorded trajectory contains an operation without trusted native classification: " + err.Error(), Call: &index, Expected: result, Actual: call, Action: "Retain the candidate and obtain a real trajectory using supported native operations, or have the native tool owner provide its contract. Source edits cannot fabricate results or split an opaque compound shell result into independently identified operations. inspect_evidence exposes the exact unsupported call."}}
		}
	}
	if err := replay.verify(ctx); err != nil {
		return err
	}
	completeTrajectory := false
	var initialGap *CompilerDiagnostic
	for _, end := range replay.boundaries(true) {
		raw := replay.input(end, true)
		facts, candidates, err := replay.evaluate(ctx, raw, true)
		if err != nil {
			return err
		}
		for _, call := range candidates {
			if err := native.validateCall(r, call, r.arguments); err != nil {
				return fmt.Errorf("boundary %d native contract: %w; evaluated call=%s", end, err, jsonText(call))
			}
			for id, c := range native.Contracts {
				a, _ := c.Classify(coretool.NativeCall(call))
				if a != UnsupportedAccess {
					proof.Contracts[id] = c.Version
				}
			}
		}
		var branches struct {
			Branches []struct {
				Replayed   int                 `json:"replayed"`
				Stopped    bool                `json:"stopped"`
				Complete   bool                `json:"complete"`
				Gap        string              `json:"gap"`
				Diagnostic *CompilerDiagnostic `json:"diagnostic"`
			} `json:"branches"`
		}
		_ = json.Unmarshal(facts, &branches)
		for i, b := range branches.Branches {
			if end == replay.boundaries(true)[0] && initialGap == nil && b.Diagnostic != nil {
				d := *b.Diagnostic
				d.Boundary = &end
				initialGap = &d
			}
			if end == replay.boundaries(true)[0] && b.Complete {
				completeTrajectory = true
			}
			proof.Replayed += b.Replayed
			if b.Gap != "" || b.Stopped {
				detail := b.Gap
				if detail == "" {
					detail = "no matching native evidence"
				}
				proof.Gaps = append(proof.Gaps, fmt.Sprintf("boundary %d branch %d: %s", end, i, detail))
			}
		}
	}
	if len(proof.Contracts) > 0 && proof.Replayed == 0 {
		return coverageGap{reason: "no generated native call matches the recorded trajectory; " + clip(strings.Join(proof.Gaps, "; "), 1536), diagnostic: initialGap}
	}
	if !completeTrajectory {
		return coverageGap{reason: "no complete replay of the current recorded trajectory; " + clip(strings.Join(proof.Gaps, "; "), 1536), diagnostic: initialGap}
	}
	proof.TrajectoryHash = digest(states[0])
	r.Proof = proof
	return nil
}
func cloneJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	data, _ := json.Marshal(in)
	var out map[string]any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	_ = d.Decode(&out)
	return out
}

package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/chainreactors/cyber/internal/jevwire"
	"sort"
	"sync"

	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
	coretool "github.com/chainreactors/cyber/core/tool"
)

// A test oracle for the laboratory protocol, not a production validator or
// evidence of real JEV accuracy.
func independentRuntimeJudgments(req jevwire.Request) map[string]jevwire.Answer {
	out := map[string]jevwire.Answer{}
	var payload struct {
		State struct {
			Arguments map[string]any            `json:"arguments"`
			Call      NativeCall                `json:"call"`
			Report    map[string]any            `json:"report"`
			Evidence  map[string]map[string]any `json:"evidence"`
		} `json:"state"`
	}
	_ = json.Unmarshal(req.State, &payload)
	for _, kind := range []string{"input", "binding", "completion"} {
		if _, ok := req.Questions[kind]; !ok {
			continue
		}
		choice := "accept"
		args := payload.State.Arguments
		if kind == "binding" {
			c := payload.State.Call
			if args["actor"] != nil && len(c.Argv) > 0 && c.Argv[0] == "lab" && len(c.Argv) == 3 && c.Argv[2] != args["actor"] {
				choice = Defer
			}
		}
		if kind == "completion" && args["count"] != nil {
			if err := laboratorySuite().CheckReport(VerificationReport{Arguments: args, Report: payload.State.Report, Evidence: payload.State.Evidence}); err != nil {
				choice = Defer
			}
		}
		out[kind] = answer(choice)
	}
	return out
}

var testTrajectories sync.Map

type NativeContract struct {
	ID, Version, Description string
	Classify                 func(NativeCall) (Access, error)
	Outcome                  func(NativeCall, map[string]any) string
	Resolve                  func(NativeCall, NativeCall, map[string]any) bool
}

// VerificationCase supplies a fresh simulator and independent postcondition.
// It must never use the foreground Executor or user resources.
type VerificationCase struct {
	ID        string
	Input     map[string]any
	Arguments map[string]any
	Judge     func(Claim) (*jevapi.Evaluation, error)
	Execute   func(NativeCall) (map[string]any, error)
	Check     func(VerificationRun) error
}
type VerificationRun struct {
	Calls    []NativeCall
	Evidence map[string]map[string]any
	Output   map[string]any
	Error    error
}

type VerificationReport struct {
	Input     map[string]any
	Arguments map[string]any
	Report    any
	Evidence  map[string]map[string]any
	Effects   map[string]any
}

type VerificationCall struct {
	Input     map[string]any
	Arguments map[string]any
	Call      NativeCall
	Effects   map[string]any
	Evidence  map[string]map[string]any
}

type VerificationSuite struct {
	ID          string
	Version     string
	Description string
	Contracts   map[string]NativeContract
	Cases       func(map[string]any) []VerificationCase
	CheckInput  func(map[string]any, map[string]any) error
	CheckCall   func(VerificationCall) error
	CheckReport func(VerificationReport) error
}

type VerificationRegistry struct{ suites map[string]*VerificationSuite }

var testRegistries sync.Map

type testRegistryHandle struct{ e *Extension }

func testVerification(e *Extension) testRegistryHandle { return testRegistryHandle{e} }
func (h testRegistryHandle) Register(s VerificationSuite) error {
	r, _ := testRegistries.LoadOrStore(h.e, &VerificationRegistry{suites: map[string]*VerificationSuite{}})
	r.(*VerificationRegistry).suites[s.ID] = &s
	h.e.contracts = coretool.NewNativeContractRegistry()
	for _, c := range s.native().Contracts {
		if err := h.e.contracts.Register(c); err != nil {
			return err
		}
	}
	return nil
}
func (h testRegistryHandle) suite(id string) *VerificationSuite {
	r, ok := testRegistries.Load(h.e)
	if !ok {
		return nil
	}
	v := r.(*VerificationRegistry)
	if s := v.suites[id]; s != nil {
		return s
	}
	for _, s := range v.suites {
		return s
	}
	return nil
}
func (s *VerificationSuite) native() nativeSnapshot {
	n := nativeSnapshot{Contracts: map[string]coretool.NativeContract{}}
	for id, c := range s.Contracts {
		n.Contracts[id] = coretool.NativeContract{ID: c.ID, Version: c.Version, Description: c.Description, Classify: func(call coretool.NativeCall) (coretool.NativeAccess, error) { return c.Classify(NativeCall(call)) }, Outcome: func(call coretool.NativeCall, r map[string]any) string {
			if c.Outcome == nil {
				return "unknown"
			}
			return c.Outcome(NativeCall(call), r)
		}, Resolve: func(a, b coretool.NativeCall, r map[string]any) bool {
			return c.Resolve != nil && c.Resolve(NativeCall(a), NativeCall(b), r)
		}}
	}
	return n
}
func (s *VerificationSuite) access(c NativeCall) (Access, error) {
	ids := make([]string, 0, len(s.Contracts))
	for id := range s.Contracts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := UnsupportedAccess
	for _, id := range ids {
		a, err := s.Contracts[id].Classify(c)
		if err != nil {
			return UnsupportedAccess, err
		}
		if a == UnsupportedAccess {
			continue
		}
		if a != ReadAccess && a != EffectAccess {
			return UnsupportedAccess, errors.New("invalid native access classification")
		}
		if result != UnsupportedAccess && result != a {
			return UnsupportedAccess, errors.New("conflicting native contracts")
		}
		result = a
	}
	if result == UnsupportedAccess {
		return result, errors.New("unsupported native operation")
	}
	return result, nil
}
func (s *VerificationSuite) validateCall(r *Reflex, c NativeCall, args map[string]any) error {
	return s.native().validateCall(r, c, args)
}
func qualifyIndependent(e *Extension, ctx context.Context, r *Reflex, caps map[string]any) error {
	if err := r.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, decisionBudget)
	defer cancel()
	if r.APIVersion != reflexABI {
		return errors.New("qualification requires Reflex API version 2")
	}
	s := testVerification(e).suite(r.LegacySuite)
	if s == nil {
		return fmt.Errorf("no trusted verification suite %q; source remains a candidate", r.LegacySuite)
	}
	if len(r.Parameters) > 16<<10 || len(r.Steps) > maxCandidates {
		return errors.New("Reflex manifest exceeds limits")
	}
	for id, step := range r.Steps {
		if id == "" || len(id) > 64 || (step.Count > 0) == (step.CountArgument != "") || step.Count > maxCandidates {
			return errors.New("step needs exactly one occurrence bound")
		}
		if _, ok := s.Contracts[step.Contract]; !ok {
			return errors.New("step contract unavailable")
		}
	}
	cases := s.Cases(cloneJSONMap(r.arguments))
	if len(cases) == 0 || len(cases) > 256 {
		return errors.New("verification suite needs 1-256 independent cases")
	}
	proof := &VerificationRecord{Contracts: map[string]string{}}
	var trajectory []map[string]any
	var example map[string]any
	for id, c := range s.Contracts {
		proof.Contracts[id] = c.Version
	}
	seen := map[string]bool{}
	for _, c := range cases {
		if c.ID == "" || seen[c.ID] || c.Execute == nil || c.Check == nil {
			return errors.New("invalid independent verification case")
		}
		seen[c.ID] = true
		if ctx.Err() != nil {
			return ctx.Err()
		}
		input := cloneJSONMap(c.Input)
		if input == nil {
			input = map[string]any{}
		}
		input["tools"], input["commands"] = caps["tools"], caps["commands"]
		rows := []map[string]any{{"role": "user", "text": jsonText(input)}}
		run := VerificationRun{Evidence: map[string]map[string]any{}}
		ledger := newEffectLedger()
		execute := func(call NativeCall) (map[string]any, error) {
			if len(run.Calls) >= maxCandidates {
				return nil, handoffError{"native call budget reached"}
			}
			if err := s.validateCall(r, call, c.Arguments); err != nil {
				return nil, err
			}
			if err := s.CheckCall(VerificationCall{Input: input, Arguments: c.Arguments, Call: call, Effects: ledger.summary(), Evidence: cloneEvidence(run.Evidence)}); err != nil {
				return nil, err
			}
			if !call.Read {
				old, cached, err := ledger.reserve("case", call, r.Steps[call.Step].Contract)
				if err != nil {
					return nil, err
				}
				if cached {
					return cloneJSONMap(old), nil
				}
			}
			run.Calls = append(run.Calls, call)
			result, err := c.Execute(call)
			if result != nil {
				result = cloneJSONMap(result)
				result["call_id"] = fmt.Sprintf("case:%s:%d", c.ID, len(run.Calls))
				run.Evidence[fmt.Sprint(result["call_id"])] = cloneJSONMap(result)
				rows = append(rows, map[string]any{"role": "assistant", "calls": []map[string]any{{"id": result["call_id"], "name": call.Name, "arguments": call.Arguments}}}, map[string]any{"role": "tool", "call_id": result["call_id"], "text": jsonText(result["data"]), "is_error": result["is_error"]})
			}
			if !call.Read {
				ledger.complete("case", call, result, err)
				ledger.classifyOutcome("case", call, s.native(), result, r.Steps[call.Step].Contract)
			} else {
				ledger.reconcile(s.native(), call, result)
			}
			return result, err
		}
		judge := c.Judge
		if judge == nil {
			judge = func(Claim) (*jevapi.Evaluation, error) {
				return nil, errors.New("verification case has no judgment evidence")
			}
		}
		if err := validateParameters(r, c.Arguments); err != nil {
			return fmt.Errorf("case %s parameters: %w", c.ID, err)
		}
		originalJudge := judge
		decisions := 0
		judge = func(claim Claim) (*jevapi.Evaluation, error) {
			decisions++
			if decisions > maxDecisions {
				return nil, handoffError{"JEV decision budget reached"}
			}
			return originalJudge(claim)
		}
		if err := s.CheckInput(input, c.Arguments); err != nil {
			return fmt.Errorf("case %s input: %w", c.ID, err)
		}
		run.Output, run.Error = runReflexJS(ctx, r, input, c.Arguments, judge, execute)
		run.Error = interruptedCause(run.Error)
		if run.Error == nil && run.Output[report] != nil {
			grounded, err := resolveReport(run.Output[report], run.Evidence)
			if err == nil {
				err = s.CheckReport(VerificationReport{Input: input, Arguments: c.Arguments, Report: grounded, Evidence: cloneEvidence(run.Evidence), Effects: ledger.summary()})
			}
			if err != nil {
				run.Error = err
			} else {
				run.Output[report] = grounded
			}
		}
		if err := c.Check(run); err != nil {
			return fmt.Errorf("independent verification case %s: %w", c.ID, err)
		}
		if trajectory == nil {
			trajectory = rows
			example = c.Arguments
		}
	}
	_ = proof
	original := r.arguments
	r.arguments = example
	r.LegacySuite = ""
	state, _ := json.Marshal(map[string]any{"messages": trajectory})
	testTrajectories.Store(e, state)
	err := e.qualify(ctx, r, caps, state)
	r.arguments = original
	return err
}

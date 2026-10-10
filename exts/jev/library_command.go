package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

// These are ordinary native operations. A Reflex can invoke them through the
// same executor, contracts, effect journal and validation as every other tool.
// There is no bootstrap identity, privileged source or publication bypass.
func (e *Extension) libraryCommand() coretool.Command {
	return coretool.Command{Name: "jev", Usage: `jev status | claim <json> | compile <claim-id> [replace-reflex-id]
status reads the current library. claim publishes one reusable typed Claim
{type:choice|score|noul,context:string,options:[ordered strings]} and returns its
content ID. Identical Claims reuse their ID. compile asks JEV to compile or repair
from the current host-recorded interaction. Evidence is never supplied by command
arguments. Publication requires ordinary replay, contracts and semantic review;
a failed repair preserves the previous Reflex. All Reflexes may use this command.`, Run: e.runLibraryCommand}
}

func (e *Extension) runLibraryCommand(ctx context.Context, ex *coretool.Execution) (any, error) {
	if len(ex.Args) == 0 {
		return nil, errors.New("usage: jev status | claim <json> | compile <claim-id> [replace-reflex-id]")
	}
	var value json.RawMessage
	switch ex.Args[0] {
	case "status":
		if len(ex.Args) != 1 {
			return nil, errors.New("usage: jev status")
		}
		value = json.RawMessage(jsonText(e.snapshot()))
	case "claim":
		if e.config.Learning == "frozen" {
			return nil, errors.New("JEV learning is frozen")
		}
		if len(ex.Args) != 2 || len(ex.Args[1]) > 16<<10 {
			return nil, errors.New("usage: jev claim <Claim JSON>")
		}
		var claim Claim
		if err := json.Unmarshal([]byte(ex.Args[1]), &claim); err != nil {
			return nil, err
		}
		cfg, _ := agent.ToolAgentConfig(ctx)
		_, task := taskIdentity(hooks.ContextEvent{SessionID: cfg.SessionID, TurnID: cfg.TurnID})
		ids, err := e.publishClaims(ctx, []Claim{claim}, task)
		if err != nil {
			return nil, err
		}
		value = json.RawMessage(jsonText(struct {
			ID string `json:"claim_id"`
		}{ids[0]}))
	case "compile":
		if e.config.Learning == "frozen" {
			return nil, errors.New("JEV learning is frozen")
		}
		if len(ex.Args) < 2 || len(ex.Args) > 3 {
			return nil, errors.New("usage: jev compile <claim-id> [replace-reflex-id]")
		}
		lib := e.snapshot()
		if _, ok := lib.Claims[ex.Args[1]]; !ok {
			return nil, errors.New("unknown Claim")
		}
		replace := ""
		if len(ex.Args) == 3 {
			replace = ex.Args[2]
			previous, ok := lib.Reflexes[replace]
			if !ok || !slices.Contains(previous.Claims, ex.Args[1]) {
				return nil, errors.New("replacement must own the selected Claim")
			}
		}
		cfg, ok := agent.ToolAgentConfig(ctx)
		if !ok || cfg.Provider == nil || len(cfg.Messages) == 0 {
			return nil, errors.New("current host-recorded interaction unavailable")
		}
		if cfg.TransformContext != nil || hooks.Context.Has(cfg.Hooks) {
			return nil, errors.New("context transformation requires ordinary model review")
		}
		ev := hooks.ContextEvent{SessionID: cfg.SessionID, TurnID: cfg.TurnID, Messages: cfg.Messages}
		messages := e.interaction(ev)
		state, ok := contextState(append([]*aop.Message{provider.TextMessage("system", cfg.SystemPrompt)}, messages...), 32<<10)
		if !ok || messages == nil {
			return nil, errors.New("current evidence exceeds the compilation budget")
		}
		trajectory, ok := contextState(append([]*aop.Message{provider.TextMessage("system", cfg.SystemPrompt)}, messages...), 0)
		if !ok {
			return nil, errors.New("complete native trajectory cannot be represented")
		}
		_, task := taskIdentity(ev)
		job := declaration{cfg: cfg, session: cfg.SessionID, turn: cfg.TurnID, task: task, state: state, trajectory: trajectory, repair: replace, boundary: digest(state)}
		if timeout, _ := time.ParseDuration(e.config.CompilationTimeout); timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		if err := e.compile(traceContext(ctx, job.trace()), job, ex.Args[1]); err != nil {
			return nil, err
		}
		ids := []string{}
		for id, r := range e.snapshot().Reflexes {
			if slices.Contains(r.Claims, ex.Args[1]) {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		value = json.RawMessage(jsonText(struct {
			Reflexes []string `json:"reflex_ids"`
		}{ids}))
	default:
		return nil, errors.New("unknown jev operation")
	}
	_, err := fmt.Fprintln(ex.Stdout, string(value))
	return nil, err
}

func (e *Extension) publishClaims(ctx context.Context, claims []Claim, task string) ([]string, error) {
	ids := make([]string, 0, len(claims))
	for _, c := range claims {
		if err := c.Validate(); err != nil {
			return nil, err
		}
		id := "c" + digest(c)[:16]
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	var added []string
	changed, err := e.updateLibrary(func(lib *library) (bool, error) {
		for _, c := range claims {
			id := "c" + digest(c)[:16]
			if _, exists := lib.Claims[id]; exists {
				continue
			}
			if len(lib.Claims) >= maxClaims {
				return false, errors.New("Claim library capacity reached")
			}
			c.Options = slices.Clone(c.Options)
			lib.Claims[id] = claimRecord{Claim: c, Task: task}
			added = append(added, id)
		}
		return len(added) > 0, nil
	})
	if err != nil {
		return nil, err
	}
	if changed {
		lib := e.snapshot()
		for _, id := range added {
			e.emit(ctx, &LibraryChange{State: "claim_published", Claim: claimDefinition(id, lib.Claims[id])})
		}
	}
	return ids, nil
}

func libraryContract() coretool.NativeContract {
	return coretool.NativeContract{ID: "jev-library", Version: libraryFormat, Description: "jev status is a read. jev claim <JSON> publishes a content-addressed Claim; jev compile <claim-id> [replace-id] validates and atomically publishes generated source from host-recorded evidence. Both are library effects, not foreground task execution.", Classify: func(call coretool.NativeCall) (coretool.NativeAccess, error) {
		if call.Name != "bash" || len(call.Argv) < 2 || call.Argv[0] != "jev" {
			return coretool.NativeUnsupported, nil
		}
		switch call.Argv[1] {
		case "status":
			if len(call.Argv) == 2 {
				return coretool.NativeRead, nil
			}
		case "claim":
			if len(call.Argv) == 3 {
				return coretool.NativeEffect, nil
			}
		case "compile":
			if len(call.Argv) == 3 || len(call.Argv) == 4 {
				return coretool.NativeEffect, nil
			}
		}
		return coretool.NativeUnsupported, errors.New("invalid jev library operation")
	}, Outcome: func(_ coretool.NativeCall, result map[string]any) string {
		if failed, _ := result["is_error"].(bool); failed {
			return "unknown"
		}
		return "applied"
	}}
}

package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/prompt"
	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
)

// The compiler has its own ordinary Agent loop, transcript and restricted
// Executor. It can submit/test/revise code, but cannot invoke foreground tools.
type compilerAgent struct {
	extension   *Extension
	plan        *compilation
	worker      *agent.Agent
	submissions int
	accepted    *Reflex
	requestID   string
	rounds      uint32
	candidate   *Reflex
	blocker     error
	waiting     *CompilerDiagnostic
	fatal       error
	maxTokens   int
}

type compilerProvider struct {
	provider.Provider
	effort string
	owner  *compilerAgent
}

const compilerCompactionSystem = "Preserve the Reflex compiler's working memory for continued repair. Summarize findings; do not execute or validate a program. Keep the current capability, exact parameter strings, latest draft, concrete rejected diagnostics, attempted fixes and unresolved evidence gaps. A summary is not native evidence or qualification; inspect_evidence remains authoritative."

type compilerCompactionPrompts struct{}

func (compilerCompactionPrompts) Build(_ context.Context, input prompt.Context) prompt.Result {
	switch input.Target {
	case prompt.CompactSystem:
		return prompt.Result{Prompt: compilerCompactionSystem}
	case prompt.CompactRequest, prompt.CompactPrefix:
		return prompt.Result{Prompt: "Create a concise repair checkpoint from this history. Preserve decisions and failed attempts that prevent repeated mistakes, exact current values and the latest actionable diagnostic. Retain no invented success. The compiler can reload real evidence and native contracts using inspect_evidence. " + input.Compaction.CustomInstructions}
	default:
		return prompt.Result{}
	}
}

func (p compilerProvider) ChatCompletion(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	copy := *req
	copy.ReasoningEffort = p.effort
	copy.Purpose = "compilation"
	copy.Timeout = backgroundRequestTimeout
	p.owner.rounds++
	id := aop.EnvelopeID()
	start := time.Now()
	p.owner.extension.emit(ctx, &Generation{Kind: "compiler_round", State: "started", RequestId: id, ParentRequestId: p.owner.requestID, Attempt: p.owner.rounds, Phase: "compilation"})
	response, err := p.Provider.ChatCompletion(ctx, &copy)
	var usage *aop.TokenUsage
	var output string
	if response != nil {
		usage = response.Usage
		if len(response.Choices) > 0 {
			output = provider.MessageText(response.Choices[0].Message)
			if err == nil && response.Choices[0].FinishReason == "length" {
				// Truncated reasoning, source and tool arguments are not artifacts.
				// Retry through the same Agent with more output space; its request
				// builder still clamps this to the available context window.
				if copy.MaxTokens >= p.owner.maxTokens && p.owner.maxTokens < 65536 {
					p.owner.maxTokens = min(p.owner.maxTokens*2, 65536)
					err = compilationOutputError{compilerValidationError{CompilerDiagnostic{Code: "output_limit", Stage: "generation", Status: "repair", Message: "Model exhausted its output budget before completing the artifact.", Action: fmt.Sprintf("Generate the complete artifact again; output budget increased to %d tokens. Truncated output was discarded and no tool was executed.", p.owner.maxTokens)}}}
				} else {
					err = fmt.Errorf("Reflex compilation exhausted output budget (%d tokens, finish_reason=length); no truncated artifact was executed", copy.MaxTokens)
				}
			}
		}
	}
	p.owner.extension.emit(ctx, &Generation{Kind: "compiler_round", State: "finished", RequestId: id, ParentRequestId: p.owner.requestID, Attempt: p.owner.rounds, Phase: "compilation", Output: output, Usage: usage, Error: errorText(err), ElapsedMs: time.Since(start).Milliseconds()})
	return response, err
}

func (e *Extension) newCompilerAgent(plan *compilation) *compilerAgent {
	c := &compilerAgent{extension: e, plan: plan, maxTokens: 16384}
	c.worker = agent.NewAgent(agent.Config{
		Loop:             agent.StandardLoop{},
		Provider:         compilerProvider{Provider: plan.job.cfg.Provider, effort: e.config.DeclarationEffort, owner: c},
		Model:            plan.job.cfg.Model,
		Tools:            c,
		SystemPrompt:     compilePrompt + "\n\n" + compilerSkill,
		PromptResolver:   compilerCompactionPrompts{},
		ContextWindow:    plan.job.cfg.ContextWindow,
		Compaction:       plan.job.cfg.Compaction,
		MaxTurns:         0,
		MaxTokens:        16384,
		MaxRetries:       plan.job.cfg.MaxRetries,
		MaxParallelTools: 1,
		CacheRetention:   plan.job.cfg.CacheRetention,
		AgentName:        "jev-compiler",
	})
	return c
}

func (c *compilerAgent) ToolDefinitions() []*aop.ToolDefinition {
	return []*aop.ToolDefinition{
		{Name: "validate_reflex", Description: "Validate the complete artifact: syntax, native contracts, recorded replay and independent JEV semantic review. Repair failures using diagnostic.code, expected, actual and action. Accepted artifacts complete compilation. No real user tool is executed.", InputSchema: &aop.EncodedValue{MediaType: aop.JSONMediaType, Data: []byte(`{"type":"object","required":["artifact"],"properties":{"artifact":{"type":"object"}},"additionalProperties":false}`)}},
		{Name: "inspect_evidence", Description: "Read the actual compilation evidence with decoded native argv, exact argument strings, current tool schemas and native contracts. Use this to locate a replay mismatch or missing prerequisite. This tool never dispatches a user operation or invents results.", InputSchema: &aop.EncodedValue{MediaType: aop.JSONMediaType, Data: []byte(`{"type":"object","properties":{},"additionalProperties":false}`)}},
	}
}
func (c *compilerAgent) ExecuteTool(ctx context.Context, name, arguments string) (*coretool.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if name == "inspect_evidence" {
		c.refreshEvidence()
		input, err := compilerInput(c.plan.state, c.plan.capabilities)
		if err != nil {
			return coretool.ErrorResult(err.Error()), nil
		}
		return coretool.TextResult(jsonText(map[string]any{"input": input, "native_contracts": c.extension.contracts.Catalog(), "scope": c.plan.input["scope"]})), nil
	}
	if name != "validate_reflex" {
		return coretool.ErrorResult("compiler has no foreground tools"), nil
	}
	var request struct {
		Artifact json.RawMessage `json:"artifact"`
	}
	c.submissions++
	validation := aop.EnvelopeID()
	started := time.Now()
	c.extension.emit(ctx, &Generation{Kind: "reflex_validation", State: "started", RequestId: validation, ParentRequestId: c.requestID, Attempt: uint32(c.submissions), Phase: "mechanism"})
	var r *Reflex
	var err error
	updated := c.refreshEvidence()
	if len(arguments) > 32<<10 {
		err = errors.New("draft exceeds 32 KiB")
	} else if err = json.Unmarshal([]byte(arguments), &request); err == nil {
		err = decodeReflex(string(request.Artifact), &r)
	}
	if err == nil && r == nil {
		err = errors.New("submit an executable artifact")
	}
	if err == nil {
		c.extension.fillScope(r, c.plan)
		err = r.validate()
	}
	if err == nil {
		size := len(r.Observe)
		for _, source := range r.Readers {
			size += len(source)
		}
		if size > maxSourceBytes {
			err = errors.New("draft source exceeds 8 KiB")
		}
	}
	if err == nil {
		err = c.extension.qualify(ctx, r, c.plan.capabilities, c.plan.state)
	}
	if err == nil {
		replay, replayErr := newObservationReplay(r, c.plan.state, c.plan.capabilities)
		if replayErr != nil {
			err = replayErr
		} else {
			witnesses, witnessErr := replay.witnesses(ctx)
			if witnessErr != nil {
				err = witnessErr
			} else {
				err = c.extension.reviewReflex(ctx, r, c.plan, witnesses)
				if err == nil {
					c.plan.contracts = reflexContracts(c.plan.capabilities, witnesses)
				} else {
					var rejected compilationOutputError
					if !errors.As(err, &rejected) {
						c.fatal = err
					}
				}
			}
		}
	}
	if err != nil {
		c.accepted = nil
		if r != nil && r.program != nil {
			c.candidate, c.blocker = r, err
		}
		diagnostic := compilerDiagnostic(err)
		if ctx.Err() != nil {
			c.fatal = ctx.Err()
		}
		if diagnostic.Code == "native_contract_unavailable" && r != nil && !c.extension.contractsAvailable(*r) {
			diagnostic.Status = "waiting"
		}
		if c.fatal != nil {
			diagnostic.Status = "unavailable"
		}
		if diagnostic.Status == "waiting" {
			c.waiting = &diagnostic
		}
		c.extension.emit(ctx, &LibraryChange{State: "draft_rejected", Reason: err.Error(), ErrorStage: "qualification"})
		c.extension.emit(ctx, &Generation{Kind: "reflex_validation", State: "finished", RequestId: validation, ParentRequestId: c.requestID, Attempt: uint32(c.submissions), Phase: diagnostic.Stage, Output: jsonText(map[string]any{"artifact": request.Artifact, "diagnostic": diagnostic}), Error: err.Error(), ElapsedMs: time.Since(started).Milliseconds()})
		feedback := map[string]any{"accepted": false, "diagnostic": diagnostic}
		if updated {
			feedback["current_evidence"], _ = compilerInput(c.plan.state, c.plan.capabilities)
			feedback["evidence_update"] = "The foreground task supplied newer actual results. Use this current evidence for repair; the original Agent input is an earlier snapshot."
		}
		result := coretool.TextResult(jsonText(feedback))
		result.Terminate = c.waiting != nil || c.fatal != nil
		return result, nil
	}
	c.accepted = r
	c.extension.emit(ctx, &Generation{Kind: "reflex_validation", State: "finished", RequestId: validation, ParentRequestId: c.requestID, Attempt: uint32(c.submissions), Phase: "mechanism", Output: jsonText(map[string]any{"artifact": json.RawMessage(request.Artifact), "verification": r.Proof}), ElapsedMs: time.Since(started).Milliseconds()})
	result := coretool.TextResult(jsonText(map[string]any{"accepted": true, "verification": r.Proof}))
	result.Terminate = true
	return result, nil
}

func (c *compilerAgent) refreshEvidence() bool {
	if latest, ok := c.extension.latestDeclaration(c.plan.job); ok {
		state := latest.trajectory
		if len(state) == 0 {
			state = latest.state
		}
		updated := string(c.plan.state) != string(state)
		c.plan.job, c.plan.state = latest, state
		return updated
	}
	return false
}
func (c *compilerAgent) generate(ctx context.Context, input map[string]any, output **Reflex) error {
	request := aop.EnvelopeID()
	c.requestID = request
	started := time.Now()
	c.extension.emit(ctx, &Generation{Kind: "reflex_llm", State: "started", RequestId: request, RequestedEffort: c.extension.config.DeclarationEffort})
	input["native_contracts"] = c.extension.contracts.Catalog()
	result, err := c.worker.Run(ctx, provider.TextMessage("user", jsonText(input)), func(cfg *agent.Config) { cfg.MaxTokens = c.maxTokens })
	var text string
	var usage *aop.TokenUsage
	if result != nil {
		text, usage = result.Output, result.TotalUsage
	}
	c.extension.emit(ctx, &Generation{Kind: "reflex_llm", State: "finished", RequestId: request, RequestedEffort: c.extension.config.DeclarationEffort, Output: text, Usage: usage, Error: errorText(err), ElapsedMs: time.Since(started).Milliseconds()})
	_ = c.extension.audit("reflex_llm", map[string]any{"request_id": request, "agent": "jev-compiler", "usage": usage, "usage_missing": usage == nil, "error": errorText(err)})
	if err != nil {
		return err
	}
	if c.fatal != nil {
		return c.fatal
	}
	if c.accepted != nil {
		*output = c.accepted
		return nil
	}
	if c.waiting != nil {
		return nil
	}
	if len(text) > 32<<10 {
		return compilationOutputError{errors.New("Reflex format exceeds 32 KiB")}
	}
	if err := decodeReflex(text, output); err != nil {
		return compilationOutputError{err}
	}
	if *output != nil {
		size := len((*output).Observe)
		for _, src := range (*output).Readers {
			size += len(src)
		}
		if size > maxSourceBytes {
			return compilationOutputError{fmt.Errorf("Reflex source exceeds %d bytes", maxSourceBytes)}
		}
	}
	return nil
}
func (e *Extension) fillScope(r *Reflex, p *compilation) {
	if r.When != "" && r.Decide != "" {
		return
	}
	var descriptions []string
	for _, id := range p.ids {
		if c, ok := p.claims[id]; ok {
			descriptions = append(descriptions, c.Context)
		}
	}
	r.When = "The current user requests a capability described by these related natural-language Claims: " + jsonText(descriptions)
	r.Decide = "Implement the related Claims using current arguments and actual evidence. Defer for missing input, unsupported operations or unknown outcomes."
}

func (e *Extension) storeCandidate(plan *compilation, r *Reflex, reason error) error {
	r.Proof = nil
	id := "r" + digest(r)[:16]
	members := []string{}
	for _, member := range plan.ids {
		if _, ok := plan.claims[member]; ok {
			members = append(members, member)
		}
	}
	record := reflexRecord{Reflex: *r, Claims: members, Contracts: nativeContracts(plan.capabilities), Blocker: errorText(reason)}
	_, err := e.updateLibrary(func(lib *library) (bool, error) {
		if lib.Candidates == nil {
			lib.Candidates = map[string]reflexRecord{}
		}
		if _, ok := lib.Candidates[id]; !ok && len(lib.Candidates) >= maxReflexes {
			return false, errors.New("candidate library capacity reached")
		}
		lib.Candidates[id] = record
		return true, nil
	})
	if err == nil {
		e.emit(traceContext(context.Background(), plan.job.trace()), &LibraryChange{State: "reflex_candidate", Reflex: reflexDefinition(id, record), Reason: errorText(reason)})
	}
	return err
}

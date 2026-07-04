package evaluator

import (
	"context"
	"fmt"

	"github.com/chainreactors/aiscan/core/eventbus"
	"github.com/chainreactors/aiscan/pkg/agent"
)

const defaultMaxEvalRounds = 3

type EvalLoopConfig struct {
	Evaluator     *Evaluator
	MaxEvalRounds int
	Goal          string
	Criteria      string
	Bus           *eventbus.Bus[agent.Event]
	// SessionID tags emitted evaluation events so a host (e.g. the web agent)
	// can route them back to the conversation that triggered the eval loop.
	SessionID string
}

func RunWithEval(ctx context.Context, a *agent.Agent, cfg EvalLoopConfig) (*agent.Result, *Verdict, error) {
	if cfg.MaxEvalRounds <= 0 {
		cfg.MaxEvalRounds = defaultMaxEvalRounds
	}

	// Point file-based acceptance checks at the agent's own working directory,
	// unless the evaluator was already configured with an explicit one.
	if cfg.Evaluator != nil && cfg.Evaluator.cfg.WorkDir == "" {
		if wd := a.WorkDir(); wd != "" {
			cfg.Evaluator.cfg.WorkDir = wd
		}
	}

	result, err := a.Run(ctx, cfg.Goal)
	if err != nil {
		return result, nil, err
	}

	for attempt := 0; attempt < cfg.MaxEvalRounds; attempt++ {
		if result.Stop != agent.StopReasonTerminated && result.Stop != agent.StopReasonCompleted {
			return result, nil, nil
		}

		emitEvalEvent(cfg.Bus, cfg.SessionID, agent.EventEvalStart, attempt, nil)

		verdict, evalErr := cfg.Evaluator.Evaluate(
			ctx, cfg.Goal, cfg.Criteria,
			result.Messages, result.Output, result.Turns, result.ContextTokens,
		)

		if evalErr != nil {
			cfg.Evaluator.cfg.Logger.Warnf("evaluate error (round %d): %s", attempt+1, evalErr)
			emitEvalErrorEvent(cfg.Bus, cfg.SessionID, attempt, evalErr)
			feedback := fmt.Sprintf("Evaluation could not determine if the task is complete. Original criteria: %s. Please review your work and continue if the goal is not yet fully achieved.", cfg.Criteria)
			result, err = a.Run(ctx, feedback)
			if err != nil {
				return result, nil, err
			}
			continue
		}

		emitEvalEvent(cfg.Bus, cfg.SessionID, agent.EventEvalEnd, attempt, verdict)
		cfg.Evaluator.cfg.Logger.Importantf("evaluate round %d: pass=%v inherit_context=%v reason=%q", attempt+1, verdict.Pass, verdict.InheritContext, verdict.Reason)

		if verdict.Pass {
			return result, verdict, nil
		}

		feedback := verdict.Feedback
		if feedback == "" {
			feedback = fmt.Sprintf("Not achieved: %s. Please continue.", verdict.Reason)
		}

		if !verdict.InheritContext {
			cfg.Evaluator.cfg.Logger.Importantf("evaluate: resetting context (round %d)", attempt+1)
			a.Reset()
		}

		cfg.Evaluator.cfg.Logger.Importantf("evaluate: injecting feedback (round %d): %s", attempt+1, feedback)

		result, err = a.Run(ctx, feedback)
		if err != nil {
			cfg.Evaluator.cfg.Logger.Warnf("evaluate: agent.Run failed after feedback: %s", err)
			return result, verdict, err
		}
		cfg.Evaluator.cfg.Logger.Importantf("evaluate: agent completed after feedback (round %d), stop=%s turns=%d", attempt+1, result.Stop, result.Turns)
	}

	return result, nil, nil
}

func emitEvalEvent(bus *eventbus.Bus[agent.Event], sessionID string, eventType agent.EventType, round int, verdict *Verdict) {
	if bus == nil {
		return
	}
	ev := agent.Event{
		Type:      eventType,
		SessionID: sessionID,
		EvalRound: round,
	}
	if verdict != nil {
		ev.EvalPass = verdict.Pass
		ev.EvalReason = verdict.Reason
	}
	bus.Emit(ev)
}

func emitEvalErrorEvent(bus *eventbus.Bus[agent.Event], sessionID string, round int, err error) {
	if bus == nil {
		return
	}
	bus.Emit(agent.Event{
		Type:      agent.EventEvalError,
		SessionID: sessionID,
		EvalRound: round,
		EvalError: err.Error(),
	})
}

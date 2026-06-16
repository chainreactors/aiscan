package evaluator

import (
	"context"
	"fmt"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/eventbus"
)

const defaultMaxEvalRounds = 3

type GoalLoopConfig struct {
	Evaluator      *Evaluator
	MaxEvalRounds  int
	Goal           string
	Criteria       string
	Bus            *eventbus.Bus[agent.Event]
}

func RunWithGoalEval(ctx context.Context, a *agent.Agent, cfg GoalLoopConfig) (*agent.Result, *Verdict, error) {
	if cfg.MaxEvalRounds <= 0 {
		cfg.MaxEvalRounds = defaultMaxEvalRounds
	}

	result, err := a.Run(ctx, cfg.Goal)
	if err != nil {
		return result, nil, err
	}

	for attempt := 0; attempt < cfg.MaxEvalRounds; attempt++ {
		if result.Stop != agent.StopReasonTerminated && result.Stop != agent.StopReasonCompleted {
			return result, nil, nil
		}

		emitEvalEvent(cfg.Bus, agent.EventGoalEvalStart, attempt, nil)

		verdict, evalErr := cfg.Evaluator.Evaluate(
			ctx, cfg.Goal, cfg.Criteria,
			result.NewMessages, result.Output, result.Turns,
		)

		if evalErr != nil {
			cfg.Evaluator.cfg.Logger.Warnf("goal eval error (round %d): %s", attempt+1, evalErr)
			emitEvalErrorEvent(cfg.Bus, attempt, evalErr)
			feedback := fmt.Sprintf("Goal evaluation could not determine if the task is complete. Original criteria: %s. Please review your work and continue if the goal is not yet fully achieved.", cfg.Criteria)
			result, err = a.Run(ctx, feedback)
			if err != nil {
				return result, nil, err
			}
			continue
		}

		emitEvalEvent(cfg.Bus, agent.EventGoalEvalEnd, attempt, verdict)
		cfg.Evaluator.cfg.Logger.Importantf("goal eval round %d: pass=%v reason=%q", attempt+1, verdict.Pass, verdict.Reason)

		if verdict.Pass {
			return result, verdict, nil
		}

		feedback := verdict.Feedback
		if feedback == "" {
			feedback = fmt.Sprintf("Goal not achieved: %s. Please continue.", verdict.Reason)
		}
		cfg.Evaluator.cfg.Logger.Importantf("goal eval: injecting feedback (round %d): %s", attempt+1, feedback)

		result, err = a.Run(ctx, feedback)
		if err != nil {
			cfg.Evaluator.cfg.Logger.Warnf("goal eval: agent.Run failed after feedback: %s", err)
			return result, verdict, err
		}
		cfg.Evaluator.cfg.Logger.Importantf("goal eval: agent completed after feedback (round %d), stop=%s turns=%d", attempt+1, result.Stop, result.Turns)
	}

	return result, nil, nil
}

func emitEvalEvent(bus *eventbus.Bus[agent.Event], eventType agent.EventType, round int, verdict *Verdict) {
	if bus == nil {
		return
	}
	ev := agent.Event{
		Type:      eventType,
		EvalRound: round,
	}
	if verdict != nil {
		ev.EvalPass = verdict.Pass
		ev.EvalReason = verdict.Reason
	}
	bus.Emit(ev)
}

func emitEvalErrorEvent(bus *eventbus.Bus[agent.Event], round int, err error) {
	if bus == nil {
		return
	}
	bus.Emit(agent.Event{
		Type:      agent.EventGoalEvalError,
		EvalRound: round,
		EvalError: err.Error(),
	})
}

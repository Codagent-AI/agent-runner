package exec

import (
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
)

// DispatchStep routes a step to the correct executor based on its type.
func DispatchStep(
	step *model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (StepOutcome, error) {
	if step.Loop != nil && len(step.Steps) > 0 {
		result, err := ExecuteLoopStep(step, ctx, runner, glob, log, LoopExecuteOptions{})
		if err != nil {
			return OutcomeFailed, err
		}
		return mapLoopOutcome(step, result.Outcome), nil
	}

	if step.Workflow != "" {
		return ExecuteSubWorkflowStep(step, ctx, runner, glob, log)
	}

	if len(step.Steps) > 0 {
		return executeGroupStep(step, step.Steps, ctx, runner, glob, log)
	}

	if step.Command != "" {
		if ctx.PrepareStepHook != nil {
			ctx.PrepareStepHook(step.Mode == model.ModeInteractive)
		}
		return ExecuteShellStep(step, ctx, runner, log)
	}

	if step.Script != "" {
		if ctx.PrepareStepHook != nil {
			ctx.PrepareStepHook(false)
		}
		return ExecuteScriptStep(step, ctx, runner, log)
	}

	if step.Mode == model.ModeUI {
		if ctx.PrepareStepHook != nil {
			ctx.PrepareStepHook(false)
		}
		return ExecuteUIStep(step, ctx, log)
	}

	if step.Agent != "" || step.Prompt != "" {
		if ctx.PrepareStepHook != nil {
			mode := ResolveAgentStepMode(step, ctx)
			ctx.PrepareStepHook(mode == model.ModeInteractive)
		}
		return ExecuteAgentStep(step, ctx, runner, log)
	}

	return OutcomeFailed, nil
}

// MapLoopOutcomeForRunner maps loop outcomes for the runner's step dispatch.
func MapLoopOutcomeForRunner(step *model.Step, outcome StepOutcome) StepOutcome {
	return mapLoopOutcome(step, outcome)
}

func mapLoopOutcome(step *model.Step, outcome StepOutcome) StepOutcome {
	if outcome == OutcomeSuccess {
		return OutcomeSuccess
	}
	if outcome == OutcomeExhausted && !hasBreakCondition(step.Steps) {
		return OutcomeSuccess
	}
	if outcome == OutcomeAborted {
		return OutcomeAborted
	}
	return OutcomeFailed
}

func hasBreakCondition(steps []model.Step) bool {
	for i := range steps {
		if steps[i].BreakIf != "" || hasBreakCondition(steps[i].Steps) {
			return true
		}
	}
	return false
}

func executeGroupStep(
	step *model.Step,
	steps []model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (StepOutcome, error) {
	prefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
	startTime := time.Now()
	emitStepStart(ctx, prefix, startTime, nil)
	for i := range steps {
		outcome, err := DispatchStep(&steps[i], ctx, runner, glob, log)
		if err != nil {
			emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), map[string]any{"error": err.Error()})
			return OutcomeFailed, err
		}
		if outcome == OutcomeAborted {
			emitStepEnd(ctx, prefix, startTime, string(OutcomeAborted), nil)
			return OutcomeAborted, nil
		}
		o := string(outcome)
		ctx.LastStepOutcome = &o
		if outcome == OutcomeFailed && !steps[i].ContinueOnFailure {
			emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), nil)
			return OutcomeFailed, nil
		}
	}
	emitStepEnd(ctx, prefix, startTime, string(OutcomeSuccess), nil)
	return OutcomeSuccess, nil
}

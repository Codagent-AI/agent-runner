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
			invocationContext := ResolveAgentInvocationContext(step, ctx)
			ctx.PrepareStepHook(!invocationContext.IsHeadless())
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
	originalNestingPath := ctx.NestingPath
	childNestingPath := make([]model.NestingSegment, len(originalNestingPath)+1)
	copy(childNestingPath, originalNestingPath)
	childNestingPath[len(originalNestingPath)] = model.NestingSegment{StepID: step.ID}
	ctx.NestingPath = childNestingPath
	defer func() { ctx.NestingPath = originalNestingPath }()
	for i := range steps {
		outcome, err := DispatchStep(&steps[i], ctx, runner, glob, log)
		if err != nil {
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), map[string]any{"error": err.Error()}, step)
			return OutcomeFailed, err
		}
		if outcome == OutcomeAborted {
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeAborted), nil, step)
			return OutcomeAborted, nil
		}
		recordLastStepOutcome(ctx, outcome)
		if outcome == OutcomeFailed && !steps[i].ContinueOnFailure {
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), nil, step)
			return OutcomeFailed, nil
		}
	}
	ctx.NestingPath = originalNestingPath
	emitStepEnd(ctx, prefix, startTime, string(OutcomeSuccess), nil, step)
	return OutcomeSuccess, nil
}

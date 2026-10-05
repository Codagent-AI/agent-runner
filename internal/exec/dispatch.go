package exec

import (
	"fmt"
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
	return runClassified(ctx, func() (StepOutcome, error) {
		return dispatchStep(step, ctx, runner, glob, log)
	})
}

func runClassified(ctx *model.ExecutionContext, run func() (StepOutcome, error)) (StepOutcome, error) {
	ctx.StepFailure = model.StepFailure{}
	outcome, err := run()
	if outcome == OutcomeFailed || outcome == OutcomeExhausted || err != nil {
		if ctx.StepFailure.Kind == "" {
			ctx.StepFailure = model.StepFailure{Kind: model.FailureStep}
		}
	} else {
		ctx.StepFailure = model.StepFailure{}
	}
	return outcome, err
}

func RunClassified(ctx *model.ExecutionContext, run func() (StepOutcome, error)) (StepOutcome, error) {
	return runClassified(ctx, run)
}

func dispatchStep(step *model.Step, ctx *model.ExecutionContext, runner ProcessRunner, glob GlobExpander, log Logger) (StepOutcome, error) {
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
		return ExecuteCheckStep(step, ctx, runner, log)
	}

	if step.Script != "" {
		if ctx.PrepareStepHook != nil {
			ctx.PrepareStepHook(false)
		}
		return ExecuteCheckStep(step, ctx, runner, log)
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

//nolint:funlen // The group sequencer keeps its flow-control branches together.
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
	// Groups share the parent context (no child ExecutionContext is created),
	// so a check inside the group must not see an agent that ran after the
	// group in a sibling scope, and a check after the group must not see an
	// agent that only ran inside it.
	originalLastAgentExecution := ctx.LastAgentExecution
	defer func() { ctx.LastAgentExecution = originalLastAgentExecution }()
	basePath := childNestingPath
	if err := PrimeReplayResume(ctx, basePath); err != nil {
		ctx.StepFailure = model.StepFailure{Kind: model.FailureStep}
		ctx.NestingPath = originalNestingPath
		emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), map[string]any{"error": err.Error()}, step)
		return OutcomeFailed, err
	}
	for i := 0; i < len(steps); i++ {
		// Members honour skip_if exactly as loop bodies and sub-workflow steps do.
		skip, skipErr := ShouldSkipStep(steps[i].SkipIf, ctx, steps[i].ID)
		if skipErr != nil {
			ctx.StepFailure = model.StepFailure{Kind: model.FailureStep}
			err := fmt.Errorf("step %q skip_if evaluation failed: %w", steps[i].ID, skipErr)
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), map[string]any{"error": err.Error()}, step)
			return OutcomeFailed, err
		}
		if skip {
			emitSkippedChildStep(ctx, &steps[i])
			recordPreviousStep(ctx, &steps[i], OutcomeSkipped)
			continue
		}
		outcome, err := DispatchStep(&steps[i], ctx, runner, glob, log)
		if err != nil {
			if ctx.StepFailure.Kind == "" {
				ctx.StepFailure = model.StepFailure{Kind: model.FailureStep}
			}
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), map[string]any{"error": err.Error()}, step)
			return OutcomeFailed, err
		}

		rw, absorbed := AfterStepDispatch(ctx, steps, i, basePath, outcome)
		if rw.Stopped {
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), nil, step)
			return OutcomeFailed, nil
		}
		if rw.Rewound {
			i = rw.NextIndex - 1
			continue
		}
		if absorbed {
			ctx.StepFailure = model.StepFailure{}
			continue
		}

		if outcome == OutcomeAborted {
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeAborted), nil, step)
			return OutcomeAborted, nil
		}
		closeToleratedFrame(ctx, &steps[i], outcome)
		recordPreviousStep(ctx, &steps[i], outcome)
		if isBlockingOutcome(&steps[i], outcome) {
			ctx.NestingPath = originalNestingPath
			emitStepEnd(ctx, prefix, startTime, string(OutcomeFailed), nil, step)
			return OutcomeFailed, nil
		}
		ctx.StepFailure = model.StepFailure{}
	}
	ctx.NestingPath = originalNestingPath
	emitStepEnd(ctx, prefix, startTime, string(OutcomeSuccess), nil, step)
	return OutcomeSuccess, nil
}

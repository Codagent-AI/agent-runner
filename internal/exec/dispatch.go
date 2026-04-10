package exec

import (
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
		return mapLoopOutcome(result.Outcome), nil
	}

	if step.Workflow != "" {
		return ExecuteSubWorkflowStep(step, ctx, runner, glob, log)
	}

	if len(step.Steps) > 0 {
		return executeGroupStep(step.Steps, ctx, runner, glob, log)
	}

	if step.Command != "" {
		return ExecuteShellStep(step, ctx, runner, log)
	}

	if step.Agent != "" || step.Prompt != "" {
		return ExecuteAgentStep(step, ctx, runner, log)
	}

	return OutcomeFailed, nil
}

// MapLoopOutcomeForRunner maps loop outcomes for the runner's step dispatch.
func MapLoopOutcomeForRunner(outcome StepOutcome) StepOutcome {
	return mapLoopOutcome(outcome)
}

func mapLoopOutcome(outcome StepOutcome) StepOutcome {
	if outcome == OutcomeSuccess || outcome == OutcomeExhausted {
		return OutcomeSuccess
	}
	if outcome == OutcomeAborted {
		return OutcomeAborted
	}
	return OutcomeFailed
}

func executeGroupStep(
	steps []model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	glob GlobExpander,
	log Logger,
) (StepOutcome, error) {
	for i := range steps {
		outcome, err := DispatchStep(&steps[i], ctx, runner, glob, log)
		if err != nil {
			return OutcomeFailed, err
		}
		if outcome == OutcomeAborted {
			return OutcomeAborted, nil
		}
		o := string(outcome)
		ctx.LastStepOutcome = &o
		if outcome == OutcomeFailed && !steps[i].ContinueOnFailure {
			return OutcomeFailed, nil
		}
	}
	return OutcomeSuccess, nil
}

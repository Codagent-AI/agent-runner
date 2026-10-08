package exec

import "github.com/codagent/agent-runner/internal/model"

func recordPreviousStep(ctx *model.ExecutionContext, step *model.Step, outcome StepOutcome) {
	o := string(outcome)
	previous := &model.PreviousStepRecord{Outcome: o}
	if outcome == OutcomeFailed || outcome == OutcomeExhausted {
		previous.FailureKind = ctx.StepFailure.Kind
		if previous.FailureKind == "" {
			previous.FailureKind = model.FailureStep
		}
	}
	if step != nil && outcome != OutcomeSkipped {
		previous.CrashObserved = ctx.Crashes.ObservedUnder(stepPath(ctx, step))
	}
	ctx.PreviousStep = previous
}

// RecordPreviousStep updates flow-control and built-in evidence together.
func RecordPreviousStep(ctx *model.ExecutionContext, step *model.Step, outcome StepOutcome) {
	recordPreviousStep(ctx, step, outcome)
}

func stepPath(ctx *model.ExecutionContext, step *model.Step) []model.NestingSegment {
	path := append([]model.NestingSegment(nil), ctx.NestingPath...)
	if step != nil {
		path = append(path, model.NestingSegment{StepID: step.ID})
	}
	return path
}

// IsWarningOutcome reports whether step deliberately makes this terminal
// execution outcome non-blocking. Aborted executions are never warnings.
func IsWarningOutcome(step *model.Step, outcome StepOutcome) bool {
	return step != nil && step.WarnOnFailure && (outcome == OutcomeFailed || outcome == OutcomeExhausted)
}

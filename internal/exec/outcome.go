package exec

import "github.com/codagent/agent-runner/internal/model"

func recordLastStepOutcome(ctx *model.ExecutionContext, outcome StepOutcome) {
	o := string(outcome)
	ctx.LastStepOutcome = &o
}

// IsWarningOutcome reports whether step deliberately makes this terminal
// execution outcome non-blocking. Aborted executions are never warnings.
func IsWarningOutcome(step *model.Step, outcome StepOutcome) bool {
	return step != nil && step.WarnOnFailure && (outcome == OutcomeFailed || outcome == OutcomeExhausted)
}

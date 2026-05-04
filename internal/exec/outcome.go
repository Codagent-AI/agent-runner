package exec

import "github.com/codagent/agent-runner/internal/model"

func recordLastStepOutcome(ctx *model.ExecutionContext, outcome StepOutcome) {
	o := string(outcome)
	ctx.LastStepOutcome = &o
}

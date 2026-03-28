package validate

import (
	"fmt"

	"github.com/codagent/agent-runner/internal/model"
)

// Options controls constraint validation behavior.
type Options struct {
	IsSubWorkflow bool
}

// WorkflowConstraints validates positional rules that cannot be expressed
// in the schema alone (e.g., skip_if on first step, break_if outside loop).
func WorkflowConstraints(w model.Workflow, opts Options) error {
	isTopLevel := !opts.IsSubWorkflow
	return validateStepList(w.Steps, stepContext{insideLoop: false, isTopLevel: isTopLevel})
}

type stepContext struct {
	insideLoop bool
	isTopLevel bool
}

func validateStepList(steps []model.Step, ctx stepContext) error {
	for i := range steps {
		step := &steps[i]

		if err := validateSingleStep(step, i, ctx); err != nil {
			return err
		}

		if len(step.Steps) > 0 {
			childCtx := stepContext{
				insideLoop: ctx.insideLoop || step.Loop != nil,
				isTopLevel: false,
			}
			if err := validateStepList(step.Steps, childCtx); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSingleStep(step *model.Step, index int, ctx stepContext) error {
	if step.SkipIf != "" && index == 0 {
		return fmt.Errorf(`step %q: skip_if cannot be used on the first step in scope`, step.ID)
	}

	if step.BreakIf != "" && !ctx.insideLoop {
		return fmt.Errorf(`step %q: break_if is only allowed inside a loop body`, step.ID)
	}

	if step.Session == model.SessionInherit && ctx.isTopLevel {
		return fmt.Errorf(`step %q: session "inherit" is not allowed in a top-level workflow`, step.ID)
	}

	return nil
}

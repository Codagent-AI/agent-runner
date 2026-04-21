// Package validate checks workflow-level constraints beyond schema validation.
package validate

import (
	"fmt"

	"github.com/codagent/agent-runner/internal/flowctl"
	"github.com/codagent/agent-runner/internal/model"
)

// Options controls constraint validation behavior.
type Options struct {
	IsSubWorkflow bool
}

// WorkflowConstraints validates positional rules that cannot be expressed
// in the schema alone (e.g., skip_if on first step, break_if outside loop).
// Also validates named session declarations and references.
func WorkflowConstraints(w *model.Workflow, opts Options) error {
	isTopLevel := !opts.IsSubWorkflow

	declared, err := validateSessionDeclarations(w)
	if err != nil {
		return err
	}

	if err := validateStepList(w.Steps, stepContext{insideLoop: false, isTopLevel: isTopLevel}, declared); err != nil {
		return err
	}
	return nil
}

// validateSessionDeclarations checks the sessions: block of a workflow:
//   - reserved names (new, resume, inherit) are rejected
//   - duplicate names within the same file are rejected
//
// Returns the set of declared session names for reference validation.
func validateSessionDeclarations(w *model.Workflow) (map[string]bool, error) {
	declared := make(map[string]bool, len(w.Sessions))
	for _, decl := range w.Sessions {
		if decl.Name == "" {
			return nil, fmt.Errorf("sessions: each declaration must have a non-empty name")
		}
		if !model.IsNamedSession(model.SessionStrategy(decl.Name)) {
			return nil, fmt.Errorf("sessions: %q is a reserved session keyword and cannot be used as a session name", decl.Name)
		}
		if decl.Agent == "" {
			return nil, fmt.Errorf("sessions: declaration %q must specify an agent", decl.Name)
		}
		if declared[decl.Name] {
			return nil, fmt.Errorf("sessions: duplicate declaration %q", decl.Name)
		}
		declared[decl.Name] = true
	}
	return declared, nil
}

type stepContext struct {
	insideLoop bool
	isTopLevel bool
}

func validateStepList(steps []model.Step, ctx stepContext, declared map[string]bool) error {
	for i := range steps {
		step := &steps[i]

		if err := validateSingleStep(step, i, ctx, declared); err != nil {
			return err
		}

		if len(step.Steps) > 0 {
			childCtx := stepContext{
				insideLoop: ctx.insideLoop || step.Loop != nil,
				isTopLevel: false,
			}
			if err := validateStepList(step.Steps, childCtx, declared); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateSingleStep(step *model.Step, index int, ctx stepContext, declared map[string]bool) error {
	if step.SkipIf != "" && index == 0 {
		if _, isShell := flowctl.ShellSkipCommand(step.SkipIf); !isShell {
			return fmt.Errorf(`step %q: skip_if cannot be used on the first step in scope`, step.ID)
		}
	}

	if step.BreakIf != "" && !ctx.insideLoop {
		return fmt.Errorf(`step %q: break_if is only allowed inside a loop body`, step.ID)
	}

	if step.Session == model.SessionInherit && ctx.isTopLevel {
		return fmt.Errorf(`step %q: session "inherit" is not allowed in a top-level workflow`, step.ID)
	}

	// Named session reference: must be declared in this file's sessions block.
	// At runtime a sub-workflow may inherit declarations from its parent, but
	// standalone validation cannot see the parent's declarations.
	if model.IsNamedSession(step.Session) && !declared[string(step.Session)] {
		return fmt.Errorf(`step %q: session %q is not declared in this workflow's sessions block`, step.ID, step.Session)
	}

	return nil
}

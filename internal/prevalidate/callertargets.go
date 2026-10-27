package prevalidate

import (
	"errors"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
)

// CallerTargets validates the sub-workflow targets of a fresh builtin run that
// depend on parameter values the caller supplied. Builtins are checked at build
// time, but a caller-supplied target (for example validation_workflow) is only
// known at run start. Each such target is resolved strictly and then validated
// in full together with its own composition. Fixed builtin helpers are only
// traversed to follow caller-derived values into deeper targets; they are not
// validated here. A run with no caller-supplied params validates nothing.
func CallerTargets(rootPath string, bound map[string]string, supplied []string, opts Options) (Result, error) {
	if len(supplied) == 0 {
		return Result{}, nil
	}
	state, err := newWalkState(Strict, opts)
	if err != nil {
		return Result{}, err
	}
	derived := map[string]bool{}
	for _, name := range supplied {
		derived[name] = true
	}
	w := &callerWalk{walkState: state, seen: map[string]bool{}}
	if err := w.file(rootPath, bound, derived); err != nil {
		return state.result, err
	}
	if err := state.probeTriples(); err != nil {
		return state.result, err
	}
	return state.result, nil
}

type callerWalk struct {
	*walkState
	seen map[string]bool
}

func (w *callerWalk) file(path string, params map[string]string, derived map[string]bool) error {
	key := loader.SourceID(path) + "\x00" + stableParamKey(params)
	if w.seen[key] {
		return nil
	}
	w.seen[key] = true
	workflow, err := loader.LoadWorkflow(path, loader.Options{})
	if err != nil {
		return ValidationError{File: path, Message: err.Error()}
	}
	if err := w.registerSessions(path, workflow.Sessions); err != nil {
		return err
	}
	visible := bindParamDefaults(workflow.Params, params)
	names := workflowParamNames(workflow.Params)
	return w.steps(path, workflow.Steps, visible, names, derived, map[string]bool{})
}

func (w *callerWalk) steps(path string, steps []model.Step, params map[string]string, names, derived, captured map[string]bool) error {
	for i := range steps {
		step := &steps[i]
		if err := w.step(path, step, params, names, derived, captured); err != nil {
			return err
		}
		if step.Capture != "" {
			captured[step.Capture] = true
		}
		if step.OutcomeCapture != "" {
			captured[step.OutcomeCapture] = true
		}
	}
	return nil
}

func (w *callerWalk) step(path string, step *model.Step, params map[string]string, names, derived, captured map[string]bool) error {
	if step.Workflow != "" {
		if err := w.subWorkflow(path, step, params, names, derived, captured); err != nil {
			return err
		}
	}
	if len(step.Steps) == 0 {
		return nil
	}
	childParams, childNames := childScopeForStep(step, params, names)
	return w.steps(path, step.Steps, childParams, childNames, derived, copyBoolMap(captured))
}

func (w *callerWalk) subWorkflow(path string, step *model.Step, params map[string]string, names, derived, captured map[string]bool) error {
	if referencesAny(step.Workflow, derived) {
		subPath, subParams, err := w.resolveSubWorkflow(path, step, params, names, captured)
		if err != nil || subPath == "" {
			return err
		}
		err = w.walkFile(subPath, subParams, true, nil)
		var validationErr ValidationError
		if errors.As(err, &validationErr) && validationErr.StepID == "" {
			validationErr.StepID = step.ID
			validationErr.Field = "workflow"
			validationErr.Value = step.Workflow
			return validationErr
		}
		return err
	}
	childDerived := map[string]bool{}
	for name, value := range step.Params {
		if referencesAny(value, derived) {
			childDerived[name] = true
		}
	}
	if len(childDerived) == 0 {
		return nil
	}
	// A fixed target that cannot be resolved here is build-time checked.
	subPath, subParams, err := w.resolveSubWorkflow(path, step, params, names, captured)
	if err != nil || subPath == "" {
		return nil //nolint:nilerr // fixed builtin targets are validated at build time
	}
	return w.file(subPath, subParams, childDerived)
}

func referencesAny(s string, names map[string]bool) bool {
	for _, ref := range placeholders(s) {
		if names[ref] {
			return true
		}
	}
	return false
}

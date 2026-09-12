package model

import "fmt"

// RepairForm identifies which shape a Repair block uses.
type RepairForm string

// Repair form constants.
const (
	RepairInline RepairForm = "inline"
	RepairRerun  RepairForm = "rerun"
)

// Repair declares how a failed shell or script check may be repaired.
type Repair struct {
	Prompt  string          `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Session SessionStrategy `yaml:"session,omitempty" json:"session,omitempty"`
	Agent   string          `yaml:"agent,omitempty" json:"agent,omitempty"`
	Rerun   string          `yaml:"rerun,omitempty" json:"rerun,omitempty"`
	Max     *int            `yaml:"max,omitempty" json:"max,omitempty"`
	// RangeCaptures is computed at load time (validateRepairTargets), not
	// read from YAML: the names of every capture variable produced by a step
	// in the rerun replay range (the target through the step before the
	// check), in that order. The check executor replays these steps and
	// re-captures these variables before re-running the check.
	RangeCaptures []string `yaml:"-" json:"rangeCaptures,omitempty"`
}

// Form reports which repair shape this block uses. Callers should validate
// the block before relying on this; on an invalid block (both or neither form
// set) it defaults to RepairInline.
func (r *Repair) Form() RepairForm {
	if r.Rerun != "" {
		return RepairRerun
	}
	return RepairInline
}

// Budget returns the maximum number of repair attempts, defaulting to 1.
func (r *Repair) Budget() int {
	if r.Max == nil {
		return 1
	}
	return *r.Max
}

// validate checks field-level rules for a repair block on a shell or script
// step. Scope-level rules (rerun target position, replay range) are checked
// separately by validateRepairTargets, which has access to sibling order.
func (r *Repair) validate(isCheckStep bool) error {
	if !isCheckStep {
		return fmt.Errorf(`"repair" is only allowed on shell and script steps`)
	}

	hasRerun := r.Rerun != ""
	hasInline := r.Prompt != "" || r.Session != "" || r.Agent != ""
	switch {
	case hasRerun && hasInline:
		return fmt.Errorf(`"repair" must use exactly one repair form: inline (prompt) or rerun`)
	case !hasRerun && !hasInline:
		return fmt.Errorf(`"repair" must use exactly one repair form: inline (prompt) or rerun`)
	}

	if r.Max != nil && *r.Max < 1 {
		return fmt.Errorf(`"repair.max" must be a positive integer`)
	}

	if hasRerun {
		return nil
	}

	hasSession := r.Session != ""
	hasAgent := r.Agent != ""
	if hasSession && hasAgent {
		return fmt.Errorf(`inline "repair" must name "session" or "agent", not both`)
	}
	if !hasSession && !hasAgent {
		return fmt.Errorf(`inline "repair" must name "session" or "agent"`)
	}
	if r.Session == SessionNew {
		return fmt.Errorf(`inline "repair" cannot use "session: new"; a fresh session must use "agent" instead`)
	}
	if hasSession && r.Session != SessionResume && r.Session != SessionInherit && !IsNamedSession(r.Session) {
		return fmt.Errorf(`invalid repair session strategy %q`, r.Session)
	}

	return nil
}

// validateRepairTargets checks scope-level repair rules for one sequential
// list of steps: a rerun target must be an earlier sibling in this same list,
// and the replay range (target through the step before the check) must be
// free of break_if and outcome-relative skip_if. It recurses into every
// nested steps: list (loop bodies, groups) since only the containing list
// knows sibling order.
func validateRepairTargets(steps []Step) error {
	for i := range steps {
		step := &steps[i]
		if step.Repair != nil && step.Repair.Form() == RepairRerun {
			if err := validateRerunTarget(steps, i); err != nil {
				return err
			}
		}
		if len(step.Steps) > 0 {
			if err := validateRepairTargets(step.Steps); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRerunTarget(steps []Step, checkIndex int) error {
	target := steps[checkIndex].Repair.Rerun
	targetIndex := -1
	for i := range checkIndex {
		if steps[i].ID == target {
			targetIndex = i
			break
		}
	}
	if targetIndex == -1 {
		return fmt.Errorf(
			`step %q: repair rerun target %q must be an earlier step in the same scope`,
			steps[checkIndex].ID, target,
		)
	}
	var rangeCaptures []string
	for i := targetIndex; i < checkIndex; i++ {
		if steps[i].BreakIf != "" {
			return fmt.Errorf(
				`step %q: repair replay range cannot contain %q, which declares "break_if"`,
				steps[checkIndex].ID, steps[i].ID,
			)
		}
		if steps[i].SkipIf == "previous_success" {
			return fmt.Errorf(
				`step %q: repair replay range cannot contain %q, which declares outcome-relative "skip_if"`,
				steps[checkIndex].ID, steps[i].ID,
			)
		}
		if steps[i].Capture != "" {
			rangeCaptures = append(rangeCaptures, steps[i].Capture)
		}
	}
	steps[checkIndex].Repair.RangeCaptures = rangeCaptures
	return nil
}

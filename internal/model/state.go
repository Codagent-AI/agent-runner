package model

import (
	"encoding/json"
	"fmt"
)

// NestedStepState tracks execution position within nested workflows/loops.
//
// For a loop step, Iteration is the next iteration index to execute on resume.
// At iteration N start it is set to N; when iteration N completes it advances
// to N+1. When the loop finishes, Iteration equals the total count and
// Completed is true.
type NestedStepState struct {
	StepID            string            `json:"stepId"`
	SessionIDs        map[string]string `json:"sessionIds"`
	SessionProfiles   map[string]string `json:"sessionProfiles,omitempty"`
	CapturedVariables map[string]string `json:"capturedVariables"`
	LastSessionStepID string            `json:"lastSessionStepId,omitempty"`
	// NamedSessions and NamedSessionDecls are only meaningful at the root
	// NestedStepState level (written by runner.writeStepState). Nested entries
	// produced by sub-workflow and loop progress records leave these nil.
	NamedSessions     map[string]string `json:"namedSessions,omitempty"`
	NamedSessionDecls map[string]string `json:"namedSessionDecls,omitempty"`
	Completed         bool              `json:"completed,omitempty"`
	Iteration         *int              `json:"iteration,omitempty"`
	Child             *NestedStepState  `json:"child"`
}

// CurrentStep can be either a plain string (legacy) or a NestedStepState.
type CurrentStep struct {
	StepID string           // Set when the value is a plain string.
	Nested *NestedStepState // Set when the value is an object.
}

// MarshalJSON encodes CurrentStep as either a string or an object.
func (cs CurrentStep) MarshalJSON() ([]byte, error) {
	if cs.Nested != nil {
		return json.Marshal(cs.Nested)
	}
	return json.Marshal(cs.StepID)
}

// UnmarshalJSON decodes CurrentStep from either a string or an object.
func (cs *CurrentStep) UnmarshalJSON(data []byte) error {
	// Try string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		cs.StepID = s
		cs.Nested = nil
		return nil
	}

	// Try object.
	var ns NestedStepState
	if err := json.Unmarshal(data, &ns); err != nil {
		return err
	}
	cs.Nested = &ns
	cs.StepID = ""
	return nil
}

// RunState is the serialized workflow execution state.
type RunState struct {
	WorkflowFile string            `json:"workflowFile"`
	WorkflowName string            `json:"workflowName"`
	CurrentStep  CurrentStep       `json:"currentStep"`
	Params       map[string]string `json:"params"`
	WorkflowHash string            `json:"workflowHash"`
	// Completed is set to true when the workflow has finished successfully.
	// The state file is preserved (not deleted) so the TUI can still display
	// the run's metadata after completion.
	Completed bool `json:"completed,omitempty"`
}

// ResolveResumeStepResult holds the outcome of resolving which step to resume from.
type ResolveResumeStepResult struct {
	StepID  string // The step ID to resume from (empty if all steps completed).
	AllDone bool   // True when the recorded step was the last step and it completed.
}

// ResolveResumeStep determines which step to actually start executing on resume.
// If the recorded step completed successfully, it advances to the next step.
// If the recorded step did not complete, it returns that step (to re-run it).
func ResolveResumeStep(steps []Step, recordedStepID string, completed bool) (ResolveResumeStepResult, error) {
	for i := range steps {
		if steps[i].ID == recordedStepID {
			if completed {
				if i+1 < len(steps) {
					return ResolveResumeStepResult{StepID: steps[i+1].ID}, nil
				}
				return ResolveResumeStepResult{AllDone: true}, nil
			}
			return ResolveResumeStepResult{StepID: recordedStepID}, nil
		}
	}
	return ResolveResumeStepResult{}, fmt.Errorf("step %q not found", recordedStepID)
}

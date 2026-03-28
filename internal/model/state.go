package model

import "encoding/json"

// NestedStepState tracks execution position within nested workflows/loops.
type NestedStepState struct {
	StepID            string            `json:"stepId"`
	SessionIDs        map[string]string `json:"sessionIds"`
	CapturedVariables map[string]string `json:"capturedVariables"`
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
}

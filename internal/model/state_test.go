package model

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestCurrentStepMarshal(t *testing.T) {
	t.Run("marshals string currentStep", func(t *testing.T) {
		state := RunState{
			WorkflowFile: "test.yaml",
			WorkflowName: "test",
			CurrentStep:  CurrentStep{StepID: "step2"},
			Params:       map[string]string{"key": "val"},
			WorkflowHash: "abc123",
		}

		data, err := json.Marshal(state)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var restored RunState
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if restored.CurrentStep.StepID != "step2" {
			t.Fatalf("expected stepID 'step2', got %q", restored.CurrentStep.StepID)
		}
		if restored.CurrentStep.Nested != nil {
			t.Fatal("expected nil nested")
		}
	})

	t.Run("marshals nested currentStep", func(t *testing.T) {
		state := RunState{
			WorkflowFile: "test.yaml",
			WorkflowName: "test",
			CurrentStep: CurrentStep{
				Nested: &NestedStepState{
					StepID:            "outer",
					SessionIDs:        map[string]string{"s1": "abc"},
					CapturedVariables: map[string]string{"out": "val"},
					Child: &NestedStepState{
						StepID:            "inner",
						SessionIDs:        map[string]string{},
						CapturedVariables: map[string]string{},
						Child:             nil,
					},
				},
			},
			Params:       map[string]string{},
			WorkflowHash: "def456",
		}

		data, err := json.Marshal(state)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var restored RunState
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if restored.CurrentStep.Nested == nil {
			t.Fatal("expected nested step state")
		}
		if restored.CurrentStep.Nested.StepID != "outer" {
			t.Fatalf("expected 'outer', got %q", restored.CurrentStep.Nested.StepID)
		}
		if restored.CurrentStep.Nested.Child == nil {
			t.Fatal("expected child state")
		}
		if restored.CurrentStep.Nested.Child.StepID != "inner" {
			t.Fatalf("expected 'inner', got %q", restored.CurrentStep.Nested.Child.StepID)
		}
	})

	t.Run("reads legacy flat currentStep", func(t *testing.T) {
		raw := `{"workflowFile":"test.yaml","workflowName":"test","currentStep":"step2","params":{},"workflowHash":"abc"}`
		var state RunState
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if state.CurrentStep.StepID != "step2" {
			t.Fatalf("expected 'step2', got %q", state.CurrentStep.StepID)
		}
	})

	t.Run("round-trips nested state with capturedVariables", func(t *testing.T) {
		original := RunState{
			WorkflowFile: "w.yaml",
			WorkflowName: "w",
			CurrentStep: CurrentStep{
				Nested: &NestedStepState{
					StepID:            "loop1",
					SessionIDs:        map[string]string{"step1": "s1"},
					CapturedVariables: map[string]string{"output": "hello"},
					Child:             nil,
				},
			},
			Params:       map[string]string{"p": "v"},
			WorkflowHash: "hash",
		}

		data, _ := json.Marshal(original)
		var restored RunState
		json.Unmarshal(data, &restored)

		if diff := cmp.Diff(original, restored); diff != "" {
			t.Fatalf("round-trip mismatch:\n%s", diff)
		}
	})
}

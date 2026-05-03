package model

import (
	"encoding/json"
	"strings"
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
					CapturedVariables: map[string]CapturedValue{"out": {Kind: CaptureString, Str: "val"}},
					Child: &NestedStepState{
						StepID:            "inner",
						SessionIDs:        map[string]string{},
						CapturedVariables: map[string]CapturedValue{},
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
					CapturedVariables: map[string]CapturedValue{"output": {Kind: CaptureString, Str: "hello"}},
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

func TestNestedStepStateCapturedVariablesUseTypedEnvelope(t *testing.T) {
	original := RunState{
		WorkflowFile: "w.yaml",
		WorkflowName: "w",
		CurrentStep: CurrentStep{
			Nested: &NestedStepState{
				StepID:     "detect",
				SessionIDs: map[string]string{},
				CapturedVariables: map[string]CapturedValue{
					"text": {Kind: CaptureString, Str: "hello"},
					"list": {Kind: CaptureList, List: []string{"claude", "codex"}},
					"map":  {Kind: CaptureMap, Map: map[string]string{"adapter": "claude"}},
				},
			},
		},
		Params:       map[string]string{},
		WorkflowHash: "hash",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	wantFragments := []string{
		`"text":{"kind":"string","value":"hello"}`,
		`"list":{"kind":"list","value":["claude","codex"]}`,
		`"map":{"kind":"map","value":{"adapter":"claude"}}`,
	}
	for _, fragment := range wantFragments {
		if !strings.Contains(string(data), fragment) {
			t.Fatalf("encoded state missing %s in %s", fragment, data)
		}
	}

	var restored RunState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if diff := cmp.Diff(original, restored); diff != "" {
		t.Fatalf("round-trip mismatch (-want +got):\n%s", diff)
	}
}

func TestNestedStepStateReadsLegacyStringCaptures(t *testing.T) {
	raw := `{"workflowFile":"w.yaml","workflowName":"w","currentStep":{"stepId":"s","sessionIds":{},"capturedVariables":{"out":"legacy"}},"params":{},"workflowHash":"hash"}`

	var state RunState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("unmarshal legacy: %v", err)
	}

	got := state.CurrentStep.Nested.CapturedVariables["out"]
	want := CapturedValue{Kind: CaptureString, Str: "legacy"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("legacy capture mismatch (-want +got):\n%s", diff)
	}
}

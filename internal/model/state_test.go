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

func TestResolveResumeStep(t *testing.T) {
	steps := []Step{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	t.Run("no frame, not completed, stays at recorded step", func(t *testing.T) {
		got, err := ResolveResumeStep(steps, "b", false, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.StepID != "b" || got.AllDone {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("no frame, completed, advances to next step", func(t *testing.T) {
		got, err := ResolveResumeStep(steps, "b", true, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.StepID != "c" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("no frame, completed on last step, all done", func(t *testing.T) {
		got, err := ResolveResumeStep(steps, "c", true, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got.AllDone {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("no frame, recorded step missing errors", func(t *testing.T) {
		_, err := ResolveResumeStep(steps, "missing", false, nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	for _, phase := range []string{RepairPhaseChecking, RepairPhaseRepairing, RepairPhaseReplaying} {
		t.Run("frame phase "+phase+" keeps attempts and recorded step", func(t *testing.T) {
			frame := &RepairFrame{CheckID: "check", Form: "inline", Phase: phase, Attempts: 1, Budget: 2}
			got, err := ResolveResumeStep(steps, "b", false, frame)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.StepID != "b" {
				t.Fatalf("got %+v", got)
			}
			if frame.Attempts != 1 || frame.Phase != phase {
				t.Fatalf("frame mutated unexpectedly: %+v", frame)
			}
		})
	}

	t.Run("frame phase replaying, recorded step completed, advances", func(t *testing.T) {
		frame := &RepairFrame{CheckID: "check", Form: "rerun", Phase: RepairPhaseReplaying, Attempts: 1, Budget: 2, Target: "a"}
		got, err := ResolveResumeStep(steps, "a", true, frame)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.StepID != "b" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("frame phase failed, form rerun, resumes at target with fresh budget", func(t *testing.T) {
		frame := &RepairFrame{CheckID: "check", Form: "rerun", Phase: RepairPhaseFailed, Attempts: 2, Budget: 2, Target: "a"}
		got, err := ResolveResumeStep(steps, "check", false, frame)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.StepID != "a" {
			t.Fatalf("got %+v", got)
		}
		if frame.Attempts != 0 {
			t.Fatalf("expected reset attempts, got %d", frame.Attempts)
		}
		if frame.Phase != RepairPhaseReplaying {
			t.Fatalf("expected phase replaying, got %q", frame.Phase)
		}
	})

	t.Run("frame phase failed, form inline, resumes at check with fresh budget", func(t *testing.T) {
		frame := &RepairFrame{CheckID: "check", Form: "inline", Phase: RepairPhaseFailed, Attempts: 2, Budget: 2}
		got, err := ResolveResumeStep(steps, "b", false, frame)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.StepID != "b" {
			t.Fatalf("got %+v", got)
		}
		if frame.Attempts != 0 {
			t.Fatalf("expected reset attempts, got %d", frame.Attempts)
		}
	})

	t.Run("frame phase failed, form rerun, stale target errors naming the target", func(t *testing.T) {
		frame := &RepairFrame{CheckID: "check", Form: "rerun", Phase: RepairPhaseFailed, Attempts: 2, Budget: 2, Target: "gone"}
		_, err := ResolveResumeStep(steps, "check", false, frame)
		if err == nil || !strings.Contains(err.Error(), "gone") {
			t.Fatalf("expected error naming missing target, got %v", err)
		}
	})
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

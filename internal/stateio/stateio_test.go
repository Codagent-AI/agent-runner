package stateio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestWriteAndReadState(t *testing.T) {
	t.Run("writes and reads state file", func(t *testing.T) {
		dir := t.TempDir()
		state := model.RunState{
			WorkflowFile: "test.yaml",
			WorkflowName: "Test",
			CurrentStep:  model.CurrentStep{StepID: "step2"},
			Params:       map[string]string{"key": "val"},
			WorkflowHash: "abc123",
		}
		if err := WriteState(state, dir); err != nil {
			t.Fatalf("write error: %v", err)
		}

		restored, err := ReadState(filepath.Join(dir, "agent-runner-state.json"))
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
		if restored.CurrentStep.StepID != "step2" {
			t.Fatalf("expected step2, got %q", restored.CurrentStep.StepID)
		}
		if restored.Params["key"] != "val" {
			t.Fatal("expected param key=val")
		}
		if restored.WorkflowName != "Test" {
			t.Fatal("expected workflow name Test")
		}
	})

	t.Run("deletes state file", func(t *testing.T) {
		dir := t.TempDir()
		state := model.RunState{
			WorkflowFile: "test.yaml",
			WorkflowName: "Test",
			CurrentStep:  model.CurrentStep{StepID: "s"},
			Params:       map[string]string{},
			WorkflowHash: "h",
		}
		WriteState(state, dir)
		DeleteState(dir)
		_, err := os.Stat(filepath.Join(dir, "agent-runner-state.json"))
		if !os.IsNotExist(err) {
			t.Fatal("expected file to be deleted")
		}
	})

	t.Run("deleteState is a no-op if file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		DeleteState(dir) // should not panic
	})

	t.Run("readState throws for missing file", func(t *testing.T) {
		_, err := ReadState("/nonexistent/state.json")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "State file not found") {
			t.Fatalf("expected 'State file not found', got: %v", err)
		}
	})

	t.Run("writes state with nested currentStep", func(t *testing.T) {
		dir := t.TempDir()
		state := model.RunState{
			WorkflowFile: "test.yaml",
			WorkflowName: "Test",
			CurrentStep: model.CurrentStep{
				Nested: &model.NestedStepState{
					StepID:            "outer",
					SessionIDs:        map[string]string{"s1": "abc"},
					CapturedVariables: map[string]string{"out": "val"},
					Child:             nil,
				},
			},
			Params:       map[string]string{},
			WorkflowHash: "h",
		}
		WriteState(state, dir)
		restored, _ := ReadState(filepath.Join(dir, "agent-runner-state.json"))
		if restored.CurrentStep.Nested == nil {
			t.Fatal("expected nested state")
		}
		if restored.CurrentStep.Nested.StepID != "outer" {
			t.Fatal("expected stepId 'outer'")
		}
	})
}

func TestComputeWorkflowHash(t *testing.T) {
	t.Run("returns consistent hash for same content", func(t *testing.T) {
		h1 := ComputeWorkflowHash("hello world")
		h2 := ComputeWorkflowHash("hello world")
		if h1 != h2 {
			t.Fatalf("expected same hash, got %q and %q", h1, h2)
		}
	})

	t.Run("returns different hashes for different content", func(t *testing.T) {
		h1 := ComputeWorkflowHash("hello")
		h2 := ComputeWorkflowHash("world")
		if h1 == h2 {
			t.Fatal("expected different hashes")
		}
	})
}

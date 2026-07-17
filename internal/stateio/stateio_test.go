package stateio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestWriteJSONAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.json")

	if err := WriteJSONAtomic(path, map[string]any{"version": 1, "value": "before"}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteJSONAtomic(path, map[string]any{"version": 1, "value": "after"}); err != nil {
		t.Fatalf("replacement write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if got["value"] != "after" {
		t.Fatalf("value = %v, want after", got["value"])
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".metrics.json-*.tmp"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain after atomic write: %v", matches)
	}
}

func TestWriteJSONAtomicNeverExposesPartialJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.json")
	if err := WriteJSONAtomic(path, map[string]any{"sequence": 0, "payload": strings.Repeat("x", 32_000)}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	errors := make(chan error, 1)
	go func() {
		defer close(done)
		for i := 1; i <= 50; i++ {
			if err := WriteJSONAtomic(path, map[string]any{"sequence": i, "payload": strings.Repeat("x", 32_000)}); err != nil {
				errors <- err
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			select {
			case err := <-errors:
				t.Fatal(err)
			default:
			}
			return
		default:
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatalf("reader observed partial JSON: %v", err)
			}
		}
	}
}

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
		if err := WriteState(&state, dir); err != nil {
			t.Fatalf("write error: %v", err)
		}

		restored, err := ReadState(filepath.Join(dir, "state.json"))
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
		WriteState(&state, dir)
		DeleteState(dir)
		_, err := os.Stat(filepath.Join(dir, "state.json"))
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
		if !strings.Contains(err.Error(), "state file not found") {
			t.Fatalf("expected 'state file not found', got: %v", err)
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
					CapturedVariables: map[string]model.CapturedValue{"out": model.NewCapturedString("val")},
					Child:             nil,
				},
			},
			Params:       map[string]string{},
			WorkflowHash: "h",
		}
		WriteState(&state, dir)
		restored, _ := ReadState(filepath.Join(dir, "state.json"))
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

package openspec

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/model"
)

type mockCmdRunner struct {
	calls   [][]string
	results map[string]string // key = joined args
	errors  map[string]error
}

func (m *mockCmdRunner) Run(args []string) (string, error) {
	m.calls = append(m.calls, args)
	key := strings.Join(args, " ")
	if err, ok := m.errors[key]; ok {
		return "", err
	}
	if result, ok := m.results[key]; ok {
		return result, nil
	}
	return "", fmt.Errorf("unexpected command: %s", key)
}

func statusJSON(artifacts []artifact) string {
	data, _ := json.Marshal(statusOutput{
		ChangeName: "test-change",
		ChangeDir:  "openspec/changes/test-change",
		Artifacts:  artifacts,
	})
	return string(data)
}

func instructionsJSON(artifactID, instruction string) string {
	data, _ := json.Marshal(instructionsOutput{
		ArtifactID:  artifactID,
		SchemaName:  "spec-driven",
		Instruction: instruction,
		OutputPath:  artifactID + ".md",
		Template:    artifactID + ".md",
		ChangeDir:   "openspec/changes/test-change",
	})
	return string(data)
}

func TestOpenSpecEngine(t *testing.T) {
	t.Run("ValidateWorkflow checks all artifacts have steps", func(t *testing.T) {
		cmd := &mockCmdRunner{
			results: map[string]string{
				"status --change my-change --json": statusJSON([]artifact{
					{ID: "proposal", Status: "pending"},
					{ID: "design", Status: "pending"},
				}),
			},
		}
		eng := NewEngineWithRunner(map[string]any{"change_param": "change_name"}, cmd)
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				{ID: "proposal", Mode: model.ModeAutonomous, Prompt: "p", Session: model.SessionNew},
				{ID: "design", Mode: model.ModeAutonomous, Prompt: "p", Session: model.SessionNew},
			},
		}
		err := eng.ValidateWorkflow(&w, map[string]string{"change_name": "my-change"}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ValidateWorkflow errors when steps missing for artifacts", func(t *testing.T) {
		cmd := &mockCmdRunner{
			results: map[string]string{
				"status --change my-change --json": statusJSON([]artifact{
					{ID: "proposal", Status: "pending"},
					{ID: "missing-step", Status: "pending"},
				}),
			},
		}
		eng := NewEngineWithRunner(map[string]any{"change_param": "change_name"}, cmd)
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				{ID: "proposal", Mode: model.ModeAutonomous, Prompt: "p", Session: model.SessionNew},
			},
		}
		err := eng.ValidateWorkflow(&w, map[string]string{"change_name": "my-change"}, "")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "missing-step") {
			t.Fatalf("expected error about missing-step, got: %v", err)
		}
	})

	t.Run("ValidateWorkflow passes for parent workflow with sub-workflow steps", func(t *testing.T) {
		cmd := &mockCmdRunner{
			results: map[string]string{
				"status --change my-change --json": statusJSON([]artifact{
					{ID: "proposal", Status: "pending"},
					{ID: "specs", Status: "pending"},
					{ID: "design", Status: "pending"},
				}),
			},
		}
		eng := NewEngineWithRunner(map[string]any{"change_param": "change_name"}, cmd)
		w := model.Workflow{
			Name: "parent",
			Steps: []model.Step{
				{ID: "plan", Workflow: "plan-change.yaml"},
				{ID: "implement", Workflow: "implement-change.yaml"},
			},
		}
		err := eng.ValidateWorkflow(&w, map[string]string{"change_name": "my-change"}, "")
		if err != nil {
			t.Fatalf("expected no error for parent workflow with sub-workflows, got: %v", err)
		}
	})

	t.Run("EnrichPrompt returns enrichment for matching artifact", func(t *testing.T) {
		cmd := &mockCmdRunner{
			results: map[string]string{
				"status --change my-change --json": statusJSON([]artifact{
					{ID: "proposal", Status: "pending"},
				}),
				"instructions proposal --change my-change --json": instructionsJSON("proposal", "Write the proposal"),
			},
		}
		eng := NewEngineWithRunner(map[string]any{"change_param": "change_name"}, cmd)
		result := eng.EnrichPrompt("proposal", map[string]string{"change_name": "my-change"}, engine.EnrichOptions{})
		if result == "" {
			t.Fatal("expected enrichment")
		}
		if !strings.Contains(result, "Output path") {
			t.Fatal("expected Output path in enrichment")
		}
		if !strings.Contains(result, "Write the proposal") {
			t.Fatal("expected instruction in enrichment")
		}
	})

	t.Run("EnrichPrompt returns empty for non-artifact step", func(t *testing.T) {
		cmd := &mockCmdRunner{
			results: map[string]string{
				"status --change my-change --json": statusJSON([]artifact{
					{ID: "proposal", Status: "pending"},
				}),
			},
		}
		eng := NewEngineWithRunner(map[string]any{"change_param": "change_name"}, cmd)
		result := eng.EnrichPrompt("not-an-artifact", map[string]string{"change_name": "my-change"}, engine.EnrichOptions{})
		if result != "" {
			t.Fatalf("expected empty, got %q", result)
		}
	})

	t.Run("ValidateStep returns true when artifact is done", func(t *testing.T) {
		cmd := &mockCmdRunner{
			results: map[string]string{
				"status --change my-change --json": statusJSON([]artifact{
					{ID: "proposal", Status: "done"},
				}),
			},
		}
		eng := NewEngineWithRunner(map[string]any{"change_param": "change_name"}, cmd)
		valid, err := eng.ValidateStep("proposal", map[string]string{"change_name": "my-change"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !valid {
			t.Fatal("expected valid")
		}
	})

	t.Run("ValidateStep returns false when artifact is not done", func(t *testing.T) {
		cmd := &mockCmdRunner{
			results: map[string]string{
				"status --change my-change --json": statusJSON([]artifact{
					{ID: "proposal", Status: "pending"},
				}),
			},
		}
		eng := NewEngineWithRunner(map[string]any{"change_param": "change_name"}, cmd)
		valid, err := eng.ValidateStep("proposal", map[string]string{"change_name": "my-change"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if valid {
			t.Fatal("expected not valid")
		}
	})

	t.Run("ValidateStep returns true for non-artifact steps", func(t *testing.T) {
		cmd := &mockCmdRunner{
			results: map[string]string{
				"status --change my-change --json": statusJSON([]artifact{
					{ID: "proposal", Status: "pending"},
				}),
			},
		}
		eng := NewEngineWithRunner(map[string]any{"change_param": "change_name"}, cmd)
		valid, err := eng.ValidateStep("shell-step", map[string]string{"change_name": "my-change"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !valid {
			t.Fatal("expected valid for non-artifact step")
		}
	})
}

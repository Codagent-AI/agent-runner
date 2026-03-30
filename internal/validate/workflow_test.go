package validate

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func intPtr(n int) *int { return &n }

func shellStep(id string) model.Step {
	return model.Step{ID: id, Mode: model.ModeShell, Command: "echo", Session: model.SessionNew}
}

func TestWorkflowConstraints(t *testing.T) {
	t.Run("rejects skip_if on first step in workflow", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				{ID: "s1", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew, SkipIf: "previous_success"},
			},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "skip_if") {
			t.Fatalf("expected skip_if error, got: %v", err)
		}
	})

	t.Run("accepts skip_if on second step in workflow", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1"),
				{ID: "s2", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew, SkipIf: "previous_success"},
			},
		}
		if err := WorkflowConstraints(&w, Options{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects skip_if on first step in loop body", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{{
				ID: "loop", Session: model.SessionNew,
				Loop: &model.Loop{Max: intPtr(3)},
				Steps: []model.Step{
					{ID: "s1", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew, SkipIf: "previous_success"},
				},
			}},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects break_if outside loop body", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				{ID: "s1", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew, BreakIf: "success"},
			},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "break_if") {
			t.Fatalf("expected break_if error, got: %v", err)
		}
	})

	t.Run("accepts break_if inside loop body", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{{
				ID: "loop", Session: model.SessionNew,
				Loop: &model.Loop{Max: intPtr(3)},
				Steps: []model.Step{
					{ID: "s1", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew, BreakIf: "success"},
				},
			}},
		}
		if err := WorkflowConstraints(&w, Options{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects session inherit in top-level workflow", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				{ID: "s1", Mode: model.ModeHeadless, Prompt: "p", Session: model.SessionInherit},
			},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "inherit") {
			t.Fatalf("expected inherit error, got: %v", err)
		}
	})

	t.Run("accepts session inherit in nested steps", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{{
				ID: "loop", Session: model.SessionNew,
				Loop: &model.Loop{Max: intPtr(3)},
				Steps: []model.Step{
					{ID: "s1", Mode: model.ModeHeadless, Prompt: "p", Session: model.SessionInherit},
				},
			}},
		}
		if err := WorkflowConstraints(&w, Options{IsSubWorkflow: true}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts break_if inside nested group within loop", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{{
				ID: "loop", Session: model.SessionNew,
				Loop: &model.Loop{Max: intPtr(3)},
				Steps: []model.Step{{
					ID: "group", Session: model.SessionNew,
					Steps: []model.Step{
						{ID: "s1", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew, BreakIf: "success"},
					},
				}},
			}},
		}
		if err := WorkflowConstraints(&w, Options{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

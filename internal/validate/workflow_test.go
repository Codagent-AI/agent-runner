package validate

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func intPtr(n int) *int { return &n }

func shellStep(id string) model.Step {
	return model.Step{ID: id, Command: "echo", Session: model.SessionNew}
}

func TestWorkflowConstraints(t *testing.T) {
	t.Run("rejects skip_if on first step in workflow", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				{ID: "s1", Command: "echo", Session: model.SessionNew, SkipIf: "previous_success"},
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
				{ID: "s2", Command: "echo", Session: model.SessionNew, SkipIf: "previous_success"},
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
					{ID: "s1", Command: "echo", Session: model.SessionNew, SkipIf: "previous_success"},
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
				{ID: "s1", Command: "echo", Session: model.SessionNew, BreakIf: "success"},
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
					{ID: "s1", Command: "echo", Session: model.SessionNew, BreakIf: "success"},
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
				{ID: "s1", Mode: model.ModeAutonomous, Prompt: "p", Session: model.SessionInherit},
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
					{ID: "s1", Mode: model.ModeAutonomous, Prompt: "p", Session: model.SessionInherit},
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
						{ID: "s1", Command: "echo", Session: model.SessionNew, BreakIf: "success"},
					},
				}},
			}},
		}
		if err := WorkflowConstraints(&w, Options{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestNamedSessionConstraints(t *testing.T) {
	t.Run("accepts workflow with declared and referenced named session", func(t *testing.T) {
		w := model.Workflow{
			Name:     "test",
			Sessions: []model.SessionDecl{{Name: "planner", Agent: "planner-profile"}},
			Steps:    []model.Step{{ID: "s1", Prompt: "do it", Session: "planner"}},
		}
		if err := WorkflowConstraints(&w, Options{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects reserved name 'new' in sessions block", func(t *testing.T) {
		w := model.Workflow{
			Name:     "test",
			Sessions: []model.SessionDecl{{Name: "new", Agent: "planner-profile"}},
			Steps:    []model.Step{shellStep("s1")},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("expected reserved keyword error, got: %v", err)
		}
	})

	t.Run("rejects reserved name 'resume' in sessions block", func(t *testing.T) {
		w := model.Workflow{
			Name:     "test",
			Sessions: []model.SessionDecl{{Name: "resume", Agent: "planner-profile"}},
			Steps:    []model.Step{shellStep("s1")},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("expected reserved keyword error, got: %v", err)
		}
	})

	t.Run("rejects reserved name 'inherit' in sessions block", func(t *testing.T) {
		w := model.Workflow{
			Name:     "test",
			Sessions: []model.SessionDecl{{Name: "inherit", Agent: "planner-profile"}},
			Steps:    []model.Step{shellStep("s1")},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("expected reserved keyword error, got: %v", err)
		}
	})

	t.Run("rejects duplicate session name in sessions block", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Sessions: []model.SessionDecl{
				{Name: "planner", Agent: "planner-profile"},
				{Name: "planner", Agent: "another-profile"},
			},
			Steps: []model.Step{shellStep("s1")},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("expected duplicate error, got: %v", err)
		}
	})

	t.Run("rejects step referencing undeclared named session", func(t *testing.T) {
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{{ID: "s1", Prompt: "do it", Session: "planner"}},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not declared") {
			t.Fatalf("expected not-declared error, got: %v", err)
		}
	})

	t.Run("rejects step referencing session not in local sessions block", func(t *testing.T) {
		w := model.Workflow{
			Name:     "test",
			Sessions: []model.SessionDecl{{Name: "implementor", Agent: "impl-profile"}},
			Steps: []model.Step{
				{ID: "s1", Prompt: "plan", Session: "planner"},
			},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not declared") {
			t.Fatalf("expected not-declared error, got: %v", err)
		}
	})

	t.Run("accepts multiple declared sessions with different names", func(t *testing.T) {
		w := model.Workflow{
			Name: "test",
			Sessions: []model.SessionDecl{
				{Name: "planner", Agent: "planner-profile"},
				{Name: "implementor", Agent: "impl-profile"},
			},
			Steps: []model.Step{
				{ID: "s1", Prompt: "plan", Session: "planner"},
				{ID: "s2", Prompt: "impl", Session: "implementor"},
			},
		}
		if err := WorkflowConstraints(&w, Options{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("named session step in loop body resolves against workflow declarations", func(t *testing.T) {
		w := model.Workflow{
			Name:     "test",
			Sessions: []model.SessionDecl{{Name: "planner", Agent: "planner-profile"}},
			Steps: []model.Step{{
				ID: "loop", Session: model.SessionNew,
				Loop: &model.Loop{Max: intPtr(3)},
				Steps: []model.Step{
					{ID: "body", Prompt: "do it", Session: "planner"},
				},
			}},
		}
		if err := WorkflowConstraints(&w, Options{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects session declaration with empty name", func(t *testing.T) {
		w := model.Workflow{
			Name:     "test",
			Sessions: []model.SessionDecl{{Name: "", Agent: "planner-profile"}},
			Steps:    []model.Step{shellStep("s1")},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("rejects session declaration with empty agent", func(t *testing.T) {
		w := model.Workflow{
			Name:     "test",
			Sessions: []model.SessionDecl{{Name: "planner", Agent: ""}},
			Steps:    []model.Step{shellStep("s1")},
		}
		err := WorkflowConstraints(&w, Options{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

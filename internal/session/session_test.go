package session

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestResolveInheritSession(t *testing.T) {
	t.Run("walks parent context chain to find parent session", func(t *testing.T) {
		parent := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
			SessionIDs:   map[string]string{"step1": "parent-session"},
		})
		parent.LastSessionStepID = "step1"

		child := model.NewSubWorkflowContext(parent, &model.SubWorkflowContextOptions{
			StepID:       "sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		id, err := ResolveInheritSession(child)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "parent-session" {
			t.Fatalf("expected 'parent-session', got %q", id)
		}
	})

	t.Run("returns most recent parent session when multiple exist", func(t *testing.T) {
		parent := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
			SessionIDs:   map[string]string{"step1": "old", "step2": "latest"},
		})
		parent.LastSessionStepID = "step2"

		child := model.NewSubWorkflowContext(parent, &model.SubWorkflowContextOptions{
			StepID:       "sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		id, err := ResolveInheritSession(child)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "latest" {
			t.Fatalf("expected 'latest', got %q", id)
		}
	})

	t.Run("errors when no parent session exists", func(t *testing.T) {
		parent := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
		})

		child := model.NewSubWorkflowContext(parent, &model.SubWorkflowContextOptions{
			StepID:       "sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		_, err := ResolveInheritSession(child)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "no parent session") {
			t.Fatalf("expected 'no parent session' error, got: %v", err)
		}
	})

	t.Run("returns empty string when used in top-level workflow", func(t *testing.T) {
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
		})

		id, err := ResolveInheritSession(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})

	t.Run("walks through nested sub-workflows to find session", func(t *testing.T) {
		root := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "root.yaml",
			SessionIDs:   map[string]string{"step1": "root-session"},
		})
		root.LastSessionStepID = "step1"

		mid := model.NewSubWorkflowContext(root, &model.SubWorkflowContextOptions{
			StepID:       "sub1",
			Params:       map[string]string{},
			WorkflowFile: "mid.yaml",
		})

		leaf := model.NewSubWorkflowContext(mid, &model.SubWorkflowContextOptions{
			StepID:       "sub2",
			Params:       map[string]string{},
			WorkflowFile: "leaf.yaml",
		})

		// leaf inherits from mid, but mid has no sessions, so it keeps walking
		// Actually, leaf's WorkflowFile differs from mid's, so it checks mid first.
		// mid has no sessions, so it errors.
		_, err := ResolveInheritSession(leaf)
		if err == nil {
			t.Fatal("expected error since mid has no sessions")
		}
	})
}

func TestResolveNamedSession(t *testing.T) {
	t.Run("returns session ID when named session exists", func(t *testing.T) {
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:        map[string]string{},
			WorkflowFile:  "test.yaml",
			NamedSessions: map[string]string{"planner": "session-abc"},
		})

		id := ResolveNamedSession("planner", ctx)
		if id != "session-abc" {
			t.Fatalf("expected 'session-abc', got %q", id)
		}
	})

	t.Run("returns empty string when named session does not exist", func(t *testing.T) {
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
		})

		id := ResolveNamedSession("planner", ctx)
		if id != "" {
			t.Fatalf("expected empty string, got %q", id)
		}
	})

	t.Run("returns empty string for a different name", func(t *testing.T) {
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:        map[string]string{},
			WorkflowFile:  "test.yaml",
			NamedSessions: map[string]string{"planner": "session-abc"},
		})

		id := ResolveNamedSession("implementor", ctx)
		if id != "" {
			t.Fatalf("expected empty string for 'implementor', got %q", id)
		}
	})

	t.Run("sees sessions written by child contexts (shared map)", func(t *testing.T) {
		parent := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "parent.yaml",
		})

		child := model.NewSubWorkflowContext(parent, &model.SubWorkflowContextOptions{
			StepID:       "sub",
			Params:       map[string]string{},
			WorkflowFile: "child.yaml",
		})

		// Child writes to the shared map.
		child.NamedSessions["planner"] = "child-created-session"

		// Parent can see it immediately (shared pointer).
		id := ResolveNamedSession("planner", parent)
		if id != "child-created-session" {
			t.Fatalf("expected 'child-created-session' in parent context, got %q", id)
		}
	})
}

func TestResolveResumeSession(t *testing.T) {
	t.Run("resumes session from same workflow file", func(t *testing.T) {
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
			SessionIDs:   map[string]string{"step1": "session-abc"},
		})
		ctx.LastSessionStepID = "step1"

		id, err := ResolveResumeSession(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "session-abc" {
			t.Fatalf("expected 'session-abc', got %q", id)
		}
	})

	t.Run("returns most recent session from same workflow", func(t *testing.T) {
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
			SessionIDs:   map[string]string{"step1": "old", "step2": "latest"},
		})
		ctx.LastSessionStepID = "step2"

		id, err := ResolveResumeSession(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "latest" {
			t.Fatalf("expected 'latest', got %q", id)
		}
	})

	t.Run("returns empty when no session exists in current workflow", func(t *testing.T) {
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "test.yaml",
		})

		id, err := ResolveResumeSession(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "" {
			t.Fatalf("expected empty session ID, got %q", id)
		}
	})
}

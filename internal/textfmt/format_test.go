package textfmt

import (
	"testing"
)

func intP(n int) *int { return &n }

func TestBuildBreadcrumb(t *testing.T) {
	t.Run("returns just the step id for a top-level step", func(t *testing.T) {
		result := BuildBreadcrumb(nil, "validate")
		if result != "validate" {
			t.Fatalf("expected 'validate', got %q", result)
		}
	})

	t.Run("returns breadcrumb for a step inside a loop iteration", func(t *testing.T) {
		path := []NestingInfo{{StepID: "task-loop", Iteration: intP(0)}}
		result := BuildBreadcrumb(path, "implement")
		expected := "task-loop > iteration 1 > implement"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("returns breadcrumb for a step inside a sub-workflow inside a loop", func(t *testing.T) {
		path := []NestingInfo{
			{StepID: "task-loop", Iteration: intP(0)},
			{StepID: "verify", SubWorkflowName: "verify-task"},
		}
		result := BuildBreadcrumb(path, "check")
		expected := "task-loop > iteration 1 > verify > verify-task > check"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("returns breadcrumb for plain nesting segment", func(t *testing.T) {
		path := []NestingInfo{{StepID: "parent"}}
		result := BuildBreadcrumb(path, "child")
		expected := "parent > child"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("converts 0-indexed iteration to 1-indexed display", func(t *testing.T) {
		path := []NestingInfo{{StepID: "loop", Iteration: intP(4)}}
		result := BuildBreadcrumb(path, "step")
		expected := "loop > iteration 5 > step"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})
}


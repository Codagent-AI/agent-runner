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

func TestSeparator(t *testing.T) {
	t.Run("returns a fixed-width line of separator characters", func(t *testing.T) {
		sep := Separator()
		// 60 runes of ━
		runes := []rune(sep)
		if len(runes) != 60 {
			t.Fatalf("expected 60 runes, got %d", len(runes))
		}
	})
}

func TestStepHeading(t *testing.T) {
	t.Run("prints heading for a top-level step", func(t *testing.T) {
		result := StepHeading(0, 5, "validate", "shell", false)
		expected := "━━ step 1/5: validate [shell] ━━"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("prints heading for a step inside a loop", func(t *testing.T) {
		breadcrumb := BuildBreadcrumb([]NestingInfo{{StepID: "task-loop", Iteration: intP(0)}}, "implement")
		result := StepHeading(0, 3, breadcrumb, "headless", false)
		expected := "━━ step 1/3: task-loop > iteration 1 > implement [headless] ━━"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("prints heading with skipped for skipped steps", func(t *testing.T) {
		result := StepHeading(2, 5, "deploy", "shell", true)
		expected := "━━ step 3/5: deploy [skipped] ━━"
		if result != expected {
			t.Fatalf("expected %q, got %q", expected, result)
		}
	})

	t.Run("converts 0-based index to 1-based display", func(t *testing.T) {
		result := StepHeading(4, 10, "step", "shell", false)
		if result != "━━ step 5/10: step [shell] ━━" {
			t.Fatalf("unexpected result: %q", result)
		}
	})
}

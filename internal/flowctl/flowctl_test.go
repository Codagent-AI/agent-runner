package flowctl

import "testing"

func strPtr(s string) *string { return &s }

func TestShouldSkip(t *testing.T) {
	t.Run("returns false when no skip_if", func(t *testing.T) {
		if ShouldSkip("", strPtr("success")) {
			t.Fatal("expected false")
		}
	})

	t.Run("returns true when skip_if=previous_success and last outcome was success", func(t *testing.T) {
		if !ShouldSkip("previous_success", strPtr("success")) {
			t.Fatal("expected true")
		}
	})

	t.Run("returns false when skip_if=previous_success and last outcome was failed", func(t *testing.T) {
		if ShouldSkip("previous_success", strPtr("failed")) {
			t.Fatal("expected false")
		}
	})

	t.Run("returns false when skip_if=previous_success and last outcome is null", func(t *testing.T) {
		if ShouldSkip("previous_success", nil) {
			t.Fatal("expected false")
		}
	})

	t.Run("returns false for sh: form (caller must evaluate)", func(t *testing.T) {
		if ShouldSkip("sh: true", strPtr("success")) {
			t.Fatal("expected false — shell form is not evaluated by ShouldSkip")
		}
	})
}

func TestShellSkipCommand(t *testing.T) {
	t.Run("returns command with sh: prefix stripped and trimmed", func(t *testing.T) {
		cmd, ok := ShellSkipCommand("sh: test foo = bar")
		if !ok {
			t.Fatal("expected ok")
		}
		if cmd != "test foo = bar" {
			t.Fatalf("expected %q, got %q", "test foo = bar", cmd)
		}
	})

	t.Run("returns not ok for previous_success", func(t *testing.T) {
		if _, ok := ShellSkipCommand("previous_success"); ok {
			t.Fatal("expected not ok")
		}
	})

	t.Run("returns not ok for empty string", func(t *testing.T) {
		if _, ok := ShellSkipCommand(""); ok {
			t.Fatal("expected not ok")
		}
	})
}

func TestEvaluateBreakIf(t *testing.T) {
	t.Run("returns true when break_if=success and outcome is success", func(t *testing.T) {
		if !EvaluateBreakIf("success", "success") {
			t.Fatal("expected true")
		}
	})

	t.Run("returns false when break_if=success and outcome is failed", func(t *testing.T) {
		if EvaluateBreakIf("success", "failed") {
			t.Fatal("expected false")
		}
	})

	t.Run("returns true when break_if=failure and outcome is failed", func(t *testing.T) {
		if !EvaluateBreakIf("failure", "failed") {
			t.Fatal("expected true")
		}
	})

	t.Run("returns false when break_if=failure and outcome is success", func(t *testing.T) {
		if EvaluateBreakIf("failure", "success") {
			t.Fatal("expected false")
		}
	})

	t.Run("returns false when no break_if is set", func(t *testing.T) {
		if EvaluateBreakIf("", "success") {
			t.Fatal("expected false")
		}
	})
}

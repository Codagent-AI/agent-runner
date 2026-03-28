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

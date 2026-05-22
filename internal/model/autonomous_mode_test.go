package model

import (
	"strings"
	"testing"
)

func TestAutonomousModeValidation(t *testing.T) {
	step := Step{ID: "impl", Mode: ModeAutonomous, Prompt: "Implement", Session: SessionResume}
	if err := step.Validate(nil); err != nil {
		t.Fatalf("Validate returned error for autonomous mode: %v", err)
	}
}

func TestHeadlessModeValidationRejected(t *testing.T) {
	step := Step{ID: "impl", Mode: StepMode("headless"), Prompt: "Implement", Session: SessionResume}
	err := step.Validate(nil)
	if err == nil {
		t.Fatal("expected error for legacy headless mode")
	}
	if !strings.Contains(err.Error(), `invalid mode: "headless"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

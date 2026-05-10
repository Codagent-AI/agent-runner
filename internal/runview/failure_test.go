package runview

import "testing"

func TestFailureReason_PrefersConcreteFailedStepOverExhaustedLoop(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusFailed}
	loop := &StepNode{
		ID:                  "ci-fix-loop",
		Type:                NodeLoop,
		Status:              StatusSuccess,
		Parent:              root,
		Outcome:             "exhausted",
		IterationsCompleted: 3,
		StaticLoopMax:       intPtr(3),
	}
	exitCode := 2
	failed := &StepNode{
		ID:       "verify-final",
		Type:     NodeShell,
		Status:   StatusFailed,
		Parent:   root,
		ExitCode: &exitCode,
	}
	root.Children = []*StepNode{loop, failed}

	if got, want := failureReason(root), "verify-final failed with exit code 2"; got != want {
		t.Fatalf("failureReason() = %q, want %q", got, want)
	}
}

func TestFailureReason_IgnoresNonLoopExhaustedOutcome(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusFailed}
	agent := &StepNode{
		ID:      "planner",
		Type:    NodeHeadlessAgent,
		Status:  StatusSuccess,
		Parent:  root,
		Outcome: "exhausted",
	}
	root.Children = []*StepNode{agent}

	if got, want := failureReason(root), "wf failed"; got != want {
		t.Fatalf("failureReason() = %q, want %q", got, want)
	}
}

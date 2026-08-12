package runview

import "testing"

func TestFailureReason_PrefersConcreteFailureOutsideExhaustedLoop(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusFailed}
	loop := &StepNode{
		ID:                  "retry",
		Type:                NodeLoop,
		Status:              StatusSuccess,
		Parent:              root,
		Outcome:             "exhausted",
		IterationsCompleted: 3,
		StaticLoopMax:       intPtr(3),
	}
	verify := &StepNode{
		ID:           "verify",
		Type:         NodeShell,
		Status:       StatusFailed,
		Parent:       root,
		ErrorMessage: "still broken",
	}
	root.Children = []*StepNode{loop, verify}

	got := failureReason(root)
	want := "verify failed: still broken"
	if got != want {
		t.Fatalf("failureReason() = %q, want %q", got, want)
	}
}

func TestFailureReason_PrefersFailedParentErrorOverExhaustedChild(t *testing.T) {
	root := &StepNode{
		ID:           "wf",
		Type:         NodeRoot,
		Status:       StatusFailed,
		ErrorMessage: "shell failed",
	}
	loop := &StepNode{
		ID:                  "retry",
		Type:                NodeLoop,
		Status:              StatusSuccess,
		Parent:              root,
		Outcome:             "exhausted",
		IterationsCompleted: 3,
		StaticLoopMax:       intPtr(3),
	}
	root.Children = []*StepNode{loop}

	got := failureReason(root)
	want := "wf failed: shell failed"
	if got != want {
		t.Fatalf("failureReason() = %q, want %q", got, want)
	}
}

func TestFailureReason_PrefersFailedStepExitCodeOverExhaustedLoop(t *testing.T) {
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

func TestFindFailedLeafPrefersDeepestFailedExecution(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusFailed}
	shallow := &StepNode{ID: "shallow", Type: NodeShell, Status: StatusFailed, Parent: root}
	container := &StepNode{ID: "nested", Type: NodeSubWorkflow, Status: StatusFailed, Parent: root}
	deep := &StepNode{ID: "deep", Type: NodeScript, Status: StatusFailed, Parent: container}
	container.Children = []*StepNode{deep}
	root.Children = []*StepNode{shallow, container}

	if got := findFailedLeaf(root); got != deep {
		t.Fatalf("failed leaf = %v, want deepest %v", got, deep)
	}
}

func TestFindFailedLeafPrefersMostRecentlyRecordedFailureAtEqualDepth(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	first := &StepNode{ID: "first", Type: NodeShell, Status: StatusPending, Parent: root}
	latest := &StepNode{ID: "latest", Type: NodeShell, Status: StatusPending, Parent: root}
	root.Children = []*StepNode{first, latest}
	tree := &Tree{Root: root}

	for _, event := range []RawEvent{
		{Timestamp: "2026-08-12T00:00:01Z", Prefix: "[first]", Type: "step_start", Data: map[string]any{"command": "false"}},
		{Timestamp: "2026-08-12T00:00:02Z", Prefix: "[first]", Type: "step_end", Data: map[string]any{"outcome": "failed"}},
		{Timestamp: "2026-08-12T00:00:03Z", Prefix: "[latest]", Type: "step_start", Data: map[string]any{"command": "false"}},
		{Timestamp: "2026-08-12T00:00:04Z", Prefix: "[latest]", Type: "step_end", Data: map[string]any{"outcome": "failed"}},
	} {
		tree.ApplyEvent(event)
	}

	if got := findFailedLeaf(root); got != latest {
		t.Fatalf("equal-depth failed leaf = %v, want latest recorded failure %v", got, latest)
	}
}

func TestFindFailedLeafPrefersLatestDurableErrorAtEqualDepth(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusFailed}
	first := &StepNode{ID: "first", Type: NodeShell, Status: StatusFailed, Parent: root}
	latest := &StepNode{ID: "latest", Type: NodeShell, Status: StatusFailed, Parent: root}
	root.Children = []*StepNode{first, latest}
	tree := &Tree{Root: root}

	for _, event := range []RawEvent{
		{Timestamp: "2026-08-12T00:00:01Z", Prefix: "[first]", Type: "error", Data: map[string]any{"message": "first failed"}},
		{Timestamp: "2026-08-12T00:00:02Z", Prefix: "[latest]", Type: "error", Data: map[string]any{"message": "latest failed"}},
	} {
		tree.ApplyEvent(event)
	}

	if got := findFailedLeaf(root); got != latest {
		t.Fatalf("equal-depth failed leaf = %v, want latest durable error %v", got, latest)
	}
}

package cli

import (
	"strings"
	"testing"
)

// TestCursorGrantsRouteSubmissionCommand proves Cursor's private config carries
// an exact narrow allow rule for `step submit-route` when the step is
// route-eligible. Cursor is the only adapter with a real shell allow-list, so
// this is where the "runs without a permission prompt" guarantee lives.
func TestCursorGrantsRouteSubmissionCommand(t *testing.T) {
	rules, _, err := cursorPrivateConfigRules([]RunnerCommand{
		{Kind: RunnerCommandCompleteStep, Executable: "/abs/agent-runner"},
		{Kind: RunnerCommandSubmitRoute, Executable: "/abs/agent-runner"},
	}, nil, false)
	if err != nil {
		t.Fatalf("cursorPrivateConfigRules: %v", err)
	}
	joined := strings.Join(rules, " | ")
	for _, want := range []string{
		"Shell(/abs/agent-runner:step complete)",
		"Shell(/abs/agent-runner:step submit-route)",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("rules %q missing exact grant %q", joined, want)
		}
	}
}

// TestCursorOmitsRouteGrantWhenNotEligible proves the route grant is absent for
// an ordinary step, so the wider permission is never handed out by default.
func TestCursorOmitsRouteGrantWhenNotEligible(t *testing.T) {
	rules, _, err := cursorPrivateConfigRules([]RunnerCommand{
		{Kind: RunnerCommandCompleteStep, Executable: "/abs/agent-runner"},
	}, nil, false)
	if err != nil {
		t.Fatalf("cursorPrivateConfigRules: %v", err)
	}
	if joined := strings.Join(rules, " | "); strings.Contains(joined, "submit-route") {
		t.Errorf("rules %q granted submit-route for a non-eligible step", joined)
	}
}

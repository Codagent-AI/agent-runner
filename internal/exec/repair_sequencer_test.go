package exec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

// TestRerunRepairAcrossAllSequencers proves the shared rewind helpers behave
// identically at the top-level runner (via ExecuteLoopStep/DispatchStep
// entry points available from this package), inside a loop body, inside a
// sub-workflow, and inside a group. internal/runner has its own equivalent
// top-level coverage; this table exercises the three exec-owned sequencers
// plus the group form directly against the same fixture shape so they cannot
// drift from one another.
func TestRerunRepairAcrossAllSequencers(t *testing.T) {
	maxAttempts := 1
	bodySteps := []model.Step{
		{ID: "open-draft-pr", Mode: model.ModeAutonomous, Prompt: "open it", Session: model.SessionNew},
		{ID: "verify-draft-pr", Command: "exit 1", Repair: &model.Repair{Rerun: "open-draft-pr", Max: &maxAttempts}},
	}
	freshResults := func() []ProcessResult {
		return []ProcessResult{
			{ExitCode: 0, Stdout: claudeUsageOutput("opened draft", 0)},
			{ExitCode: 1, Stderr: "not open"},
			{ExitCode: 0, Stdout: claudeUsageOutput("opened ready", 0)},
			{ExitCode: 0, Stdout: "pr open"},
		}
	}

	t.Run("loop body", func(t *testing.T) {
		step := model.Step{ID: "loop", Loop: &model.Loop{Max: intPtr(1)}, Steps: bodySteps}
		runner := &mockRunner{results: freshResults()}
		result, err := ExecuteLoopStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Outcome != OutcomeExhausted {
			t.Fatalf("expected exhausted (loop's normal completion), got %q", result.Outcome)
		}
		if len(runner.calls) != 4 {
			t.Fatalf("expected 4 process calls, got %d", len(runner.calls))
		}
	})

	t.Run("group", func(t *testing.T) {
		step := model.Step{ID: "g", Session: model.SessionNew, Steps: bodySteps}
		runner := &mockRunner{results: freshResults()}
		outcome, err := DispatchStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
		if len(runner.calls) != 4 {
			t.Fatalf("expected 4 process calls, got %d", len(runner.calls))
		}
	})

	t.Run("sub-workflow", func(t *testing.T) {
		dir := t.TempDir()
		childYAML := `name: child
steps:
  - id: open-draft-pr
    prompt: open it
    mode: autonomous
    agent: test-agent
    session: new
  - id: verify-draft-pr
    command: exit 1
    repair:
      rerun: open-draft-pr
      max: 1
`
		childPath := filepath.Join(dir, "child-v1.0.yaml")
		if err := os.WriteFile(childPath, []byte(childYAML), 0o600); err != nil {
			t.Fatal(err)
		}
		runner := &mockRunner{results: freshResults()}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params: map[string]string{}, WorkflowFile: filepath.Join(dir, "parent-v1.0.yaml"),
		})
		step := model.Step{ID: "sub", Workflow: "child-v1.0.yaml", Session: model.SessionNew}
		outcome, err := ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
		if len(runner.calls) != 4 {
			t.Fatalf("expected 4 process calls, got %d", len(runner.calls))
		}
	})
}

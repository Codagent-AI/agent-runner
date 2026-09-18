package exec

import (
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestExecuteLoopStepRerunRepairReplaysTargetWithinIteration(t *testing.T) {
	maxAttempts := 1
	step := model.Step{
		ID: "loop", Loop: &model.Loop{Max: intPtr(1)},
		Steps: []model.Step{
			{ID: "open-draft-pr", Mode: model.ModeAutonomous, Prompt: "open it", Agent: "test-agent", Session: model.SessionNew},
			{ID: "verify-draft-pr", Command: "exit 1", Repair: &model.Repair{Rerun: "open-draft-pr", Max: &maxAttempts}},
		},
	}
	runner := &mockRunner{results: []ProcessResult{
		{ExitCode: 0, Stdout: claudeUsageOutput("opened draft", 0)},
		{ExitCode: 1, Stderr: "not open"},
		{ExitCode: 0, Stdout: claudeUsageOutput("opened ready", 0)},
		{ExitCode: 0, Stdout: "pr open"},
	}}

	result, err := ExecuteLoopStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != OutcomeExhausted {
		t.Fatalf("expected exhausted (the loop's normal completion without break_if), got %q", result.Outcome)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("expected 4 process calls, got %d", len(runner.calls))
	}
}

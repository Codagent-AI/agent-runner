package exec

import (
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func TestGroupRerunRepairReplaysTargetWithinGroup(t *testing.T) {
	maxAttempts := 1
	step := model.Step{
		ID: "g", Session: model.SessionNew,
		Steps: []model.Step{
			{ID: "open-draft-pr", Mode: model.ModeAutonomous, Prompt: "open it", Session: model.SessionNew},
			{ID: "verify-draft-pr", Command: "exit 1", Repair: &model.Repair{Rerun: "open-draft-pr", Max: &maxAttempts}},
		},
	}
	runner := &mockRunner{results: []ProcessResult{
		{ExitCode: 0, Stdout: claudeUsageOutput("opened draft", 0)},
		{ExitCode: 1, Stderr: "not open"},
		{ExitCode: 0, Stdout: claudeUsageOutput("opened ready", 0)},
		{ExitCode: 0, Stdout: "pr open"},
	}}

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
}

package exec

import (
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func boolPtr(b bool) *bool { return &b }

func TestExecuteLoopStep(t *testing.T) {
	t.Run("counted loop runs N iterations", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}, {ExitCode: 0}, {ExitCode: 0}}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop:  &model.Loop{Max: intPtr(3)},
			Steps: []model.Step{{ID: "a", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew}},
		}
		result, err := ExecuteLoopStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Outcome != OutcomeExhausted {
			t.Fatalf("expected exhausted, got %q", result.Outcome)
		}
		if len(runner.calls) != 3 {
			t.Fatalf("expected 3 calls, got %d", len(runner.calls))
		}
	})

	t.Run("counted loop with break_if success", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop: &model.Loop{Max: intPtr(5)},
			Steps: []model.Step{{
				ID: "a", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew,
				BreakIf: "success",
			}},
		}
		result, _ := ExecuteLoopStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{})
		if result.Outcome != OutcomeSuccess {
			t.Fatalf("expected success (break), got %q", result.Outcome)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (broke after first), got %d", len(runner.calls))
		}
	})

	t.Run("counted loop stops on failure", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1}}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop:  &model.Loop{Max: intPtr(3)},
			Steps: []model.Step{{ID: "a", Mode: model.ModeShell, Command: "false", Session: model.SessionNew}},
		}
		result, _ := ExecuteLoopStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{})
		if result.Outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", result.Outcome)
		}
	})

	t.Run("for-each loop iterates over matches", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}, {ExitCode: 0}}}
		glob := &mockGlob{matches: []string{"a.go", "b.go"}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop:  &model.Loop{Over: "*.go", As: "file"},
			Steps: []model.Step{{ID: "process", Mode: model.ModeShell, Command: "echo {{file}}", Session: model.SessionNew}},
		}
		result, err := ExecuteLoopStep(&step, makeCtx(), runner, glob, &mockLogger{}, LoopExecuteOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", result.Outcome)
		}
		if len(runner.calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(runner.calls))
		}
	})

	t.Run("for-each loop with zero matches succeeds", func(t *testing.T) {
		runner := &mockRunner{}
		glob := &mockGlob{matches: []string{}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop:  &model.Loop{Over: "*.go", As: "file"},
			Steps: []model.Step{{ID: "a", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew}},
		}
		result, _ := ExecuteLoopStep(&step, makeCtx(), runner, glob, &mockLogger{}, LoopExecuteOptions{})
		if result.Outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", result.Outcome)
		}
	})

	t.Run("for-each loop with require_matches fails on zero matches", func(t *testing.T) {
		runner := &mockRunner{}
		glob := &mockGlob{matches: []string{}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop:  &model.Loop{Over: "*.go", As: "file", RequireMatches: boolPtr(true)},
			Steps: []model.Step{{ID: "a", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew}},
		}
		result, _ := ExecuteLoopStep(&step, makeCtx(), runner, glob, &mockLogger{}, LoopExecuteOptions{})
		if result.Outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", result.Outcome)
		}
	})

	t.Run("resume from iteration", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop:  &model.Loop{Max: intPtr(3)},
			Steps: []model.Step{{ID: "a", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew}},
		}
		result, _ := ExecuteLoopStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{ResumeFromIteration: 2})
		if result.Outcome != OutcomeExhausted {
			t.Fatalf("expected exhausted, got %q", result.Outcome)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (resumed from 2, ran only iteration 2), got %d", len(runner.calls))
		}
	})

	t.Run("returns failed for missing loop config", func(t *testing.T) {
		step := model.Step{ID: "s", Session: model.SessionNew}
		result, _ := ExecuteLoopStep(&step, makeCtx(), &mockRunner{}, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{})
		if result.Outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", result.Outcome)
		}
	})
}

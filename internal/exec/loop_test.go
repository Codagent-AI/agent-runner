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
			Steps: []model.Step{{ID: "a", Command: "echo", Session: model.SessionNew}},
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
				ID: "a", Command: "echo", Session: model.SessionNew,
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
			Steps: []model.Step{{ID: "a", Command: "false", Session: model.SessionNew}},
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
			Steps: []model.Step{{ID: "process", Command: "echo {{file}}", Session: model.SessionNew}},
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
			Steps: []model.Step{{ID: "a", Command: "echo", Session: model.SessionNew}},
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
			Steps: []model.Step{{ID: "a", Command: "echo", Session: model.SessionNew}},
		}
		result, _ := ExecuteLoopStep(&step, makeCtx(), runner, glob, &mockLogger{}, LoopExecuteOptions{})
		if result.Outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", result.Outcome)
		}
	})

	t.Run("counted loop exposes iteration index via as_index", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}, {ExitCode: 0}, {ExitCode: 0}}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop:  &model.Loop{Max: intPtr(3), AsIndex: "i"},
			Steps: []model.Step{{ID: "a", Command: "echo iter {{i}}", Session: model.SessionNew}},
		}
		_, err := ExecuteLoopStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"echo iter 0", "echo iter 1", "echo iter 2"}
		for idx, call := range runner.calls {
			if call[2] != want[idx] {
				t.Fatalf("call %d: expected %q, got %q", idx, want[idx], call[2])
			}
		}
	})

	t.Run("for-each loop exposes iteration index via as_index", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}, {ExitCode: 0}}}
		glob := &mockGlob{matches: []string{"a.go", "b.go"}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop:  &model.Loop{Over: "*.go", As: "file", AsIndex: "i"},
			Steps: []model.Step{{ID: "p", Command: "echo {{i}}:{{file}}", Session: model.SessionNew}},
		}
		_, err := ExecuteLoopStep(&step, makeCtx(), runner, glob, &mockLogger{}, LoopExecuteOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"echo 0:a.go", "echo 1:b.go"}
		for idx, call := range runner.calls {
			if call[2] != want[idx] {
				t.Fatalf("call %d: expected %q, got %q", idx, want[idx], call[2])
			}
		}
	})

	t.Run("resume from iteration", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop:  &model.Loop{Max: intPtr(3)},
			Steps: []model.Step{{ID: "a", Command: "echo", Session: model.SessionNew}},
		}
		result, _ := ExecuteLoopStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{ResumeFromIteration: 2})
		if result.Outcome != OutcomeExhausted {
			t.Fatalf("expected exhausted, got %q", result.Outcome)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (resumed from 2, ran only iteration 2), got %d", len(runner.calls))
		}
	})

	t.Run("resume enters iteration at mid body step", func(t *testing.T) {
		// Iteration 0 had body step "a" completed and body step "b" in progress
		// when the run failed. Resume should skip "a" and start at "b".
		// Iteration 1 runs fresh.
		runner := &mockRunner{results: []ProcessResult{
			{ExitCode: 0}, // iter 0 resumed at b
			{ExitCode: 0}, // iter 0 c
			{ExitCode: 0}, // iter 1 a
			{ExitCode: 0}, // iter 1 b
			{ExitCode: 0}, // iter 1 c
		}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop: &model.Loop{Max: intPtr(2)},
			Steps: []model.Step{
				{ID: "a", Command: "echo a", Session: model.SessionNew},
				{ID: "b", Command: "echo b", Session: model.SessionNew},
				{ID: "c", Command: "echo c", Session: model.SessionNew},
			},
		}

		iterIdx := 0
		ctx := makeCtx()
		ctx.ResumeChildState = &model.NestedStepState{
			StepID:    "loop",
			Iteration: &iterIdx,
			Child: &model.NestedStepState{
				StepID:    "b",
				Completed: false,
			},
		}

		result, err := ExecuteLoopStep(&step, ctx, runner, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Outcome != OutcomeExhausted {
			t.Fatalf("expected exhausted, got %q", result.Outcome)
		}
		// Expected: 5 calls total (iter 0 skips "a"; iter 0 runs b,c; iter 1 runs a,b,c).
		if len(runner.calls) != 5 {
			t.Fatalf("expected 5 calls (iter 0 a skipped), got %d", len(runner.calls))
		}
		// First call should be "echo b" (iter 0, body step b).
		firstCmd := runner.calls[0][2]
		if firstCmd != "echo b" {
			t.Fatalf("expected first call %q, got %q", "echo b", firstCmd)
		}
	})

	t.Run("resume advances past completed body step", func(t *testing.T) {
		// Iteration 0 had "a" completed=true; resume should start at "b".
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}, {ExitCode: 0}, {ExitCode: 0}, {ExitCode: 0}}}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop: &model.Loop{Max: intPtr(2)},
			Steps: []model.Step{
				{ID: "a", Command: "echo a", Session: model.SessionNew},
				{ID: "b", Command: "echo b", Session: model.SessionNew},
			},
		}

		iterIdx := 0
		ctx := makeCtx()
		ctx.ResumeChildState = &model.NestedStepState{
			StepID:    "loop",
			Iteration: &iterIdx,
			Child: &model.NestedStepState{
				StepID:    "a",
				Completed: true,
			},
		}

		result, _ := ExecuteLoopStep(&step, ctx, runner, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{})
		if result.Outcome != OutcomeExhausted {
			t.Fatalf("expected exhausted, got %q", result.Outcome)
		}
		// iter 0: only b runs (a completed). iter 1: a, b run. Total 3.
		if len(runner.calls) != 3 {
			t.Fatalf("expected 3 calls (iter 0 a completed), got %d", len(runner.calls))
		}
		if runner.calls[0][2] != "echo b" {
			t.Fatalf("expected first call %q, got %q", "echo b", runner.calls[0][2])
		}
	})

	t.Run("returns failed for missing loop config", func(t *testing.T) {
		step := model.Step{ID: "s", Session: model.SessionNew}
		result, _ := ExecuteLoopStep(&step, makeCtx(), &mockRunner{}, &mockGlob{}, &mockLogger{}, LoopExecuteOptions{})
		if result.Outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", result.Outcome)
		}
	})

	t.Run("executes loop body steps for each iteration", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}, {ExitCode: 0}}}
		log := &mockLogger{}
		step := model.Step{
			ID: "loop", Session: model.SessionNew,
			Loop: &model.Loop{Max: intPtr(2)},
			Steps: []model.Step{
				{ID: "work", Command: "echo", Session: model.SessionNew},
			},
		}
		ExecuteLoopStep(&step, makeCtx(), runner, &mockGlob{}, log, LoopExecuteOptions{})

		// Both iterations should have dispatched a shell step.
		if len(runner.calls) != 2 {
			t.Fatalf("expected 2 iterations to run; got %d call(s)", len(runner.calls))
		}
	})
}

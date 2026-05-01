package exec

import (
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

type mockGlob struct {
	matches []string
}

func (g *mockGlob) Expand(_ string) ([]string, error) {
	return g.matches, nil
}

func TestDispatchStep(t *testing.T) {
	t.Run("dispatches shell step", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Command: "echo hi", Session: model.SessionNew}
		outcome, err := DispatchStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
	})

	t.Run("dispatches agent step", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew}
		outcome, err := DispatchStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
	})

	t.Run("dispatches loop step", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{
			ID: "l", Session: model.SessionNew,
			Loop: &model.Loop{Max: intPtr(1)},
			Steps: []model.Step{
				{ID: "a", Command: "echo", Session: model.SessionNew},
			},
		}
		outcome, err := DispatchStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
	})

	t.Run("dispatches exhausted retry loop as failed", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1}}}
		step := model.Step{
			ID: "l", Session: model.SessionNew,
			Loop: &model.Loop{Max: intPtr(1)},
			Steps: []model.Step{
				{
					ID: "a", Command: "false", Session: model.SessionNew,
					ContinueOnFailure: true,
					BreakIf:           "success",
				},
			},
		}
		outcome, err := DispatchStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
	})

	t.Run("dispatches group step", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}, {ExitCode: 0}}}
		step := model.Step{
			ID: "g", Session: model.SessionNew,
			Steps: []model.Step{
				{ID: "a", Command: "echo a", Session: model.SessionNew},
				{ID: "b", Command: "echo b", Session: model.SessionNew},
			},
		}
		outcome, err := DispatchStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
	})

	t.Run("group stops on failure", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1}}}
		step := model.Step{
			ID: "g", Session: model.SessionNew,
			Steps: []model.Step{
				{ID: "a", Command: "false", Session: model.SessionNew},
				{ID: "b", Command: "echo b", Session: model.SessionNew},
			},
		}
		outcome, _ := DispatchStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{})
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (stopped after failure), got %d", len(runner.calls))
		}
	})
}

func TestDispatchStep_PrepareStepHook(t *testing.T) {
	t.Run("shell step calls hook with false", func(t *testing.T) {
		var called []bool
		ctx := makeCtx()
		ctx.PrepareStepHook = func(interactive bool) { called = append(called, interactive) }
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Command: "echo hi", Session: model.SessionNew}
		DispatchStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if len(called) != 1 || called[0] != false {
			t.Fatalf("expected hook called with false, got %v", called)
		}
	})

	t.Run("interactive shell step calls hook with true", func(t *testing.T) {
		var called []bool
		ctx := makeCtx()
		ctx.PrepareStepHook = func(interactive bool) { called = append(called, interactive) }
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Command: "echo hi", Mode: model.ModeInteractive, Session: model.SessionNew}
		DispatchStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if len(called) != 1 || called[0] != true {
			t.Fatalf("expected hook called with true, got %v", called)
		}
	})

	t.Run("headless agent step calls hook with false", func(t *testing.T) {
		var called []bool
		ctx := makeCtx()
		ctx.PrepareStepHook = func(interactive bool) { called = append(called, interactive) }
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew}
		DispatchStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if len(called) != 1 || called[0] != false {
			t.Fatalf("expected hook called with false, got %v", called)
		}
	})

	t.Run("interactive agent step calls hook with true", func(t *testing.T) {
		var called []bool
		ctx := makeCtx()
		ctx.PrepareStepHook = func(interactive bool) { called = append(called, interactive) }
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "do it", Session: model.SessionNew}
		// Interactive agent steps need PTY — this will fail, but the hook should fire first
		DispatchStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if len(called) != 1 || called[0] != true {
			t.Fatalf("expected hook called with true, got %v", called)
		}
	})

	t.Run("default mode agent step calls hook with true", func(t *testing.T) {
		var called []bool
		ctx := makeCtx()
		ctx.PrepareStepHook = func(interactive bool) { called = append(called, interactive) }
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Prompt: "do it", Session: model.SessionNew}
		DispatchStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if len(called) != 1 || called[0] != true {
			t.Fatalf("expected hook called with true, got %v", called)
		}
	})

	t.Run("loop step does not call hook directly", func(t *testing.T) {
		var calls int
		ctx := makeCtx()
		ctx.PrepareStepHook = func(bool) { calls++ }
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{
			ID: "l", Session: model.SessionNew,
			Loop:  &model.Loop{Max: intPtr(1)},
			Steps: []model.Step{{ID: "a", Command: "echo", Session: model.SessionNew}},
		}
		DispatchStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		// Hook should be called for the child shell step, not the loop itself
		if calls != 1 {
			t.Fatalf("expected 1 hook call (from child), got %d", calls)
		}
	})
}

func intPtr(n int) *int { return &n }

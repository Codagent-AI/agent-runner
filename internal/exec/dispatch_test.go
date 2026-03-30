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
		step := model.Step{ID: "s", Mode: model.ModeShell, Command: "echo hi", Session: model.SessionNew}
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
				{ID: "a", Mode: model.ModeShell, Command: "echo", Session: model.SessionNew},
			},
		}
		outcome, err := DispatchStep(&step, makeCtx(), runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Loop exhausts after 1 iteration, which maps to failed
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed (exhausted), got %q", outcome)
		}
	})

	t.Run("dispatches group step", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}, {ExitCode: 0}}}
		step := model.Step{
			ID: "g", Session: model.SessionNew,
			Steps: []model.Step{
				{ID: "a", Mode: model.ModeShell, Command: "echo a", Session: model.SessionNew},
				{ID: "b", Mode: model.ModeShell, Command: "echo b", Session: model.SessionNew},
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
				{ID: "a", Mode: model.ModeShell, Command: "false", Session: model.SessionNew},
				{ID: "b", Mode: model.ModeShell, Command: "echo b", Session: model.SessionNew},
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

func intPtr(n int) *int { return &n }

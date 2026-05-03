package exec

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
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

func TestDispatchStepEmitsAuditEnvelopeForEveryStepType(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "ok.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf ok\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}
	childPath := filepath.Join(dir, "child.yaml")
	if err := os.WriteFile(childPath, []byte("name: child\nsteps:\n  - id: child-step\n    command: echo child\n"), 0o600); err != nil {
		t.Fatalf("write child workflow: %v", err)
	}

	cases := []struct {
		name    string
		step    model.Step
		results []ProcessResult
		setup   func(*model.ExecutionContext)
	}{
		{
			name:    "shell",
			step:    model.Step{ID: "shell", Command: "echo hi", Session: model.SessionNew},
			results: []ProcessResult{{ExitCode: 0}},
		},
		{
			name:    "script",
			step:    model.Step{ID: "script", Script: "ok.sh", Capture: "out"},
			results: []ProcessResult{{ExitCode: 0, Stdout: "ok"}},
		},
		{
			name: "ui",
			step: model.Step{ID: "ui", Mode: model.ModeUI, Title: "Pick", Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}}},
			setup: func(ctx *model.ExecutionContext) {
				ctx.UIStepHandler = func(*model.UIStepRequest) (model.UIStepResult, error) {
					return model.UIStepResult{Outcome: "continue"}, nil
				}
			},
		},
		{
			name:    "agent",
			step:    model.Step{ID: "agent", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew},
			results: []ProcessResult{{ExitCode: 0}},
		},
		{
			name: "loop",
			step: model.Step{
				ID: "loop", Loop: &model.Loop{Max: intPtr(1)}, Steps: []model.Step{
					{ID: "body", Command: "echo body", Session: model.SessionNew},
				},
			},
			results: []ProcessResult{{ExitCode: 0}},
		},
		{
			name: "group",
			step: model.Step{
				ID: "group", Steps: []model.Step{
					{ID: "child", Command: "echo child", Session: model.SessionNew},
				},
			},
			results: []ProcessResult{{ExitCode: 0}},
		},
		{
			name:    "sub-workflow",
			step:    model.Step{ID: "sub", Workflow: "child.yaml"},
			results: []ProcessResult{{ExitCode: 0}},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &mockAuditLogger{}
			ctx := model.NewRootContext(&model.RootContextOptions{
				Params:       map[string]string{},
				WorkflowFile: filepath.Join(dir, "parent.yaml"),
			})
			ctx.AuditLogger = recorder
			if tt.setup != nil {
				tt.setup(ctx)
			}

			outcome, err := DispatchStep(&tt.step, ctx, &mockRunner{results: tt.results}, &mockGlob{}, &mockLogger{})
			if err != nil {
				t.Fatalf("DispatchStep() error = %v", err)
			}
			if outcome != OutcomeSuccess {
				t.Fatalf("DispatchStep() outcome = %s, want success", outcome)
			}
			assertStepAuditEnvelope(t, recorder.events, "["+tt.step.ID+"]")
		})
	}
}

func assertStepAuditEnvelope(t *testing.T, events []audit.Event, prefix string) {
	t.Helper()
	var start, end bool
	for _, ev := range events {
		if ev.Prefix != prefix {
			continue
		}
		switch ev.Type {
		case audit.EventStepStart:
			start = true
		case audit.EventStepEnd:
			end = true
		}
	}
	if !start || !end {
		t.Fatalf("missing audit envelope for %s: start=%v end=%v events=%+v", prefix, start, end, events)
	}
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

	t.Run("ui step calls hook with false", func(t *testing.T) {
		var called []bool
		ctx := makeCtx()
		ctx.PrepareStepHook = func(interactive bool) { called = append(called, interactive) }
		ctx.UIStepHandler = func(*model.UIStepRequest) (model.UIStepResult, error) {
			return model.UIStepResult{Outcome: "continue"}, nil
		}
		step := model.Step{
			ID:      "welcome",
			Mode:    model.ModeUI,
			Title:   "Welcome",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		}
		DispatchStep(&step, ctx, &mockRunner{}, &mockGlob{}, &mockLogger{})
		if len(called) != 1 || called[0] != false {
			t.Fatalf("expected hook called with false, got %v", called)
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

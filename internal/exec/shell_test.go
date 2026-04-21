package exec

import (
	"fmt"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/pty"
)

type mockAuditLogger struct {
	events []audit.Event
}

func (m *mockAuditLogger) Emit(e audit.Event) { m.events = append(m.events, e) }

// --- Test helpers ---

type mockRunner struct {
	calls   [][]string
	results []ProcessResult
	idx     int
}

func (m *mockRunner) RunShell(cmd string, capture bool, _ string) (ProcessResult, error) {
	m.calls = append(m.calls, []string{"sh", "-c", cmd})
	if m.idx >= len(m.results) {
		return ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

func (m *mockRunner) RunAgent(args []string, _ bool, _ string) (ProcessResult, error) {
	m.calls = append(m.calls, args)
	if m.idx >= len(m.results) {
		return ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

type mockLogger struct {
	lines []string
}

func (l *mockLogger) Println(args ...any) {
	l.lines = append(l.lines, fmt.Sprint(args...))
}

func (l *mockLogger) Printf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *mockLogger) Errorf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func makeCtx() *model.ExecutionContext {
	return model.NewRootContext(&model.RootContextOptions{
		Params:       map[string]string{},
		WorkflowFile: "test.yaml",
	})
}

func TestExecuteShellStep(t *testing.T) {
	t.Run("returns success for exit code 0", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Command: "echo hi", Session: model.SessionNew}
		outcome, err := ExecuteShellStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
	})

	t.Run("returns failed for non-zero exit code", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1}}}
		step := model.Step{ID: "s", Command: "false", Session: model.SessionNew}
		outcome, _ := ExecuteShellStep(&step, makeCtx(), runner, &mockLogger{})
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
	})

	t.Run("interpolates command with params", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{"name": "world"},
			WorkflowFile: "test.yaml",
		})
		step := model.Step{ID: "s", Command: "echo {{name}}", Session: model.SessionNew}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		if len(runner.calls) == 0 {
			t.Fatal("expected command to be called")
		}
		if runner.calls[0][2] != "echo world" {
			t.Fatalf("expected interpolated command, got %q", runner.calls[0][2])
		}
	})

	t.Run("interpolates command with session_dir builtin", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := model.NewRootContext(&model.RootContextOptions{
			WorkflowFile: "test.yaml",
			SessionDir:   "/tmp/runs/abc",
		})
		step := model.Step{ID: "s", Command: "ls {{session_dir}}/output", Session: model.SessionNew}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		if len(runner.calls) == 0 {
			t.Fatal("expected command to be called")
		}
		if runner.calls[0][2] != "ls /tmp/runs/abc/output" {
			t.Fatalf("expected interpolated command, got %q", runner.calls[0][2])
		}
	})

	t.Run("captures stdout to variable", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "captured-output"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Command: "echo hi", Session: model.SessionNew, Capture: "output"}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		if ctx.CapturedVariables["output"] != "captured-output" {
			t.Fatalf("expected captured output, got %q", ctx.CapturedVariables["output"])
		}
	})

	t.Run("captures stdout and stderr on failure when capture_stderr enabled", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1, Stdout: "Status: error", Stderr: "config parse failed: bad format"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Command: "validator run", Session: model.SessionNew, Capture: "output", CaptureStderr: true}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		captured := ctx.CapturedVariables["output"]
		if !strings.Contains(captured, "Status: error") {
			t.Fatalf("expected stdout in capture, got %q", captured)
		}
		if !strings.Contains(captured, "STDERR:") || !strings.Contains(captured, "config parse failed") {
			t.Fatalf("expected stderr appended on failure, got %q", captured)
		}
	})

	t.Run("does not append stderr on failure without capture_stderr", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1, Stdout: "Status: error", Stderr: "secret-token-abc123"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Command: "validator run", Session: model.SessionNew, Capture: "output"}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		captured := ctx.CapturedVariables["output"]
		if strings.Contains(captured, "secret-token") {
			t.Fatalf("stderr should not be captured without capture_stderr, got %q", captured)
		}
	})

	t.Run("does not append stderr on success even with capture_stderr", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "ok", Stderr: "some warning"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Command: "echo ok", Session: model.SessionNew, Capture: "output", CaptureStderr: true}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		if ctx.CapturedVariables["output"] != "ok" {
			t.Fatalf("expected only stdout on success, got %q", ctx.CapturedVariables["output"])
		}
	})

	t.Run("returns failed for empty command", func(t *testing.T) {
		runner := &mockRunner{}
		step := model.Step{ID: "s", Command: "", Session: model.SessionNew}
		outcome, _ := ExecuteShellStep(&step, makeCtx(), runner, &mockLogger{})
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
	})

	t.Run("returns error for undefined variable in command", func(t *testing.T) {
		runner := &mockRunner{}
		step := model.Step{ID: "s", Command: "echo {{missing}}", Session: model.SessionNew}
		_, err := ExecuteShellStep(&step, makeCtx(), runner, &mockLogger{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "undefined variable") {
			t.Fatalf("expected interpolation error, got: %v", err)
		}
	})

	t.Run("logs the command", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		log := &mockLogger{}
		step := model.Step{ID: "s", Command: "echo hi", Session: model.SessionNew}
		ExecuteShellStep(&step, makeCtx(), runner, log)
		found := false
		for _, line := range log.lines {
			if strings.Contains(line, "echo hi") {
				found = true
			}
		}
		if !found {
			t.Fatal("expected command to be logged")
		}
	})

	t.Run("interpolates with captured variables", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := makeCtx()
		ctx.CapturedVariables["prev"] = "previous-value"
		step := model.Step{ID: "s", Command: "echo {{prev}}", Session: model.SessionNew}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		if runner.calls[0][2] != "echo previous-value" {
			t.Fatalf("expected interpolated command, got %q", runner.calls[0][2])
		}
	})

	t.Run("interactive mode uses PTY shell runner with suspend and resume hooks", func(t *testing.T) {
		oldFn := interactiveShellRunnerFn
		var gotCommand string
		var gotOpts pty.Options
		interactiveShellRunnerFn = func(command string, opts pty.Options) (pty.Result, error) {
			gotCommand = command
			gotOpts = opts
			return pty.Result{ExitCode: 0}, nil
		}
		defer func() { interactiveShellRunnerFn = oldFn }()

		runner := &mockRunner{}
		ctx := makeCtx()
		var hooks []string
		ctx.SuspendHook = func() { hooks = append(hooks, "suspend") }
		ctx.ResumeHook = func() { hooks = append(hooks, "resume") }

		step := model.Step{
			ID:      "s",
			Command: "read -p 'Name? ' name",
			Session: model.SessionNew,
			Mode:    model.ModeInteractive,
			Workdir: "/tmp/project",
		}
		outcome, err := ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("expected ProcessRunner.RunShell not to be used, got %v", runner.calls)
		}
		if gotCommand != "read -p 'Name? ' name" {
			t.Fatalf("expected interactive shell command, got %q", gotCommand)
		}
		if gotOpts.Workdir != "/tmp/project" {
			t.Fatalf("expected workdir to be forwarded, got %q", gotOpts.Workdir)
		}
		if strings.Join(hooks, ",") != "suspend,resume" {
			t.Fatalf("expected suspend/resume hooks, got %v", hooks)
		}
	})

	t.Run("interactive mode surfaces PTY transcript as step stdout in audit log", func(t *testing.T) {
		oldFn := interactiveShellRunnerFn
		interactiveShellRunnerFn = func(_ string, _ pty.Options) (pty.Result, error) {
			return pty.Result{ExitCode: 0, Stdout: "What's your favorite color? blue\nNice choice — blue it is.\n"}, nil
		}
		defer func() { interactiveShellRunnerFn = oldFn }()

		recorder := &mockAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = recorder

		step := model.Step{ID: "s", Command: "read -p 'color? ' c", Session: model.SessionNew, Mode: model.ModeInteractive}
		if _, err := ExecuteShellStep(&step, ctx, &mockRunner{}, &mockLogger{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var end audit.Event
		for _, ev := range recorder.events {
			if ev.Type == audit.EventStepEnd {
				end = ev
			}
		}
		if end.Type != audit.EventStepEnd {
			t.Fatalf("expected step_end event, got %+v", recorder.events)
		}
		got, _ := end.Data["stdout"].(string)
		want := "What's your favorite color? blue\nNice choice — blue it is.\n"
		if got != want {
			t.Errorf("audit stdout = %q, want %q", got, want)
		}
	})

	t.Run("interactive mode maps nonzero exit code to failed", func(t *testing.T) {
		oldFn := interactiveShellRunnerFn
		interactiveShellRunnerFn = func(_ string, _ pty.Options) (pty.Result, error) {
			return pty.Result{ExitCode: 2}, nil
		}
		defer func() { interactiveShellRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Command: "exit 2", Session: model.SessionNew, Mode: model.ModeInteractive}
		outcome, err := ExecuteShellStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("expected ProcessRunner.RunShell not to be used, got %v", runner.calls)
		}
	})
}

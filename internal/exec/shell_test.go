package exec

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/interactive"
	"github.com/codagent/agent-runner/internal/model"
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

func (m *mockRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	m.calls = append(m.calls, options.Args)
	if m.idx >= len(m.results) {
		return ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

func (m *mockRunner) RunScript(path string, stdin []byte, capture bool, workdir string) (ProcessResult, error) {
	m.calls = append(m.calls, []string{path, string(stdin), workdir})
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
	t.Run("emits identity and measured zero usage", func(t *testing.T) {
		auditLog := &mockAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = auditLog
		step := model.Step{ID: "s", Command: "echo hi", Session: model.SessionNew}

		_, err := ExecuteShellStep(&step, ctx, &mockRunner{results: []ProcessResult{{ExitCode: 0}}}, &mockLogger{})
		if err != nil {
			t.Fatal(err)
		}
		end := auditLog.events[len(auditLog.events)-1]
		identity, ok := end.Data["identity"].(model.ExecutionIdentity)
		if !ok || identity.StepID != "s" || identity.StepType != "shell" || identity.Kind != "step" || identity.AgentInvoked {
			t.Fatalf("identity = %#v", end.Data["identity"])
		}
		usage, ok := end.Data["usage"].(model.UsageRecord)
		if !ok || usage.Status != model.UsageCollected || usage.Tokens == nil || len(usage.Tokens) != 0 {
			t.Fatalf("usage = %#v", end.Data["usage"])
		}
		if value, exists := end.Data["estimated_api_cost_usd"]; !exists || value != (*float64)(nil) {
			t.Fatalf("cost = %#v, exists=%v", value, exists)
		}
	})

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
		if runner.calls[0][2] != "echo 'world'" {
			t.Fatalf("expected interpolated command, got %q", runner.calls[0][2])
		}
	})

	t.Run("shell-quotes interpolated params", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{"task_file": `openspec/tasks.md"; touch /tmp/pwned; echo "`},
			WorkflowFile: "test.yaml",
		})
		step := model.Step{ID: "s", Command: `agent-validator run --context-file "{{task_file}}"`, Session: model.SessionNew}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		if len(runner.calls) == 0 {
			t.Fatal("expected command to be called")
		}
		want := `agent-validator run --context-file "openspec/tasks.md\"; touch /tmp/pwned; echo \""`
		if runner.calls[0][2] != want {
			t.Fatalf("expected shell-safe interpolation, got %q", runner.calls[0][2])
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
		if runner.calls[0][2] != "ls '/tmp/runs/abc'/output" {
			t.Fatalf("expected interpolated command, got %q", runner.calls[0][2])
		}
	})

	t.Run("captures stdout to variable", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "captured-output"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Command: "echo hi", Session: model.SessionNew, Capture: "output"}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		if ctx.CapturedVariables["output"].Str != "captured-output" {
			t.Fatalf("expected captured output, got %q", ctx.CapturedVariables["output"])
		}
	})

	t.Run("captures stdout and stderr on failure when capture_stderr enabled", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1, Stdout: "Status: error", Stderr: "config parse failed: bad format"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Command: "validator run", Session: model.SessionNew, Capture: "output", CaptureStderr: true}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		captured := ctx.CapturedVariables["output"].Str
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
		captured := ctx.CapturedVariables["output"].Str
		if strings.Contains(captured, "secret-token") {
			t.Fatalf("stderr should not be captured without capture_stderr, got %q", captured)
		}
	})

	t.Run("does not append stderr on success even with capture_stderr", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "ok", Stderr: "some warning"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Command: "echo ok", Session: model.SessionNew, Capture: "output", CaptureStderr: true}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		if ctx.CapturedVariables["output"].Str != "ok" {
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
		ctx.CapturedVariables["prev"] = model.NewCapturedString("previous-value")
		step := model.Step{ID: "s", Command: "echo {{prev}}", Session: model.SessionNew}
		ExecuteShellStep(&step, ctx, runner, &mockLogger{})
		if runner.calls[0][2] != "echo 'previous-value'" {
			t.Fatalf("expected interpolated command, got %q", runner.calls[0][2])
		}
	})

	t.Run("autonomous mode records stdout in the audit log without a capture variable", func(t *testing.T) {
		recorder := &mockAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = recorder
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "observable output"}}}
		step := model.Step{ID: "s", Command: "echo observable output", Session: model.SessionNew}

		if _, err := ExecuteShellStep(&step, ctx, runner, &mockLogger{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		end := findAuditEvent(recorder.events, audit.EventStepEnd)
		if end == nil {
			t.Fatalf("expected step_end event, got %+v", recorder.events)
		}
		if got, exists := end.Data["stdout"]; !exists || got != "observable output" {
			t.Fatalf("autonomous step_end stdout = %q, exists = %v", got, exists)
		}
	})

	t.Run("interactive mode uses direct terminal runner with suspend and resume hooks", func(t *testing.T) {
		oldFn := interactiveShellRunnerFn
		var gotOpts *interactive.TerminalOptions
		interactiveShellRunnerFn = func(_ context.Context, opts *interactive.TerminalOptions) (interactive.TerminalResult, error) {
			gotOpts = opts
			if err := opts.Before(); err != nil {
				return interactive.TerminalResult{}, err
			}
			if err := opts.After(); err != nil {
				return interactive.TerminalResult{}, err
			}
			return interactive.TerminalResult{ExitCode: 0}, nil
		}
		defer func() { interactiveShellRunnerFn = oldFn }()

		runner := &mockRunner{}
		ctx := makeCtx()
		var hooks []string
		ctx.SuspendHook = func() error { hooks = append(hooks, "suspend"); return nil }
		ctx.ResumeHook = func() error { hooks = append(hooks, "resume"); return nil }

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
		if got := strings.Join(gotOpts.Args, " "); got != "sh -c read -p 'Name? ' name" {
			t.Fatalf("expected interactive shell command, got %q", got)
		}
		if gotOpts.Workdir != "/tmp/project" {
			t.Fatalf("expected workdir to be forwarded, got %q", gotOpts.Workdir)
		}
		if strings.Join(hooks, ",") != "suspend,resume" {
			t.Fatalf("expected suspend/resume hooks, got %v", hooks)
		}
	})

	t.Run("interactive mode does not record terminal output in the audit log", func(t *testing.T) {
		oldFn := interactiveShellRunnerFn
		interactiveShellRunnerFn = func(_ context.Context, _ *interactive.TerminalOptions) (interactive.TerminalResult, error) {
			return interactive.TerminalResult{ExitCode: 0}, nil
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
		if _, exists := end.Data["stdout"]; exists {
			t.Errorf("interactive step_end unexpectedly recorded terminal stdout: %+v", end.Data)
		}
	})

	t.Run("interactive mode maps nonzero exit code to failed", func(t *testing.T) {
		oldFn := interactiveShellRunnerFn
		interactiveShellRunnerFn = func(_ context.Context, _ *interactive.TerminalOptions) (interactive.TerminalResult, error) {
			return interactive.TerminalResult{ExitCode: 2}, nil
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

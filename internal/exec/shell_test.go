package exec

import (
	"fmt"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

// --- Test helpers ---

type mockRunner struct {
	calls   [][]string
	results []ProcessResult
	idx     int
}

func (m *mockRunner) RunShell(cmd string, capture bool) (ProcessResult, error) {
	m.calls = append(m.calls, []string{"sh", "-c", cmd})
	if m.idx >= len(m.results) {
		return ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

func (m *mockRunner) RunAgent(args []string) (ProcessResult, error) {
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
	return model.NewRootContext(model.RootContextOptions{
		Params:       map[string]string{},
		WorkflowFile: "test.yaml",
	})
}

func TestExecuteShellStep(t *testing.T) {
	t.Run("returns success for exit code 0", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeShell, Command: "echo hi", Session: model.SessionNew}
		outcome, err := ExecuteShellStep(step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
	})

	t.Run("returns failed for non-zero exit code", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1}}}
		step := model.Step{ID: "s", Mode: model.ModeShell, Command: "false", Session: model.SessionNew}
		outcome, _ := ExecuteShellStep(step, makeCtx(), runner, &mockLogger{})
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
	})

	t.Run("interpolates command with params", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := model.NewRootContext(model.RootContextOptions{
			Params:       map[string]string{"name": "world"},
			WorkflowFile: "test.yaml",
		})
		step := model.Step{ID: "s", Mode: model.ModeShell, Command: "echo {{name}}", Session: model.SessionNew}
		ExecuteShellStep(step, ctx, runner, &mockLogger{})
		if len(runner.calls) == 0 {
			t.Fatal("expected command to be called")
		}
		if runner.calls[0][2] != "echo world" {
			t.Fatalf("expected interpolated command, got %q", runner.calls[0][2])
		}
	})

	t.Run("captures stdout to variable", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "captured-output"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Mode: model.ModeShell, Command: "echo hi", Session: model.SessionNew, Capture: "output"}
		ExecuteShellStep(step, ctx, runner, &mockLogger{})
		if ctx.CapturedVariables["output"] != "captured-output" {
			t.Fatalf("expected captured output, got %q", ctx.CapturedVariables["output"])
		}
	})

	t.Run("returns failed for empty command", func(t *testing.T) {
		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeShell, Command: "", Session: model.SessionNew}
		outcome, _ := ExecuteShellStep(step, makeCtx(), runner, &mockLogger{})
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
	})

	t.Run("returns error for undefined variable in command", func(t *testing.T) {
		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeShell, Command: "echo {{missing}}", Session: model.SessionNew}
		_, err := ExecuteShellStep(step, makeCtx(), runner, &mockLogger{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "Undefined variable") {
			t.Fatalf("expected interpolation error, got: %v", err)
		}
	})

	t.Run("logs the command", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		log := &mockLogger{}
		step := model.Step{ID: "s", Mode: model.ModeShell, Command: "echo hi", Session: model.SessionNew}
		ExecuteShellStep(step, makeCtx(), runner, log)
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
		step := model.Step{ID: "s", Mode: model.ModeShell, Command: "echo {{prev}}", Session: model.SessionNew}
		ExecuteShellStep(step, ctx, runner, &mockLogger{})
		if runner.calls[0][2] != "echo previous-value" {
			t.Fatalf("expected interpolated command, got %q", runner.calls[0][2])
		}
	})
}

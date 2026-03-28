package runner

import (
	"fmt"
	"testing"

	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/model"
)

type mockRunner struct {
	calls   [][]string
	results []exec.ProcessResult
	idx     int
}

func (m *mockRunner) RunShell(cmd string, capture bool) (exec.ProcessResult, error) {
	m.calls = append(m.calls, []string{"sh", "-c", cmd})
	if m.idx >= len(m.results) {
		return exec.ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

func (m *mockRunner) RunAgent(args []string) (exec.ProcessResult, error) {
	m.calls = append(m.calls, args)
	if m.idx >= len(m.results) {
		return exec.ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

func (m *mockRunner) StartAgent(args []string) (exec.AgentProcess, error) {
	m.calls = append(m.calls, args)
	if m.idx >= len(m.results) {
		return &mockAgentProcess{result: exec.ProcessResult{ExitCode: 0}}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return &mockAgentProcess{result: r}, nil
}

type mockAgentProcess struct {
	result exec.ProcessResult
}

func (p *mockAgentProcess) Wait() (exec.ProcessResult, error) {
	return p.result, nil
}

func (p *mockAgentProcess) Kill() error {
	return nil
}

type mockGlob struct{ matches []string }

func (g *mockGlob) Expand(_ string) ([]string, error) { return g.matches, nil }

type mockLog struct{ lines []string }

func (l *mockLog) Println(args ...any) { l.lines = append(l.lines, fmt.Sprint(args...)) }
func (l *mockLog) Printf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}
func (l *mockLog) Errorf(format string, args ...any) {
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func shellStep(id, cmd string) model.Step {
	return model.Step{ID: id, Mode: model.ModeShell, Command: cmd, Session: model.SessionNew}
}

func TestRunWorkflow(t *testing.T) {
	t.Run("runs single step workflow", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "echo hi")},
		}
		w.ApplyDefaults()
		result, err := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			StateDir:      t.TempDir(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
	})

	t.Run("runs multi-step workflow in order", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}, {ExitCode: 0}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1", "echo first"),
				shellStep("s2", "echo second"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			StateDir:      t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if len(runner.calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(runner.calls))
		}
	})

	t.Run("stops on failure", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1", "false"),
				shellStep("s2", "echo never"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			StateDir:      t.TempDir(),
		})
		if result != ResultFailed {
			t.Fatalf("expected failed, got %q", result)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (stopped), got %d", len(runner.calls))
		}
	})

	t.Run("continues on failure with continue_on_failure", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}, {ExitCode: 0}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				{ID: "s1", Mode: model.ModeShell, Command: "false", Session: model.SessionNew, ContinueOnFailure: true},
				shellStep("s2", "echo yes"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			StateDir:      t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if len(runner.calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(runner.calls))
		}
	})

	t.Run("skip_if previous_success skips step", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1", "echo ok"),
				{ID: "s2", Mode: model.ModeShell, Command: "echo skip me", Session: model.SessionNew, SkipIf: "previous_success"},
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			StateDir:      t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (second skipped), got %d", len(runner.calls))
		}
	})

	t.Run("validates required params", func(t *testing.T) {
		w := model.Workflow{
			Name:   "test",
			Params: []model.Param{{Name: "required_param"}},
			Steps:  []model.Step{shellStep("s1", "echo")},
		}
		w.ApplyDefaults()
		_, err := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: &mockRunner{},
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			StateDir:      t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected error for missing required param")
		}
	})

	t.Run("applies default param values", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name:   "test",
			Params: []model.Param{{Name: "env", Default: "dev"}},
			Steps:  []model.Step{shellStep("s1", "echo {{env}}")},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			StateDir:      t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if runner.calls[0][2] != "echo dev" {
			t.Fatalf("expected 'echo dev', got %q", runner.calls[0][2])
		}
	})

	t.Run("resumes from specified step", func(t *testing.T) {
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name: "test",
			Steps: []model.Step{
				shellStep("s1", "echo skip"),
				shellStep("s2", "echo run"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			From:          "s2",
			StateDir:      t.TempDir(),
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call (s1 skipped), got %d", len(runner.calls))
		}
		if runner.calls[0][2] != "echo run" {
			t.Fatalf("expected 'echo run', got %q", runner.calls[0][2])
		}
	})

	t.Run("errors for unknown from step", func(t *testing.T) {
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "echo")},
		}
		w.ApplyDefaults()
		_, err := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: &mockRunner{},
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			From:          "nonexistent",
			StateDir:      t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected error for unknown step")
		}
	})
}

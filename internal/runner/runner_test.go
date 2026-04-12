package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/model"
)

type mockRunner struct {
	calls   [][]string
	results []exec.ProcessResult
	idx     int
}

func (m *mockRunner) RunShell(cmd string, capture bool, _ string) (exec.ProcessResult, error) {
	m.calls = append(m.calls, []string{"sh", "-c", cmd})
	if m.idx >= len(m.results) {
		return exec.ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
}

func (m *mockRunner) RunAgent(args []string, _ bool, _ string) (exec.ProcessResult, error) {
	m.calls = append(m.calls, args)
	if m.idx >= len(m.results) {
		return exec.ProcessResult{ExitCode: 0}, nil
	}
	r := m.results[m.idx]
	m.idx++
	return r, nil
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
	return model.Step{ID: id, Command: cmd, Session: model.SessionNew}
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
			SessionDir:    t.TempDir(),
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
			SessionDir:    t.TempDir(),
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
			SessionDir:    t.TempDir(),
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
				{ID: "s1", Command: "false", Session: model.SessionNew, ContinueOnFailure: true},
				shellStep("s2", "echo yes"),
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
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
				{ID: "s2", Command: "echo skip me", Session: model.SessionNew, SkipIf: "previous_success"},
			},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    t.TempDir(),
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
			SessionDir:    t.TempDir(),
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
			SessionDir:    t.TempDir(),
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
			SessionDir:    t.TempDir(),
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
			SessionDir:    t.TempDir(),
		})
		if err == nil {
			t.Fatal("expected error for unknown step")
		}
	})

	t.Run("writes state.json and audit.log into session directory", func(t *testing.T) {
		sessionDir := t.TempDir()
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "false")},
		}
		w.ApplyDefaults()
		RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    sessionDir,
		})

		// State file should be state.json (not agent-runner-state.json).
		stateFile := filepath.Join(sessionDir, "state.json")
		if _, err := os.Stat(stateFile); os.IsNotExist(err) {
			t.Fatal("expected state.json in session directory")
		}

		// Audit log should exist in the same directory.
		auditFile := filepath.Join(sessionDir, "audit.log")
		if _, err := os.Stat(auditFile); os.IsNotExist(err) {
			t.Fatal("expected audit.log in session directory")
		}
	})

	t.Run("deletes state.json on success", func(t *testing.T) {
		sessionDir := t.TempDir()
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "echo ok")},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    sessionDir,
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}

		stateFile := filepath.Join(sessionDir, "state.json")
		if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
			t.Fatal("expected state.json to be deleted on success")
		}
	})

	t.Run("prints resume hint with session ID on failure", func(t *testing.T) {
		sessionDir := filepath.Join(t.TempDir(), "deploy-service-test")
		log := &mockLog{}
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
		w := model.Workflow{
			Name:  "deploy-service",
			Steps: []model.Step{shellStep("s1", "false")},
		}
		w.ApplyDefaults()
		RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           log,
			SessionDir:    sessionDir,
		})

		found := false
		for _, line := range log.lines {
			if contains(line, "--resume deploy-service-") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected resume hint with session ID, got log lines: %v", log.lines)
		}
	})

	t.Run("creates lock file during run and deletes it on success", func(t *testing.T) {
		sessionDir := t.TempDir()
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "echo ok")},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    sessionDir,
		})
		if result != ResultSuccess {
			t.Fatalf("expected success, got %q", result)
		}

		lockFile := filepath.Join(sessionDir, "lock")
		if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
			t.Fatal("expected lock file to be deleted after successful run")
		}
	})

	t.Run("deletes lock file on failure", func(t *testing.T) {
		sessionDir := t.TempDir()
		runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
		w := model.Workflow{
			Name:  "test",
			Steps: []model.Step{shellStep("s1", "false")},
		}
		w.ApplyDefaults()
		result, _ := RunWorkflow(&w, map[string]string{}, &Options{
			ProcessRunner: runner,
			GlobExpander:  &mockGlob{},
			Log:           &mockLog{},
			SessionDir:    sessionDir,
		})
		if result != ResultFailed {
			t.Fatalf("expected failed, got %q", result)
		}

		lockFile := filepath.Join(sessionDir, "lock")
		if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
			t.Fatal("expected lock file to be deleted after failed run")
		}
	})
}

func TestWriteMetaJSON(t *testing.T) {
	t.Run("creates meta.json with project path", func(t *testing.T) {
		dir := t.TempDir()
		writeMetaJSON(dir, "/home/user/myproject")

		data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
		if err != nil {
			t.Fatalf("failed to read meta.json: %v", err)
		}
		if !contains(string(data), "/home/user/myproject") {
			t.Fatalf("expected path in meta.json, got %s", data)
		}
	})

	t.Run("does not overwrite existing meta.json", func(t *testing.T) {
		dir := t.TempDir()
		existing := `{"path":"/original/path"}`
		os.WriteFile(filepath.Join(dir, "meta.json"), []byte(existing), 0o600)

		writeMetaJSON(dir, "/new/path")

		data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
		if err != nil {
			t.Fatalf("failed to read meta.json: %v", err)
		}
		if string(data) != existing {
			t.Fatalf("meta.json was overwritten: got %s", data)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/resumehandoff"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestPostWorkflowResumeHandoff(t *testing.T) {
	t.Run("success with valid target execs resume", func(t *testing.T) {
		home, repo, targetSession := setupResumeTargetRun(t, "target-run")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		sourceSession := t.TempDir()
		if err := os.WriteFile(resumehandoff.MarkerPath(sourceSession), []byte("target-run\n"), 0o600); err != nil {
			t.Fatalf("write marker: %v", err)
		}

		restore, calls := stubExecSelf(t)
		defer restore()

		result := handlePostWorkflowResumeHandoff(sourceSession, runner.ResultSuccess)
		if result.inlineError != "" {
			t.Fatalf("inlineError = %q, want empty", result.inlineError)
		}
		if len(*calls) != 1 {
			t.Fatalf("exec calls = %d, want 1; target session %s", len(*calls), targetSession)
		}
		if !strings.Contains(strings.Join((*calls)[0].args, " "), "--resume target-run") {
			t.Fatalf("exec args = %#v, want --resume target-run", (*calls)[0].args)
		}
	})

	t.Run("non success discards marker", func(t *testing.T) {
		sourceSession := t.TempDir()
		if err := os.WriteFile(resumehandoff.MarkerPath(sourceSession), []byte("target-run\n"), 0o600); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		restore, calls := stubExecSelf(t)
		defer restore()

		result := handlePostWorkflowResumeHandoff(sourceSession, runner.ResultFailed)
		if result.inlineError != "" {
			t.Fatalf("inlineError = %q, want empty", result.inlineError)
		}
		if len(*calls) != 0 {
			t.Fatalf("exec calls = %d, want 0", len(*calls))
		}
	})

	t.Run("success without marker returns", func(t *testing.T) {
		restore, calls := stubExecSelf(t)
		defer restore()

		result := handlePostWorkflowResumeHandoff(t.TempDir(), runner.ResultSuccess)
		if result.inlineError != "" {
			t.Fatalf("inlineError = %q, want empty", result.inlineError)
		}
		if len(*calls) != 0 {
			t.Fatalf("exec calls = %d, want 0", len(*calls))
		}
	})

	t.Run("invalid target reports inline error", func(t *testing.T) {
		home := t.TempDir()
		repo := t.TempDir()
		t.Setenv("HOME", home)
		t.Chdir(repo)
		sourceSession := t.TempDir()
		if err := os.WriteFile(resumehandoff.MarkerPath(sourceSession), []byte("missing-run\n"), 0o600); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		restore, calls := stubExecSelf(t)
		defer restore()

		result := handlePostWorkflowResumeHandoff(sourceSession, runner.ResultSuccess)
		if result.inlineError == "" || !strings.Contains(result.inlineError, "missing-run") {
			t.Fatalf("inlineError = %q, want missing-run error", result.inlineError)
		}
		if len(*calls) != 0 {
			t.Fatalf("exec calls = %d, want 0", len(*calls))
		}
	})
}

type execCall struct {
	path string
	args []string
	env  []string
}

func stubExecSelf(t *testing.T) (func(), *[]execCall) {
	t.Helper()
	originalExecutable := currentExecutable
	originalExec := execProcess
	calls := []execCall{}
	currentExecutable = func() (string, error) {
		return "/tmp/agent-runner", nil
	}
	execProcess = func(path string, args []string, env []string) error {
		calls = append(calls, execCall{
			path: path,
			args: append([]string(nil), args...),
			env:  append([]string(nil), env...),
		})
		return nil
	}
	return func() {
		currentExecutable = originalExecutable
		execProcess = originalExec
	}, &calls
}

func setupResumeTargetRun(t *testing.T, runID string) (home, repo, sessionDir string) {
	t.Helper()
	home = t.TempDir()
	repo = t.TempDir()
	sessionDir = filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(repo), "runs", runID)
	state := &model.RunState{
		WorkflowFile: "workflow.yaml",
		WorkflowName: "workflow",
		CurrentStep:  model.CurrentStep{StepID: "s"},
		Params:       map[string]string{},
	}
	if err := stateio.WriteState(state, sessionDir); err != nil {
		t.Fatalf("write state: %v", err)
	}
	return home, repo, sessionDir
}

func TestPostWorkflowResumeHandoffUnreadableState(t *testing.T) {
	home, repo, sessionDir := setupResumeTargetRun(t, "target-run")
	t.Setenv("HOME", home)
	t.Chdir(repo)
	if err := os.WriteFile(filepath.Join(sessionDir, "state.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("corrupt state: %v", err)
	}
	sourceSession := t.TempDir()
	if err := os.WriteFile(resumehandoff.MarkerPath(sourceSession), []byte("target-run\n"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	restore, calls := stubExecSelf(t)
	defer restore()

	result := handlePostWorkflowResumeHandoff(sourceSession, runner.ResultSuccess)
	if result.inlineError == "" || !strings.Contains(result.inlineError, "target-run") {
		t.Fatalf("inlineError = %q, want target-run read error", result.inlineError)
	}
	if len(*calls) != 0 {
		t.Fatalf("exec calls = %d, want 0", len(*calls))
	}
}

func TestPostWorkflowResumeHandoffExecError(t *testing.T) {
	home, repo, _ := setupResumeTargetRun(t, "target-run")
	t.Setenv("HOME", home)
	t.Chdir(repo)
	sourceSession := t.TempDir()
	if err := os.WriteFile(resumehandoff.MarkerPath(sourceSession), []byte("target-run\n"), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	originalExecutable := currentExecutable
	originalExec := execProcess
	t.Cleanup(func() {
		currentExecutable = originalExecutable
		execProcess = originalExec
	})
	currentExecutable = func() (string, error) { return "/tmp/agent-runner", nil }
	execProcess = func(path string, args []string, env []string) error {
		return errors.New("boom")
	}

	result := handlePostWorkflowResumeHandoff(sourceSession, runner.ResultSuccess)
	if result.exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", result.exitCode)
	}
}

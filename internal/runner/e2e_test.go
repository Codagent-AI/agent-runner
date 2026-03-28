package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
)

// E2E tests use real YAML fixtures but mock the process runner.

func TestE2ECapture(t *testing.T) {
	dir := t.TempDir()
	workflowYAML := `name: e2e-capture
steps:
  - id: capture-step
    mode: shell
    command: echo hello
    capture: output
  - id: use-capture
    mode: shell
    command: echo {{output}}
`
	wfPath := filepath.Join(dir, "workflow.yaml")
	os.WriteFile(wfPath, []byte(workflowYAML), 0o644)

	workflow, err := loader.LoadWorkflow(wfPath, loader.Options{})
	if err != nil {
		t.Fatalf("load workflow: %v", err)
	}

	runner := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: "hello"},
		{ExitCode: 0},
	}}
	result, err := RunWorkflow(workflow, map[string]string{}, Options{
		WorkflowFile:  wfPath,
		ProcessRunner: runner,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
		StateDir:      dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(runner.calls))
	}
	// Second command should have interpolated the captured value
	if runner.calls[1][2] != "echo hello" {
		t.Fatalf("expected captured value interpolated, got %q", runner.calls[1][2])
	}
}

func TestE2EContinueOnFailure(t *testing.T) {
	dir := t.TempDir()
	workflowYAML := `name: e2e-continue
steps:
  - id: failing-step
    mode: shell
    command: "false"
    continue_on_failure: true
  - id: next-step
    mode: shell
    command: echo ok
`
	wfPath := filepath.Join(dir, "workflow.yaml")
	os.WriteFile(wfPath, []byte(workflowYAML), 0o644)

	workflow, _ := loader.LoadWorkflow(wfPath, loader.Options{})
	runner := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 1},
		{ExitCode: 0},
	}}
	result, _ := RunWorkflow(workflow, map[string]string{}, Options{
		WorkflowFile:  wfPath,
		ProcessRunner: runner,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
		StateDir:      dir,
	})
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(runner.calls))
	}
}

func TestE2ESkipIfSuccess(t *testing.T) {
	dir := t.TempDir()
	workflowYAML := `name: e2e-skip
steps:
  - id: first
    mode: shell
    command: echo ok
  - id: second
    mode: shell
    command: echo skip me
    skip_if: previous_success
`
	wfPath := filepath.Join(dir, "workflow.yaml")
	os.WriteFile(wfPath, []byte(workflowYAML), 0o644)

	workflow, _ := loader.LoadWorkflow(wfPath, loader.Options{})
	runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
	result, _ := RunWorkflow(workflow, map[string]string{}, Options{
		WorkflowFile:  wfPath,
		ProcessRunner: runner,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
		StateDir:      dir,
	})
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 call (second skipped), got %d", len(runner.calls))
	}
}

func TestE2EFailurePreservesState(t *testing.T) {
	dir := t.TempDir()
	workflowYAML := `name: e2e-failure
steps:
  - id: first
    mode: shell
    command: echo ok
  - id: second
    mode: shell
    command: "false"
`
	wfPath := filepath.Join(dir, "workflow.yaml")
	os.WriteFile(wfPath, []byte(workflowYAML), 0o644)

	workflow, _ := loader.LoadWorkflow(wfPath, loader.Options{})
	runner := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0},
		{ExitCode: 1},
	}}
	result, _ := RunWorkflow(workflow, map[string]string{}, Options{
		WorkflowFile:  wfPath,
		ProcessRunner: runner,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
		StateDir:      dir,
	})
	if result != ResultFailed {
		t.Fatalf("expected failed, got %q", result)
	}
	// State file should exist
	_, err := os.Stat(filepath.Join(dir, "agent-runner-state.json"))
	if os.IsNotExist(err) {
		t.Fatal("expected state file to exist after failure")
	}
}

func TestE2ESuccessDeletesState(t *testing.T) {
	dir := t.TempDir()
	workflowYAML := `name: e2e-success
steps:
  - id: only
    mode: shell
    command: echo ok
`
	wfPath := filepath.Join(dir, "workflow.yaml")
	os.WriteFile(wfPath, []byte(workflowYAML), 0o644)

	workflow, _ := loader.LoadWorkflow(wfPath, loader.Options{})
	runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
	result, _ := RunWorkflow(workflow, map[string]string{}, Options{
		WorkflowFile:  wfPath,
		ProcessRunner: runner,
		GlobExpander:  &mockGlob{},
		Log:           &mockLog{},
		StateDir:      dir,
	})
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	// State file should be deleted on success
	_, err := os.Stat(filepath.Join(dir, "agent-runner-state.json"))
	if !os.IsNotExist(err) {
		t.Fatal("expected state file to be deleted after success")
	}
}

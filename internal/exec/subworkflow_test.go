package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

func TestExecuteSubWorkflowStep(t *testing.T) {
	t.Run("executes child workflow steps", func(t *testing.T) {
		// Create a temp workflow file
		dir := t.TempDir()
		childYAML := `name: child
steps:
  - id: s1
    command: echo hello
`
		childPath := filepath.Join(dir, "child.yaml")
		os.WriteFile(childPath, []byte(childYAML), 0o644)

		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: filepath.Join(dir, "parent.yaml"),
		})

		step := model.Step{ID: "sub", Workflow: "child.yaml", Session: model.SessionNew}
		outcome, err := ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(runner.calls))
		}
	})

	t.Run("passes params to child workflow", func(t *testing.T) {
		dir := t.TempDir()
		childYAML := `name: child
params:
  - name: msg
steps:
  - id: s1
    command: echo {{msg}}
`
		childPath := filepath.Join(dir, "child.yaml")
		os.WriteFile(childPath, []byte(childYAML), 0o644)

		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{"greeting": "hi"},
			WorkflowFile: filepath.Join(dir, "parent.yaml"),
		})

		step := model.Step{
			ID: "sub", Workflow: "child.yaml", Session: model.SessionNew,
			Params: map[string]string{"msg": "{{greeting}}"},
		}
		outcome, err := ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
		// The shell command should have been interpolated with msg=hi
		if len(runner.calls) != 1 {
			t.Fatalf("expected 1 call, got %d", len(runner.calls))
		}
		cmd := runner.calls[0][2] // sh -c <cmd>
		if cmd != "echo hi" {
			t.Fatalf("expected 'echo hi', got %q", cmd)
		}
	})

	t.Run("child context does not inherit parent params", func(t *testing.T) {
		dir := t.TempDir()
		childYAML := `name: child
steps:
  - id: s1
    command: echo {{parent_secret}}
`
		childPath := filepath.Join(dir, "child.yaml")
		os.WriteFile(childPath, []byte(childYAML), 0o644)

		runner := &mockRunner{}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{"parent_secret": "secret"},
			WorkflowFile: filepath.Join(dir, "parent.yaml"),
		})

		step := model.Step{ID: "sub", Workflow: "child.yaml", Session: model.SessionNew}
		// Should fail because child doesn't have parent_secret
		_, err := ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if err == nil {
			t.Fatal("expected error for undefined variable")
		}
	})

	t.Run("errors for missing workflow file", func(t *testing.T) {
		runner := &mockRunner{}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: "/tmp/parent.yaml",
		})

		step := model.Step{ID: "sub", Workflow: "nonexistent.yaml", Session: model.SessionNew}
		_, err := ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("errors for missing required params", func(t *testing.T) {
		dir := t.TempDir()
		childYAML := `name: child
params:
  - name: required_param
steps:
  - id: s1
    command: echo test
`
		childPath := filepath.Join(dir, "child.yaml")
		os.WriteFile(childPath, []byte(childYAML), 0o644)

		runner := &mockRunner{}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: filepath.Join(dir, "parent.yaml"),
		})

		step := model.Step{ID: "sub", Workflow: "child.yaml", Session: model.SessionNew}
		_, err := ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, &mockLogger{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "missing required parameter") {
			t.Fatalf("expected 'missing required parameter', got: %v", err)
		}
	})

	t.Run("returns failed for empty workflow field", func(t *testing.T) {
		step := model.Step{ID: "sub", Workflow: "", Session: model.SessionNew}
		outcome, _ := ExecuteSubWorkflowStep(&step, makeCtx(), &mockRunner{}, &mockGlob{}, &mockLogger{})
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
	})

	t.Run("prints step heading for child workflow steps", func(t *testing.T) {
		dir := t.TempDir()
		childYAML := `name: child
steps:
  - id: s1
    command: echo hello
  - id: s2
    command: echo world
`
		os.WriteFile(filepath.Join(dir, "child.yaml"), []byte(childYAML), 0o644)

		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}, {ExitCode: 0}}}
		log := &mockLogger{}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: filepath.Join(dir, "parent.yaml"),
		})

		step := model.Step{ID: "sub", Workflow: "child.yaml", Session: model.SessionNew}
		ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, log)

		sep := textfmt.Separator()
		sepCount := 0
		heading1Found, heading2Found := false, false
		for _, line := range log.lines {
			if line == sep {
				sepCount++
			}
			if strings.Contains(line, "━━ step 1/2:") && strings.Contains(line, "s1") {
				heading1Found = true
			}
			if strings.Contains(line, "━━ step 2/2:") && strings.Contains(line, "s2") {
				heading2Found = true
			}
		}
		if sepCount < 2 {
			t.Fatalf("expected at least 2 separators, got %d", sepCount)
		}
		if !heading1Found {
			t.Fatal("expected heading for step s1 (1/2)")
		}
		if !heading2Found {
			t.Fatal("expected heading for step s2 (2/2)")
		}
	})

	t.Run("prints skipped heading for child steps with skip_if", func(t *testing.T) {
		dir := t.TempDir()
		childYAML := `name: child
steps:
  - id: s1
    command: echo hello
  - id: s2
    command: echo world
    skip_if: previous_success
`
		os.WriteFile(filepath.Join(dir, "child.yaml"), []byte(childYAML), 0o644)

		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		log := &mockLogger{}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: filepath.Join(dir, "parent.yaml"),
		})

		step := model.Step{ID: "sub", Workflow: "child.yaml", Session: model.SessionNew}
		ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, log)

		skippedFound := false
		for _, line := range log.lines {
			if strings.Contains(line, "[skipped]") && strings.Contains(line, "s2") {
				skippedFound = true
			}
		}
		if !skippedFound {
			t.Fatal("expected skipped heading for step s2")
		}
	})
}

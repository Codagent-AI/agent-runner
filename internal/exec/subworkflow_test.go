package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
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

	t.Run("executes all child workflow steps", func(t *testing.T) {
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
		outcome, err := ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}

		// Both child steps should have been dispatched.
		if len(runner.calls) != 2 {
			t.Fatalf("expected both child steps to run; got %d call(s)", len(runner.calls))
		}
	})

	t.Run("skips child steps matching skip_if", func(t *testing.T) {
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

		// Only one result needed because s2 is skipped after s1 succeeds.
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		log := &mockLogger{}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: filepath.Join(dir, "parent.yaml"),
		})

		step := model.Step{ID: "sub", Workflow: "child.yaml", Session: model.SessionNew}
		outcome, err := ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}

		// The runner should have been called exactly once (s1 ran, s2 was skipped).
		if len(runner.calls) != 1 {
			t.Fatalf("expected s2 to be skipped; got %d call(s)", len(runner.calls))
		}
	})

	t.Run("emits audit events for skipped child steps", func(t *testing.T) {
		dir := t.TempDir()
		childYAML := `name: child
steps:
  - id: s1
    command: echo hello
  - id: s2
    command: echo skipped
    skip_if: previous_success
  - id: s3
    command: echo after
`
		os.WriteFile(filepath.Join(dir, "child.yaml"), []byte(childYAML), 0o644)

		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}, {ExitCode: 0}}}
		log := &mockLogger{}
		recorder := &mockAuditLogger{}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{},
			WorkflowFile: filepath.Join(dir, "parent.yaml"),
			AuditLogger:  recorder,
		})

		step := model.Step{ID: "sub", Workflow: "child.yaml", Session: model.SessionNew}
		outcome, err := ExecuteSubWorkflowStep(&step, ctx, runner, &mockGlob{}, log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
		if len(runner.calls) != 2 {
			t.Fatalf("expected s1 and s3 to run; got %d call(s)", len(runner.calls))
		}

		var skippedEnd *audit.Event
		for i := range recorder.events {
			ev := &recorder.events[i]
			if ev.Type == audit.EventStepEnd && ev.Prefix == "[sub, sub:child, s2]" && ev.Data["outcome"] == "skipped" {
				skippedEnd = ev
				break
			}
		}
		if skippedEnd == nil {
			t.Fatalf("expected skipped step_end for s2, got events: %+v", recorder.events)
		}
		if got := skippedEnd.Data["skip_if"]; got != "previous_success" {
			t.Fatalf("skip_if audit data = %v, want previous_success", got)
		}
	})
}

// Regression: sub-workflow state must preserve LastSessionStepID across
// recordChildProgress / applyResumeState so that resumed session:resume steps
// can look up their profile in SessionProfiles. Prior bug dropped the field,
// causing "no profile found for session-originating step \"\"" on resume.
func TestSubWorkflowState_PreservesLastSessionStepID(t *testing.T) {
	parent := model.NewRootContext(&model.RootContextOptions{Params: map[string]string{}})
	child := model.NewSubWorkflowContext(parent, &model.SubWorkflowContextOptions{StepID: "plan"})
	child.SessionProfiles["proposal"] = "planner"
	child.LastSessionStepID = "proposal"

	recordChildProgress(child, "proposal", true)

	entry := parent.LastSubWorkflowChild
	if entry == nil {
		t.Fatal("expected LastSubWorkflowChild to be set")
	}
	if entry.LastSessionStepID != "proposal" {
		t.Fatalf("entry.LastSessionStepID = %q, want %q", entry.LastSessionStepID, "proposal")
	}

	parent.ResumeChildState = entry
	resumedChild := model.NewSubWorkflowContext(parent, &model.SubWorkflowContextOptions{StepID: "plan"})
	resumedChild.LastSessionStepID = ""
	applyResumeState(parent, resumedChild)
	if resumedChild.LastSessionStepID != "proposal" {
		t.Fatalf("resumedChild.LastSessionStepID = %q, want %q", resumedChild.LastSessionStepID, "proposal")
	}
}

func TestResolveWorkflowPath_EmbeddedParentStaysEmbedded(t *testing.T) {
	ctx := model.NewRootContext(&model.RootContextOptions{
		Params:       map[string]string{},
		WorkflowFile: "builtin:spec-driven/change.yaml",
	})

	got, err := resolveWorkflowPath("plan-change.yaml", ctx, "plan")
	if err != nil {
		t.Fatalf("resolveWorkflowPath returned error: %v", err)
	}
	if got != "builtin:spec-driven/plan-change.yaml" {
		t.Fatalf("resolveWorkflowPath = %q, want %q", got, "builtin:spec-driven/plan-change.yaml")
	}
}

func TestBuiltinImplementTaskSessionReportSkipIf(t *testing.T) {
	workflow, err := loader.LoadWorkflow("builtin:core/implement-task.yaml", loader.Options{IsSubWorkflow: true})
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}
	var sessionReport *model.Step
	for i := range workflow.Steps {
		if workflow.Steps[i].ID == "session-report" {
			sessionReport = &workflow.Steps[i]
			break
		}
	}
	if sessionReport == nil {
		t.Fatal("builtin implement-task workflow missing session-report step")
	}

	ctx := model.NewRootContext(&model.RootContextOptions{
		Params: map[string]string{
			"run_session_report": "true",
		},
	})
	skip, err := ShouldSkipStep(sessionReport.SkipIf, nil, ctx, sessionReport.ID)
	if err != nil {
		t.Fatalf("ShouldSkipStep(true): %v", err)
	}
	if skip {
		t.Fatal("session-report should run when run_session_report is true")
	}

	ctx.Params["run_session_report"] = "false"
	skip, err = ShouldSkipStep(sessionReport.SkipIf, nil, ctx, sessionReport.ID)
	if err != nil {
		t.Fatalf("ShouldSkipStep(false): %v", err)
	}
	if !skip {
		t.Fatal("session-report should skip when run_session_report is false")
	}

	ctx.Params["run_session_report"] = "false value"
	skip, err = ShouldSkipStep(sessionReport.SkipIf, nil, ctx, sessionReport.ID)
	if err != nil {
		t.Fatalf("ShouldSkipStep(whitespace): %v", err)
	}
	if !skip {
		t.Fatal("session-report should skip deterministically for non-true values with whitespace")
	}

	sentinel := filepath.Join(t.TempDir(), "created-by-injection")
	ctx.Params["run_session_report"] = "true; touch " + sentinel
	skip, err = ShouldSkipStep(sessionReport.SkipIf, nil, ctx, sessionReport.ID)
	if err != nil {
		t.Fatalf("ShouldSkipStep(shell metacharacters): %v", err)
	}
	if !skip {
		t.Fatal("session-report should skip for non-true values containing shell metacharacters")
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("skip_if interpolation executed shell metacharacters; sentinel stat err = %v", err)
	}
}

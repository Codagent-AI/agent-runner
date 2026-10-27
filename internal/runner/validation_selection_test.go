package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

const autoRef = "builtin:core/validation-auto-v1.0.yaml"

func aliasedWorkflow() *model.Workflow {
	workflow := &model.Workflow{
		Name: "aliased",
		Params: []model.Param{{
			Name:    "validation_workflow",
			Default: "auto",
			Aliases: map[string]string{"auto": autoRef, "none": "builtin:core/validation-none-v1.0.yaml"},
		}},
		Steps: []model.Step{{ID: "show", Command: "echo {{validation_workflow}}"}},
	}
	workflow.ApplyDefaults()
	return workflow
}

func TestRunWorkflowBindsAliasedParam(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params map[string]string
		want   string
	}{
		{"default", nil, autoRef},
		{"alias", map[string]string{"validation_workflow": "none"}, "builtin:core/validation-none-v1.0.yaml"},
		{"literal passes through", map[string]string{"validation_workflow": "/abs/mine-v1.0.yaml"}, "/abs/mine-v1.0.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &mockRunner{}
			result, err := RunWorkflow(aliasedWorkflow(), tc.params, &Options{
				SessionDir: t.TempDir(), ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: &mockLog{},
			})
			if err != nil || result != ResultSuccess {
				t.Fatalf("RunWorkflow() = (%q, %v)", result, err)
			}
			if len(runner.calls) != 1 || !strings.Contains(runner.calls[0][2], tc.want) {
				t.Errorf("calls = %v, want one command containing %q", runner.calls, tc.want)
			}
		})
	}
}

func warningWorkflow() *model.Workflow {
	workflow := &model.Workflow{
		Name: "warns",
		Steps: []model.Step{
			{ID: "first", Command: "true"},
			{ID: "check", Command: "false", WarnOnFailure: true},
			{ID: "last", Command: "true"},
		},
	}
	workflow.ApplyDefaults()
	return workflow
}

func TestRunStatePersistsWarningOrigins(t *testing.T) {
	dir := t.TempDir()
	runner := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}, {ExitCode: 1, Stderr: "boom"}, {ExitCode: 0}}}
	result, err := RunWorkflow(warningWorkflow(), nil, &Options{
		SessionDir: dir, ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: &mockLog{},
	})
	if err != nil || result != ResultSuccess {
		t.Fatalf("RunWorkflow() = (%q, %v)", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if len(state.Warnings) != 1 || state.Warnings[0].Path != "[check]" {
		t.Fatalf("warnings = %+v, want one for check", state.Warnings)
	}
}

func TestWarningStateRestoreMergesWithNewOrigins(t *testing.T) {
	state := model.NewWarningState()
	records := []model.WarningRecord{{Path: "b", Message: "two"}, {Path: "a", Message: "one"}}
	state.Restore(records)
	state.Add("c", "three")
	want := []model.WarningRecord{{Path: "a", Message: "one"}, {Path: "b", Message: "two"}, {Path: "c", Message: "three"}}
	if diff := cmp.Diff(want, state.Records()); diff != "" {
		t.Errorf("records mismatch (-want +got):\n%s", diff)
	}
}

func TestResumeKeepsRecordedWarnings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "warns-v1.0.yaml")
	source := "name: warns\nsteps:\n  - id: check\n    command: \"false\"\n    warn_on_failure: true\n  - id: last\n    command: \"true\"\n"
	if err := os.WriteFile(workflowPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	state := model.RunState{
		WorkflowFile: workflowPath, WorkflowName: "warns", WorkflowHash: stateio.ComputeWorkflowHash(source),
		CurrentStep: model.CurrentStep{StepID: "last"},
		Warnings:    []model.WarningRecord{{Path: "[check]", Message: "boom"}},
	}
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}
	result, err := ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{
		ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
	})
	if err != nil || result != ResultSuccess {
		t.Fatalf("resume = (%q, %v)", result, err)
	}
	got, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(state.Warnings, got.Warnings); diff != "" {
		t.Errorf("warnings after resume (-want +got):\n%s", diff)
	}
	audit := readRunEnd(t, dir)
	if !strings.Contains(audit, `"completed_with_warnings":true`) || !strings.Contains(audit, `"warning_count":1`) {
		t.Errorf("run_end after resume does not count the earlier warning: %s", audit)
	}
}

func readRunEnd(t *testing.T, dir string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	last := ""
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if strings.Contains(line, "Z run_end {") {
			last = line
		}
	}
	if last == "" {
		t.Fatalf("no run_end in audit.log:\n%s", body)
	}
	return last
}

func TestResumeWithoutRecordedWarningsReportsNone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "plain-v1.0.yaml")
	source := "name: plain\nsteps:\n  - id: only\n    command: \"true\"\n"
	if err := os.WriteFile(workflowPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	state := model.RunState{
		WorkflowFile: workflowPath, WorkflowName: "plain", WorkflowHash: stateio.ComputeWorkflowHash(source),
		CurrentStep: model.CurrentStep{StepID: "only"},
	}
	if err := stateio.WriteState(&state, dir); err != nil {
		t.Fatal(err)
	}
	if result, err := ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{
		ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
	}); err != nil || result != ResultSuccess {
		t.Fatalf("resume = (%q, %v)", result, err)
	}
	if audit := readRunEnd(t, dir); strings.Contains(audit, `"completed_with_warnings":true`) || !strings.Contains(audit, `"warning_count":0`) {
		t.Errorf("run_end reports warnings for a clean run: %s", audit)
	}
}

// INT-011: a warning raised by one step is persisted when a later step fails,
// and the resumed run completes with that warning counted.
func TestWarningSurvivesFailureAndResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "warns-v1.0.yaml")
	source := "name: warns\nsteps:\n  - id: validate\n    command: \"false\"\n    warn_on_failure: true\n  - id: push\n    command: \"push\"\n  - id: last\n    command: \"true\"\n"
	if err := os.WriteFile(workflowPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	failing := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}, {ExitCode: 1, Stderr: "push rejected"}}}
	result, err := RunWorkflow(&workflow, nil, &Options{
		SessionDir: dir, WorkflowFile: workflowPath, ProcessRunner: failing, GlobExpander: &mockGlob{}, Log: &mockLog{},
	})
	if err != nil || result != ResultFailed {
		t.Fatalf("first run = (%q, %v), want failed at push", result, err)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Warnings) != 1 || state.Warnings[0].Path != "[validate]" {
		t.Fatalf("warnings after failure = %+v, want one for validate", state.Warnings)
	}
	result, err = ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{
		ProcessRunner: &mockRunner{}, GlobExpander: &mockGlob{}, Log: &mockLog{},
	})
	if err != nil || result != ResultSuccess {
		t.Fatalf("resume = (%q, %v)", result, err)
	}
	if audit := readRunEnd(t, dir); !strings.Contains(audit, `"completed_with_warnings":true`) || !strings.Contains(audit, `"warning_count":1`) {
		t.Errorf("run_end after resume: %s", audit)
	}
}

// A resumed run keeps the validation selection the original run bound.
func TestResumeKeepsBoundValidationSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "select-v1.0.yaml")
	source := `name: select
params:
  - name: validation_workflow
    required: false
    default: auto
    aliases:
      auto: builtin:core/validation-auto-v1.0.yaml
      none: builtin:core/validation-none-v1.0.yaml
steps:
  - id: gate
    command: "gate"
  - id: show
    command: echo {{validation_workflow}}
`
	if err := os.WriteFile(workflowPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	workflow, err := loader.LoadWorkflow(workflowPath, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	failing := &mockRunner{results: []exec.ProcessResult{{ExitCode: 1}}}
	if result, err := RunWorkflow(&workflow, map[string]string{"validation_workflow": "none"}, &Options{
		SessionDir: dir, WorkflowFile: workflowPath, ProcessRunner: failing, GlobExpander: &mockGlob{}, Log: &mockLog{},
	}); err != nil || result != ResultFailed {
		t.Fatalf("first run = (%q, %v), want failed", result, err)
	}
	resumed := &mockRunner{}
	if result, err := ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{
		ProcessRunner: resumed, GlobExpander: &mockGlob{}, Log: &mockLog{},
	}); err != nil || result != ResultSuccess {
		t.Fatalf("resume = (%q, %v)", result, err)
	}
	last := resumed.calls[len(resumed.calls)-1]
	if !strings.Contains(last[2], "builtin:core/validation-none-v1.0.yaml") {
		t.Errorf("resumed command %q does not carry the original selection", last[2])
	}
}

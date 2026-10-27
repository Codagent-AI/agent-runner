package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/exec"
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
}

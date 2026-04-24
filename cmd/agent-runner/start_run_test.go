package main

import (
	"path/filepath"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/paramform"
)

func TestExecStartRun_ExecsSelfWithCanonicalNameAndOrderedParams(t *testing.T) {
	originalExecutable := currentExecutable
	originalExec := execProcess
	t.Cleanup(func() {
		currentExecutable = originalExecutable
		execProcess = originalExec
	})

	currentExecutable = func() (string, error) {
		return "/tmp/agent-runner", nil
	}

	var gotPath string
	var gotArgs []string
	execProcess = func(path string, args []string, env []string) error {
		gotPath = path
		gotArgs = append([]string(nil), args...)
		return nil
	}

	entry := discovery.WorkflowEntry{
		CanonicalName: "core:finalize-pr",
		Params: []model.Param{
			{Name: "task_file"},
			{Name: "branch"},
			{Name: "tag"},
		},
	}

	code := execStartRun(&entry, map[string]string{
		"branch":    "main",
		"tag":       "",
		"task_file": "tasks/param-form-run-launch.md",
	})
	if code != 0 {
		t.Fatalf("execStartRun() = %d, want 0", code)
	}

	if gotPath != "/tmp/agent-runner" {
		t.Fatalf("exec path = %q, want %q", gotPath, "/tmp/agent-runner")
	}

	wantArgs := []string{
		filepath.Base("/tmp/agent-runner"),
		"run",
		"core:finalize-pr",
		"task_file=tasks/param-form-run-launch.md",
		"branch=main",
		"tag=",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("exec args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestExecRunnerResume_EmptyRunIDOmitExtraArg(t *testing.T) {
	originalExecutable := currentExecutable
	originalExec := execProcess
	t.Cleanup(func() {
		currentExecutable = originalExecutable
		execProcess = originalExec
	})

	currentExecutable = func() (string, error) {
		return "/tmp/agent-runner", nil
	}

	var gotArgs []string
	execProcess = func(path string, args []string, env []string) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}

	code := execRunnerResume("", "")
	if code != 0 {
		t.Fatalf("execRunnerResume() = %d, want 0", code)
	}

	wantArgs := []string{filepath.Base("/tmp/agent-runner"), "--resume"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("exec args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestSwitcher_StartRunWithParams_ShowsParamFormAndCancelRestoresList(t *testing.T) {
	sw := &switcher{mode: showingList}
	entry := discovery.WorkflowEntry{
		CanonicalName: "deploy",
		Params: []model.Param{
			{Name: "task_file"},
		},
	}

	newModel, cmd := sw.Update(discovery.StartRunMsg{Entry: entry})
	if cmd != nil {
		t.Fatalf("StartRunMsg with params should not quit immediately, got %v", cmd)
	}

	sw = newModel.(*switcher)
	if sw.mode != showingParamForm {
		t.Fatalf("mode = %v, want showingParamForm", sw.mode)
	}

	newModel, cmd = sw.Update(paramform.CancelledMsg{})
	if cmd != nil {
		t.Fatalf("cancel should not quit, got %v", cmd)
	}

	sw = newModel.(*switcher)
	if sw.mode != showingList {
		t.Fatalf("mode after cancel = %v, want showingList", sw.mode)
	}
}

func TestSwitcher_SubmittedParamForm_QueuesRunLaunchAndQuits(t *testing.T) {
	sw := &switcher{
		mode: showingParamForm,
		startRunEntry: &discovery.WorkflowEntry{
			CanonicalName: "deploy",
			Params: []model.Param{
				{Name: "task_file"},
			},
		},
	}

	newModel, cmd := sw.Update(paramform.SubmittedMsg{
		"task_file": "tasks/param-form-run-launch.md",
	})
	if cmd == nil {
		t.Fatal("submitted param form should produce a quit cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}

	sw = newModel.(*switcher)
	if sw.startRunEntry == nil || sw.startRunEntry.CanonicalName != "deploy" {
		t.Fatalf("startRunEntry = %#v, want deploy", sw.startRunEntry)
	}
	if got := sw.startRunParams["task_file"]; got != "tasks/param-form-run-launch.md" {
		t.Fatalf("startRunParams[task_file] = %q, want %q", got, "tasks/param-form-run-launch.md")
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/intakeroute"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestLaunchFrozenIntakeRouteGatesOnSuccessfulCompletedRunAndAppendsEvidence(t *testing.T) {
	sessionDir := t.TempDir()
	handoff := filepath.Join(sessionDir, "sealed-handoff.md")
	if err := os.WriteFile(handoff, []byte("selected context"), 0o600); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(sessionDir, "intake-route.json")
	sealed := intakeroute.Sealed{
		State: intakeroute.Frozen, ParentRunID: "intake-parent", Workflow: "define-change",
		SourceRef:   "builtin:core/define-change-v1.0.yaml",
		Params:      map[string]string{"change_name": "intake", "change_dir": "specs/changes/intake", "change_label": "change"},
		HandoffPath: handoff, StagedAt: "2026-07-28T00:00:00Z", FrozenAt: "2026-07-28T00:01:00Z",
	}
	data, err := json.Marshal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecar, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "intake-parent", Completed: true}, sessionDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "audit.log"), []byte("2026-07-28T00:02:00Z run_end {\"outcome\":\"success\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalExecutable, originalExec := currentExecutable, execProcess
	t.Cleanup(func() {
		currentExecutable, execProcess = originalExecutable, originalExec
	})
	currentExecutable = func() (string, error) { return "/tmp/agent-runner", nil }
	var gotArgs []string
	execProcess = func(_ string, args []string, _ []string) error {
		gotArgs = append([]string(nil), args...)
		return errors.New("exec failed")
	}

	if code := launchFrozenIntakeRoute(runner.ResultSuccess, sessionDir); code != 1 {
		t.Fatalf("launchFrozenIntakeRoute() = %d, want failed exec code", code)
	}
	wantArgs := []string{"agent-runner", "internal", "launch-intake-route", sidecar}
	if strings.Join(gotArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("exec args = %#v, want %#v", gotArgs, wantArgs)
	}
	auditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(auditData), "run_end") || !strings.Contains(string(auditData), "route_launch_attempted") || !strings.Contains(string(auditData), "route_launch_failed") {
		t.Fatalf("audit evidence missing from:\n%s", auditData)
	}
	if bytes.Index(auditData, []byte("route_launch_attempted")) < bytes.Index(auditData, []byte("run_end")) {
		t.Fatalf("launch event was not appended after run_end:\n%s", auditData)
	}
}

func TestLaunchFrozenIntakeRouteDoesNotLaunchFailedOrIncompleteRuns(t *testing.T) {
	sessionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionDir, "intake-route.json"), []byte(`{"state":"frozen"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, result := range []runner.WorkflowResult{runner.ResultFailed, runner.ResultStopped} {
		if code := launchFrozenIntakeRoute(result, sessionDir); code != 0 {
			t.Fatalf("result %q launch code = %d, want 0", result, code)
		}
	}
	if err := stateio.WriteState(&model.RunState{Completed: false}, sessionDir); err != nil {
		t.Fatal(err)
	}
	if code := launchFrozenIntakeRoute(runner.ResultSuccess, sessionDir); code != 0 {
		t.Fatalf("incomplete run launch code = %d, want 0", code)
	}
}

func TestLaunchFrozenIntakeRouteFailsWhenCompletedStateCannotBeRead(t *testing.T) {
	sessionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionDir, "intake-route.json"), []byte(`{"state":"frozen"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sessionDir, "state.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if code := launchFrozenIntakeRoute(runner.ResultSuccess, sessionDir); code != 1 {
		t.Fatalf("launchFrozenIntakeRoute() = %d, want state-read failure", code)
	}
}

func TestLaunchResultAfterRunLaunchesFrozenRouteDespiteAutomaticExit(t *testing.T) {
	sessionDir := t.TempDir()
	handoff := filepath.Join(sessionDir, "sealed-handoff.md")
	if err := os.WriteFile(handoff, []byte("context"), 0o600); err != nil {
		t.Fatal(err)
	}
	sealed := intakeroute.Sealed{State: intakeroute.Frozen, ParentRunID: "parent", Workflow: "target", SourceRef: "builtin:core/target-v1.0.yaml", Params: map[string]string{}, HandoffPath: handoff, StagedAt: "now", FrozenAt: "now"}
	data, err := json.Marshal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "intake-route.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{Completed: true}, sessionDir); err != nil {
		t.Fatal(err)
	}
	originalExecutable, originalExec := currentExecutable, execProcess
	t.Cleanup(func() { currentExecutable, execProcess = originalExecutable, originalExec })
	currentExecutable = func() (string, error) { return "/tmp/agent-runner", nil }
	execProcess = func(_ string, _ []string, _ []string) error { return errors.New("exec failed") }

	result := launchResultAfterRun(liveTUIResult{exitRequested: true, workflowResult: runner.ResultSuccess, sessionDir: sessionDir}, false)
	if result.exitCode != 1 {
		t.Fatalf("launchResultAfterRun() = %#v, want failed launch result", result)
	}
}

func TestShouldExitAfterFrozenIntakeRouteForCompletedSuccess(t *testing.T) {
	sessionDir := t.TempDir()
	if err := stateio.WriteState(&model.RunState{Completed: true}, sessionDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "intake-route.json"), []byte(`{"state":"frozen","parent_run_id":"parent","workflow":"target","source_ref":"builtin:core/target-v1.0.yaml","params":{},"handoff_path":"/tmp/handoff","staged_at":"now"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !shouldExitAfterFrozenIntakeRoute(runner.ResultSuccess, sessionDir) {
		t.Fatal("completed frozen intake route did not request live TUI exit")
	}
}

func TestExplorationHandoffPathReturnsNonEmptyIntakeHandoff(t *testing.T) {
	sessionDir := t.TempDir()
	handoff := filepath.Join(sessionDir, "intake-handoff.md")
	if err := os.WriteFile(handoff, []byte("exploration notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{WorkflowFile: "builtin:core/intake-v1.0.yaml", Completed: true}, sessionDir); err != nil {
		t.Fatal(err)
	}
	if got := explorationHandoffPath(sessionDir); got != handoff {
		t.Fatalf("explorationHandoffPath() = %q, want %q", got, handoff)
	}
}

func TestPrepareFreshRunPrevalidationFailureLeavesNoRunDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)
	workflowPath := filepath.Join(project, "invalid-v1.0.yaml")
	if err := os.WriteFile(workflowPath, []byte("name: invalid\nsteps:\n  - id: bad\n    command: 'echo {{missing}}'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := prepareFreshRun(&freshRunRequest{SourceRef: workflowPath})
	if err == nil || !strings.Contains(err.Error(), "undefined variable") {
		t.Fatalf("prepareFreshRun() error = %v, want strict prevalidation failure", err)
	}
	runsDir := filepath.Join(home, ".agent-runner", "projects")
	if err := filepath.WalkDir(runsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && filepath.Base(filepath.Dir(path)) == "runs" {
			t.Fatalf("prevalidation left a run directory: %s", path)
		}
		return nil
	}); err != nil && !os.IsNotExist(err) {
		t.Fatalf("inspect prepared runs: %v", err)
	}
}

func TestInternalLaunchIntakeRoutePreparesSealedSourceWithCopiedHandoff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AGENT_RUNNER_NO_TUI", "1")
	project := t.TempDir()
	t.Chdir(project)
	target := filepath.Join(project, "target-v1.0.yaml")
	if err := os.WriteFile(target, []byte("name: target\nsteps:\n  - id: done\n    command: echo launched\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	handoff := filepath.Join(parent, "sealed-handoff.md")
	if err := os.WriteFile(handoff, []byte("intake conclusions"), 0o600); err != nil {
		t.Fatal(err)
	}
	sealed := intakeroute.Sealed{State: intakeroute.Frozen, ParentRunID: "intake-parent", Workflow: "target", SourceRef: target, Params: map[string]string{}, HandoffPath: handoff, StagedAt: "2026-07-28T00:00:00Z", FrozenAt: "2026-07-28T00:01:00Z"}
	data, err := json.Marshal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(parent, "intake-route.json")
	if err := os.WriteFile(sidecar, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	if code := handleInternalWithIO([]string{"launch-intake-route", sidecar}, strings.NewReader(""), &stderr); code != 0 {
		t.Fatalf("internal launch = %d, stderr: %s", code, stderr.String())
	}
	runsDir := filepath.Join(home, ".agent-runner", "projects")
	var childState *model.RunState
	if err := filepath.WalkDir(runsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "state.json" {
			return err
		}
		state, err := stateio.ReadState(path)
		if err != nil {
			return err
		}
		childState = &state
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if childState == nil || childState.IntakeParentRunID != "intake-parent" || childState.IntakeHandoff == "" {
		t.Fatalf("child provenance = %#v", childState)
	}
	contents, err := os.ReadFile(childState.IntakeHandoff)
	if err != nil || string(contents) != "intake conclusions" {
		t.Fatalf("copied handoff = %q, %v", contents, err)
	}
}

func TestInternalLaunchIntakeRouteRejectsMissingParentProvenance(t *testing.T) {
	parent := t.TempDir()
	handoff := filepath.Join(parent, "sealed-handoff.md")
	if err := os.WriteFile(handoff, []byte("context"), 0o600); err != nil {
		t.Fatal(err)
	}
	sealed := intakeroute.Sealed{State: intakeroute.Frozen, Workflow: "target", SourceRef: "builtin:core/target-v1.0.yaml", Params: map[string]string{}, HandoffPath: handoff, StagedAt: "now", FrozenAt: "now"}
	data, err := json.Marshal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(parent, "intake-route.json")
	if err := os.WriteFile(sidecar, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := handleInternalWithIO([]string{"launch-intake-route", sidecar}, strings.NewReader(""), &stderr); code != 1 {
		t.Fatalf("internal launch = %d, want invariant rejection", code)
	}
	if !strings.Contains(stderr.String(), "parent run ID") {
		t.Fatalf("stderr = %q, want parent provenance error", stderr.String())
	}
}

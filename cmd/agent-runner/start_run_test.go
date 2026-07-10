package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/model"
	nativesetup "github.com/codagent/agent-runner/internal/onboarding/native"
	"github.com/codagent/agent-runner/internal/paramform"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/runview"
	"github.com/codagent/agent-runner/internal/stateio"
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
	var gotEnv []string
	execProcess = func(path string, args []string, env []string) error {
		gotPath = path
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
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
		"core:finalize-pr",
		"task_file=tasks/param-form-run-launch.md",
		"branch=main",
		"tag=",
	}
	if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
		t.Fatalf("exec args mismatch (-want +got):\n%s", diff)
	}
	if !envContains(gotEnv, liveRunImmediateAltScreenEnv+"=1") {
		t.Fatalf("exec env missing %s=1", liveRunImmediateAltScreenEnv)
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
	if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
		t.Fatalf("exec args mismatch (-want +got):\n%s", diff)
	}
}

func TestExecRunnerResume_RunIDRequestsImmediateAltScreen(t *testing.T) {
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
	var gotEnv []string
	execProcess = func(path string, args []string, env []string) error {
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return nil
	}

	code := execRunnerResume("run-123", "")
	if code != 0 {
		t.Fatalf("execRunnerResume() = %d, want 0", code)
	}

	wantArgs := []string{filepath.Base("/tmp/agent-runner"), "--resume", "run-123"}
	if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
		t.Fatalf("exec args mismatch (-want +got):\n%s", diff)
	}
	if !envContains(gotEnv, liveRunImmediateAltScreenEnv+"=1") {
		t.Fatalf("exec env missing %s=1", liveRunImmediateAltScreenEnv)
	}
}

func TestLiveTUIOptionsReadsImmediateAltScreenEnv(t *testing.T) {
	t.Setenv(liveRunImmediateAltScreenEnv, "1")

	opts := liveTUIOptions{}.withEnv()
	if !opts.startInAltScreen {
		t.Fatalf("startInAltScreen = false, want true when %s=1", liveRunImmediateAltScreenEnv)
	}
}

func TestCompletedLiveTUIResultIgnoresResumeTargetMarker(t *testing.T) {
	originalExecutable := currentExecutable
	originalExec := execProcess
	t.Cleanup(func() {
		currentExecutable = originalExecutable
		execProcess = originalExec
	})
	currentExecutable = func() (string, error) { return "/tmp/agent-runner", nil }
	var execCalls int
	execProcess = func(path string, args []string, env []string) error {
		execCalls++
		return nil
	}

	sessionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionDir, "resume-target"), []byte("target-run\n"), 0o600); err != nil {
		t.Fatalf("write stale marker: %v", err)
	}
	resultCh := make(chan runner.WorkflowResult, 1)
	resultCh <- runner.ResultSuccess

	result := completedLiveTUIResult(resultCh, sessionDir)
	if result.exitCode != 0 || result.workflowResult != runner.ResultSuccess || result.sessionDir != sessionDir {
		t.Fatalf("completedLiveTUIResult = %#v, want ordinary success", result)
	}
	if execCalls != 0 {
		t.Fatalf("exec calls = %d, want 0", execCalls)
	}
}

func TestOnboardingDemoPromptFlowNotNowDoesNotPrepareRun(t *testing.T) {
	m := &onboardingDemoPromptFlow{
		prompt: nativesetup.NewDemoPromptModel(&nativesetup.Deps{}),
		ref:    "builtin:onboarding/onboarding.yaml",
		opts:   liveTUIOptions{quitOnDone: true, startInAltScreen: true},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(*onboardingDemoPromptFlow)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*onboardingDemoPromptFlow)

	if cmd == nil {
		t.Fatal("Not now should quit the prompt flow")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
	if m.handle != nil {
		t.Fatal("Not now should not prepare an onboarding run")
	}
	if m.mode != onboardingDemoPromptMode {
		t.Fatalf("mode = %v, want prompt mode", m.mode)
	}
}

func TestOnboardingDemoPromptFlowWindowSizeDoesNotCompletePrompt(t *testing.T) {
	m := &onboardingDemoPromptFlow{
		prompt: nativesetup.NewDemoPromptModel(&nativesetup.Deps{}),
		ref:    "builtin:onboarding/onboarding.yaml",
		opts:   liveTUIOptions{quitOnDone: true, startInAltScreen: true},
	}

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(*onboardingDemoPromptFlow)

	if cmd != nil {
		t.Fatalf("window size should not quit or start workflow, got cmd %T", cmd())
	}
	if m.prompt.Done() {
		t.Fatal("prompt should remain pending after window size")
	}
	if m.mode != onboardingDemoPromptMode {
		t.Fatalf("mode = %v, want prompt mode", m.mode)
	}
	if m.termWidth != 100 || m.termHeight != 40 {
		t.Fatalf("stored size = %dx%d, want 100x40", m.termWidth, m.termHeight)
	}
}

func TestOnboardingDemoPromptFlowContinueShowsPreparingState(t *testing.T) {
	m := &onboardingDemoPromptFlow{
		prompt: nativesetup.NewDemoPromptModel(&nativesetup.Deps{}),
		ref:    "builtin:onboarding/onboarding.yaml",
		opts:   liveTUIOptions{quitOnDone: true, startInAltScreen: true},
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(*onboardingDemoPromptFlow)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*onboardingDemoPromptFlow)

	if cmd == nil {
		t.Fatal("Continue should start preparing the onboarding demo")
	}
	if !m.preparingRun {
		t.Fatal("Continue should enter preparing state while demo run is prepared")
	}
	view := m.View()
	if !strings.Contains(view, "Preparing Onboarding Demo") {
		t.Fatalf("view missing preparing title:\n%s", view)
	}
	if !strings.Contains(view, "This can take a moment") {
		t.Fatalf("view missing wait guidance:\n%s", view)
	}
}

func TestOnboardingDemoLaunchFlowStartsInPreparingState(t *testing.T) {
	m := &onboardingDemoPromptFlow{
		ref:          "builtin:onboarding/onboarding.yaml",
		opts:         liveTUIOptions{quitOnDone: true, startInAltScreen: true},
		preparingRun: true,
	}

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("launch flow should start preparation commands immediately")
	}
	view := m.View()
	if !strings.Contains(view, "Preparing Onboarding Demo") {
		t.Fatalf("view missing preparing title:\n%s", view)
	}
}

func TestOnboardingDemoLaunchFlowViewHandlesFailedPreparation(t *testing.T) {
	m := &onboardingDemoPromptFlow{
		ref:          "builtin:onboarding/onboarding.yaml",
		opts:         liveTUIOptions{quitOnDone: true, startInAltScreen: true},
		preparingRun: true,
	}

	updated, cmd := m.Update(onboardingDemoPrepareMsg{exitCode: 1})
	m = updated.(*onboardingDemoPromptFlow)

	if cmd == nil {
		t.Fatal("failed preparation should quit")
	}
	if m.exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", m.exitCode)
	}
	if got := m.View(); got != "" {
		t.Fatalf("failed preparation without prompt should render empty view, got %q", got)
	}
}

func TestOnboardingDemoPromptFlowConfirmedLiveRunQuitExitsApp(t *testing.T) {
	sessionDir := writeRunState(t, t.TempDir(), "onboarding-run", "builtin:onboarding/onboarding.yaml", false)
	rv, err := runview.New(sessionDir, "", runview.FromLiveRun)
	if err != nil {
		t.Fatalf("new runview: %v", err)
	}
	m := &onboardingDemoPromptFlow{
		run:  rv,
		mode: onboardingDemoRunMode,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(*onboardingDemoPromptFlow)
	if cmd != nil {
		t.Fatalf("q should only open quit confirmation, got cmd %T", cmd())
	}

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(*onboardingDemoPromptFlow)
	if cmd == nil {
		t.Fatal("confirming quit should produce a quit cmd")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
	if !m.exitRequested {
		t.Fatal("confirmed live-run quit should mark the wrapper as exit-requested")
	}
}

func TestFindLatestIncompleteOnboardingRunState(t *testing.T) {
	originalHome := userHomeDir
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })
	t.Setenv("HOME", home)

	startCwd := filepath.Join(t.TempDir(), "start")
	resumeCwd := filepath.Join(t.TempDir(), "resume")
	for _, dir := range []string{startCwd, resumeCwd} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	t.Chdir(resumeCwd)

	ref := "builtin:onboarding/onboarding.yaml"
	runsDir := filepath.Join(home, ".agent-runner", "onboarding", "runs")
	writeRunState(t, runsDir, "onboarding-onboarding-2026-05-10T10-00-00Z", ref, false)
	wantDir := writeRunState(t, runsDir, "onboarding-onboarding-2026-05-10T11-00-00Z", ref, false)
	writeRunState(t, runsDir, "onboarding-onboarding-2026-05-10T12-00-00Z", ref, true)
	writeRunState(t, runsDir, "other-2026-05-10T13-00-00Z", "builtin:core/other.yaml", false)

	got, ok, err := findLatestIncompleteOnboardingRunState(ref)
	if err != nil {
		t.Fatalf("findLatestIncompleteOnboardingRunState: %v", err)
	}
	if !ok {
		t.Fatal("findLatestIncompleteOnboardingRunState ok = false, want true")
	}
	want := filepath.Join(wantDir, "state.json")
	if got != want {
		t.Fatalf("state path = %q, want %q", got, want)
	}
	if filepath.Dir(filepath.Dir(got)) != runsDir {
		t.Fatalf("state path = %q, want under global onboarding runs dir %q", got, runsDir)
	}
}

func TestFindLatestIncompleteOnboardingRunStateRepairsEmptyCurrentStepToFirstStep(t *testing.T) {
	originalHome := userHomeDir
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })
	t.Setenv("HOME", home)

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	t.Chdir(repo)

	ref := "builtin:onboarding/onboarding.yaml"
	runsDir := filepath.Join(home, ".agent-runner", "onboarding", "runs")
	writeRunState(t, runsDir, "onboarding-onboarding-2026-05-10T10-00-00Z", ref, false)
	emptyDir := filepath.Join(runsDir, "onboarding-onboarding-2026-05-10T11-00-00Z")
	if err := stateio.WriteState(&model.RunState{
		WorkflowFile: ref,
		WorkflowName: "onboarding-onboarding",
		WorkflowHash: "test-hash",
	}, emptyDir); err != nil {
		t.Fatalf("write empty current step state: %v", err)
	}

	got, ok, err := findLatestIncompleteOnboardingRunState(ref)
	if err != nil {
		t.Fatalf("findLatestIncompleteOnboardingRunState: %v", err)
	}
	if !ok {
		t.Fatal("findLatestIncompleteOnboardingRunState ok = false, want repaired candidate")
	}
	want := filepath.Join(emptyDir, "state.json")
	if got != want {
		t.Fatalf("state path = %q, want %q", got, want)
	}
	state, err := stateio.ReadState(got)
	if err != nil {
		t.Fatalf("read repaired state: %v", err)
	}
	if state.CurrentStep.Nested == nil || state.CurrentStep.Nested.StepID != "step-types-demo" {
		t.Fatalf("repaired CurrentStep = %#v, want step-types-demo", state.CurrentStep)
	}
}

func TestFindLatestIncompleteOnboardingRunStateRepairsEmptyCurrentStepFromAudit(t *testing.T) {
	originalHome := userHomeDir
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })
	t.Setenv("HOME", home)

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	t.Chdir(repo)

	ref := "builtin:onboarding/onboarding.yaml"
	runsDir := filepath.Join(home, ".agent-runner", "onboarding", "runs")
	writeRunState(t, runsDir, "onboarding-onboarding-2026-05-10T10-00-00Z", ref, false)
	emptyDir := filepath.Join(runsDir, "onboarding-onboarding-2026-05-10T11-00-00Z")
	if err := stateio.WriteState(&model.RunState{
		WorkflowFile: ref,
		WorkflowName: "onboarding-onboarding",
		WorkflowHash: "test-hash",
	}, emptyDir); err != nil {
		t.Fatalf("write empty current step state: %v", err)
	}
	auditLine := `2026-05-10T11:00:00Z run_start {"resumed":true,"resume_from":"validator"}` + "\n"
	if err := os.WriteFile(filepath.Join(emptyDir, "audit.log"), []byte(auditLine), 0o600); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	got, ok, err := findLatestIncompleteOnboardingRunState(ref)
	if err != nil {
		t.Fatalf("findLatestIncompleteOnboardingRunState: %v", err)
	}
	if !ok {
		t.Fatal("findLatestIncompleteOnboardingRunState ok = false, want repaired candidate")
	}
	want := filepath.Join(emptyDir, "state.json")
	if got != want {
		t.Fatalf("state path = %q, want %q", got, want)
	}
	state, err := stateio.ReadState(got)
	if err != nil {
		t.Fatalf("read repaired state: %v", err)
	}
	if state.CurrentStep.Nested == nil || state.CurrentStep.Nested.StepID != "validator" {
		t.Fatalf("repaired CurrentStep = %#v, want validator", state.CurrentStep)
	}
}

func TestFindLatestIncompleteOnboardingRunStateRewindsGuidedDirectoryConfirmationAfterCwdChange(t *testing.T) {
	originalHome := userHomeDir
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })
	t.Setenv("HOME", home)

	startCwd := filepath.Join(t.TempDir(), "start")
	resumeCwd := filepath.Join(t.TempDir(), "resume")
	for _, dir := range []string{startCwd, resumeCwd} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	t.Chdir(resumeCwd)

	ref := "builtin:onboarding/onboarding.yaml"
	runsDir := filepath.Join(home, ".agent-runner", "onboarding", "runs")
	sessionDir := filepath.Join(runsDir, "onboarding-onboarding-2026-05-10T11-00-00Z")
	if err := stateio.WriteState(&model.RunState{
		WorkflowFile: ref,
		WorkflowName: "onboarding-onboarding",
		WorkflowHash: "test-hash",
		CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{
			StepID: "guided-workflow",
			Child: &model.NestedStepState{
				StepID:    "confirm-cwd",
				Completed: false,
				CapturedVariables: map[string]model.CapturedValue{
					"cwd":            model.NewCapturedString(startCwd),
					"project_status": model.NewCapturedString("ok"),
				},
			},
		}},
	}, sessionDir); err != nil {
		t.Fatalf("write state: %v", err)
	}

	got, ok, err := findLatestIncompleteOnboardingRunState(ref)
	if err != nil {
		t.Fatalf("findLatestIncompleteOnboardingRunState: %v", err)
	}
	if !ok {
		t.Fatal("findLatestIncompleteOnboardingRunState ok = false, want repaired candidate")
	}
	if got != filepath.Join(sessionDir, "state.json") {
		t.Fatalf("state path = %q, want %q", got, filepath.Join(sessionDir, "state.json"))
	}

	state, err := stateio.ReadState(got)
	if err != nil {
		t.Fatalf("read repaired state: %v", err)
	}
	child := state.CurrentStep.Nested.Child
	if child == nil || child.StepID != "capture-cwd" {
		t.Fatalf("guided child = %#v, want capture-cwd", child)
	}
	if child.Completed {
		t.Fatal("capture-cwd should be incomplete so resume reruns pwd in the new directory")
	}
	if _, ok := child.CapturedVariables["cwd"]; ok {
		t.Fatalf("child captured cwd = %#v, want cleared before rerun", child.CapturedVariables["cwd"])
	}
}

func TestFindLatestIncompleteOnboardingRunStateReturnsRepairWriteError(t *testing.T) {
	originalHome := userHomeDir
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })
	t.Setenv("HOME", home)

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	t.Chdir(repo)

	ref := "builtin:onboarding/onboarding.yaml"
	runsDir := filepath.Join(home, ".agent-runner", "onboarding", "runs")
	sessionDir := filepath.Join(runsDir, "onboarding-onboarding-2026-05-10T11-00-00Z")
	if err := stateio.WriteState(&model.RunState{
		WorkflowFile: ref,
		WorkflowName: "onboarding-onboarding",
		WorkflowHash: "test-hash",
	}, sessionDir); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.Chmod(sessionDir, 0o500); err != nil {
		t.Fatalf("chmod session dir read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(sessionDir, 0o700)
	})

	_, ok, err := findLatestIncompleteOnboardingRunState(ref)
	if err == nil {
		t.Fatal("findLatestIncompleteOnboardingRunState error = nil, want repair write error")
	}
	if ok {
		t.Fatal("findLatestIncompleteOnboardingRunState ok = true, want false on repair write error")
	}
	if !strings.Contains(err.Error(), "persist repaired onboarding state") {
		t.Fatalf("error = %v, want persist repaired onboarding state", err)
	}
}

func TestSameCleanPathTreatsSymlinkAliasAsSameDirectory(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o750); err != nil {
		t.Fatalf("mkdir real dir: %v", err)
	}
	linkDir := filepath.Join(root, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if !sameCleanPath(linkDir, realDir) {
		t.Fatalf("sameCleanPath(%q, %q) = false, want true", linkDir, realDir)
	}
}

func TestFindLatestIncompleteOnboardingRunStateMissingRunsDir(t *testing.T) {
	originalHome := userHomeDir
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	t.Chdir(repo)

	got, ok, err := findLatestIncompleteOnboardingRunState("builtin:onboarding/onboarding.yaml")
	if err != nil {
		t.Fatalf("findLatestIncompleteOnboardingRunState: %v", err)
	}
	if ok || got != "" {
		t.Fatalf("result = %q, %v; want no candidate", got, ok)
	}
}

func TestPrepareBuiltinOnboardingRunStartsFromRequestedTopLevelStep(t *testing.T) {
	originalHome := userHomeDir
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })
	t.Setenv("HOME", home)

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	t.Chdir(repo)

	ref := "builtin:onboarding/onboarding.yaml"
	runsDir := filepath.Join(home, ".agent-runner", "onboarding", "runs")
	writeRunState(t, runsDir, "onboarding-onboarding-2026-05-10T10-00-00Z", ref, false)

	handle, exitCode := prepareBuiltinOnboardingRun(ref, "validator")
	if exitCode != 0 {
		t.Fatalf("prepareBuiltinOnboardingRun exit = %d, want 0", exitCode)
	}
	t.Cleanup(func() { runlock.Delete(handle.SessionDir) })

	if strings.Contains(handle.SessionDir, "2026-05-10T10-00-00Z") {
		t.Fatalf("session dir = %q, reused incomplete run despite explicit start step", handle.SessionDir)
	}

	event := readFirstAuditLine(t, filepath.Join(handle.SessionDir, "audit.log"))
	for _, want := range []string{` run_start `, `"resumed":true`, `"resume_from":"validator"`} {
		if !strings.Contains(event, want) {
			t.Fatalf("run_start event = %q, want to contain %q", event, want)
		}
	}
}

func TestNewOnboardingSessionDirUsesGlobalOnboardingScope(t *testing.T) {
	originalHome := userHomeDir
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o750); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	t.Chdir(repo)

	got, err := newOnboardingSessionDir("onboarding-onboarding")
	if err != nil {
		t.Fatalf("newOnboardingSessionDir: %v", err)
	}
	wantPrefix := filepath.Join(home, ".agent-runner", "onboarding", "runs", "onboarding-onboarding-")
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("session dir = %q, want prefix %q", got, wantPrefix)
	}
}

func readFirstAuditLine(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	line, _, ok := strings.Cut(string(data), "\n")
	if !ok {
		t.Fatalf("audit log has no newline-delimited event: %q", data)
	}
	return line
}

func envContains(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

func writeRunState(t *testing.T, runsDir, sessionID, workflowFile string, completed bool) string {
	t.Helper()
	sessionDir := filepath.Join(runsDir, sessionID)
	state := &model.RunState{
		WorkflowFile: workflowFile,
		WorkflowName: "onboarding-onboarding",
		CurrentStep:  model.CurrentStep{StepID: "step-types-demo"},
		WorkflowHash: "test-hash",
		Completed:    completed,
	}
	if err := stateio.WriteState(state, sessionDir); err != nil {
		t.Fatalf("write state %s: %v", sessionID, err)
	}
	return sessionDir
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
	if sw.startRunEntry != nil {
		t.Fatalf("startRunEntry = %#v, want nil after cancel", sw.startRunEntry)
	}
	if sw.startRunParams != nil {
		t.Fatalf("startRunParams = %#v, want nil after cancel", sw.startRunParams)
	}
	if sw.startRunReady {
		t.Fatal("startRunReady should be false after cancel")
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
	if !sw.startRunReady {
		t.Fatal("startRunReady should be true after submit")
	}
}

func TestSwitcher_LaunchDebugMsg_QueuesDirectDebugExec(t *testing.T) {
	sw := &switcher{mode: showingRunView}

	newModel, cmd := sw.Update(runview.LaunchDebugMsg{
		FailedRunID:      "run-123",
		FailedSessionDir: "/state/runs/run-123",
		FailedProjectDir: "/workspace/project",
	})
	if cmd == nil {
		t.Fatal("LaunchDebugMsg should quit so the run can be exec-replaced")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}

	sw = newModel.(*switcher)
	if sw.launchDebugRunID != "run-123" {
		t.Fatalf("launchDebugRunID = %q, want run-123", sw.launchDebugRunID)
	}
	if sw.launchDebugSessionDir != "/state/runs/run-123" {
		t.Fatalf("launchDebugSessionDir = %q, want /state/runs/run-123", sw.launchDebugSessionDir)
	}
	if sw.launchDebugProjectDir != "/workspace/project" {
		t.Fatalf("launchDebugProjectDir = %q, want /workspace/project", sw.launchDebugProjectDir)
	}
	if sw.startRunEntry != nil {
		t.Fatalf("startRunEntry = %#v, want nil for direct debug exec", sw.startRunEntry)
	}
	if sw.startRunParams != nil {
		t.Fatalf("startRunParams = %#v, want nil for direct debug exec", sw.startRunParams)
	}
	if sw.startRunReady {
		t.Fatal("startRunReady should be false for direct debug exec")
	}
}

func TestLaunchDebugArgs_UsesRunSubcommandAndParamFlag(t *testing.T) {
	want := []string{"run", "core:debug", "--param", "failed_run_id=run-123"}
	if diff := cmp.Diff(want, launchDebugArgs("run-123", "")); diff != "" {
		t.Fatalf("debug launch args mismatch (-want +got):\n%s", diff)
	}
}

func TestLaunchDebugArgs_PrefersSessionDirParam(t *testing.T) {
	want := []string{"run", "core:debug", "--param", "failed_session_dir=/state/runs/run-123"}
	if diff := cmp.Diff(want, launchDebugArgs("run-123", "/state/runs/run-123")); diff != "" {
		t.Fatalf("debug launch args mismatch (-want +got):\n%s", diff)
	}
}

func TestExecRunnerDebug_ChdirsToFailedRunProjectBeforeExec(t *testing.T) {
	originalExecutable := currentExecutable
	originalExec := execProcess
	originalDebug := execRunnerDebug
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		currentExecutable = originalExecutable
		execProcess = originalExec
		execRunnerDebug = originalDebug
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	projectDir := t.TempDir()
	currentExecutable = func() (string, error) {
		return "/tmp/agent-runner", nil
	}

	var gotWD string
	var gotArgs []string
	var gotEnv []string
	execProcess = func(path string, args []string, env []string) error {
		wd, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd during exec: %v", err)
		}
		gotWD = wd
		gotArgs = append([]string(nil), args...)
		gotEnv = append([]string(nil), env...)
		return nil
	}

	sessionDir := "/state/runs/run-123"
	code := execRunnerDebug("run-123", sessionDir, projectDir)
	if code != 0 {
		t.Fatalf("execRunnerDebug() = %d, want 0", code)
	}
	wantWD, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		t.Fatalf("eval project dir: %v", err)
	}
	gotResolvedWD, err := filepath.EvalSymlinks(gotWD)
	if err != nil {
		t.Fatalf("eval exec cwd: %v", err)
	}
	if gotResolvedWD != wantWD {
		t.Fatalf("exec cwd = %q, want %q", gotResolvedWD, wantWD)
	}
	wantArgs := []string{filepath.Base("/tmp/agent-runner"), "run", "core:debug", "--param", "failed_session_dir=" + sessionDir}
	if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
		t.Fatalf("exec args mismatch (-want +got):\n%s", diff)
	}
	if !envContains(gotEnv, liveRunImmediateAltScreenEnv+"=1") {
		t.Fatalf("exec env missing %s=1", liveRunImmediateAltScreenEnv)
	}
}

func TestTerminalLiveTUIResult_LaunchDebugExecsSelectedRun(t *testing.T) {
	originalDebug := execRunnerDebug
	t.Cleanup(func() { execRunnerDebug = originalDebug })

	var gotRunID string
	var gotProjectDir string
	var gotSessionDir string
	execRunnerDebug = func(runID, sessionDir, projectDir string) int {
		gotRunID = runID
		gotSessionDir = sessionDir
		gotProjectDir = projectDir
		return 7
	}

	rv, err := runview.NewForDefinition(&discovery.WorkflowEntry{CanonicalName: "wf"}, "/current/project")
	if err != nil {
		t.Fatalf("NewForDefinition: %v", err)
	}
	next, cmd := rv.Update(runview.LaunchDebugMsg{
		FailedRunID:      "run-123",
		FailedSessionDir: "/state/runs/run-123",
		FailedProjectDir: "/failed/project",
	})
	if cmd == nil {
		t.Fatal("LaunchDebugMsg should quit the top-level run view")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
	rv = next.(*runview.Model)

	resultCh := make(chan runner.WorkflowResult, 1)
	resultCh <- runner.ResultFailed

	result, ok := terminalLiveTUIResult(rv, resultCh, "/current/project", "/runs/run-123", liveTUIOptions{})
	if !ok {
		t.Fatal("terminalLiveTUIResult did not handle debug launch")
	}
	if result.exitCode != 7 || result.sessionDir != "/runs/run-123" {
		t.Fatalf("terminalLiveTUIResult = %#v, want exitCode 7 and session dir", result)
	}
	if gotRunID != "run-123" || gotSessionDir != "/state/runs/run-123" || gotProjectDir != "/failed/project" {
		t.Fatalf("debug target = (%q, %q, %q), want (run-123, /state/runs/run-123, /failed/project)", gotRunID, gotSessionDir, gotProjectDir)
	}
}

func TestNormalizeRunCommandArgs_SupportsRunSubcommandParamFlag(t *testing.T) {
	got, err := normalizeRunCommandArgs([]string{"run", "core:debug", "--param", "failed_run_id=run-123"})
	if err != nil {
		t.Fatalf("normalizeRunCommandArgs returned error: %v", err)
	}

	want := []string{"core:debug", "failed_run_id=run-123"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("normalized args mismatch (-want +got):\n%s", diff)
	}
}

func TestParseRunCommandArgsSupportsUntilFlag(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "separate value", args: []string{"run", "core:debug", "--until", "summarize", "failed_run_id=run-123"}},
		{name: "equals value", args: []string{"run", "core:debug", "--until=summarize", "failed_run_id=run-123"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gotArgs, gotOpts, err := parseRunCommandArgs(tt.args)
			if err != nil {
				t.Fatalf("parseRunCommandArgs returned error: %v", err)
			}

			wantArgs := []string{"core:debug", "failed_run_id=run-123"}
			if diff := cmp.Diff(wantArgs, gotArgs); diff != "" {
				t.Fatalf("normalized args mismatch (-want +got):\n%s", diff)
			}
			if gotOpts.until != "summarize" {
				t.Fatalf("until = %q, want %q", gotOpts.until, "summarize")
			}
		})
	}
}

func TestNormalizeRunCommandArgs_PreservesSingleRunWorkflowName(t *testing.T) {
	got, err := normalizeRunCommandArgs([]string{"run"})
	if err != nil {
		t.Fatalf("normalizeRunCommandArgs returned error: %v", err)
	}

	want := []string{"run"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("normalized args mismatch (-want +got):\n%s", diff)
	}
}

func TestIsRunCommandHelp_DetectsHelpFlags(t *testing.T) {
	for _, args := range [][]string{
		{"run", "--help"},
		{"run", "-h"},
	} {
		if !isRunCommandHelp(args) {
			t.Fatalf("isRunCommandHelp(%v) = false, want true", args)
		}
	}
}

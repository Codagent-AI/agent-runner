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

func TestFindLatestIncompleteOnboardingRunState(t *testing.T) {
	originalHome := userHomeDir
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })

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

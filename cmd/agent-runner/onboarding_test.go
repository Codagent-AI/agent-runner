package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/codagent/agent-runner/internal/usersettings"
	"github.com/google/go-cmp/cmp"
)

func TestEnsureOnboardingForTUIFiresOnlyForFreshTTY(t *testing.T) {
	var launched []string
	code := ensureOnboardingForTUI(onboardingDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{}, nil
		},
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		resumeRun: func(string) int {
			t.Fatal("resumeRun should not be called")
			return 0
		},
		runWorkflow: func(ref string) int {
			launched = append(launched, ref)
			return 7
		},
	})

	if code != 7 {
		t.Fatalf("ensureOnboardingForTUI() = %d, want workflow exit code 7", code)
	}
	if diff := cmp.Diff([]string{"builtin:onboarding/welcome.yaml"}, launched); diff != "" {
		t.Fatalf("launched mismatch (-want +got):\n%s", diff)
	}
}

func TestEnsureOnboardingForTUIResumesMostRecentIncompleteRun(t *testing.T) {
	var resumed []string
	code := ensureOnboardingForTUI(onboardingDeps{
		load:        func() (usersettings.Settings, error) { return usersettings.Settings{}, nil },
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		incompleteOnboardingRun: func() (string, error) {
			return "/tmp/onboarding/state.json", nil
		},
		resumeRun: func(statePath string) int {
			resumed = append(resumed, statePath)
			return 9
		},
		runWorkflow: func(string) int {
			t.Fatal("runWorkflow should not be called when an incomplete run exists")
			return 0
		},
	})

	if code != 9 {
		t.Fatalf("ensureOnboardingForTUI() = %d, want resume exit code 9", code)
	}
	if diff := cmp.Diff([]string{"/tmp/onboarding/state.json"}, resumed); diff != "" {
		t.Fatalf("resumed mismatch (-want +got):\n%s", diff)
	}
}

func TestFindIncompleteOnboardingRunScansAllProjects(t *testing.T) {
	home := t.TempDir()
	originalHome := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })

	currentProject := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(currentProject); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	oldState := writeOnboardingState(t, home, "current-project", "onboarding-welcome-2026-05-02T12-00-00Z", false)
	wantState := writeOnboardingState(t, home, "other-project", "onboarding-welcome-2026-05-03T12-00-00Z", false)
	_ = writeOnboardingState(t, home, "newer-completed-project", "onboarding-welcome-2026-05-04T12-00-00Z", true)
	badRunsPath := filepath.Join(home, ".agent-runner", "projects", "aaa-bad-project", "runs")
	if err := os.MkdirAll(filepath.Dir(badRunsPath), 0o755); err != nil {
		t.Fatalf("create bad project dir: %v", err)
	}
	if err := os.WriteFile(badRunsPath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write bad runs path: %v", err)
	}

	oldTime := time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldState, oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes old state: %v", err)
	}
	if err := os.Chtimes(wantState, newTime, newTime); err != nil {
		t.Fatalf("Chtimes want state: %v", err)
	}

	got, err := findIncompleteOnboardingRun()
	if err != nil {
		t.Fatalf("findIncompleteOnboardingRun() returned error: %v", err)
	}
	if got != wantState {
		t.Fatalf("findIncompleteOnboardingRun() = %q, want %q", got, wantState)
	}
}

func TestEnsureOnboardingForTUISkipsWhenCompletedDismissedOrNonTTY(t *testing.T) {
	tests := []struct {
		name       string
		settings   usersettings.Settings
		stdinTTY   bool
		stdoutTTY  bool
		wantLaunch bool
	}{
		{name: "completed", settings: usersettings.Settings{Onboarding: usersettings.OnboardingSettings{CompletedAt: "2026-05-01T00:00:00Z"}}, stdinTTY: true, stdoutTTY: true},
		{name: "dismissed", settings: usersettings.Settings{Onboarding: usersettings.OnboardingSettings{Dismissed: "2026-05-01T00:00:00Z"}}, stdinTTY: true, stdoutTTY: true},
		{name: "stdin pipe", stdinTTY: false, stdoutTTY: true},
		{name: "stdout pipe", stdinTTY: true, stdoutTTY: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launched := false
			code := ensureOnboardingForTUI(onboardingDeps{
				load:        func() (usersettings.Settings, error) { return tt.settings, nil },
				isStdinTTY:  func() bool { return tt.stdinTTY },
				isStdoutTTY: func() bool { return tt.stdoutTTY },
				runWorkflow: func(string) int {
					launched = true
					return 0
				},
			})
			if code != 0 {
				t.Fatalf("ensureOnboardingForTUI() = %d, want 0", code)
			}
			if launched != tt.wantLaunch {
				t.Fatalf("launched = %v, want %v", launched, tt.wantLaunch)
			}
		})
	}
}

func TestEnsureOnboardingForTUILoadErrorFails(t *testing.T) {
	code := ensureOnboardingForTUI(onboardingDeps{
		load:        func() (usersettings.Settings, error) { return usersettings.Settings{}, errors.New("boom") },
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runWorkflow: func(string) int {
			t.Fatal("runWorkflow should not be called")
			return 0
		},
	})
	if code == 0 {
		t.Fatal("ensureOnboardingForTUI() = 0, want non-zero")
	}
}

func writeOnboardingState(t *testing.T, home, projectName, sessionID string, completed bool) string {
	t.Helper()

	sessionDir := filepath.Join(home, ".agent-runner", "projects", projectName, "runs", sessionID)
	state := model.RunState{
		WorkflowFile: "builtin:onboarding/welcome.yaml",
		WorkflowName: "onboarding-welcome",
		CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{
			StepID:            "welcome",
			SessionIDs:        map[string]string{},
			CapturedVariables: map[string]model.CapturedValue{},
			Child:             nil,
		}},
		Params:       map[string]string{},
		WorkflowHash: "test",
		Completed:    completed,
	}
	if err := stateio.WriteState(&state, sessionDir); err != nil {
		t.Fatalf("WriteState: %v", err)
	}
	return filepath.Join(sessionDir, "state.json")
}

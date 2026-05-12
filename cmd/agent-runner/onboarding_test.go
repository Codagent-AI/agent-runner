package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/usersettings"
	"github.com/google/go-cmp/cmp"
)

func TestEnsureFirstRunForTUIRunsNativeSetupThenDemo(t *testing.T) {
	var setupRuns int
	var launched []string
	result := ensureFirstRunForTUI(firstRunDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{}, nil
		},
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runNativeSetup: func(onboardingCompleted bool) (nativeSetupResult, error) {
			setupRuns++
			if onboardingCompleted {
				t.Fatal("onboardingCompleted should be false for fresh setup")
			}
			return nativeSetupDemo, nil
		},
		runWorkflow: func(ref string) firstRunWorkflowResult {
			launched = append(launched, ref)
			return firstRunWorkflowResult{exitCode: 7}
		},
	})

	requireFirstRunExit(t, result, 7)
	if setupRuns != 1 {
		t.Fatalf("setupRuns = %d, want 1", setupRuns)
	}
	if diff := cmp.Diff([]string{"builtin:onboarding/onboarding.yaml"}, launched); diff != "" {
		t.Fatalf("launched mismatch (-want +got):\n%s", diff)
	}
}

func TestEnsureFirstRunForTUIShowsDemoPromptWhenSetupAlreadyCompleted(t *testing.T) {
	var launched []string
	result := ensureFirstRunForTUI(firstRunDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{Setup: usersettings.SetupSettings{CompletedAt: "2026-05-04T00:00:00Z"}}, nil
		},
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runNativeSetup: func(bool) (nativeSetupResult, error) {
			t.Fatal("runNativeSetup should not be called when setup is complete")
			return nativeSetupCancelled, nil
		},
		runDemoPrompt: func() (nativeSetupResult, error) {
			return nativeSetupDemo, nil
		},
		runWorkflow: func(ref string) firstRunWorkflowResult {
			launched = append(launched, ref)
			return firstRunWorkflowResult{exitCode: 9}
		},
	})

	requireFirstRunExit(t, result, 9)
	if diff := cmp.Diff([]string{"builtin:onboarding/onboarding.yaml"}, launched); diff != "" {
		t.Fatalf("launched mismatch (-want +got):\n%s", diff)
	}
}

func TestEnsureFirstRunForTUIQuitDuringDemoExitsApp(t *testing.T) {
	result := ensureFirstRunForTUI(firstRunDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{Setup: usersettings.SetupSettings{CompletedAt: "2026-05-04T00:00:00Z"}}, nil
		},
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runDemoPrompt: func() (nativeSetupResult, error) {
			return nativeSetupDemo, nil
		},
		runWorkflow: func(string) firstRunWorkflowResult {
			return firstRunWorkflowResult{exitRequested: true}
		},
	})

	requireFirstRunExit(t, result, 0)
}

func TestEnsureFirstRunForTUISkipsWhenCompletedDismissedOrNonTTY(t *testing.T) {
	tests := []struct {
		name       string
		settings   usersettings.Settings
		stdinTTY   bool
		stdoutTTY  bool
		wantLaunch bool
	}{
		{name: "onboarding completed", settings: usersettings.Settings{Setup: usersettings.SetupSettings{CompletedAt: "2026-05-04T00:00:00Z"}, Onboarding: usersettings.OnboardingSettings{CompletedAt: "2026-05-01T00:00:00Z"}}, stdinTTY: true, stdoutTTY: true},
		{name: "onboarding dismissed", settings: usersettings.Settings{Setup: usersettings.SetupSettings{CompletedAt: "2026-05-04T00:00:00Z"}, Onboarding: usersettings.OnboardingSettings{Dismissed: "2026-05-01T00:00:00Z"}}, stdinTTY: true, stdoutTTY: true},
		{name: "stdin pipe", stdinTTY: false, stdoutTTY: true},
		{name: "stdout pipe", stdinTTY: true, stdoutTTY: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launched := false
			setupRan := false
			result := ensureFirstRunForTUI(firstRunDeps{
				load:        func() (usersettings.Settings, error) { return tt.settings, nil },
				isStdinTTY:  func() bool { return tt.stdinTTY },
				isStdoutTTY: func() bool { return tt.stdoutTTY },
				runNativeSetup: func(bool) (nativeSetupResult, error) {
					setupRan = true
					return nativeSetupCompleted, nil
				},
				runDemoPrompt: func() (nativeSetupResult, error) {
					launched = true
					return nativeSetupCompleted, nil
				},
				runWorkflow: func(string) firstRunWorkflowResult {
					launched = true
					return firstRunWorkflowResult{}
				},
			})
			requireFirstRunContinue(t, result)
			if launched != tt.wantLaunch {
				t.Fatalf("launched = %v, want %v", launched, tt.wantLaunch)
			}
			if (!tt.stdinTTY || !tt.stdoutTTY) && setupRan {
				t.Fatal("setup ran for non-TTY")
			}
		})
	}
}

func TestEnsureFirstRunForTUICancelledSetupDoesNotStartDemo(t *testing.T) {
	var launched bool
	result := ensureFirstRunForTUI(firstRunDeps{
		load:        func() (usersettings.Settings, error) { return usersettings.Settings{}, nil },
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runNativeSetup: func(bool) (nativeSetupResult, error) {
			return nativeSetupCancelled, nil
		},
		runWorkflow: func(string) firstRunWorkflowResult {
			launched = true
			return firstRunWorkflowResult{}
		},
	})
	requireFirstRunContinue(t, result)
	if launched {
		t.Fatal("onboarding demo launched after cancelled setup")
	}
}

func TestEnsureFirstRunForTUISetupErrorGoesHome(t *testing.T) {
	result := ensureFirstRunForTUI(firstRunDeps{
		load:        func() (usersettings.Settings, error) { return usersettings.Settings{}, nil },
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runNativeSetup: func(bool) (nativeSetupResult, error) {
			return nativeSetupFailed, errors.New("write failed")
		},
		continueAfterNativeSetupError: true,
		runWorkflow: func(string) firstRunWorkflowResult {
			t.Fatal("runWorkflow should not be called")
			return firstRunWorkflowResult{}
		},
	})
	requireFirstRunContinue(t, result)
}

func TestEnsureFirstRunForTUISetupErrorFailsWhenNonFatalModeDisabled(t *testing.T) {
	result := ensureFirstRunForTUI(firstRunDeps{
		load:        func() (usersettings.Settings, error) { return usersettings.Settings{}, nil },
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runNativeSetup: func(bool) (nativeSetupResult, error) {
			return nativeSetupFailed, errors.New("write failed")
		},
		runWorkflow: func(string) firstRunWorkflowResult {
			t.Fatal("runWorkflow should not be called")
			return firstRunWorkflowResult{}
		},
	})
	requireFirstRunExit(t, result, 1)
}

func TestDefaultFirstRunDepsReportsNativeSetupErrors(t *testing.T) {
	if !defaultFirstRunDeps.continueAfterNativeSetupError {
		t.Fatal("default first-run setup should continue to the normal TUI after native setup errors")
	}
}

func TestResetOnboardingStateClearsSettingsProjectValidatorAndRuns(t *testing.T) {
	originalHome := userHomeDir
	home := t.TempDir()
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = originalHome })
	t.Setenv("HOME", home)

	repo := filepath.Join(t.TempDir(), "repo")
	validatorDir := filepath.Join(repo, ".validator")
	if err := os.MkdirAll(validatorDir, 0o750); err != nil {
		t.Fatalf("mkdir validator dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(validatorDir, "config.yml"), []byte("checks: []\n"), 0o600); err != nil {
		t.Fatalf("write validator config: %v", err)
	}
	t.Chdir(repo)

	settings := usersettings.Settings{
		Theme: usersettings.ThemeDark,
		Setup: usersettings.SetupSettings{CompletedAt: "2026-05-04T00:00:00Z"},
		Onboarding: usersettings.OnboardingSettings{
			CompletedAt: "2026-05-05T00:00:00Z",
			Dismissed:   "2026-05-06T00:00:00Z",
		},
	}
	if err := usersettings.Save(settings); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	runsDir := filepath.Join(home, ".agent-runner", "onboarding", "runs")
	if err := os.MkdirAll(filepath.Join(runsDir, "run-1"), 0o750); err != nil {
		t.Fatalf("mkdir onboarding run: %v", err)
	}

	if err := resetOnboardingState(); err != nil {
		t.Fatalf("resetOnboardingState: %v", err)
	}

	got, err := usersettings.Load()
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if got.Theme != usersettings.ThemeDark {
		t.Fatalf("theme = %q, want preserved dark", got.Theme)
	}
	if got.Setup.CompletedAt != "2026-05-04T00:00:00Z" {
		t.Fatalf("setup.completed_at = %q, want preserved", got.Setup.CompletedAt)
	}
	if got.Onboarding.CompletedAt != "" || got.Onboarding.Dismissed != "" {
		t.Fatalf("onboarding settings = %#v, want cleared", got.Onboarding)
	}
	if _, err := os.Stat(validatorDir); !os.IsNotExist(err) {
		t.Fatalf(".validator stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(runsDir); !os.IsNotExist(err) {
		t.Fatalf("onboarding runs stat err = %v, want not exist", err)
	}
}

func TestEnsureFirstRunForTUILoadErrorFails(t *testing.T) {
	result := ensureFirstRunForTUI(firstRunDeps{
		load:        func() (usersettings.Settings, error) { return usersettings.Settings{}, errors.New("boom") },
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runWorkflow: func(string) firstRunWorkflowResult {
			t.Fatal("runWorkflow should not be called")
			return firstRunWorkflowResult{}
		},
	})
	requireFirstRunExit(t, result, 1)
}

func TestDemoPromptNotNowOnReShowDoesNotLaunchWorkflow(t *testing.T) {
	var launched bool
	result := ensureFirstRunForTUI(firstRunDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{Setup: usersettings.SetupSettings{CompletedAt: "2026-05-04T00:00:00Z"}}, nil
		},
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runDemoPrompt: func() (nativeSetupResult, error) {
			return nativeSetupCompleted, nil
		},
		runWorkflow: func(string) firstRunWorkflowResult {
			launched = true
			return firstRunWorkflowResult{}
		},
	})
	requireFirstRunContinue(t, result)
	if launched {
		t.Fatal("workflow launched after Not now on re-show")
	}
}

func requireFirstRunContinue(t *testing.T, result firstRunResult) {
	t.Helper()
	if !result.continueToList {
		t.Fatalf("ensureFirstRunForTUI() exits with %d, want continue to list", result.exitCode)
	}
}

func requireFirstRunExit(t *testing.T, result firstRunResult, wantCode int) {
	t.Helper()
	if result.continueToList {
		t.Fatalf("ensureFirstRunForTUI() continues to list, want exit code %d", wantCode)
	}
	if result.exitCode != wantCode {
		t.Fatalf("ensureFirstRunForTUI() exits with %d, want %d", result.exitCode, wantCode)
	}
}

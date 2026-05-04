package main

import (
	"errors"
	"testing"

	"github.com/codagent/agent-runner/internal/usersettings"
	"github.com/google/go-cmp/cmp"
)

func TestEnsureFirstRunForTUIRunsNativeSetupBeforeOnboardingDemo(t *testing.T) {
	var setupRuns int
	var launched []string
	code := ensureFirstRunForTUI(firstRunDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{}, nil
		},
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runNativeSetup: func() (nativeSetupResult, error) {
			setupRuns++
			return nativeSetupCompleted, nil
		},
		runWorkflow: func(ref string) int {
			launched = append(launched, ref)
			return 7
		},
	})

	if code != 7 {
		t.Fatalf("ensureOnboardingForTUI() = %d, want workflow exit code 7", code)
	}
	if setupRuns != 1 {
		t.Fatalf("setupRuns = %d, want 1", setupRuns)
	}
	if diff := cmp.Diff([]string{"builtin:onboarding/onboarding.yaml"}, launched); diff != "" {
		t.Fatalf("launched mismatch (-want +got):\n%s", diff)
	}
}

func TestEnsureFirstRunForTUIStartsDemoWhenSetupAlreadyCompleted(t *testing.T) {
	var launched []string
	code := ensureFirstRunForTUI(firstRunDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{Setup: usersettings.SetupSettings{CompletedAt: "2026-05-04T00:00:00Z"}}, nil
		},
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runNativeSetup: func() (nativeSetupResult, error) {
			t.Fatal("runNativeSetup should not be called")
			return nativeSetupCancelled, nil
		},
		runWorkflow: func(ref string) int {
			launched = append(launched, ref)
			return 9
		},
	})

	if code != 9 {
		t.Fatalf("ensureFirstRunForTUI() = %d, want workflow exit code 9", code)
	}
	if diff := cmp.Diff([]string{"builtin:onboarding/onboarding.yaml"}, launched); diff != "" {
		t.Fatalf("launched mismatch (-want +got):\n%s", diff)
	}
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
			code := ensureFirstRunForTUI(firstRunDeps{
				load:        func() (usersettings.Settings, error) { return tt.settings, nil },
				isStdinTTY:  func() bool { return tt.stdinTTY },
				isStdoutTTY: func() bool { return tt.stdoutTTY },
				runNativeSetup: func() (nativeSetupResult, error) {
					setupRan = true
					return nativeSetupCompleted, nil
				},
				runWorkflow: func(string) int {
					launched = true
					return 0
				},
			})
			if code != 0 {
				t.Fatalf("ensureFirstRunForTUI() = %d, want 0", code)
			}
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
	code := ensureFirstRunForTUI(firstRunDeps{
		load:        func() (usersettings.Settings, error) { return usersettings.Settings{}, nil },
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runNativeSetup: func() (nativeSetupResult, error) {
			return nativeSetupCancelled, nil
		},
		runWorkflow: func(string) int {
			launched = true
			return 0
		},
	})
	if code != 0 {
		t.Fatalf("ensureFirstRunForTUI() = %d, want 0", code)
	}
	if launched {
		t.Fatal("onboarding demo launched after cancelled setup")
	}
}

func TestEnsureFirstRunForTUILoadErrorFails(t *testing.T) {
	code := ensureFirstRunForTUI(firstRunDeps{
		load:        func() (usersettings.Settings, error) { return usersettings.Settings{}, errors.New("boom") },
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		runWorkflow: func(string) int {
			t.Fatal("runWorkflow should not be called")
			return 0
		},
	})
	if code == 0 {
		t.Fatal("ensureFirstRunForTUI() = 0, want non-zero")
	}
}

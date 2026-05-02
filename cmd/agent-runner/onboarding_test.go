package main

import (
	"errors"
	"reflect"
	"testing"

	"github.com/codagent/agent-runner/internal/usersettings"
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
	if !reflect.DeepEqual(launched, []string{"builtin:onboarding/welcome.yaml"}) {
		t.Fatalf("launched = %#v", launched)
	}
}

func TestEnsureOnboardingForTUIResumesMostRecentIncompleteRun(t *testing.T) {
	var resumed []string
	code := ensureOnboardingForTUI(onboardingDeps{
		load:        func() (usersettings.Settings, error) { return usersettings.Settings{}, nil },
		isStdinTTY:  func() bool { return true },
		isStdoutTTY: func() bool { return true },
		incompleteOnboardingRun: func() (string, error) {
			return "onboarding-welcome-2026-05-02T12-00-00Z", nil
		},
		resumeRun: func(runID string) int {
			resumed = append(resumed, runID)
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
	if !reflect.DeepEqual(resumed, []string{"onboarding-welcome-2026-05-02T12-00-00Z"}) {
		t.Fatalf("resumed = %#v", resumed)
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

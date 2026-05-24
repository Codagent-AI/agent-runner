package main

import (
	"testing"

	"github.com/codagent/agent-runner/internal/usersettings"
)

func TestShouldShowSplashForFreshTTYSession(t *testing.T) {
	if !shouldShowSplash(&usersettings.Settings{}, true, true) {
		t.Fatal("fresh TTY session should show splash")
	}
}

func TestShouldShowSplashHonorsPersistentDismissalAndTTYGate(t *testing.T) {
	tests := []struct {
		name      string
		settings  usersettings.Settings
		stdinTTY  bool
		stdoutTTY bool
	}{
		{
			name:      "dismissed",
			settings:  usersettings.Settings{Splash: usersettings.SplashSettings{Dismissed: "2026-05-24T00:00:00Z"}},
			stdinTTY:  true,
			stdoutTTY: true,
		},
		{name: "stdin pipe", stdinTTY: false, stdoutTTY: true},
		{name: "stdout pipe", stdinTTY: true, stdoutTTY: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if shouldShowSplash(&tt.settings, tt.stdinTTY, tt.stdoutTTY) {
				t.Fatal("splash should be suppressed")
			}
		})
	}
}

func TestShouldShowSplashIndependentOfSetupAndOnboarding(t *testing.T) {
	settings := usersettings.Settings{
		Setup:      usersettings.SetupSettings{CompletedAt: "2026-05-24T00:00:00Z"},
		Onboarding: usersettings.OnboardingSettings{CompletedAt: "2026-05-23T00:00:00Z", Dismissed: "2026-05-22T00:00:00Z"},
	}

	if !shouldShowSplash(&settings, true, true) {
		t.Fatal("setup and onboarding state should not suppress splash")
	}
}

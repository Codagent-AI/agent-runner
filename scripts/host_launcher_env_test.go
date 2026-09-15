package scripts_test

import (
	"slices"
	"testing"
)

func TestWithoutHostLauncherEnvDropsOnlyLauncherVariables(t *testing.T) {
	got := withoutHostLauncherEnv([]string{
		"PATH=/usr/bin",
		"AGENT_RUNNER_NO_TUI=1",
		"GIT_CONFIG_GLOBAL=/private/run/gitconfig",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_COUNT=0",
		"HOME=/home/test",
	})
	want := []string{"PATH=/usr/bin", "GIT_CONFIG_COUNT=0", "HOME=/home/test"}
	if !slices.Equal(got, want) {
		t.Fatalf("withoutHostLauncherEnv() = %q, want %q", got, want)
	}
}

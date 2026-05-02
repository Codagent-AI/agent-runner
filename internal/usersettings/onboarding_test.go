package usersettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndSaveOnboardingPreservesUnknownKeys(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeSettingsFile(t, home, "experimental_foo: 7\ntheme: dark\nonboarding:\n  completed_at: 2026-05-01T00:00:00Z\n  dismissed: 2026-05-02T00:00:00Z\n")

	settings, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if settings.Theme != ThemeDark {
		t.Fatalf("Theme = %q, want dark", settings.Theme)
	}
	if settings.Onboarding.CompletedAt != "2026-05-01T00:00:00Z" || settings.Onboarding.Dismissed != "2026-05-02T00:00:00Z" {
		t.Fatalf("Onboarding = %#v", settings.Onboarding)
	}

	settings.Onboarding.Dismissed = "2026-05-03T00:00:00Z"
	if err := Save(settings); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(home, ".agent-runner", "settings.yaml"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	text := string(body)
	for _, want := range []string{"experimental_foo: 7", "theme: dark", "completed_at: 2026-05-01T00:00:00Z", "dismissed: 2026-05-03T00:00:00Z"} {
		if !strings.Contains(text, want) {
			t.Fatalf("settings body missing %q:\n%s", want, text)
		}
	}
}

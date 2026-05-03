package main

import (
	"errors"
	"syscall"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/codagent/agent-runner/internal/usersettings"
)

func TestEnsureThemeForTUIPromptsSavesAndAppliesWhenUnset(t *testing.T) {
	var saved []usersettings.Settings
	var applied []usersettings.Theme

	code := ensureThemeForTUI(themeDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{}, nil
		},
		prompt: func() (usersettings.Theme, bool, error) {
			return usersettings.ThemeDark, true, nil
		},
		save: func(s usersettings.Settings) error {
			saved = append(saved, s)
			return nil
		},
		apply: func(theme usersettings.Theme) {
			applied = append(applied, theme)
		},
	})

	if code != 0 {
		t.Fatalf("ensureThemeForTUI() = %d, want 0", code)
	}
	if diff := cmp.Diff([]usersettings.Settings{{Theme: usersettings.ThemeDark}}, saved, cmpopts.IgnoreUnexported(usersettings.Settings{})); diff != "" {
		t.Fatalf("saved mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]usersettings.Theme{usersettings.ThemeDark}, applied); diff != "" {
		t.Fatalf("applied mismatch (-want +got):\n%s", diff)
	}
}

func TestEnsureThemeForTUIUsesPersistedThemeWithoutPrompting(t *testing.T) {
	prompted := false
	saved := false
	var applied []usersettings.Theme

	code := ensureThemeForTUI(themeDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{Theme: usersettings.ThemeLight}, nil
		},
		prompt: func() (usersettings.Theme, bool, error) {
			prompted = true
			return "", false, nil
		},
		save: func(s usersettings.Settings) error {
			saved = true
			return nil
		},
		apply: func(theme usersettings.Theme) {
			applied = append(applied, theme)
		},
	})

	if code != 0 {
		t.Fatalf("ensureThemeForTUI() = %d, want 0", code)
	}
	if prompted {
		t.Fatal("prompt was called for persisted theme")
	}
	if saved {
		t.Fatal("save was called for persisted theme")
	}
	if diff := cmp.Diff([]usersettings.Theme{usersettings.ThemeLight}, applied); diff != "" {
		t.Fatalf("applied mismatch (-want +got):\n%s", diff)
	}
}

func TestEnsureThemeForTUICancelDoesNotSaveOrApply(t *testing.T) {
	saved := false
	applied := false

	code := ensureThemeForTUI(themeDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{}, nil
		},
		prompt: func() (usersettings.Theme, bool, error) {
			return "", false, nil
		},
		save: func(s usersettings.Settings) error {
			saved = true
			return nil
		},
		apply: func(theme usersettings.Theme) {
			applied = true
		},
	})

	if code == 0 {
		t.Fatal("ensureThemeForTUI() = 0, want non-zero")
	}
	if saved {
		t.Fatal("save was called after cancel")
	}
	if applied {
		t.Fatal("apply was called after cancel")
	}
}

func TestEnsureThemeForTUISaveFailureDoesNotApply(t *testing.T) {
	applied := false

	code := ensureThemeForTUI(themeDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{}, nil
		},
		prompt: func() (usersettings.Theme, bool, error) {
			return usersettings.ThemeDark, true, nil
		},
		save: func(s usersettings.Settings) error {
			return syscall.EACCES
		},
		apply: func(theme usersettings.Theme) {
			applied = true
		},
	})

	if code == 0 {
		t.Fatal("ensureThemeForTUI() = 0, want non-zero")
	}
	if applied {
		t.Fatal("apply was called after save failure")
	}
}

func TestApplyThemeSetsLipglossBackground(t *testing.T) {
	applyTheme(usersettings.ThemeDark)
	if !lipgloss.HasDarkBackground() {
		t.Fatal("dark theme did not set dark background")
	}

	applyTheme(usersettings.ThemeLight)
	if lipgloss.HasDarkBackground() {
		t.Fatal("light theme did not set light background")
	}
}

func TestEnsureThemeForTUILoadFailureReturnsNonZero(t *testing.T) {
	code := ensureThemeForTUI(themeDeps{
		load: func() (usersettings.Settings, error) {
			return usersettings.Settings{}, errors.New("read failed")
		},
		prompt: func() (usersettings.Theme, bool, error) {
			t.Fatal("prompt should not be called after load failure")
			return "", false, nil
		},
		save: func(s usersettings.Settings) error {
			t.Fatal("save should not be called after load failure")
			return nil
		},
		apply: func(theme usersettings.Theme) {
			t.Fatal("apply should not be called after load failure")
		},
	})

	if code == 0 {
		t.Fatal("ensureThemeForTUI() = 0, want non-zero")
	}
}

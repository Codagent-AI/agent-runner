package settingseditor

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/usersettings"
)

func TestEditorRendersOnlyEditableThemeSetting(t *testing.T) {
	m := New(usersettings.Settings{
		Theme: usersettings.ThemeDark,
		Setup: usersettings.SetupSettings{
			CompletedAt: "2026-05-17T10:00:00Z",
		},
		Onboarding: usersettings.OnboardingSettings{
			CompletedAt: "2026-05-17T11:00:00Z",
			Dismissed:   "2026-05-17T12:00:00Z",
		},
	})

	view := m.View()
	for _, want := range []string{"Theme", "Light", "Dark"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q:\n%s", want, view)
		}
	}
	for _, hidden := range []string{"2026-05-17T10:00:00Z", "2026-05-17T11:00:00Z", "2026-05-17T12:00:00Z", "completed_at", "dismissed"} {
		if strings.Contains(view, hidden) {
			t.Fatalf("View() exposed lifecycle value/key %q:\n%s", hidden, view)
		}
	}
}

func TestEditorPreselectsPersistedTheme(t *testing.T) {
	tests := []struct {
		name  string
		theme usersettings.Theme
		want  usersettings.Theme
		label string
	}{
		{name: "dark", theme: usersettings.ThemeDark, want: usersettings.ThemeDark, label: "Dark"},
		{name: "light", theme: usersettings.ThemeLight, want: usersettings.ThemeLight, label: "Light"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(usersettings.Settings{Theme: tt.theme})
			if got := m.SelectedTheme(); got != tt.want {
				t.Fatalf("SelectedTheme() = %q, want %q", got, tt.want)
			}
			if !strings.Contains(m.View(), "> "+tt.label) {
				t.Fatalf("View() does not mark %q selected:\n%s", tt.label, m.View())
			}
		})
	}
}

func TestEditorKeysMoveThemeOption(t *testing.T) {
	tests := []struct {
		name  string
		start usersettings.Theme
		key   tea.KeyMsg
		want  usersettings.Theme
	}{
		{name: "up moves dark to light", start: usersettings.ThemeDark, key: tea.KeyMsg{Type: tea.KeyUp}, want: usersettings.ThemeLight},
		{name: "left moves dark to light", start: usersettings.ThemeDark, key: tea.KeyMsg{Type: tea.KeyLeft}, want: usersettings.ThemeLight},
		{name: "tab moves dark to light", start: usersettings.ThemeDark, key: tea.KeyMsg{Type: tea.KeyTab}, want: usersettings.ThemeLight},
		{name: "down moves light to dark", start: usersettings.ThemeLight, key: tea.KeyMsg{Type: tea.KeyDown}, want: usersettings.ThemeDark},
		{name: "right moves light to dark", start: usersettings.ThemeLight, key: tea.KeyMsg{Type: tea.KeyRight}, want: usersettings.ThemeDark},
		{name: "shift tab moves light to dark", start: usersettings.ThemeLight, key: tea.KeyMsg{Type: tea.KeyShiftTab}, want: usersettings.ThemeDark},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(usersettings.Settings{Theme: tt.start})
			next, cmd := m.Update(tt.key)
			m = next.(*Model)
			if cmd != nil {
				t.Fatal("movement key should not produce a command")
			}
			if got := m.SelectedTheme(); got != tt.want {
				t.Fatalf("SelectedTheme() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEditorSavePersistsVisibleSettingsAndEmitsSaved(t *testing.T) {
	var saved usersettings.Settings
	m := New(
		usersettings.Settings{
			Theme: usersettings.ThemeLight,
			Setup: usersettings.SetupSettings{CompletedAt: "2026-05-17T10:00:00Z"},
		},
		WithSave(func(settings usersettings.Settings) error {
			saved = settings
			return nil
		}),
	)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(*Model)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*Model)

	if cmd == nil {
		t.Fatal("enter should emit a save completion command")
	}
	if saved.Theme != usersettings.ThemeDark {
		t.Fatalf("saved Theme = %q, want dark", saved.Theme)
	}
	if saved.Setup.CompletedAt != "2026-05-17T10:00:00Z" {
		t.Fatalf("saved setup completed_at = %q, want preserved timestamp", saved.Setup.CompletedAt)
	}
	msg := cmd()
	if got, ok := msg.(SavedMsg); !ok {
		t.Fatalf("command emitted %T, want SavedMsg", msg)
	} else if got.Settings.Theme != usersettings.ThemeDark {
		t.Fatalf("SavedMsg theme = %q, want dark", got.Settings.Theme)
	}
	if strings.Contains(m.View(), "failed") {
		t.Fatalf("View() should not show save error after successful save:\n%s", m.View())
	}
}

func TestEditorSaveFailureStaysOpenAndShowsPath(t *testing.T) {
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight},
		WithSave(func(usersettings.Settings) error {
			return errors.New("permission denied")
		}),
		WithPath(func() (string, error) {
			return "/home/me/.agent-runner/settings.yaml", nil
		}),
	)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(*Model)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*Model)

	if cmd != nil {
		t.Fatal("failed save should not emit completion command")
	}
	if got := m.SelectedTheme(); got != usersettings.ThemeDark {
		t.Fatalf("SelectedTheme() = %q, want unsaved dark cursor to remain", got)
	}
	view := m.View()
	for _, want := range []string{"/home/me/.agent-runner/settings.yaml", "permission denied"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing save error detail %q:\n%s", want, view)
		}
	}
}

func TestEditorCancelEmitsCancelledWithoutSaving(t *testing.T) {
	saved := false
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight},
		WithSave(func(usersettings.Settings) error {
			saved = true
			return nil
		}),
	)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(*Model)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if saved {
		t.Fatal("esc should not save")
	}
	if cmd == nil {
		t.Fatal("esc should emit a cancel completion command")
	}
	if msg := cmd(); msg != (CancelledMsg{}) {
		t.Fatalf("command emitted %#v, want CancelledMsg{}", msg)
	}
}

func TestEditorDoesNotInterceptCtrlC(t *testing.T) {
	m := New(usersettings.Settings{Theme: usersettings.ThemeDark})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if next != m {
		t.Fatal("ctrl+c should leave editor unchanged")
	}
	if cmd != nil {
		t.Fatal("ctrl+c should not be converted to an editor command")
	}
}

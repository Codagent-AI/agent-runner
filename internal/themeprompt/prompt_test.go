package themeprompt

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/usersettings"
)

func TestModelPreselectsThemeFromDetection(t *testing.T) {
	tests := []struct {
		name     string
		dark     bool
		selected usersettings.Theme
		label    string
	}{
		{name: "dark", dark: true, selected: usersettings.ThemeDark, label: "Dark"},
		{name: "light", dark: false, selected: usersettings.ThemeLight, label: "Light"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(tt.dark)
			if got := m.selectedTheme(); got != tt.selected {
				t.Fatalf("selectedTheme() = %q, want %q", got, tt.selected)
			}
			if !strings.Contains(m.View(), "> "+tt.label) {
				t.Fatalf("View() does not show selected label %q:\n%s", tt.label, m.View())
			}
		})
	}
}

func TestModelArrowKeysChangeSelection(t *testing.T) {
	m := newModel(true)

	for _, key := range []string{"up", "left"} {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = next.(model)
		if got := m.selectedTheme(); got != usersettings.ThemeLight {
			t.Fatalf("after %s selectedTheme() = %q, want light", key, got)
		}
	}

	for _, key := range []string{"down", "right"} {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = next.(model)
		if got := m.selectedTheme(); got != usersettings.ThemeDark {
			t.Fatalf("after %s selectedTheme() = %q, want dark", key, got)
		}
	}
}

func TestModelEnterConfirmsSelectedThemeAndQuits(t *testing.T) {
	m := newModel(true)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("up")})
	m = next.(model)

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)

	if !m.confirmed {
		t.Fatal("confirmed = false, want true")
	}
	if m.cancelled {
		t.Fatal("cancelled = true, want false")
	}
	if got := m.selectedTheme(); got != usersettings.ThemeLight {
		t.Fatalf("selectedTheme() = %q, want light", got)
	}
	if cmd == nil {
		t.Fatal("Enter should return a quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("Enter command = %T, want tea.QuitMsg", cmd())
	}
}

func TestModelCancelQuitsWithoutConfirming(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlC} {
		t.Run(key.String(), func(t *testing.T) {
			m := newModel(true)
			next, cmd := m.Update(tea.KeyMsg{Type: key})
			m = next.(model)

			if !m.cancelled {
				t.Fatal("cancelled = false, want true")
			}
			if m.confirmed {
				t.Fatal("confirmed = true, want false")
			}
			if cmd == nil {
				t.Fatal("cancel should return a quit command")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatalf("cancel command = %T, want tea.QuitMsg", cmd())
			}
		})
	}
}

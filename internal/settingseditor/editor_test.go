package settingseditor

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/usersettings"
)

func TestEditorRendersEveryFieldWithCurrentValue(t *testing.T) {
	m := New(usersettings.Settings{
		Theme:                    usersettings.ThemeDark,
		AutonomousBackend:        usersettings.BackendInteractiveClaude,
		AutonomousPermissionMode: usersettings.PermissionModeYOLO,
		Setup: usersettings.SetupSettings{
			CompletedAt: "2026-05-17T10:00:00Z",
		},
		Onboarding: usersettings.OnboardingSettings{
			CompletedAt: "2026-05-17T11:00:00Z",
			Dismissed:   "2026-05-17T12:00:00Z",
		},
	})

	view := m.View()
	for _, want := range []string{
		"Theme",
		"Autonomous Backend",
		"Autonomous Permission Mode",
		"Dark",
		"Interactive for Claude",
		"YOLO",
	} {
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

func TestEditorRendersAgentSettingsConfigPath(t *testing.T) {
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeDark},
		WithAgentConfigPath(func() (string, error) {
			return "/home/me/.agent-runner/config.yaml", nil
		}),
	)

	view := m.View()
	for _, want := range []string{
		"Looking for agent settings",
		"planner / implementor CLI",
		"model",
		"/home/me/.agent-runner/config.yaml",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing agent settings copy %q:\n%s", want, view)
		}
	}
}

func TestEditorOpensWithCursorOnFirstRow(t *testing.T) {
	m := New(usersettings.Settings{Theme: usersettings.ThemeDark})
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (Theme row highlighted on open)", m.cursor)
	}
	if !strings.Contains(m.View(), "▶") {
		t.Fatalf("View() missing cursor marker '▶':\n%s", m.View())
	}
}

func TestEditorPresentsPersistedThemeAsValue(t *testing.T) {
	for _, tt := range []struct {
		name  string
		theme usersettings.Theme
		want  string
	}{
		{name: "dark", theme: usersettings.ThemeDark, want: "Dark"},
		{name: "light", theme: usersettings.ThemeLight, want: "Light"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := New(usersettings.Settings{Theme: tt.theme})
			view := m.View()
			if !strings.Contains(view, "Theme") || !strings.Contains(view, tt.want) {
				t.Fatalf("View() should show Theme = %q:\n%s", tt.want, view)
			}
		})
	}
}

func TestEditorPresentsPersistedAutonomousBackendAsValue(t *testing.T) {
	for _, tt := range []struct {
		name    string
		backend usersettings.AutonomousBackend
		want    string
	}{
		{name: "headless", backend: usersettings.BackendHeadless, want: "Headless"},
		{name: "interactive", backend: usersettings.BackendInteractive, want: "Interactive"},
		{name: "interactive-claude", backend: usersettings.BackendInteractiveClaude, want: "Interactive for Claude"},
		{name: "absent defaults headless", backend: "", want: "Headless"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := New(usersettings.Settings{Theme: usersettings.ThemeLight, AutonomousBackend: tt.backend})
			view := m.View()
			if !strings.Contains(view, "Autonomous Backend") || !strings.Contains(view, tt.want) {
				t.Fatalf("View() should show Autonomous Backend = %q:\n%s", tt.want, view)
			}
		})
	}
}

func TestEditorPresentsPersistedAutonomousPermissionModeAsValue(t *testing.T) {
	for _, tt := range []struct {
		name string
		mode usersettings.AutonomousPermissionMode
		want string
	}{
		{name: "conservative", mode: usersettings.PermissionModeConservative, want: "Conservative"},
		{name: "yolo", mode: usersettings.PermissionModeYOLO, want: "YOLO"},
		{name: "absent defaults conservative", mode: "", want: "Conservative"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := New(usersettings.Settings{Theme: usersettings.ThemeLight, AutonomousPermissionMode: tt.mode})
			view := m.View()
			if !strings.Contains(view, "Autonomous Permission Mode") || !strings.Contains(view, tt.want) {
				t.Fatalf("View() should show Autonomous Permission Mode = %q:\n%s", tt.want, view)
			}
		})
	}
}

func TestEditorCursorRowYoloShowsRiskCopy(t *testing.T) {
	m := New(usersettings.Settings{
		Theme:                    usersettings.ThemeDark,
		AutonomousPermissionMode: usersettings.PermissionModeYOLO,
	})
	// Move cursor to the Autonomous Permission Mode row (index 2).
	for range 2 {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(*Model)
	}
	view := m.View()
	for _, want := range []string{"per-command approval", "external sandbox"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() should show YOLO risk copy %q when cursor on Permission Mode row with value YOLO:\n%s", want, view)
		}
	}
}

func TestEditorAgentSettingsCopyAppearsBelowSelectedOptionDescription(t *testing.T) {
	m := New(usersettings.Settings{
		Theme:                    usersettings.ThemeDark,
		AutonomousPermissionMode: usersettings.PermissionModeYOLO,
	})
	// Move cursor to the Autonomous Permission Mode row (index 2).
	for range 2 {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(*Model)
	}

	view := m.View()
	descIdx := strings.Index(view, "per-command approval")
	agentSettingsIdx := strings.Index(view, "Looking for agent settings")
	if descIdx == -1 || agentSettingsIdx == -1 {
		t.Fatalf("View() missing expected copy:\n%s", view)
	}
	if agentSettingsIdx < descIdx {
		t.Fatalf("agent settings copy should render below selected option description:\n%s", view)
	}
}

func TestEditorCursorRowYoloRiskCopyDisappearsWhenCursorMovesAway(t *testing.T) {
	m := New(usersettings.Settings{
		Theme:                    usersettings.ThemeDark,
		AutonomousPermissionMode: usersettings.PermissionModeYOLO,
	})
	// Walk to Permission Mode row to confirm risk copy is there, then walk away.
	for range 2 {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(*Model)
	}
	if !strings.Contains(m.View(), "per-command approval") {
		t.Fatalf("precondition: risk copy should be visible at Permission Mode row")
	}
	// Move back to Theme row.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(*Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(*Model)
	if strings.Contains(m.View(), "per-command approval") {
		t.Fatalf("risk copy should be hidden when cursor is not on Permission Mode row:\n%s", m.View())
	}
}

func TestEditorDownUpMoveCursorBetweenRows(t *testing.T) {
	for _, tt := range []struct {
		name string
		keys []tea.KeyMsg
		want int
	}{
		{name: "down moves from 0 to 1", keys: []tea.KeyMsg{{Type: tea.KeyDown}}, want: 1},
		{name: "down twice moves from 0 to 2", keys: []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyDown}}, want: 2},
		{name: "down three times wraps back to 0", keys: []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyDown}, {Type: tea.KeyDown}}, want: 0},
		{name: "up from 0 wraps to last", keys: []tea.KeyMsg{{Type: tea.KeyUp}}, want: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := New(usersettings.Settings{Theme: usersettings.ThemeDark})
			for _, k := range tt.keys {
				next, _ := m.Update(k)
				m = next.(*Model)
			}
			if m.cursor != tt.want {
				t.Fatalf("cursor = %d, want %d", m.cursor, tt.want)
			}
		})
	}
}

func TestEditorCursorMovementDoesNotCycleValue(t *testing.T) {
	saved := false
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight},
		WithSave(func(usersettings.Settings) error {
			saved = true
			return nil
		}),
	)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(*Model)
	if cmd != nil {
		t.Fatal("cursor movement should not emit a command")
	}
	if saved {
		t.Fatal("cursor movement should not invoke save")
	}
	if m.SelectedTheme() != usersettings.ThemeLight {
		t.Fatalf("SelectedTheme() = %q, want light (cursor movement should not change values)", m.SelectedTheme())
	}
}

func TestEditorTabCyclesCursorRowForwardWithoutSaving(t *testing.T) {
	var saved []usersettings.Settings
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight},
		WithSave(func(s usersettings.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*Model)
	if cmd != nil {
		t.Fatal("Tab should not emit a save command; saves are deferred to Enter")
	}
	if m.SelectedTheme() != usersettings.ThemeDark {
		t.Fatalf("SelectedTheme() = %q, want dark (Tab should cycle Light → Dark)", m.SelectedTheme())
	}
	if len(saved) != 0 {
		t.Fatalf("saved settings = %#v, want no saves before Enter", saved)
	}
}

func TestEditorSpaceCyclesCursorRowForwardWithoutSaving(t *testing.T) {
	var saved []usersettings.Settings
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight},
		WithSave(func(s usersettings.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = next.(*Model)
	if m.SelectedTheme() != usersettings.ThemeDark {
		t.Fatalf("SelectedTheme() = %q, want dark (Space should cycle Light → Dark)", m.SelectedTheme())
	}
	if len(saved) != 0 {
		t.Fatalf("saved count = %d, want 0 (Space should not save)", len(saved))
	}
}

func TestEditorEnterCommitsAllPendingChangesAndEmitsSaved(t *testing.T) {
	var saved []usersettings.Settings
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight, AutonomousBackend: usersettings.BackendHeadless},
		WithSave(func(s usersettings.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)
	// Cycle Theme: Light → Dark.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*Model)
	// Move to Backend row and cycle Headless → Interactive.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(*Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*Model)
	if len(saved) != 0 {
		t.Fatalf("saved count after two cycles = %d, want 0 (no save before Enter)", len(saved))
	}
	// Enter commits everything at once.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should emit a save command")
	}
	if len(saved) != 1 {
		t.Fatalf("saved count = %d, want 1 (Enter triggers a single combined save)", len(saved))
	}
	if saved[0].Theme != usersettings.ThemeDark || saved[0].AutonomousBackend != usersettings.BackendInteractive {
		t.Fatalf("saved settings = %#v, want theme=dark backend=interactive", saved[0])
	}
	msg := cmd()
	got, ok := msg.(SavedMsg)
	if !ok {
		t.Fatalf("command emitted %T, want SavedMsg", msg)
	}
	if got.Settings.Theme != usersettings.ThemeDark || got.Settings.AutonomousBackend != usersettings.BackendInteractive {
		t.Fatalf("SavedMsg.Settings = %#v, want dark + interactive", got.Settings)
	}
}

func TestEditorEscDiscardsPendingChanges(t *testing.T) {
	var saved []usersettings.Settings
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight},
		WithSave(func(s usersettings.Settings) error {
			saved = append(saved, s)
			return nil
		}),
	)
	// Cycle Theme but do NOT press Enter.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*Model)
	// Esc closes without saving.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should emit a close command")
	}
	if msg := cmd(); msg != (CancelledMsg{}) {
		t.Fatalf("command emitted %#v, want CancelledMsg{}", msg)
	}
	if len(saved) != 0 {
		t.Fatalf("saved count = %d, want 0 (Esc should not persist pending changes)", len(saved))
	}
}

func TestEditorShiftTabCyclesCursorRowBackward(t *testing.T) {
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight, AutonomousBackend: usersettings.BackendInteractive},
		WithSave(func(usersettings.Settings) error { return nil }),
	)
	// Move cursor to Backend row, then Shift+Tab to cycle backward
	// (Interactive → Headless).
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(*Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(*Model)
	if m.SelectedAutonomousBackend() != usersettings.BackendHeadless {
		t.Fatalf("SelectedAutonomousBackend() = %q, want headless (Shift+Tab should cycle Interactive → Headless)", m.SelectedAutonomousBackend())
	}
}

func TestEditorTabWrapsLastOptionToFirst(t *testing.T) {
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight, AutonomousBackend: usersettings.BackendInteractiveClaude},
		WithSave(func(usersettings.Settings) error { return nil }),
	)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(*Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*Model)
	if m.SelectedAutonomousBackend() != usersettings.BackendHeadless {
		t.Fatalf("SelectedAutonomousBackend() = %q, want headless (Tab from Interactive for Claude should wrap to Headless)", m.SelectedAutonomousBackend())
	}
}

func TestEditorShiftTabWrapsFirstOptionToLast(t *testing.T) {
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight, AutonomousBackend: usersettings.BackendHeadless},
		WithSave(func(usersettings.Settings) error { return nil }),
	)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(*Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = next.(*Model)
	if m.SelectedAutonomousBackend() != usersettings.BackendInteractiveClaude {
		t.Fatalf("SelectedAutonomousBackend() = %q, want interactive-claude (Shift+Tab from Headless should wrap to Interactive for Claude)", m.SelectedAutonomousBackend())
	}
}

func TestEditorCycleOnlyAffectsCursorRow(t *testing.T) {
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight, AutonomousBackend: usersettings.BackendInteractive},
		WithSave(func(usersettings.Settings) error { return nil }),
	)
	// Cursor starts on Theme. Tab to cycle Theme → Dark.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*Model)
	if m.SelectedTheme() != usersettings.ThemeDark {
		t.Fatalf("SelectedTheme() = %q, want dark", m.SelectedTheme())
	}
	if m.SelectedAutonomousBackend() != usersettings.BackendInteractive {
		t.Fatalf("SelectedAutonomousBackend() = %q, want interactive (unchanged)", m.SelectedAutonomousBackend())
	}
}

func TestEditorEscEmitsCancelledMsg(t *testing.T) {
	m := New(usersettings.Settings{Theme: usersettings.ThemeLight})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc should emit a close command")
	}
	if msg := cmd(); msg != (CancelledMsg{}) {
		t.Fatalf("command emitted %#v, want CancelledMsg{}", msg)
	}
}

func TestEditorEnterSaveFailureSurfacesInlineAndStaysOpen(t *testing.T) {
	m := New(
		usersettings.Settings{Theme: usersettings.ThemeLight},
		WithSave(func(usersettings.Settings) error {
			return errors.New("permission denied")
		}),
		WithPath(func() (string, error) {
			return "/home/me/.agent-runner/settings.yaml", nil
		}),
	)
	// Cycle Theme to Dark (pending), then Enter to commit; the save fails.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*Model)
	if cmd != nil {
		t.Fatal("failed save should not emit SavedMsg")
	}
	// The editor's pending value remains so the user can retry; the embedder
	// keeps the editor open (no SavedMsg fires).
	if m.SelectedTheme() != usersettings.ThemeDark {
		t.Fatalf("SelectedTheme() = %q, want dark (pending value should remain after failed save so user can retry or Esc)", m.SelectedTheme())
	}
	view := m.View()
	for _, want := range []string{"/home/me/.agent-runner/settings.yaml", "permission denied"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing save error detail %q:\n%s", want, view)
		}
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

func TestEditorIgnoresUnrelatedKeys(t *testing.T) {
	m := New(usersettings.Settings{Theme: usersettings.ThemeLight})
	startCursor := m.cursor
	for _, runes := range [][]rune{{'r'}, {'n'}, {'c'}, {'?'}, {'q'}} {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: runes})
		m = next.(*Model)
		if cmd != nil {
			t.Fatalf("key %q produced a command", string(runes))
		}
	}
	if m.cursor != startCursor {
		t.Fatalf("cursor should not have moved, got %d", m.cursor)
	}
	if m.SelectedTheme() != usersettings.ThemeLight {
		t.Fatalf("SelectedTheme() should not have changed, got %q", m.SelectedTheme())
	}
}

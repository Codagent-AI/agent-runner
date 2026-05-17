// Package settingseditor implements the in-session user settings overlay.
package settingseditor

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codagent/agent-runner/internal/tuistyle"
	"github.com/codagent/agent-runner/internal/usersettings"
)

// SavedMsg is emitted after the editor successfully persists settings.
type SavedMsg struct {
	Settings usersettings.Settings
}

// CancelledMsg is emitted when the editor is closed without saving.
type CancelledMsg struct{}

// Model is the bubbletea submodel for editing user settings.
type Model struct {
	settings usersettings.Settings
	selected usersettings.Theme
	saveErr  string

	save func(usersettings.Settings) error
	path func() (string, error)
}

// Option configures a Model.
type Option func(*Model)

// WithSave overrides the settings write path. It is primarily for tests.
func WithSave(save func(usersettings.Settings) error) Option {
	return func(m *Model) { m.save = save }
}

// WithPath overrides the path used in inline save errors.
func WithPath(path func() (string, error)) Option {
	return func(m *Model) { m.path = path }
}

// New creates an editor seeded from persisted settings.
func New(settings usersettings.Settings, opts ...Option) *Model {
	selected := settings.Theme
	if selected != usersettings.ThemeDark && selected != usersettings.ThemeLight {
		selected = usersettings.ThemeLight
	}
	m := &Model{
		settings: settings,
		selected: selected,
		save:     usersettings.Save,
		path:     usersettings.Path,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// SelectedTheme returns the theme currently selected by the option cursor.
func (m *Model) SelectedTheme() usersettings.Theme {
	return m.selected
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "up", "left", "shift+tab":
		m.move(-1)
	case "down", "right", "tab":
		m.move(1)
	case "enter":
		return m, m.saveSelected()
	case "esc":
		return m, func() tea.Msg { return CancelledMsg{} }
	case "ctrl+c":
		return m, nil
	}
	return m, nil
}

func (m *Model) move(delta int) {
	options := []usersettings.Theme{usersettings.ThemeLight, usersettings.ThemeDark}
	idx := 0
	if m.selected == usersettings.ThemeDark {
		idx = 1
	}
	idx = (idx + delta + len(options)) % len(options)
	m.selected = options[idx]
	m.saveErr = ""
}

func (m *Model) saveSelected() tea.Cmd {
	next := m.settings
	next.Theme = m.selected
	if err := m.save(next); err != nil {
		m.saveErr = fmt.Sprintf("Failed to save %s: %v", m.settingsPath(), err)
		return nil
	}
	m.settings = next
	m.saveErr = ""
	return func() tea.Msg { return SavedMsg{Settings: next} }
}

func (m *Model) settingsPath() string {
	if m.path == nil {
		return "settings.yaml"
	}
	path, err := m.path()
	if err != nil || path == "" {
		return "settings.yaml"
	}
	return path
}

func (m *Model) View() string {
	header := tuistyle.HeaderStyle.Render("Settings")
	label := tuistyle.LabelStyle.Render("Theme")
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		label,
		m.option(usersettings.ThemeLight, "Light"),
		m.option(usersettings.ThemeDark, "Dark"),
	)
	if m.saveErr != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, content, "", tuistyle.StatusFailed.Render(m.saveErr))
	}
	return tuistyle.OverlayBox.Render(content)
}

func (m *Model) option(theme usersettings.Theme, label string) string {
	if m.selected == theme {
		return tuistyle.FocusedOption.Render("> " + label)
	}
	return "  " + label
}

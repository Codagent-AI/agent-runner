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
	theme    usersettings.Theme
	backend  usersettings.AutonomousBackend
	cursor   int
	saveErr  string

	save func(usersettings.Settings) error
	path func() (string, error)
}

type field struct {
	label   string
	options []option
}

type option struct {
	label   string
	theme   usersettings.Theme
	backend usersettings.AutonomousBackend
}

var fields = []field{
	{
		label: "Theme",
		options: []option{
			{label: "Light", theme: usersettings.ThemeLight},
			{label: "Dark", theme: usersettings.ThemeDark},
		},
	},
	{
		label: "Autonomous Backend",
		options: []option{
			{label: "Headless", backend: usersettings.BackendHeadless},
			{label: "Interactive", backend: usersettings.BackendInteractive},
			{label: "Interactive for Claude", backend: usersettings.BackendInteractiveClaude},
		},
	},
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
//
//nolint:gocritic // Settings is a small value object and callers already pass it by value throughout the TUI.
func New(settings usersettings.Settings, opts ...Option) *Model {
	theme := settings.Theme
	if theme != usersettings.ThemeDark && theme != usersettings.ThemeLight {
		theme = usersettings.ThemeLight
	}
	backend := settings.AutonomousBackend
	if backend != usersettings.BackendInteractive && backend != usersettings.BackendInteractiveClaude && backend != usersettings.BackendHeadless {
		backend = usersettings.BackendHeadless
	}
	m := &Model{
		settings: settings,
		theme:    theme,
		backend:  backend,
		cursor:   initialCursor(settings.AutonomousBackend != "", theme, backend),
		save:     usersettings.Save,
		path:     usersettings.Path,
	}
	for _, opt := range opts {
		opt(m)
	}
	m.applyCursor()
	return m
}

// SelectedTheme returns the theme currently selected by the option cursor.
func (m *Model) SelectedTheme() usersettings.Theme {
	return m.theme
}

// SelectedAutonomousBackend returns the backend currently selected by the option cursor.
func (m *Model) SelectedAutonomousBackend() usersettings.AutonomousBackend {
	return m.backend
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
		cmd := m.saveSelected()
		return m, cmd
	case "esc":
		return m, func() tea.Msg { return CancelledMsg{} }
	case "ctrl+c":
		return m, nil
	}
	return m, nil
}

func (m *Model) move(delta int) {
	total := totalOptions()
	if total == 0 {
		return
	}
	m.cursor = (m.cursor + delta + total) % total
	m.applyCursor()
	m.saveErr = ""
}

func (m *Model) saveSelected() tea.Cmd {
	next := m.settings
	next.Theme = m.theme
	next.AutonomousBackend = m.backend
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
	lines := []string{header, ""}
	idx := 0
	for fieldIdx, field := range fields {
		if fieldIdx > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, tuistyle.LabelStyle.Render(field.label))
		for _, opt := range field.options {
			lines = append(lines, m.option(idx, opt))
			idx++
		}
	}
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	if m.saveErr != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, content, "", tuistyle.StatusFailed.Render(m.saveErr))
	}
	return tuistyle.OverlayBox.Render(content)
}

func (m *Model) option(idx int, opt option) string {
	if m.cursor == idx {
		return tuistyle.FocusedOption.Render("> " + opt.label)
	}
	if opt.theme != "" && opt.theme == m.theme {
		return "> " + opt.label
	}
	if opt.backend != "" && opt.backend == m.backend {
		return "> " + opt.label
	}
	return "  " + opt.label
}

func totalOptions() int {
	total := 0
	for _, field := range fields {
		total += len(field.options)
	}
	return total
}

func initialCursor(hasPersistedBackend bool, theme usersettings.Theme, backend usersettings.AutonomousBackend) int {
	if hasPersistedBackend {
		if idx, ok := cursorForBackend(backend); ok {
			return idx
		}
	}
	if idx, ok := cursorForTheme(theme); ok {
		return idx
	}
	return 0
}

func cursorForTheme(theme usersettings.Theme) (int, bool) {
	idx := 0
	for _, field := range fields {
		for _, opt := range field.options {
			if opt.theme == theme {
				return idx, true
			}
			idx++
		}
	}
	return 0, false
}

func cursorForBackend(backend usersettings.AutonomousBackend) (int, bool) {
	idx := 0
	for _, field := range fields {
		for _, opt := range field.options {
			if opt.backend == backend {
				return idx, true
			}
			idx++
		}
	}
	return 0, false
}

func (m *Model) applyCursor() {
	idx := 0
	for _, field := range fields {
		for _, opt := range field.options {
			if idx == m.cursor {
				if opt.theme != "" {
					m.theme = opt.theme
				}
				if opt.backend != "" {
					m.backend = opt.backend
				}
				return
			}
			idx++
		}
	}
}

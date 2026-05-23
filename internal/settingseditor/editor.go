// Package settingseditor implements the in-session user settings overlay.
package settingseditor

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codagent/agent-runner/internal/tuistyle"
	"github.com/codagent/agent-runner/internal/usersettings"
)

// SavedMsg is emitted after the editor successfully persists a settings change.
// The editor stays open after a save; the embedder applies runtime-affecting
// changes (e.g., theme) but SHOULD NOT close the editor on this message.
type SavedMsg struct {
	Settings usersettings.Settings
}

// CancelledMsg is emitted when the user closes the editor (Esc).
// The embedder SHOULD close the editor on this message. All changes made
// during the session were already persisted via SavedMsg events.
type CancelledMsg struct{}

// Model is the bubbletea submodel for editing user settings.
//
// The editor presents every editable setting as a labeled row with its
// current value. A row cursor moves between rows; Tab/Space/Enter cycles
// the cursor row's value to the next option (wrapping after the last), and
// each cycle persists immediately. Esc closes the editor.
type Model struct {
	settings       usersettings.Settings
	theme          usersettings.Theme
	backend        usersettings.AutonomousBackend
	permissionMode usersettings.AutonomousPermissionMode

	cursor  int
	saveErr string

	save func(usersettings.Settings) error
	path func() (string, error)
}

type field struct {
	label   string
	options []option
}

type option struct {
	label          string
	description    string
	theme          usersettings.Theme
	backend        usersettings.AutonomousBackend
	permissionMode usersettings.AutonomousPermissionMode
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
	{
		label: "Autonomous Permission Mode",
		options: []option{
			{
				label:          "Conservative",
				description:    "Each CLI's default permission flags. Some commands may not work without separately granting the CLI tool access.",
				permissionMode: usersettings.PermissionModeConservative,
			},
			{
				label:          "YOLO",
				description:    "Bypass per-command approval for shell, file, and network actions. Recommended only inside an external sandbox such as Docker.",
				permissionMode: usersettings.PermissionModeYOLO,
			},
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
	permissionMode := usersettings.EffectiveAutonomousPermissionMode(settings.AutonomousPermissionMode)
	m := &Model{
		settings:       settings,
		theme:          theme,
		backend:        backend,
		permissionMode: permissionMode,
		save:           usersettings.Save,
		path:           usersettings.Path,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// SelectedTheme returns the persisted theme value.
func (m *Model) SelectedTheme() usersettings.Theme {
	return m.theme
}

// SelectedAutonomousBackend returns the persisted backend value.
func (m *Model) SelectedAutonomousBackend() usersettings.AutonomousBackend {
	return m.backend
}

// SelectedAutonomousPermissionMode returns the persisted permission mode value.
func (m *Model) SelectedAutonomousPermissionMode() usersettings.AutonomousPermissionMode {
	return m.permissionMode
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
	case "up", "k":
		m.cursor = (m.cursor - 1 + len(fields)) % len(fields)
		m.saveErr = ""
	case "down", "j":
		m.cursor = (m.cursor + 1) % len(fields)
		m.saveErr = ""
	case "tab", " ", "right", "l", "enter":
		cmd := m.cycle(1)
		return m, cmd
	case "shift+tab", "left", "h":
		cmd := m.cycle(-1)
		return m, cmd
	case "esc":
		return m, func() tea.Msg { return CancelledMsg{} }
	case "ctrl+c":
		return m, nil
	}
	return m, nil
}

// cycle advances the cursor row's value to the next option (with wrap) and
// persists immediately. delta is +1 to cycle forward, -1 to cycle backward.
// On save error, the persisted value is left unchanged and the error is shown
// inline; the next cycle attempt is independent.
func (m *Model) cycle(delta int) tea.Cmd {
	f := fields[m.cursor]
	currentIdx := m.currentOptionIndex(m.cursor)
	nextIdx := (currentIdx + delta + len(f.options)) % len(f.options)
	opt := f.options[nextIdx]

	next := m.settings
	next.Theme = m.theme
	next.AutonomousBackend = m.backend
	next.AutonomousPermissionMode = m.permissionMode
	if opt.theme != "" {
		next.Theme = opt.theme
	}
	if opt.backend != "" {
		next.AutonomousBackend = opt.backend
	}
	if opt.permissionMode != "" {
		next.AutonomousPermissionMode = opt.permissionMode
	}

	if err := m.save(next); err != nil {
		m.saveErr = fmt.Sprintf("Failed to save %s: %v", m.settingsPath(), err)
		return nil
	}
	m.settings = next
	m.theme = next.Theme
	m.backend = next.AutonomousBackend
	m.permissionMode = next.AutonomousPermissionMode
	m.saveErr = ""
	return func() tea.Msg { return SavedMsg{Settings: next} }
}

// currentOptionIndex returns the index of the option in the given field whose
// value matches the editor's currently persisted value for that field.
func (m *Model) currentOptionIndex(fieldIdx int) int {
	f := fields[fieldIdx]
	for i, opt := range f.options {
		if opt.theme != "" && opt.theme == m.theme {
			return i
		}
		if opt.backend != "" && opt.backend == m.backend {
			return i
		}
		if opt.permissionMode != "" && opt.permissionMode == m.permissionMode {
			return i
		}
	}
	return 0
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

// contentWidth is the visible-column width of every row the editor renders
// (cursor + label + gap + value, and the legend line). The bordered overlay
// fits to this width plus its own padding and border.
const contentWidth = 60

func (m *Model) View() string {
	header := tuistyle.HeaderStyle.Render("Settings")
	lines := []string{header, ""}
	for fieldIdx, f := range fields {
		lines = append(lines, m.fieldLine(fieldIdx, f))
	}
	if desc := m.currentDescription(); desc != "" {
		lines = append(lines, "")
		for _, wrapped := range wrapLines(desc, contentWidth-2) {
			lines = append(lines, "  "+wrapped)
		}
	}
	lines = append(lines, "", tuistyle.HelpStyle.Render("↑↓ navigate · Tab/Space cycle · Esc close"))
	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	if m.saveErr != "" {
		content = lipgloss.JoinVertical(lipgloss.Left, content, "", tuistyle.StatusFailed.Render(m.saveErr))
	}
	return tuistyle.OverlayBox.Render(content)
}

func (m *Model) fieldLine(fieldIdx int, f field) string {
	cursorPrefix := "  "
	if fieldIdx == m.cursor {
		cursorPrefix = tuistyle.FocusedOption.Render("▶ ")
	}
	label := tuistyle.LabelStyle.Render(f.label)
	value := m.currentOption(fieldIdx).label
	// Compute padding so the value sits right-aligned at column contentWidth.
	// "▶ " and "  " both render as 2 visible columns regardless of style.
	used := 2 + lipgloss.Width(label) + lipgloss.Width(value)
	padding := max(contentWidth-used, 2)
	return cursorPrefix + label + strings.Repeat(" ", padding) + value
}

// wrapLines performs a simple word wrap so the description fits within the
// editor's content width without overflowing the overlay.
func wrapLines(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var (
		lines   []string
		current strings.Builder
	)
	for _, word := range words {
		if current.Len() == 0 {
			current.WriteString(word)
			continue
		}
		if current.Len()+1+len(word) <= width {
			current.WriteByte(' ')
			current.WriteString(word)
			continue
		}
		lines = append(lines, current.String())
		current.Reset()
		current.WriteString(word)
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func (m *Model) currentOption(fieldIdx int) option {
	return fields[fieldIdx].options[m.currentOptionIndex(fieldIdx)]
}

// currentDescription returns the description copy for the cursor row's current
// value, or empty string if the cursor's value has none.
func (m *Model) currentDescription() string {
	return m.currentOption(m.cursor).description
}

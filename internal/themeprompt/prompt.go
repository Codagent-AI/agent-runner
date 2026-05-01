// Package themeprompt implements the first-launch TUI theme chooser.
package themeprompt

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codagent/agent-runner/internal/tuistyle"
	"github.com/codagent/agent-runner/internal/usersettings"
)

type model struct {
	selected  usersettings.Theme
	confirmed bool
	cancelled bool
	width     int
	height    int
}

func newModel(darkDetected bool) model {
	selected := usersettings.ThemeLight
	if darkDetected {
		selected = usersettings.ThemeDark
	}
	return model{selected: selected}
}

func (m model) selectedTheme() usersettings.Theme {
	return m.selected
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "left":
			m.selected = usersettings.ThemeLight
			return m, nil
		case "down", "right":
			m.selected = usersettings.ThemeDark
			return m, nil
		case "enter":
			m.confirmed = true
			return m, tea.Quit
		case "esc", "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) View() string {
	option := func(theme usersettings.Theme, label string) string {
		prefix := "  "
		style := tuistyle.NormalStyle
		if m.selected == theme {
			prefix = "> "
			style = tuistyle.SelectedStyle.Bold(true)
		}
		return style.Render(prefix + label)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		tuistyle.HeaderStyle.Render("Choose TUI theme"),
		"",
		option(usersettings.ThemeLight, "Light"),
		option(usersettings.ThemeDark, "Dark"),
	)

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tuistyle.AccentCyan).
		Padding(1, 2).
		Render(content)

	if m.width <= 0 || m.height <= 0 {
		return card
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

func Prompt() (usersettings.Theme, bool, error) {
	p := tea.NewProgram(newModel(lipgloss.HasDarkBackground()), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return "", false, err
	}
	m, ok := final.(model)
	if !ok {
		return "", false, fmt.Errorf("unexpected theme prompt model %T", final)
	}
	if m.cancelled || !m.confirmed {
		return "", false, nil
	}
	return m.selectedTheme(), true, nil
}

// Package splash renders an animated logo reveal before the first-run setup.
package splash

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/tuistyle"
)

const (
	sweepDuration = 800 * time.Millisecond
	holdDuration  = 900 * time.Millisecond
	tickInterval  = 16 * time.Millisecond // ~60 fps
)

type tickMsg struct{}
type holdDoneMsg struct{}

type model struct {
	width      int
	height     int
	revealCols int
	totalWidth int
	holding    bool
	done       bool
	startTime  time.Time
}

// Run displays the animated block logo and blocks until complete. Any
// keypress skips the animation immediately.
func Run() error {
	m := &model{
		width:      80,
		height:     24,
		totalWidth: tuistyle.LogoBlockWidth(),
		startTime:  time.Now(),
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *model) Init() tea.Cmd {
	return tick()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		m.done = true
		return m, tea.Quit

	case tickMsg:
		if m.done {
			return m, tea.Quit
		}
		elapsed := time.Since(m.startTime)
		progress := float64(elapsed) / float64(sweepDuration)
		if progress >= 1 {
			m.revealCols = m.totalWidth
			if !m.holding {
				m.holding = true
				return m, holdTimer()
			}
			return m, nil
		}
		// Ease-out: start fast, decelerate at the end.
		eased := 1 - (1-progress)*(1-progress)
		m.revealCols = int(eased * float64(m.totalWidth))
		return m, tick()

	case holdDoneMsg:
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *model) View() string {
	logo := tuistyle.RenderLogoBlockRevealed(m.revealCols)
	logoLines := strings.Split(logo, "\n")

	logoH := len(logoLines)
	topPad := max((m.height-logoH)/2, 0)

	var b strings.Builder
	for range topPad {
		b.WriteString("\n")
	}
	leftPad := max((m.width-m.totalWidth)/2, 0)
	padStr := strings.Repeat(" ", leftPad)
	for _, line := range logoLines {
		b.WriteString(padStr + line + "\n")
	}
	return b.String()
}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(_ time.Time) tea.Msg { return tickMsg{} })
}

func holdTimer() tea.Cmd {
	return tea.Tick(holdDuration, func(_ time.Time) tea.Msg { return holdDoneMsg{} })
}

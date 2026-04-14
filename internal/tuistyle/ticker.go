package tuistyle

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// RefreshInterval is the cadence at which TUIs poll on-disk state.
const RefreshInterval = 2 * time.Second

// PulseInterval is the cadence at which TUIs advance pulse animation phase.
const PulseInterval = 50 * time.Millisecond

// RefreshMsg is emitted every RefreshInterval to prompt a data reload.
type RefreshMsg struct{}

// PulseMsg is emitted every PulseInterval to advance pulse animation.
type PulseMsg struct{}

// DoRefresh schedules the next RefreshMsg.
func DoRefresh() tea.Cmd {
	return tea.Every(RefreshInterval, func(time.Time) tea.Msg {
		return RefreshMsg{}
	})
}

// DoPulse schedules the next PulseMsg.
func DoPulse() tea.Cmd {
	return tea.Every(PulseInterval, func(time.Time) tea.Msg {
		return PulseMsg{}
	})
}

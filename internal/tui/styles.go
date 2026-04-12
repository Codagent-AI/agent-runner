package tui

import "github.com/charmbracelet/lipgloss"

var (
	inactiveAmber = lipgloss.AdaptiveColor{Dark: "#f0a830", Light: "#b45309"}
	completedGray = lipgloss.AdaptiveColor{Dark: "#4b5a6e", Light: "#9ca3af"}
	accentCyan    = lipgloss.AdaptiveColor{Dark: "#5ce0d8", Light: "#0891b2"}
	bodyText      = lipgloss.AdaptiveColor{Dark: "#c9d1d9", Light: "#1f2937"}
	dimText       = lipgloss.AdaptiveColor{Dark: "#4b5a6e", Light: "#9ca3af"}
	selectedText  = lipgloss.AdaptiveColor{Dark: "#ffffff", Light: "#111827"}
)

var (
	headerStyle    = lipgloss.NewStyle().Foreground(accentCyan).Bold(true)
	activeTabStyle = lipgloss.NewStyle().Foreground(accentCyan).Bold(true).Underline(true)
	dimTabStyle    = lipgloss.NewStyle().Foreground(dimText)
	cursorStyle    = lipgloss.NewStyle().Foreground(accentCyan)
	selectedStyle  = lipgloss.NewStyle().Foreground(selectedText)
	normalStyle    = lipgloss.NewStyle().Foreground(bodyText)
	dimStyle       = lipgloss.NewStyle().Foreground(dimText)
	statusInactive = lipgloss.NewStyle().Foreground(inactiveAmber)
	statusDone     = lipgloss.NewStyle().Foreground(completedGray)
	helpStyle      = lipgloss.NewStyle().Foreground(dimText)
	pathStyle      = lipgloss.NewStyle().Foreground(dimText)
)

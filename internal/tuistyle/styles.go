// Package tuistyle provides styles, formatters, and tickers shared by the
// list and run-view TUIs.
package tuistyle

import "github.com/charmbracelet/lipgloss"

// Adaptive colors used across TUI screens.
var (
	InactiveAmber = lipgloss.AdaptiveColor{Dark: "#f0a830", Light: "#b45309"}
	CompletedGray = lipgloss.AdaptiveColor{Dark: "#4b5a6e", Light: "#9ca3af"}
	AccentCyan    = lipgloss.AdaptiveColor{Dark: "#5ce0d8", Light: "#0891b2"}
	BodyText      = lipgloss.AdaptiveColor{Dark: "#c9d1d9", Light: "#1f2937"}
	DimText       = lipgloss.AdaptiveColor{Dark: "#4b5a6e", Light: "#9ca3af"}
	SelectedText  = lipgloss.AdaptiveColor{Dark: "#ffffff", Light: "#111827"}
	FailedRed     = lipgloss.AdaptiveColor{Dark: "#f87171", Light: "#dc2626"}
)

// Shared style instances.
var (
	HeaderStyle    = lipgloss.NewStyle().Foreground(AccentCyan).Bold(true)
	ActiveTabStyle = lipgloss.NewStyle().Foreground(AccentCyan).Bold(true).Underline(true)
	DimTabStyle    = lipgloss.NewStyle().Foreground(DimText)
	CursorStyle    = lipgloss.NewStyle().Foreground(AccentCyan)
	SelectedStyle  = lipgloss.NewStyle().Foreground(SelectedText)
	NormalStyle    = lipgloss.NewStyle().Foreground(BodyText)
	DimStyle       = lipgloss.NewStyle().Foreground(DimText)
	StatusInactive = lipgloss.NewStyle().Foreground(InactiveAmber)
	StatusDone     = lipgloss.NewStyle().Foreground(CompletedGray)
	StatusFailed   = lipgloss.NewStyle().Foreground(FailedRed)
	HelpStyle      = lipgloss.NewStyle().Foreground(DimText)
	PathStyle      = lipgloss.NewStyle().Foreground(DimText)
)

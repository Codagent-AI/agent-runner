// Package tuistyle provides styles, formatters, and tickers shared by the
// list and run-view TUIs.
package tuistyle

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Adaptive colors used across TUI screens.
var (
	InactiveAmber = lipgloss.AdaptiveColor{Dark: "#f0a830", Light: "#b45309"}
	CompletedGray = lipgloss.AdaptiveColor{Dark: "#4b5a6e", Light: "#9ca3af"}
	AccentCyan    = lipgloss.AdaptiveColor{Dark: "#5ce0d8", Light: "#0891b2"}
	SuccessGreen  = lipgloss.AdaptiveColor{Dark: "#4ade80", Light: "#16a34a"}
	BodyText      = lipgloss.AdaptiveColor{Dark: "#c9d1d9", Light: "#1f2937"}
	DimText       = lipgloss.AdaptiveColor{Dark: "#4b5a6e", Light: "#9ca3af"}
	SelectedText  = lipgloss.AdaptiveColor{Dark: "#ffffff", Light: "#111827"}
	FailedRed     = lipgloss.AdaptiveColor{Dark: "#f87171", Light: "#dc2626"}
)

// BlinkOffStyle is the "off" half of the in-progress blink. It intentionally
// uses a non-adaptive bright white (ANSI color 15) rather than an
// AdaptiveColor because lipgloss's background-color detection is unreliable
// inside bubbletea's alt-screen — when it misdetects a dark terminal as
// light, an adaptive "white" resolves to near-black (#111827) and the blink
// off-phase appears invisible. Bright-white-15 is rendered as white by every
// dark-themed terminal in practice and is still legible on light themes.
var BlinkOffStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

// Shared style instances.
var (
	HeaderStyle       = lipgloss.NewStyle().Foreground(AccentCyan).Bold(true)
	ActiveTabStyle    = lipgloss.NewStyle().Foreground(AccentCyan).Bold(true).Underline(true)
	DimTabStyle       = lipgloss.NewStyle().Foreground(DimText)
	CursorStyle       = lipgloss.NewStyle().Foreground(AccentCyan)
	SelectedStyle     = lipgloss.NewStyle().Foreground(SelectedText)
	NormalStyle       = lipgloss.NewStyle().Foreground(BodyText)
	DimStyle          = lipgloss.NewStyle().Foreground(DimText)
	StatusInactive    = lipgloss.NewStyle().Foreground(InactiveAmber)
	StatusSuccess     = lipgloss.NewStyle().Foreground(SuccessGreen)
	StatusDone        = lipgloss.NewStyle().Foreground(CompletedGray)
	StatusFailed      = lipgloss.NewStyle().Foreground(FailedRed)
	LabelStyle        = lipgloss.NewStyle().Foreground(InactiveAmber)
	SectionStyle      = lipgloss.NewStyle().Foreground(SuccessGreen).Bold(true)
	ColumnHeader      = lipgloss.NewStyle().Foreground(AccentCyan).Underline(true)
	AccentStyle       = lipgloss.NewStyle().Foreground(AccentCyan)
	DividerStyle      = lipgloss.NewStyle().Foreground(DimText)
	DetailHeaderStyle = lipgloss.NewStyle().Foreground(AccentCyan).Bold(true).Underline(true)
	InsetBarStyle     = lipgloss.NewStyle().Foreground(SuccessGreen)
	HelpStyle         = lipgloss.NewStyle().Foreground(DimText)
	PathStyle         = lipgloss.NewStyle().Foreground(DimText)
)

// BreadcrumbSeparator is the canonical separator glyph between breadcrumb
// segments on every TUI screen. Render with AccentStyle so both screens
// share the same color treatment.
const BreadcrumbSeparator = " › "

// RenderRule returns a horizontal divider line inset 2 columns from each
// edge, sized to the given terminal width. Used by both TUI screens as the
// separator above bottom-pinned help bars and below top chrome.
func RenderRule(termWidth int) string {
	w := termWidth
	if w <= 4 {
		w = 60
	}
	return "  " + DividerStyle.Render(strings.Repeat("─", w-4))
}

// Package tuistyle provides styles, formatters, and tickers shared by the
// list and run-view TUIs.
package tuistyle

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Adaptive colors used across TUI screens.
var (
	InactiveAmber      = lipgloss.AdaptiveColor{Dark: "#f0a830", Light: "#b45309"}
	CompletedGray      = lipgloss.AdaptiveColor{Dark: "#8b949e", Light: "#6b7280"}
	AccentCyan         = lipgloss.AdaptiveColor{Dark: "#5ce0d8", Light: "#0891b2"}
	AccentMagenta      = lipgloss.AdaptiveColor{Dark: "#e879f9", Light: "#c026d3"}
	SuccessGreen       = lipgloss.AdaptiveColor{Dark: "#4ade80", Light: "#15803d"}
	BodyText           = lipgloss.AdaptiveColor{Dark: "#c9d1d9", Light: "#1f2937"}
	DimText            = lipgloss.AdaptiveColor{Dark: "#b3b3b3", Light: "#525252"}
	SelectedText       = lipgloss.AdaptiveColor{Dark: "#ffffff", Light: "#111827"}
	ButtonOnAccentText = lipgloss.AdaptiveColor{Dark: "#111827", Light: "#111827"}
	FailedRed          = lipgloss.AdaptiveColor{Dark: "#f87171", Light: "#dc2626"}
)

// BlinkHidden returns a blank string the same visual width as s: used as the
// "off" half of the in-progress blink. Earlier attempts recolored the glyph
// (AdaptiveColor white → resolves to near-black when lipgloss misdetects the
// background inside bubbletea's alt-screen; ANSI-15 → remapped to a dark
// color by some light-theme palettes). Hiding the glyph sidesteps all of
// that — the blink becomes visible/invisible, not green/some-broken-color.
func BlinkHidden(s string) string {
	return strings.Repeat(" ", lipgloss.Width(s))
}

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
	SectionStyle      = lipgloss.NewStyle().Foreground(SuccessGreen)
	ColumnHeader      = lipgloss.NewStyle().Foreground(AccentCyan).Underline(true)
	AccentStyle       = lipgloss.NewStyle().Foreground(AccentCyan)
	ButtonStyle       = lipgloss.NewStyle().Foreground(BodyText)
	FocusedButton     = lipgloss.NewStyle().Foreground(ButtonOnAccentText).Background(AccentCyan)
	FocusedOption     = lipgloss.NewStyle().Foreground(AccentCyan).Bold(true)
	DividerStyle      = lipgloss.NewStyle().Foreground(DimText)
	DetailHeaderStyle = lipgloss.NewStyle().Foreground(AccentCyan).Bold(true).Underline(true)
	InsetBarStyle     = lipgloss.NewStyle().Foreground(SuccessGreen)
	HelpStyle         = lipgloss.NewStyle().Foreground(DimText)
	PathStyle         = lipgloss.NewStyle().Foreground(DimText)
)

const (
	FocusedSelectorPrefix  = "▶"
	SelectedSelectorPrefix = "•"
)

// GroupColors cycles through the three primary TUI colors for visual grouping.
var GroupColors = []lipgloss.AdaptiveColor{AccentCyan, InactiveAmber, SuccessGreen}

// BreadcrumbSeparator is the canonical separator glyph between breadcrumb
// segments on every TUI screen. Render with AccentStyle so both screens
// share the same color treatment.
const BreadcrumbSeparator = " › "

// ScreenMargin is the shared outer left margin for TUI chrome.
const ScreenMargin = " "

// RenderRule returns a horizontal divider with a 1-column left margin,
// sized to the given terminal width. Used by both TUI screens as the
// separator above bottom-pinned help bars and below top chrome.
func RenderRule(termWidth int) string {
	w := termWidth
	if w <= 2 {
		w = 60
	}
	return ScreenMargin + DividerStyle.Render(strings.Repeat("─", w-2))
}

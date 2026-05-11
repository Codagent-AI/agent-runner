package tuistyle

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// RenderButton renders a terminal button label with the shared UI-step style.
func RenderButton(label string, focused bool) string {
	button := "[ " + label + " ]"
	if focused {
		return FocusedButton.Render(button)
	}
	return ButtonStyle.Render(button)
}

// RenderButtonRow renders buttons on one row. When width is positive, buttons
// are distributed across the row; otherwise they are separated by two spaces.
func RenderButtonRow(labels []string, focused, width int) string {
	if len(labels) == 0 {
		return ""
	}

	rendered := make([]string, len(labels))
	totalWidth := 0
	for i, label := range labels {
		raw := "[ " + label + " ]"
		rendered[i] = ButtonStyle.Render(raw)
		if i == focused {
			rendered[i] = FocusedButton.Render(raw)
		}
		totalWidth += runewidth.StringWidth(raw)
	}

	if len(rendered) == 1 {
		return rendered[0]
	}

	if width <= 0 {
		return strings.Join(rendered, "  ")
	}

	// Leave spare columns for callers that render inside width-constrained
	// lipgloss boxes; exact-width lines can otherwise wrap at the boundary.
	width = max(0, width-4)
	spaceBudget := width - totalWidth
	if spaceBudget < 2*(len(rendered)-1) {
		return strings.Join(rendered, "  ")
	}

	gaps := len(rendered) - 1
	baseGap := spaceBudget / gaps
	extra := spaceBudget % gaps
	var b strings.Builder
	for i, button := range rendered {
		if i > 0 {
			gap := baseGap
			if i <= extra {
				gap++
			}
			b.WriteString(strings.Repeat(" ", gap))
		}
		b.WriteString(button)
	}
	return b.String()
}

// RenderStepIndicator renders compact wizard progress text plus a small bar.
func RenderStepIndicator(current, total, width int) string {
	if current <= 0 || total <= 0 {
		return ""
	}
	if current > total {
		current = total
	}

	label := fmt.Sprintf("Step %d of %d", current, total)
	if width <= 0 {
		return DimStyle.Render(label)
	}

	barWidth := min(24, width-runewidth.StringWidth(label)-2)
	if barWidth < 6 {
		return DimStyle.Render(label)
	}

	filled := (barWidth * current) / total
	if filled == 0 {
		filled = 1
	}
	bar := AccentStyle.Render(strings.Repeat("━", filled)) + DimStyle.Render(strings.Repeat("─", barWidth-filled))
	return DimStyle.Render(label+"  ") + bar
}

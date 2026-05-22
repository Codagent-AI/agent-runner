package tuistyle

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var (
	amberStyle = lipgloss.NewStyle().Foreground(InactiveAmber)
	cyanStyle  = lipgloss.NewStyle().Foreground(AccentCyan)
)

var logoCompactAgent = [3]string{
	"█▀▀█ █▀▀▀ █▀▀ █▀▀▄ ▀▀█▀▀",
	"█▄▄█ █ ▀█ █▀▀ █  █   █  ",
	"█  █ ▀▀▀▀ ▀▀▀ ▀  ▀   ▀  ",
}

var logoCompactRunner = [3]string{
	" █▀▀█ █  █ █▀▀▄ █▀▀▄ █▀▀ █▀▀█",
	" █▄▄▀ █  █ █  █ █  █ █▀▀ █▄▄▀",
	" █  █ ▀▀▀▀ ▀  ▀ ▀  ▀ ▀▀▀ █  █",
}

var logoBlockAgent = [5]string{
	" █████  ██████  ███████ ███   ██ ████████",
	"██   ██ ██      ██      ████  ██    ██   ",
	"███████ ██  ███ █████   ██ ██ ██    ██   ",
	"██   ██ ██   ██ ██      ██  ████    ██   ",
	"██   ██  ██████ ███████ ██   ███    ██   ",
}

var logoBlockRunner = [5]string{
	" ██████  ██    ██ ███   ██ ███   ██ ███████ ██████ ",
	" ██   ██ ██    ██ ████  ██ ████  ██ ██      ██   ██",
	" ██████  ██    ██ ██ ██ ██ ██ ██ ██ █████   ██████ ",
	" ██   ██ ██    ██ ██  ████ ██  ████ ██      ██   ██",
	" ██   ██  ██████  ██   ███ ██   ███ ███████ ██   ██",
}

// LogoCompactWidth returns the visual width of the compact logo.
func LogoCompactWidth() int {
	return runewidth.StringWidth(logoCompactAgent[0]) + runewidth.StringWidth(logoCompactRunner[0])
}

// RenderLogoCompact returns the 3-line compact logo with Agent in amber and
// Runner in cyan. Lines are joined with newlines.
func RenderLogoCompact() string {
	var lines [3]string
	for i := range 3 {
		lines[i] = amberStyle.Render(logoCompactAgent[i]) + cyanStyle.Render(logoCompactRunner[i])
	}
	return strings.Join(lines[:], "\n")
}

// LogoBlockHeight returns the number of lines in the block logo.
func LogoBlockHeight() int { return 5 }

// LogoBlockWidth returns the visual width of one line of the block logo.
func LogoBlockWidth() int {
	return runewidth.StringWidth(logoBlockAgent[0]) + runewidth.StringWidth(logoBlockRunner[0])
}

// RenderLogoBlock returns the 5-line block logo with Agent in amber and
// Runner in cyan. Lines are joined with newlines.
func RenderLogoBlock() string {
	var lines [5]string
	for i := range 5 {
		lines[i] = amberStyle.Render(logoBlockAgent[i]) + cyanStyle.Render(logoBlockRunner[i])
	}
	return strings.Join(lines[:], "\n")
}

// RenderLogoBlockRevealed returns the block logo with only the first
// revealCols visual columns visible. Used for the sweep animation.
func RenderLogoBlockRevealed(revealCols int) string {
	totalWidth := LogoBlockWidth()
	if revealCols >= totalWidth {
		return RenderLogoBlock()
	}
	if revealCols <= 0 {
		return strings.Repeat("\n", 4)
	}
	var lines [5]string
	for i := range 5 {
		agentW := runewidth.StringWidth(logoBlockAgent[i])
		if revealCols <= agentW {
			visible := truncateToWidth(logoBlockAgent[i], revealCols)
			lines[i] = amberStyle.Render(visible)
		} else {
			runnerVisible := truncateToWidth(logoBlockRunner[i], revealCols-agentW)
			lines[i] = amberStyle.Render(logoBlockAgent[i]) + cyanStyle.Render(runnerVisible)
		}
	}
	return strings.Join(lines[:], "\n")
}

// logoMinWidth is the minimum terminal width at which the compact logo
// is shown in the chrome.
const logoMinWidth = 80

// RenderChromeWithLogo produces a 3-row top chrome block: leftContent on
// row 1, blank on row 2, rule on row 3 — with the compact logo
// right-aligned across all three rows. Falls back to a plain layout
// (content + blank + full-width rule) when the terminal is too narrow.
func RenderChromeWithLogo(leftContent string, leftWidth, termWidth int) string {
	logoW := LogoCompactWidth()
	if termWidth < logoMinWidth || leftWidth+logoW+2 > termWidth {
		return leftContent + "\n\n" + RenderRule(termWidth)
	}

	logoLines := strings.Split(RenderLogoCompact(), "\n")

	ruleArg := max(termWidth-logoW, 4)
	rule := RenderRule(ruleArg)
	ruleVisW := ruleArg - 1

	leftContents := [3]string{leftContent, "", rule}
	leftWidths := [3]int{leftWidth, 0, ruleVisW}

	var b strings.Builder
	for i, logoLine := range logoLines {
		left := leftContents[i]
		leftW := leftWidths[i]
		pad := max(termWidth-leftW-logoW, 1)
		b.WriteString(left + strings.Repeat(" ", pad) + logoLine)
		if i < len(logoLines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// RenderHelpWithCwd renders a help bar with keyboard shortcuts on the
// left and the working directory right-aligned on the same line. The
// cwd is hidden if it doesn't fit.
func RenderHelpWithCwd(helpText, cwd string, termWidth int) string {
	left := ScreenMargin + HelpStyle.Render(helpText)
	leftW := len(ScreenMargin) + runewidth.StringWidth(helpText)

	if termWidth <= 0 || cwd == "" {
		return left
	}
	cwdW := runewidth.StringWidth(cwd)
	if leftW+cwdW+2 > termWidth {
		return left
	}
	pad := termWidth - leftW - cwdW
	return left + strings.Repeat(" ", pad) + PathStyle.Render(cwd)
}

func truncateToWidth(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	w := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > maxW {
			return s[:i]
		}
		w += rw
	}
	return s
}

package runview

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/tuistyle"
)

var (
	shellGlyphStyle = lipgloss.NewStyle().Foreground(tuistyle.InactiveAmber)
	subwfGlyphStyle = lipgloss.NewStyle().Foreground(tuistyle.AccentCyan)
)

func (m *Model) View() string {
	if m.showLegend {
		return m.renderLegend()
	}
	if m.quitConfirming {
		return m.renderQuitConfirm()
	}

	var b strings.Builder

	b.WriteString("\n  ")
	b.WriteString(tuistyle.HeaderStyle.Render("Agent Runner"))
	b.WriteString("\n\n")
	b.WriteString(m.renderBreadcrumb())
	b.WriteString("\n\n")

	b.WriteString(m.renderRule())
	b.WriteString("\n")

	swHeader := m.renderSubWorkflowHeader()
	if swHeader != "" {
		b.WriteString(swHeader)
		b.WriteString("\n")
	}

	children := m.currentChildren()
	if len(children) == 0 {
		b.WriteString("  ")
		b.WriteString(tuistyle.DimStyle.Render("No steps to display."))
		b.WriteString("\n")
	} else {
		b.WriteString(m.renderTwoColumn(children))
	}

	if m.loadErr != "" {
		b.WriteString("\n  ")
		b.WriteString(tuistyle.DimStyle.Render("Error: " + m.loadErr))
	}

	b.WriteString("\n")
	b.WriteString(m.renderRule())
	b.WriteString("\n")
	b.WriteString(m.renderHelpBar())
	b.WriteString("\n")

	return b.String()
}

func (m *Model) renderRule() string {
	return tuistyle.RenderRule(m.termWidth)
}

func (m *Model) renderTwoColumn(children []*StepNode) string {
	rows := m.buildStepRows(children)

	// Cap the list column so one pathologically long row (e.g. an iteration
	// binding value that happens to be a full file path) can't starve the
	// detail pane. Prefer at most ~45% of the terminal for the list.
	listCap := m.termWidth / 2
	if listCap < 30 {
		listCap = 30
	}

	maxRowWidth := 0
	for _, r := range rows {
		w := lipgloss.Width(r)
		if w > maxRowWidth {
			maxRowWidth = w
		}
	}
	if maxRowWidth > listCap {
		maxRowWidth = listCap
		for i, r := range rows {
			if lipgloss.Width(r) > listCap {
				rows[i] = runewidth.Truncate(tuistyle.Sanitize(r), listCap, "…")
			}
		}
	}

	listWidth := maxRowWidth + 4
	// Divider "│ " consumes 2 columns between the panes.
	detailWidth := m.termWidth - listWidth - 2 - 4
	if detailWidth < 20 {
		detailWidth = 20
	}
	m.detailWidth = detailWidth
	divider := tuistyle.DividerStyle.Render("│ ")

	sel := m.selectedNode()
	detail := m.renderDetail(sel)
	detailLines := strings.Split(detail, "\n")

	offset := m.detailOffset
	if offset > len(detailLines)-1 {
		offset = max(0, len(detailLines)-1)
	}
	visibleDetail := detailLines
	if offset > 0 && offset < len(detailLines) {
		visibleDetail = detailLines[offset:]
	}

	bodyHeight := m.bodyHeight()
	if bodyHeight <= 0 {
		bodyHeight = 20
	}

	var b strings.Builder
	for i := 0; i < bodyHeight; i++ {
		leftPart := ""
		if i < len(rows) {
			leftPart = rows[i]
		}
		leftPad := listWidth - lipgloss.Width(leftPart)
		if leftPad < 0 {
			leftPad = 0
		}

		rightPart := ""
		if i < len(visibleDetail) {
			rightPart = fitDetailLine(visibleDetail[i], detailWidth)
		}

		b.WriteString(leftPart)
		b.WriteString(strings.Repeat(" ", leftPad))
		b.WriteString(divider)
		b.WriteString(rightPart)
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Model) buildStepRows(children []*StepNode) []string {
	rows := make([]string, len(children))
	for i, n := range children {
		isSel := i == m.cursor
		rows[i] = m.renderStepRow(n, isSel)
	}
	return rows
}

func (m *Model) renderStepRow(n *StepNode, selected bool) string {
	prefix := "   "
	if selected {
		prefix = tuistyle.CursorStyle.Render("▶") + "  "
	}

	glyph := m.statusGlyph(n)
	name := n.ID
	suffix := ""
	typeSuffix := ""

	switch n.Type {
	case NodeLoop:
		total := m.loopTotal(n)
		if total > 0 {
			suffix = fmt.Sprintf(" (%d/%d)", n.IterationsCompleted, total)
		}
	case NodeIteration:
		name = fmt.Sprintf("iter %d", n.IterationIndex+1)
		if n.BindingValue != "" {
			name += "   " + filepath.Base(n.BindingValue)
		}
	default:
		typeSuffix = typeGlyph(n.Type)
	}

	style := tuistyle.DimStyle
	if selected {
		style = tuistyle.SelectedStyle
	}
	if n.Status == StatusFailed {
		style = tuistyle.StatusFailed
	}

	out := prefix + glyph + "  " + style.Render(name+suffix)
	if typeSuffix != "" {
		out += " " + typeSuffix
	}
	return out
}

func (m *Model) statusGlyph(n *StepNode) string {
	switch n.Status {
	case StatusInProgress:
		if m.active && !n.Aborted {
			t := (math.Sin(m.pulsePhase) + 1) / 2
			c := tuistyle.LerpColor("#4ade80", "#2d8f57", t)
			return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("●")
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80")).Render("●")
	case StatusPending:
		return tuistyle.StatusInactive.Render("○")
	case StatusSuccess:
		return tuistyle.StatusSuccess.Render("✓")
	case StatusFailed:
		return tuistyle.StatusFailed.Render("✗")
	case StatusSkipped:
		return tuistyle.StatusDone.Render("⇥")
	}
	return " "
}

func typeGlyph(t NodeType) string {
	switch t {
	case NodeShell:
		return shellGlyphStyle.Render("$")
	case NodeHeadlessAgent:
		return subwfGlyphStyle.Render("⚙")
	case NodeInteractiveAgent:
		return subwfGlyphStyle.Render("❯")
	case NodeSubWorkflow:
		return subwfGlyphStyle.Render("↳")
	}
	return ""
}

func (m *Model) renderHelpBar() string {
	sel := m.selectedNode()
	var parts []string

	container := m.currentContainer()
	if container != nil && container.Type == NodeLoop {
		parts = append(parts, "↑↓ iteration")
	} else {
		parts = append(parts, "↑↓ step")
	}
	parts = append(parts, "pgup/pgdn scroll")

	if sel != nil {
		switch sel.Type {
		case NodeLoop, NodeSubWorkflow, NodeIteration:
			parts = append(parts, "enter drill")
		case NodeHeadlessAgent, NodeInteractiveAgent:
			if sel.SessionID != "" {
				parts = append(parts, "enter resume")
			}
		}
	}

	if m.selectedNodeHasTruncatedOutput() {
		parts = append(parts, "g load full")
	}

	parts = append(parts, "? legend", "esc back", "q quit")

	return "  " + tuistyle.HelpStyle.Render(strings.Join(parts, "   "))
}

func (m *Model) selectedNodeHasTruncatedOutput() bool {
	n := m.selectedNode()
	if n == nil {
		return false
	}
	if m.loadedFull[n] {
		return false
	}
	if n.Type != NodeShell && n.Type != NodeHeadlessAgent {
		return false
	}
	t := truncateOutput(n.Stdout)
	return t.Truncated
}

// fitDetailLine fits one detail-pane line into width visible columns. If the
// line already fits, it is returned unchanged (preserving any embedded ANSI
// escapes). If it overflows, styling is stripped and the plain text is
// truncated with an ellipsis — truncating the styled form risks cutting an
// ANSI escape in half, which corrupts rendering for everything that follows.
//
// Trailing padding is not emitted: each row ends with a newline that resets
// the terminal cursor to column 0, so column alignment is unaffected.
func fitDetailLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return runewidth.Truncate(tuistyle.Sanitize(s), width, "…")
}

func (m *Model) bodyHeight() int {
	if m.termHeight == 0 {
		return 20
	}
	chrome := 10
	if m.currentContainer() != nil && m.currentContainer().Type == NodeSubWorkflow {
		chrome += 3
	}
	h := m.termHeight - chrome
	if h < 5 {
		return 5
	}
	return h
}

func (m *Model) renderQuitConfirm() string {
	var b strings.Builder
	b.WriteString("\n\n  ")
	b.WriteString(tuistyle.HeaderStyle.Render("Agent Runner"))
	b.WriteString("\n\n")
	b.WriteString("  ")
	b.WriteString(tuistyle.DimStyle.Render("The workflow is still running. Quitting will orphan the"))
	b.WriteString("\n  ")
	b.WriteString(tuistyle.DimStyle.Render("active step — it will continue running in the background."))
	b.WriteString("\n\n  ")
	b.WriteString(tuistyle.NormalStyle.Render("Quit anyway?  "))
	b.WriteString(tuistyle.SelectedStyle.Render("[y]es"))
	b.WriteString(tuistyle.NormalStyle.Render("  "))
	b.WriteString(tuistyle.SelectedStyle.Render("[n]o"))
	b.WriteString("\n")
	return b.String()
}

func (m *Model) renderLegend() string {
	var b strings.Builder
	b.WriteString("\n  ")
	b.WriteString(tuistyle.HeaderStyle.Render("Legend"))
	b.WriteString("\n\n")

	b.WriteString("  ")
	b.WriteString(tuistyle.SelectedStyle.Render("Status Glyphs"))
	b.WriteString("\n\n")
	b.WriteString("  ●  running\n")
	b.WriteString("  ○  pending\n")
	b.WriteString("  ✓  success\n")
	b.WriteString("  ✗  failed\n")
	b.WriteString("  ⇥  skipped\n")

	b.WriteString("\n  ")
	b.WriteString(tuistyle.SelectedStyle.Render("Type Glyphs"))
	b.WriteString("\n\n")
	b.WriteString("  $  shell\n")
	b.WriteString("  ⚙  headless agent\n")
	b.WriteString("  ❯  interactive agent\n")
	b.WriteString("  ↳  sub-workflow\n")

	b.WriteString("\n\n  ")
	b.WriteString(tuistyle.HelpStyle.Render("press ? or esc to dismiss"))
	b.WriteString("\n")

	return b.String()
}

package runview

import (
	"fmt"
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

type renderedStepRow struct {
	text       string
	node       *StepNode
	selectable bool
}

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
	if m.notice != "" {
		b.WriteString("\n  ")
		b.WriteString(tuistyle.DimStyle.Render(m.notice))
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
	renderedRows := m.buildRenderedStepRows(children)
	rows := rowTexts(renderedRows)
	listWidth, rightWidth := twoColumnPaneWidths(m.termWidth, rows)

	divider := tuistyle.DividerStyle.Render("│ ")

	bodyHeight := m.bodyHeight()
	if bodyHeight <= 0 {
		bodyHeight = 20
	}

	// Build log lines for the right pane.
	logLines, _ := buildLogLines(
		children,
		m.pendingSelected(),
		rightWidth,
		m.loadedFull,
		m.pulsePhase,
		m.running,
		m.resolverCfg,
	)

	maxOffset := max(0, len(logLines)-bodyHeight)
	offset := m.logOffset
	if offset > maxOffset {
		offset = maxOffset
	}
	var visibleLines []string
	if offset > 0 && offset <= len(logLines) {
		visibleLines = logLines[offset:]
	} else {
		visibleLines = logLines
	}

	selectedRow := renderedRowIndexForNode(renderedRows, m.selectedNode())
	leftOffset := leftPaneOffset(selectedRow, len(renderedRows), bodyHeight)
	visibleRows := rows
	if leftOffset > 0 && leftOffset <= len(rows) {
		visibleRows = rows[leftOffset:]
	}

	var b strings.Builder
	for i := 0; i < bodyHeight; i++ {
		leftPart := ""
		if i < len(visibleRows) {
			leftPart = visibleRows[i]
		}
		leftPad := listWidth - lipgloss.Width(leftPart)
		if leftPad < 0 {
			leftPad = 0
		}

		rightPart := ""
		if i < len(visibleLines) {
			rightPart = fitDetailLine(visibleLines[i], rightWidth)
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
	return rowTexts(m.buildRenderedStepRows(children))
}

func (m *Model) buildRenderedStepRows(children []*StepNode) []renderedStepRow {
	rows := make([]renderedStepRow, 0, len(children))
	for i, n := range children {
		isSel := i == m.cursor
		rows = append(rows, renderedStepRow{
			text:       m.renderStepRow(n, isSel),
			node:       n,
			selectable: true,
		})
		if isSel {
			rows = append(rows, m.buildExpansionRows(n)...)
		}
	}
	return rows
}

func (m *Model) renderStepRow(n *StepNode, selected bool) string {
	prefix := "   "
	if selected {
		prefix = tuistyle.CursorStyle.Render("▶") + "  "
	}

	typeCol, label, glyph := m.stepRowParts(n)

	style := tuistyle.DimStyle
	if selected {
		style = tuistyle.SelectedStyle
	}
	if n.Status == StatusFailed {
		style = tuistyle.StatusFailed
	}

	return prefix + typeCol + style.Render(label) + "  " + glyph
}

func (m *Model) buildExpansionRows(selected *StepNode) []renderedStepRow {
	rows := make([]renderedStepRow, 0)
	for depth, current := 1, selected; ; depth++ {
		current = expansionChild(current)
		if current == nil {
			return rows
		}
		rows = append(rows, renderedStepRow{
			text:       m.renderExpansionRow(current, depth),
			node:       current,
			selectable: false,
		})
	}
}

func expansionChild(parent *StepNode) *StepNode {
	if parent == nil {
		return nil
	}

	var fallback *StepNode
	for _, child := range parent.Children {
		if child.Status == StatusInProgress {
			return child
		}
		if child.Status != StatusPending {
			fallback = child
		}
	}
	return fallback
}

func (m *Model) renderExpansionRow(n *StepNode, depth int) string {
	typeCol, label, glyph := m.stepRowParts(n)
	return strings.Repeat("  ", depth) + typeCol + tuistyle.DimStyle.Render(label) + "  " + glyph
}

func rowTexts(rows []renderedStepRow) []string {
	texts := make([]string, len(rows))
	for i, row := range rows {
		texts[i] = row.text
	}
	return texts
}

func renderedRowIndexForNode(rows []renderedStepRow, node *StepNode) int {
	if node == nil {
		return 0
	}
	for i, row := range rows {
		if row.selectable && row.node == node {
			return i
		}
	}
	return 0
}

func leftPaneOffset(selectedRow, totalRows, bodyHeight int) int {
	if bodyHeight <= 0 || totalRows <= bodyHeight || selectedRow < bodyHeight {
		return 0
	}
	maxOffset := totalRows - bodyHeight
	offset := selectedRow - bodyHeight + 1
	if offset > maxOffset {
		return maxOffset
	}
	return offset
}

func (m *Model) stepRowParts(n *StepNode) (typeCol, label, glyph string) {
	glyph = m.statusGlyph(n)
	label = n.ID
	suffix := ""
	typePrefix := ""

	switch n.Type {
	case NodeLoop:
		total := loopTotal(n)
		if total > 0 {
			suffix = fmt.Sprintf(" (%d/%d)", n.IterationsCompleted, total)
		}
	case NodeIteration:
		label = fmt.Sprintf("iter %d", n.IterationIndex+1)
		if n.BindingValue != "" {
			label += "   " + filepath.Base(n.BindingValue)
		}
	default:
		typePrefix = typeGlyph(n.Type)
	}

	typeCol = "   "
	if typePrefix != "" {
		typeCol = typePrefix + "  "
	}

	return typeCol, label + suffix, glyph
}

func (m *Model) statusGlyph(n *StepNode) string {
	switch n.Status {
	case StatusInProgress:
		if (m.active || m.running) && !n.Aborted {
			if tuistyle.BlinkOn(m.pulsePhase) {
				return tuistyle.StatusSuccess.Render("●")
			}
			return tuistyle.BlinkHidden("●")
		}
		return tuistyle.StatusSuccess.Render("●")
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
	raw := blockTypeGlyph(t)
	switch t {
	case NodeShell:
		return shellGlyphStyle.Render(raw)
	case NodeHeadlessAgent, NodeInteractiveAgent, NodeSubWorkflow:
		return subwfGlyphStyle.Render(raw)
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
	parts = append(parts, "j/k scroll")

	if sel != nil {
		switch sel.Type {
		case NodeLoop, NodeSubWorkflow, NodeIteration:
			parts = append(parts, "enter drill")
		case NodeHeadlessAgent, NodeInteractiveAgent:
			if sel.SessionID != "" && !m.running {
				parts = append(parts, "enter resume")
			}
		}
	}

	if !m.running {
		parts = append(parts, "esc back")
	}

	if m.canResumeRun() {
		parts = append(parts, "r resume")
	}

	if m.selectedNodeHasTruncatedOutput() {
		parts = append(parts, "g full output")
	}

	if !m.autoFollow {
		parts = append(parts, "l follow")
	}

	parts = append(parts, "? legend", "q quit")

	return "  " + tuistyle.HelpStyle.Render(strings.Join(parts, "   "))
}

func (m *Model) selectedNodeHasTruncatedOutput() bool {
	n := m.selectedNode()
	if n == nil {
		return false
	}
	if m.loadedFull[n.NodeKey()] {
		return false
	}
	if n.Type != NodeShell && n.Type != NodeHeadlessAgent {
		return false
	}
	return truncateOutput(n.Stdout).Truncated || truncateOutput(n.Stderr).Truncated
}

func twoColumnPaneWidths(termWidth int, rows []string) (listWidth, rightWidth int) {
	if termWidth <= 0 {
		return 4, 80
	}

	// Cap the list column so one pathologically long row can't starve the
	// log pane. Prefer at most ~45% of the terminal for the list.
	listCap := termWidth / 2
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

	listWidth = maxRowWidth + 4
	// Divider "│ " consumes 2 columns between the panes.
	rightWidth = termWidth - listWidth - 2 - 4
	if rightWidth < 20 {
		rightWidth = 20
	}
	return listWidth, rightWidth
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
	b.WriteString(tuistyle.DimStyle.Render("The workflow is still running. Quitting will close the TUI"))
	b.WriteString("\n  ")
	b.WriteString(tuistyle.DimStyle.Render("and wait for the current step to finish before exiting."))
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

	b.WriteString("\n  ")
	b.WriteString(tuistyle.SelectedStyle.Render("Live Navigation"))
	b.WriteString("\n\n")
	b.WriteString("  l  jump to active step and resume auto-follow\n")

	b.WriteString("\n\n  ")
	b.WriteString(tuistyle.HelpStyle.Render("press ? or esc to dismiss"))
	b.WriteString("\n")

	return b.String()
}

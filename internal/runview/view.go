package runview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/tuistyle"
)

var (
	shellGlyphStyle   = lipgloss.NewStyle().Foreground(tuistyle.InactiveAmber)
	scriptGlyphStyle  = lipgloss.NewStyle().Foreground(tuistyle.InactiveAmber)
	uiGlyphStyle      = lipgloss.NewStyle().Foreground(tuistyle.AccentMagenta)
	loopGlyphStyle    = lipgloss.NewStyle().Foreground(tuistyle.AccentCyan)
	subwfGlyphStyle   = lipgloss.NewStyle().Foreground(tuistyle.AccentCyan)
	defaultTextStyle  = lipgloss.NewStyle()
	selectedStepStyle = lipgloss.NewStyle().Foreground(tuistyle.SuccessGreen)
)

type renderedStepRow struct {
	text           string
	node           *StepNode
	selectable     bool
	depth          int
	suppressStatus bool
}

type treePaneLayout struct {
	sidebar int
	detail  int
	rows    []string
}

func (m *Model) View() string {
	if !m.altScreen {
		return ""
	}
	if m.showLegend {
		return m.renderLegend()
	}
	if m.quitConfirming {
		return m.renderQuitConfirm()
	}
	if m.showSummary {
		return m.renderSummary()
	}

	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(m.renderChrome())
	if reason := m.renderFailureReason(); reason != "" {
		b.WriteString("\n")
		b.WriteString(reason)
	}
	if hint := m.renderFailedRunDebugHint(); hint != "" {
		b.WriteString("\n")
		b.WriteString(hint)
	}
	b.WriteString("\n\n")

	swHeader := m.renderSubWorkflowHeader()
	if swHeader != "" {
		b.WriteString(swHeader)
		b.WriteString("\n")
	}

	if len(m.currentChildren()) == 0 {
		b.WriteString(tuistyle.ScreenMargin)
		b.WriteString(tuistyle.DimStyle.Render("No steps to display."))
		b.WriteString("\n")
	} else {
		b.WriteString(m.renderTwoColumn())
	}

	for _, warning := range m.tree.Warnings {
		b.WriteString("\n")
		b.WriteString(tuistyle.ScreenMargin)
		b.WriteString(renderWarning(warning))
	}
	if m.loadErr != "" {
		b.WriteString("\n")
		b.WriteString(tuistyle.ScreenMargin)
		b.WriteString(tuistyle.DimStyle.Render("Error: " + m.loadErr))
	}
	if m.notice != "" {
		b.WriteString("\n")
		b.WriteString(tuistyle.ScreenMargin)
		b.WriteString(tuistyle.DimStyle.Render(m.notice))
	}

	b.WriteString("\n")
	b.WriteString(m.renderRule())
	b.WriteString("\n")
	b.WriteString(m.renderHelpBarWithCwd())
	b.WriteString("\n")

	return b.String()
}

func (m *Model) renderChrome() string {
	crumb := m.renderBreadcrumb()
	crumbW := runewidth.StringWidth(tuistyle.Sanitize(crumb))
	return tuistyle.RenderChromeWithLogo(crumb, crumbW, m.termWidth)
}

func (m *Model) renderRule() string {
	return tuistyle.RenderRule(m.termWidth)
}

func (m *Model) renderFailureReason() string {
	if m.rootStatus() != StatusFailed {
		return ""
	}
	reason := failureReason(m.tree.Root)
	if reason == "" {
		reason = "workflow failed"
	}
	return tuistyle.ScreenMargin + tuistyle.StatusFailed.Render("reason: "+reason)
}

func (m *Model) renderFailedRunDebugHint() string {
	if m.rootStatus() != StatusFailed || !m.canLaunchDebug() {
		return ""
	}
	return tuistyle.ScreenMargin + tuistyle.StatusFailed.Bold(true).Render("press d to debug")
}

func (m *Model) renderTwoColumn() string {
	renderedRows := m.buildProjectedRenderedRows()
	rows := rowTexts(renderedRows)
	layout := measureTreePaneLayout(m.termWidth, rows, m.sidebarWidth)
	m.sidebarWidth = max(m.sidebarWidth, layout.sidebar)
	listWidth, rightWidth := layout.sidebar, layout.detail
	treeLines := layout.rows
	for i, row := range renderedRows {
		if row.selectable && lipgloss.Width(row.text) > listWidth {
			treeLines[i] = m.fitTreeRow(row, listWidth)
		}
	}

	bodyHeight := m.bodyHeight()
	if bodyHeight <= 0 {
		bodyHeight = 20
	}

	var detailLines []string
	if m.liveUIVisible() {
		detailLines = m.liveUIDetailLines(rightWidth)
	} else {
		detailLines = m.selectedDetailDocument(rightWidth).renderScreen()
	}

	maxOffset := max(0, len(detailLines)-bodyHeight)
	offset := m.detailOffset
	if offset > maxOffset {
		offset = maxOffset
	}
	var visibleDetailLines []string
	if offset > 0 && offset <= len(detailLines) {
		visibleDetailLines = detailLines[offset:]
	} else {
		visibleDetailLines = detailLines
	}

	selectedRow := renderedRowIndexForNode(renderedRows, m.selectedNode())
	m.ensureTreeSelectionVisible(selectedRow)
	leftOffset := min(m.treeOffset, max(0, len(renderedRows)-bodyHeight))
	visibleTreeLines := treeLines
	if leftOffset > 0 && leftOffset <= len(treeLines) {
		visibleTreeLines = treeLines[leftOffset:]
	}

	var b strings.Builder
	for i := 0; i < bodyHeight; i++ {
		leftPart := ""
		if i < len(visibleTreeLines) {
			leftPart = visibleTreeLines[i]
		}
		leftPad := listWidth - lipgloss.Width(leftPart)
		if leftPad < 0 {
			leftPad = 0
		}

		rightPart := ""
		if i < len(visibleDetailLines) {
			rightPart = fitDetailLine(visibleDetailLines[i], rightWidth)
		}

		b.WriteString(leftPart)
		b.WriteString(strings.Repeat(" ", leftPad))
		b.WriteString("    ")
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
		suppressStatus := false
		var expansion []renderedStepRow
		if isSel {
			expansion = m.buildExpansionRows(n)
			suppressStatus = n.Status == StatusInProgress && expansionHasInProgressChild(expansion)
		}
		rows = append(rows, renderedStepRow{
			text:       m.renderStepRow(n, isSel, suppressStatus),
			node:       n,
			selectable: true,
		})
		rows = append(rows, expansion...)
	}
	return rows
}

func (m *Model) buildProjectedRenderedRows() []renderedStepRow {
	projected := m.projectedRows()
	rows := make([]renderedStepRow, 0, len(projected))
	deepestActive := m.activeNode()
	for _, row := range projected {
		if row.kind == treeRowOmission {
			rows = append(rows, renderedStepRow{text: "   " + strings.Repeat("  ", row.depth) + tuistyle.DimStyle.Render(row.omissionLabel())})
			continue
		}
		suppress := row.node.Status == StatusInProgress && deepestActive != nil && row.node != deepestActive && isDescendantOf(deepestActive, row.node)
		rows = append(rows, renderedStepRow{
			text: m.renderTreeRow(row.node, row.node == m.selectedNode(), suppress, row.depth),
			node: row.node, selectable: true, depth: row.depth, suppressStatus: suppress,
		})
	}
	return rows
}

func (m *Model) renderTreeRow(n *StepNode, selected, suppressStatus bool, depth int) string {
	_, label, _ := m.stepRowParts(n)
	return m.renderTreeRowLabel(n, selected, suppressStatus, depth, label)
}

func (m *Model) renderTreeRowLabel(n *StepNode, selected, suppressStatus bool, depth int, label string) string {
	prefix := "   "
	if selected {
		prefix = tuistyle.CursorStyle.Render("▶") + "  "
	}
	typeCol, _, glyph := m.stepRowParts(n)
	if suppressStatus {
		glyph = " "
	}
	style := defaultTextStyle
	if selected {
		style = selectedStepStyle
	}
	if n.Status == StatusFailed {
		style = tuistyle.StatusFailed
	}
	return prefix + strings.Repeat("  ", depth) + glyph + "  " + style.Render(label) + "  " + typeCol
}

// fitTreeRow clips only the name portion of a node row. Cursor, indentation,
// status, suffix and type glyph are fixed chrome and survive narrow layouts.
func (m *Model) fitTreeRow(row renderedStepRow, width int) string {
	if width <= 0 {
		return ""
	}
	if row.node == nil {
		return runewidth.Truncate(tuistyle.Sanitize(row.text), width, "…")
	}
	base, suffix := stepRowLabel(row.node)
	fixed := lipgloss.Width(m.renderTreeRowLabel(row.node, row.node == m.selectedNode(), row.suppressStatus, row.depth, suffix))
	nameWidth := max(0, width-fixed)
	label := suffix
	if nameWidth > 0 {
		label = runewidth.Truncate(base, nameWidth, "…") + suffix
	}
	fitted := m.renderTreeRowLabel(row.node, row.node == m.selectedNode(), row.suppressStatus, row.depth, label)
	if lipgloss.Width(fitted) > width {
		return runewidth.Truncate(tuistyle.Sanitize(fitted), width, "…")
	}
	return fitted
}

// expansionHasInProgressChild reports whether any expansion row refers to a
// node whose status is in-progress. Used to suppress the parent's own
// status indicator so only one in-progress glyph renders at a time.
func expansionHasInProgressChild(rows []renderedStepRow) bool {
	for _, r := range rows {
		if r.node != nil && r.node.Status == StatusInProgress {
			return true
		}
	}
	return false
}

func (m *Model) renderStepRow(n *StepNode, selected, suppressStatus bool) string {
	prefix := "   "
	if selected {
		prefix = tuistyle.CursorStyle.Render("▶") + "  "
	}

	typeCol, label, glyph := m.stepRowParts(n)
	if suppressStatus {
		glyph = " "
	}

	style := defaultTextStyle
	if selected {
		style = selectedStepStyle
	}
	if n.Status == StatusFailed {
		style = tuistyle.StatusFailed
	}

	return prefix + typeCol + style.Render(label) + "  " + glyph
}

func (m *Model) buildExpansionRows(selected *StepNode) []renderedStepRow {
	children := m.expansionChildren(selected)
	rows := make([]renderedStepRow, 0, len(children))
	for _, current := range children {
		rows = append(rows, renderedStepRow{
			text:       m.renderExpansionRow(current, 1),
			node:       current,
			selectable: false,
		})
	}
	return rows
}

func (m *Model) expansionChildren(selected *StepNode) []*StepNode {
	if selected == nil || !selected.IsContainer() {
		return nil
	}
	target := selected.Drilldown()
	if target.Type == NodeSubWorkflow && !target.SubLoaded && len(target.Children) == 0 && target.ErrorMessage == "" {
		if err := m.tree.EnsureSubWorkflowLoaded(target); err != nil {
			if target.ErrorMessage == "" {
				target.ErrorMessage = err.Error()
			}
			return nil
		}
	}
	return target.Children
}

func (m *Model) renderExpansionRow(n *StepNode, depth int) string {
	typeCol, label, glyph := m.stepRowParts(n)
	return "   " + strings.Repeat("  ", depth) + typeCol + defaultTextStyle.Render(label) + "  " + glyph
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
	label, suffix := stepRowLabel(n)
	typePrefix := ""

	switch n.Type {
	case NodeLoop:
		typePrefix = typeGlyph(n.Type)
	case NodeIteration:
		typePrefix = typeGlyph(n.Type)
	case NodeAgentCall:
		typePrefix = typeGlyph(n.Type)
	default:
		typePrefix = typeGlyph(n.Type)
	}
	label += suffix

	typeCol = "   "
	if typePrefix != "" {
		typeCol = typePrefix + "  "
	}

	return typeCol, label, glyph
}

func stepRowLabel(n *StepNode) (label, suffix string) {
	if n == nil {
		return "", ""
	}
	label = n.ID
	switch n.Type {
	case NodeLoop:
		if total := loopTotal(n); total > 0 {
			suffix = fmt.Sprintf(" (%d/%d)", n.IterationsCompleted, total)
		}
	case NodeIteration:
		label = fmt.Sprintf("iter %d", n.IterationIndex+1)
	case NodeAgentCall:
		label = n.callLabel()
	}
	if (n.Type == NodeHeadlessAgent || n.Type == NodeInteractiveAgent) && len(n.Children) > 0 {
		suffix = fmt.Sprintf(" (%d calls)", len(n.Children))
	}
	return label, suffix
}

func (m *Model) statusGlyph(n *StepNode) string {
	switch n.Status {
	case StatusInProgress:
		if n.Type == NodeUI {
			return styledStatusGlyph(StatusInProgress)
		}
		if (m.active || m.running) && !n.Aborted {
			if tuistyle.BlinkOn(m.pulsePhase) {
				return styledStatusGlyph(StatusInProgress)
			}
			return tuistyle.BlinkHidden("●")
		}
		return styledStatusGlyph(StatusInProgress)
	case StatusPending:
		return styledStatusGlyph(StatusPending)
	case StatusSuccess:
		return styledStatusGlyph(StatusSuccess)
	case StatusFailed:
		return styledStatusGlyph(StatusFailed)
	case StatusSkipped:
		return styledStatusGlyph(StatusSkipped)
	}
	return " "
}

func styledStatusGlyph(status NodeStatus) string {
	switch status {
	case StatusInProgress:
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
	case NodeScript:
		return scriptGlyphStyle.Render(raw)
	case NodeUI:
		return uiGlyphStyle.Render(raw)
	case NodeLoop, NodeIteration, NodeGroup:
		return loopGlyphStyle.Render(raw)
	case NodeHeadlessAgent, NodeInteractiveAgent, NodeSubWorkflow, NodeAgentCall:
		return subwfGlyphStyle.Render(raw)
	}
	return ""
}

func (m *Model) renderHelpBar() string {
	return tuistyle.ScreenMargin + tuistyle.HelpStyle.Render(strings.Join(m.helpBarParts(), "   "))
}

func (m *Model) renderHelpBarWithCwd() string {
	helpText := strings.Join(m.helpBarParts(), "   ")
	cwd := tuistyle.Sanitize(tuistyle.ShortenPath(m.originCwd))
	return tuistyle.RenderHelpWithCwd(helpText, cwd, m.termWidth)
}

func (m *Model) helpBarParts() []string {
	if m.liveUIVisible() {
		parts := []string{"↑↓ step", "j/k scroll", "s summary"}
		for _, part := range m.liveUI.HelpParts() {
			if part == "↑↓ option" {
				part = "←→ option"
			}
			if part == "esc cancel" {
				continue
			}
			parts = append(parts, part)
		}
		if len(m.path) > 1 {
			parts = append(parts, "esc back")
		} else {
			parts = append(parts, "esc quit")
		}
		if !m.followActive {
			parts = append(parts, "l follow")
		}
		parts = append(parts, "q quit")
		return parts
	}

	sel := m.selectedNode()
	var parts []string

	container := m.currentContainer()
	if container != nil && container.Type == NodeLoop {
		parts = append(parts, "↑↓ iteration")
	} else {
		parts = append(parts, "↑↓ step")
	}
	parts = append(parts, "j/k scroll")
	parts = append(parts, m.selectedNodeHelpParts(sel)...)

	if !m.running {
		parts = append(parts, "esc back")
	}

	if m.entered == FromDefinition {
		parts = append(parts, "r start run")
	} else if m.canResumeRun() || m.resumeAgentTargetForSelection() != nil {
		parts = append(parts, "r resume")
	}

	if m.canLaunchDebug() {
		parts = append(parts, "d debug")
	}

	if m.selectedNodeHasTruncatedOutput() {
		parts = append(parts, "g full output")
	}
	if m.selectedInputIsExpandable() {
		if m.inputExpanded[m.selectedNode().NodeKey()] {
			parts = append(parts, "i collapse")
		} else {
			parts = append(parts, "i expand")
		}
	}

	parts = append(parts, "c copy")

	if !m.followTail && (m.active || m.running) {
		parts = append(parts, "t tail")
	}
	if !m.followActive && (m.active || m.running) {
		parts = append(parts, "l follow")
	}

	parts = append(parts, m.viewSwitchHelpPart(), "? legend", "q quit")

	return parts
}

func (m *Model) selectedInputIsExpandable() bool {
	node := m.selectedNode()
	if node == nil {
		return false
	}
	input := ""
	switch node.Type {
	case NodeShell:
		input = currentCommand(node)
	case NodeScript:
		input = node.StaticScript
	case NodeHeadlessAgent, NodeInteractiveAgent, NodeAgentCall:
		input = currentPrompt(node)
	}
	return len(wrappedPlainLines(input, m.rightPaneWidth()-3)) > 3
}

func (m *Model) selectedNodeHelpParts(selected *StepNode) []string {
	if selected == nil {
		return nil
	}
	switch selected.Type {
	case NodeLoop, NodeSubWorkflow, NodeIteration, NodeGroup:
		return []string{"enter drill"}
	case NodeHeadlessAgent, NodeInteractiveAgent:
		if selected.IsContainer() {
			return []string{"enter drill"}
		}
		if m.canResumeAgentSession(selected) {
			return []string{"enter resume"}
		}
	case NodeAgentCall:
		if m.canResumeAgentSession(selected) {
			return []string{"enter resume"}
		}
	}
	return nil
}

func (m *Model) viewSwitchHelpPart() string {
	if m.showSummary {
		return "v view run"
	}
	return "s summary"
}

func (m *Model) selectedNodeHasTruncatedOutput() bool {
	n := m.selectedNode()
	if n == nil {
		return false
	}
	if m.loadedFull[n.NodeKey()] {
		return false
	}
	if n.Type != NodeShell && n.Type != NodeScript && n.Type != NodeHeadlessAgent && n.Type != NodeAgentCall {
		return false
	}
	return truncateOutput(n.Stdout).Truncated || truncateOutput(n.Stderr).Truncated
}

// measureTreePaneLayout measures complete rows before any name/content is
// clipped. It has no proportional sidebar cap: the normal 20-column detail
// minimum is the only constraint until the terminal is physically too narrow.
func measureTreePaneLayout(termWidth int, rows []string, settled int) treePaneLayout {
	if termWidth <= 0 {
		return treePaneLayout{sidebar: 4, detail: 80, rows: rows}
	}
	const gap, margins, normalDetail = 4, 4, 20
	available := max(0, termWidth-margins)
	maxRow := 0
	for _, row := range rows {
		maxRow = max(maxRow, lipgloss.Width(row))
	}
	maxWithDetail := max(0, available-gap-normalDetail)
	sidebar := max(maxRow, settled)
	if available >= gap+normalDetail {
		sidebar = min(sidebar, maxWithDetail)
	} else {
		// Names/content have already yielded; preserve as much fixed tree
		// chrome as possible and let detail absorb the unavoidable shortfall.
		sidebar = min(sidebar, max(0, available-gap))
	}
	detail := max(0, available-gap-sidebar)
	display := make([]string, len(rows))
	for i, row := range rows {
		if lipgloss.Width(row) > sidebar {
			display[i] = runewidth.Truncate(tuistyle.Sanitize(row), max(0, sidebar), "…")
		} else {
			display[i] = row
		}
	}
	return treePaneLayout{sidebar: sidebar, detail: detail, rows: display}
}

func (m *Model) bodyHeight() int {
	if m.termHeight == 0 {
		return 20
	}
	chrome := 9
	chrome += m.subWorkflowHeaderChromeLines()
	return max(5, m.termHeight-chrome)
}

func (m *Model) subWorkflowHeaderChromeLines() int {
	container := m.currentContainer()
	if container == nil || container.Type != NodeSubWorkflow {
		return 0
	}
	params := container.InterpolatedParams
	if params == nil {
		params = container.StaticParams
	}
	if len(params) > 0 {
		return 3
	}
	return 2
}

func (m *Model) renderQuitConfirm() string {
	var b strings.Builder
	b.WriteString("\n\n")
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(tuistyle.HeaderStyle.Render("Agent Runner"))
	b.WriteString("\n\n")
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(tuistyle.DimStyle.Render("The workflow is still running. Quitting will close the TUI"))
	b.WriteString("\n")
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(tuistyle.DimStyle.Render("and wait for the current step to finish before exiting."))
	b.WriteString("\n\n")
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(tuistyle.NormalStyle.Render("Quit anyway?  "))
	b.WriteString(tuistyle.SelectedStyle.Render("[y]es"))
	b.WriteString(tuistyle.NormalStyle.Render("  "))
	b.WriteString(tuistyle.SelectedStyle.Render("[n]o"))
	b.WriteString("\n")
	return b.String()
}

func (m *Model) renderLegend() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(tuistyle.HeaderStyle.Render("Legend"))
	b.WriteString("\n\n")

	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(tuistyle.SelectedStyle.Render("Status Glyphs"))
	b.WriteString("\n\n")
	b.WriteString(tuistyle.ScreenMargin + styledStatusGlyph(StatusInProgress) + "  running\n")
	b.WriteString(tuistyle.ScreenMargin + styledStatusGlyph(StatusPending) + "  pending\n")
	b.WriteString(tuistyle.ScreenMargin + styledStatusGlyph(StatusSuccess) + "  success\n")
	b.WriteString(tuistyle.ScreenMargin + styledStatusGlyph(StatusFailed) + "  failed\n")
	b.WriteString(tuistyle.ScreenMargin + styledStatusGlyph(StatusSkipped) + "  skipped\n")

	b.WriteString("\n")
	b.WriteString(tuistyle.ScreenMargin)
	b.WriteString(tuistyle.SelectedStyle.Render("Type Glyphs"))
	b.WriteString("\n\n")
	b.WriteString("  " + typeGlyph(NodeShell) + "  shell\n")
	b.WriteString("  " + typeGlyph(NodeScript) + "  script\n")
	b.WriteString("  " + typeGlyph(NodeUI) + "  ui\n")
	b.WriteString("  " + typeGlyph(NodeHeadlessAgent) + "  headless agent\n")
	b.WriteString("  " + typeGlyph(NodeInteractiveAgent) + "  interactive agent\n")
	b.WriteString("  " + typeGlyph(NodeSubWorkflow) + "  sub-workflow\n")
	b.WriteString("  " + typeGlyph(NodeLoop) + "  loop\n")
	b.WriteString("  " + typeGlyph(NodeIteration) + "  iteration\n")
	b.WriteString("  " + typeGlyph(NodeGroup) + "  group\n")

	b.WriteString("\n  ")
	b.WriteString(tuistyle.SelectedStyle.Render("Live Navigation"))
	b.WriteString("\n\n")
	b.WriteString("  t  follow selected response\n")
	b.WriteString("  l  jump to active step and resume auto-follow\n")

	b.WriteString("\n\n  ")
	b.WriteString(tuistyle.HelpStyle.Render("press ? or esc to dismiss"))
	b.WriteString("\n")

	return b.String()
}

func renderWarning(message string) string {
	return tuistyle.DimStyle.Render("Warning: " + message)
}

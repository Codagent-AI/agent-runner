package tui

import (
	"math"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/runs"
)

func (m *Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	b.WriteString("\n  ")
	b.WriteString(headerStyle.Render("Agent Runner"))
	b.WriteString("\n\n")
	b.WriteString(m.renderTabs())
	b.WriteString("\n\n")
	b.WriteString(m.renderSubheader())
	b.WriteString("\n\n")
	b.WriteString(m.renderBody())

	if m.loadErr != "" {
		b.WriteString("\n")
		b.WriteString("  " + dimStyle.Render("Error loading runs: "+sanitize(m.loadErr)))
	}

	b.WriteString("\n\n\n\n")
	b.WriteString(m.renderHelp())
	b.WriteString("\n")

	return b.String()
}

func (m *Model) renderTabs() string {
	var parts []string

	renderTab := func(label string, t tab) string {
		if m.activeTab == t {
			return activeTabStyle.Render("● " + label)
		}
		return dimTabStyle.Render("○ " + label)
	}

	parts = append(parts, renderTab("Current Dir", tabCurrentDir))
	if m.worktreeTab.worktrees != nil {
		parts = append(parts, renderTab("Worktrees", tabWorktrees))
	}
	parts = append(parts, renderTab("All", tabAll))

	return "  " + strings.Join(parts, "    ")
}

func (m *Model) renderSubheader() string {
	switch m.activeTab {
	case tabCurrentDir:
		cwd, _ := os.Getwd()
		return "  " + pathStyle.Render(sanitize(shortenPath(cwd)))
	case tabWorktrees:
		if m.worktreeTab.subView == subViewRunList {
			wt := m.selectedWorktree()
			if wt != nil {
				return "  " + dimStyle.Render("← Worktrees") + dimStyle.Render("  /  ") + normalStyle.Render(sanitize(wt.Name))
			}
		}
		cwd, _ := os.Getwd()
		return "  " + pathStyle.Render(sanitize(shortenPath(cwd))+"  (git repo)")
	case tabAll:
		if m.allTab.subView == subViewRunList {
			d := m.selectedAllDir()
			if d != nil {
				return "  " + dimStyle.Render("← All") + dimStyle.Render("  /  ") + normalStyle.Render(sanitize(shortenPath(d.Path)))
			}
		}
		return "  " + pathStyle.Render("All project directories")
	}
	return ""
}

func (m *Model) renderBody() string {
	switch m.activeTab {
	case tabCurrentDir:
		if len(m.currentRuns) == 0 {
			return m.renderEmpty()
		}
		return m.renderRunList(m.currentRuns, m.currentDirCursor, &m.currentDirOffset)
	case tabWorktrees:
		if m.worktreeTab.subView == subViewPicker {
			return m.renderWorktreePicker()
		}
		wt := m.selectedWorktree()
		if wt == nil || len(wt.Runs) == 0 {
			return m.renderEmpty()
		}
		return m.renderRunList(wt.Runs, m.worktreeTab.listCursor, &m.worktreeTab.listOffset)
	case tabAll:
		if m.allTab.subView == subViewPicker {
			return m.renderAllPicker()
		}
		d := m.selectedAllDir()
		if d == nil || len(d.Runs) == 0 {
			return m.renderEmpty()
		}
		return m.renderRunList(d.Runs, m.allTab.listCursor, &m.allTab.listOffset)
	}
	return ""
}

// listMaxRows returns the number of data rows that fit in the body area.
// Accounts for the fixed chrome of View() and the column-header row.
// Returns a very large value before the first WindowSizeMsg arrives so that
// nothing is hidden on the initial render.
func (m *Model) listMaxRows(hasHeader bool) int {
	if m.termHeight == 0 {
		return 1 << 30
	}
	chrome := 12
	if m.loadErr != "" {
		chrome += 2
	}
	if hasHeader {
		chrome++
	}
	return max(3, m.termHeight-chrome)
}

func (m *Model) renderEmpty() string {
	return "\n" +
		dimStyle.Render("               No runs found for this directory.") + "\n\n" +
		dimStyle.Render("               Press tab to view other scopes.")
}

const (
	hdrWorkflow = "Workflow"
	hdrStep     = "Step"
	hdrChange   = "Change"
	hdrUpdated  = "Updated"
)

type runListCols struct {
	hasName                        bool
	wfMax, stepMax, nameMax, tsMax int
}

func measureRunListCols(runList []runs.RunInfo) runListCols {
	c := runListCols{
		wfMax:   runewidth.StringWidth(hdrWorkflow),
		stepMax: runewidth.StringWidth(hdrStep),
		tsMax:   runewidth.StringWidth(hdrUpdated),
	}
	for i := range runList {
		if runList[i].ChangeName != "" {
			c.hasName = true
			c.nameMax = runewidth.StringWidth(hdrChange)
			break
		}
	}
	for i := range runList {
		r := &runList[i]
		if w := runewidth.StringWidth(sanitize(r.WorkflowName)); w > c.wfMax {
			c.wfMax = w
		}
		step := r.CurrentStep
		if step == "" {
			step = "—"
		}
		if w := runewidth.StringWidth(sanitize(step)); w > c.stepMax {
			c.stepMax = w
		}
		if c.hasName {
			if w := runewidth.StringWidth(sanitize(r.ChangeName)); w > c.nameMax {
				c.nameMax = w
			}
		}
		if w := runewidth.StringWidth(formatTime(r.LastUpdate)); w > c.tsMax {
			c.tsMax = w
		}
	}
	return c
}

// fitTo adjusts c.wfMax, c.stepMax, c.nameMax to fit in avail columns,
// truncating workflow first, then change name, then step.
func (c *runListCols) fitTo(avail int) {
	if c.wfMax+c.stepMax+c.nameMax <= avail {
		return
	}
	newWf := max(avail-c.stepMax-c.nameMax, 8)
	if newWf < c.wfMax {
		c.wfMax = newWf
	}
	if c.wfMax+c.stepMax+c.nameMax > avail && c.hasName {
		newName := max(avail-c.wfMax-c.stepMax, 8)
		if newName < c.nameMax {
			c.nameMax = newName
		}
	}
	if c.wfMax+c.stepMax+c.nameMax > avail {
		c.stepMax = max(avail-c.wfMax-c.nameMax, 8)
	}
}

func (m *Model) renderRunList(runList []runs.RunInfo, cursor int, offset *int) string {
	c := measureRunListCols(runList)

	// Layout overhead: "   " prefix(3) + "●" status(1) + "  " sep(2) + "  " sep(2) + "  " sep(2) = 10,
	// plus "  " sep(2) before the change-name column when present.
	overhead := 10
	if c.hasName {
		overhead += 2
	}
	avail := m.termWidth - overhead - c.tsMax
	if m.termWidth == 0 {
		avail = c.wfMax + c.stepMax + c.nameMax
	}
	c.fitTo(max(avail, 16))

	maxRows := m.listMaxRows(true)
	*offset = adjustOffset(cursor, *offset, maxRows, len(runList))
	end := min(*offset+maxRows, len(runList))

	var b strings.Builder
	b.WriteString(renderRunListHeader(c))
	for i := *offset; i < end; i++ {
		b.WriteString(m.renderRunListRow(&runList[i], i == cursor, c))
	}
	return b.String()
}

func renderRunListHeader(c runListCols) string {
	// Matches data-row prefix of "   " + status(1) + "  " = 6 cells.
	h := "      " +
		dimStyle.Render(fitCell(hdrWorkflow, c.wfMax)) + "  " +
		dimStyle.Render(fitCell(hdrStep, c.stepMax))
	if c.hasName {
		h += "  " + dimStyle.Render(fitCell(hdrChange, c.nameMax))
	}
	return h + "  " + dimStyle.Render(hdrUpdated) + "\n"
}

func (m *Model) renderRunListRow(r *runs.RunInfo, isSel bool, c runListCols) string {
	prefix := "   "
	if isSel {
		prefix = cursorStyle.Render("▶") + "  "
	}

	step := r.CurrentStep
	if step == "" {
		step = "—"
	}

	style := dimStyle
	if isSel {
		style = selectedStyle
	}

	line := m.renderStatusIcon(r.Status) + "  " +
		style.Render(fitCell(sanitize(r.WorkflowName), c.wfMax)) + "  " +
		style.Render(fitCell(sanitize(step), c.stepMax))
	if c.hasName {
		line += "  " + style.Render(fitCell(sanitize(r.ChangeName), c.nameMax))
	}
	line += "  " + dimStyle.Render(formatTime(r.LastUpdate))
	return prefix + line + "\n"
}

func (m *Model) renderStatusIcon(s runs.Status) string {
	switch s {
	case runs.StatusActive:
		t := (math.Sin(m.pulsePhase) + 1) / 2
		c := lerpColor("#4ade80", "#2d8f57", t)
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("●")
	case runs.StatusInactive:
		return statusInactive.Render("○")
	case runs.StatusCompleted:
		return statusDone.Render("✓")
	}
	return " "
}

func (m *Model) renderWorktreePicker() string {
	nameMax, pathMax, sumMax, tsMax := 0, 0, 0, 0
	for _, wt := range m.worktreeTab.worktrees {
		if w := runewidth.StringWidth(sanitize(wt.Name)); w > nameMax {
			nameMax = w
		}
		if w := runewidth.StringWidth(sanitize(shortenPath(wt.Path))); w > pathMax {
			pathMax = w
		}
		if w := runewidth.StringWidth(runSummary(wt.Runs)); w > sumMax {
			sumMax = w
		}
		if w := runewidth.StringWidth(formatTime(mostRecentRun(wt.Runs))); w > tsMax {
			tsMax = w
		}
	}

	const overhead = 9 // "   " prefix + three "  " separators
	avail := m.termWidth - overhead - sumMax - tsMax
	if m.termWidth == 0 {
		avail = nameMax + pathMax
	}
	if avail < 16 {
		avail = 16
	}
	if nameMax+pathMax > avail {
		newPath := max(avail-nameMax, 16)
		if newPath < pathMax {
			pathMax = newPath
		}
		if nameMax+pathMax > avail {
			nameMax = max(avail-pathMax, 8)
		}
	}

	maxRows := m.listMaxRows(false)
	m.worktreeTab.pickerOffset = adjustOffset(m.worktreeTab.pickerCursor, m.worktreeTab.pickerOffset, maxRows, len(m.worktreeTab.worktrees))
	end := min(m.worktreeTab.pickerOffset+maxRows, len(m.worktreeTab.worktrees))

	var b strings.Builder
	for i := m.worktreeTab.pickerOffset; i < end; i++ {
		wt := m.worktreeTab.worktrees[i]
		isSel := i == m.worktreeTab.pickerCursor
		prefix := "   "
		if isSel {
			prefix = cursorStyle.Render("▶") + "  "
		}

		style := dimStyle
		if isSel {
			style = selectedStyle
		}

		name := fitCell(sanitize(wt.Name), nameMax)
		path := fitCellLeft(sanitize(shortenPath(wt.Path)), pathMax)
		summary := fitCell(runSummary(wt.Runs), sumMax)
		ts := formatTime(mostRecentRun(wt.Runs))

		line := style.Render(name) + "  " + dimStyle.Render(path) + "  " + dimStyle.Render(summary) + "  " + dimStyle.Render(ts)
		b.WriteString(prefix + line + "\n")
	}
	return b.String()
}

func (m *Model) renderAllPicker() string {
	pathMax, sumMax, tsMax := 0, 0, 0
	for _, d := range m.allTab.dirs {
		if w := runewidth.StringWidth(sanitize(shortenPath(d.Path))); w > pathMax {
			pathMax = w
		}
		if w := runewidth.StringWidth(runSummary(d.Runs)); w > sumMax {
			sumMax = w
		}
		if w := runewidth.StringWidth(formatTime(mostRecentRun(d.Runs))); w > tsMax {
			tsMax = w
		}
	}

	const overhead = 7 // "   " prefix + two "  " separators
	avail := m.termWidth - overhead - sumMax - tsMax
	if m.termWidth == 0 {
		avail = pathMax
	}
	if avail < 20 {
		avail = 20
	}
	if pathMax > avail {
		pathMax = avail
	}

	maxRows := m.listMaxRows(false)
	m.allTab.pickerOffset = adjustOffset(m.allTab.pickerCursor, m.allTab.pickerOffset, maxRows, len(m.allTab.dirs))
	end := min(m.allTab.pickerOffset+maxRows, len(m.allTab.dirs))

	var b strings.Builder
	for i := m.allTab.pickerOffset; i < end; i++ {
		d := m.allTab.dirs[i]
		isSel := i == m.allTab.pickerCursor
		prefix := "   "
		if isSel {
			prefix = cursorStyle.Render("▶") + "  "
		}

		style := dimStyle
		if isSel {
			style = selectedStyle
		}

		path := fitCellLeft(sanitize(shortenPath(d.Path)), pathMax)
		summary := fitCell(runSummary(d.Runs), sumMax)
		ts := formatTime(mostRecentRun(d.Runs))

		line := style.Render(path) + "  " + dimStyle.Render(summary) + "  " + dimStyle.Render(ts)
		b.WriteString(prefix + line + "\n")
	}
	return b.String()
}

func (m *Model) renderHelp() string {
	var parts []string

	switch m.activeTab {
	case tabCurrentDir:
		if len(m.currentRuns) > 0 {
			parts = append(parts, "↑↓ navigate", "enter view", "tab/c/w/a switch tab", "q quit")
		} else {
			parts = append(parts, "tab/c/w/a switch tab", "q quit")
		}
	case tabWorktrees:
		if m.worktreeTab.subView == subViewPicker {
			parts = append(parts, "↑↓ navigate", "enter view runs", "tab/c/w/a switch tab", "q quit")
		} else {
			parts = append(parts, "↑↓ navigate", "enter view", "esc back", "tab/c/w/a switch tab", "q quit")
		}
	case tabAll:
		if m.allTab.subView == subViewPicker {
			parts = append(parts, "↑↓ navigate", "enter view runs", "tab/c/w/a switch tab", "q quit")
		} else {
			parts = append(parts, "↑↓ navigate", "enter view", "esc back", "tab/c/w/a switch tab", "q quit")
		}
	}

	return "  " + helpStyle.Render(strings.Join(parts, "   "))
}

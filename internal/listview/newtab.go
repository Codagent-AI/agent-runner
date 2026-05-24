package listview

import (
	"sort"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

type rowKind int

const (
	workflowRow rowKind = iota
	headerRow
	separatorRow
)

type filteredRow struct {
	kind  rowKind
	index int
}

type groupKey struct {
	scope discovery.Scope
	ns    string
}

type workflowGroup struct {
	indices []int
}

var builtinGroupOrder = []string{
	"spec-driven",
	"openspec",
	"onboarding",
	"core",
}

// handleSearchKey processes a key event when the new tab search box has focus.
// Returns the updated model and a command.
func (m *Model) handleSearchKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "tab", "right":
		m.nextTab()
	case "shift+tab", "left":
		m.prevTab()
	case "esc":
		m.newTab.searchText = ""
		m.rebuildNewTabFiltered()
	case "backspace":
		if m.newTab.searchText != "" {
			r := []rune(m.newTab.searchText)
			m.newTab.searchText = string(r[:len(r)-1])
			m.rebuildNewTabFiltered()
		}
	case "down", "enter":
		m.newTab.searchFocused = false
		m.newTab.cursor = firstSelectableRow(m.newTab.filtered)
	default:
		if msg.Type == tea.KeyRunes {
			m.newTab.searchText += msg.String()
			m.rebuildNewTabFiltered()
		}
	}
	return m, nil
}

// rebuildNewTabFiltered recomputes the filtered row list and resets the cursor
// to the first selectable row, using the current searchText and showHidden state.
func (m *Model) rebuildNewTabFiltered() {
	m.newTab.filtered = buildFilteredRows(m.newTab.workflows, m.newTab.groups, m.newTab.searchText, m.newTab.showHidden)
	m.newTab.cursor = firstSelectableRow(m.newTab.filtered)
}

// buildFilteredRows computes the filtered slice for the new tab.
// Each element is tagged as a workflow, group header, or separator.
// Groups with no visible matching entries are collapsed.
// Filter is case-insensitive substring match against CanonicalName or SourcePath.
func buildFilteredRows(workflows []discovery.WorkflowEntry, groups []discovery.GroupMetadata, filter string, showHidden bool) []filteredRow {
	filter = strings.ToLower(filter)

	groupRows := make(map[groupKey]*workflowGroup)

	for i, e := range workflows {
		if e.Hidden && !showHidden {
			continue
		}
		if filter != "" && !matchesFilter(&e, filter) {
			continue
		}

		key := groupKey{scope: e.Scope, ns: e.Namespace}
		if groupRows[key] == nil {
			groupRows[key] = &workflowGroup{}
		}
		groupRows[key].indices = append(groupRows[key].indices, i)
	}

	groupIndices := make(map[groupKey]int, len(groups))
	for i, group := range groups {
		groupIndices[groupKey{scope: group.Scope, ns: group.Namespace}] = i
	}

	var result []filteredRow
	first := true
	for _, key := range orderedGroupKeys(groupRows) {
		g := groupRows[key]
		if len(g.indices) == 0 {
			continue
		}
		groupIndex, ok := groupIndices[key]
		if !ok {
			continue
		}
		if !first {
			result = append(result, filteredRow{kind: separatorRow})
		}
		result = append(result, filteredRow{kind: headerRow, index: groupIndex})
		for _, workflowIndex := range g.indices {
			result = append(result, filteredRow{kind: workflowRow, index: workflowIndex})
		}
		first = false
	}
	return result
}

func orderedGroupKeys(groups map[groupKey]*workflowGroup) []groupKey {
	var order []groupKey
	for _, key := range []groupKey{
		{scope: discovery.ScopeProject},
		{scope: discovery.ScopeUser},
	} {
		if groups[key] != nil {
			order = append(order, key)
		}
	}

	emittedBuiltins := make(map[string]bool)
	for _, namespace := range builtinGroupOrder {
		key := groupKey{scope: discovery.ScopeBuiltin, ns: namespace}
		if groups[key] != nil {
			order = append(order, key)
			emittedBuiltins[namespace] = true
		}
	}

	var remaining []string
	for key := range groups {
		if key.scope != discovery.ScopeBuiltin || emittedBuiltins[key.ns] {
			continue
		}
		remaining = append(remaining, key.ns)
	}
	sort.Strings(remaining)
	for _, namespace := range remaining {
		order = append(order, groupKey{scope: discovery.ScopeBuiltin, ns: namespace})
	}

	return order
}

func matchesFilter(e *discovery.WorkflowEntry, lowerFilter string) bool {
	return strings.Contains(strings.ToLower(e.CanonicalName), lowerFilter) ||
		strings.Contains(strings.ToLower(e.SourcePath), lowerFilter)
}

// firstSelectableRow returns the index of the first workflow row in filtered,
// or 0 if there are none.
func firstSelectableRow(filtered []filteredRow) int {
	for i, row := range filtered {
		if row.kind == workflowRow {
			return i
		}
	}
	return 0
}

// newTabCurrentEntry returns the WorkflowEntry under the cursor, or nil if the
// cursor is on a separator or the list is empty.
func (m *Model) newTabCurrentEntry() *discovery.WorkflowEntry {
	f := m.newTab.filtered
	if len(f) == 0 {
		return nil
	}
	pos := m.newTab.cursor
	if pos < 0 || pos >= len(f) {
		return nil
	}
	row := f[pos]
	if row.kind != workflowRow || row.index < 0 || row.index >= len(m.newTab.workflows) {
		return nil
	}
	e := m.newTab.workflows[row.index]
	return &e
}

// newTabEnterCmd returns a tea.Cmd that emits ViewDefinitionMsg for the
// currently selected workflow, or nil if none is selected or the entry is malformed.
func (m *Model) newTabEnterCmd() tea.Cmd {
	e := m.newTabCurrentEntry()
	if e == nil || e.ParseError != "" {
		return nil
	}
	entry := *e
	return func() tea.Msg {
		return discovery.ViewDefinitionMsg{Entry: entry}
	}
}

// newTabStartRunCmd returns a tea.Cmd that emits StartRunMsg for the currently
// selected workflow, or nil if none is selected or the entry is malformed.
func (m *Model) newTabStartRunCmd() tea.Cmd {
	e := m.newTabCurrentEntry()
	if e == nil || e.ParseError != "" {
		return nil
	}
	entry := *e
	return func() tea.Msg {
		return discovery.StartRunMsg{Entry: entry}
	}
}

// renderNewTab renders the body of the "new" tab.
func (m *Model) renderNewTab() string {
	var b strings.Builder

	// Search box.
	b.WriteString(m.renderNewTabSearch())
	b.WriteString("\n\n")

	workflows := m.newTab.workflows
	if len(workflows) == 0 {
		return b.String() + tuistyle.ScreenMargin + dimStyle.Render("No workflows found.")
	}

	filtered := m.newTab.filtered
	count := 0
	for _, row := range filtered {
		if row.kind == workflowRow {
			count++
		}
	}
	if count == 0 {
		return b.String() + tuistyle.ScreenMargin + dimStyle.Render("No workflows match the filter.")
	}

	// Compute available width for descriptions.
	maxWidth := m.termWidth
	if maxWidth <= 0 {
		maxWidth = 80
	}

	// The new tab body includes the search row and a blank separator before the
	// workflow rows, so leave those rows out of the scrollable list budget.
	maxRows := max(1, m.listMaxRows(false)-2)

	// Render each row once, then use the resulting visual heights to pick an
	// offset that guarantees the cursor row fits in the budget even when a
	// wrapped header above it consumes multiple lines.
	rendered := make([]string, len(filtered))
	heights := make([]int, len(filtered))
	for i, row := range filtered {
		switch row.kind {
		case separatorRow:
			rendered[i] = "\n"
		case headerRow:
			group := &m.newTab.groups[row.index]
			groupColor := tuistyle.GroupColors[row.index%len(tuistyle.GroupColors)]
			rendered[i] = renderNewTabGroupHeader(group.DisplayName, group.Description, maxWidth, groupColor)
		case workflowRow:
			entry := &workflows[row.index]
			isSel := i == m.newTab.cursor && !m.newTab.searchFocused
			groupIndex := groupIndexForEntry(m.newTab.groups, entry)
			groupColor := tuistyle.GroupColors[groupIndex%len(tuistyle.GroupColors)]
			rendered[i] = m.renderNewTabRow(entry, isSel, maxWidth, groupColor)
		}
		heights[i] = max(1, strings.Count(rendered[i], "\n"))
	}

	m.newTab.offset = adjustOffsetByVisualHeight(m.newTab.cursor, m.newTab.offset, maxRows, heights)

	visualBudget := maxRows
	for i := m.newTab.offset; i < len(filtered); i++ {
		h := heights[i]
		// Always render the offset row and any row up to and including the
		// cursor; for later rows stop if they would overflow the budget.
		if i > m.newTab.cursor && h > visualBudget {
			break
		}
		b.WriteString(rendered[i])
		visualBudget -= h
		if visualBudget <= 0 && i >= m.newTab.cursor {
			break
		}
	}
	return b.String()
}

// adjustOffsetByVisualHeight returns an offset that keeps the cursor row
// visible given each row's rendered visual height (lines). It first ensures
// cursor >= offset, then advances offset so that the cumulative height from
// offset through cursor fits within maxRows. The cursor row itself is always
// included even if its own height exceeds maxRows.
func adjustOffsetByVisualHeight(cursor, offset, maxRows int, heights []int) int {
	if len(heights) == 0 {
		return 0
	}
	if maxRows <= 0 {
		return cursor
	}
	if cursor < offset {
		offset = cursor
	}
	for offset < cursor {
		sum := 0
		for i := offset; i <= cursor; i++ {
			sum += heights[i]
		}
		if sum <= maxRows {
			break
		}
		offset++
	}
	return offset
}

func groupIndexForEntry(groups []discovery.GroupMetadata, entry *discovery.WorkflowEntry) int {
	key := groupKey{scope: entry.Scope, ns: entry.Namespace}
	for i, group := range groups {
		if group.Scope == key.scope && group.Namespace == key.ns {
			return i
		}
	}
	return 0
}

// renderNewTabGroupHeader renders the group's display name in its accent color,
// followed by the description in dim style on the same line. The header sits
// flush against the screen margin (no cursor-column indent) so the group name
// reads as a section divider. Long descriptions wrap onto continuation lines
// aligned with the description's start column.
func renderNewTabGroupHeader(name, description string, maxWidth int, color lipgloss.AdaptiveColor) string {
	name = sanitize(name)
	prefix := tuistyle.ScreenMargin
	avail := max(10, maxWidth-lipgloss.Width(prefix))

	namePart := lipgloss.NewStyle().Foreground(color).Bold(true).Render(name)
	nameWidth := lipgloss.Width(namePart)
	if description = sanitize(description); description == "" {
		return prefix + namePart + "\n"
	}

	descAvail := avail - nameWidth - 1
	if descAvail < 4 {
		return prefix + namePart + "\n"
	}

	lines := wrapCell(description, descAvail)
	contIndent := strings.Repeat(" ", lipgloss.Width(prefix)+nameWidth+1)

	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(namePart)
	b.WriteByte(' ')
	b.WriteString(dimStyle.Render(lines[0]))
	b.WriteByte('\n')
	for _, line := range lines[1:] {
		b.WriteString(contIndent)
		b.WriteString(dimStyle.Render(line))
		b.WriteByte('\n')
	}
	return b.String()
}

// renderNewTabSearch renders the search box line.
func (m *Model) renderNewTabSearch() string {
	const searchIcon = "🔍 "
	var searchContent string
	if m.newTab.searchText == "" {
		searchContent = dimStyle.Render("Search...")
	} else {
		searchContent = tuistyle.NormalStyle.Render(m.newTab.searchText)
	}

	var prefix string
	if m.newTab.searchFocused {
		prefix = tuistyle.ScreenMargin + cursorStyle.Render("▶") + " "
	} else {
		prefix = tuistyle.ScreenMargin + "  "
	}

	count := 0
	for _, row := range m.newTab.filtered {
		if row.kind == workflowRow {
			count++
		}
	}
	countLabel := dimStyle.Render(formatCount(count))

	left := prefix + searchIcon + searchContent
	if m.termWidth > 0 {
		gap := m.termWidth - lipgloss.Width(left) - lipgloss.Width(countLabel)
		if gap > 0 {
			return left + strings.Repeat(" ", gap) + countLabel
		}
	}
	return left + "  " + countLabel
}

func formatCount(n int) string {
	if n == 1 {
		return "(1 workflow)"
	}
	return "(" + strconv.Itoa(n) + " workflows)"
}

// renderNewTabRow renders a single workflow row. Long descriptions wrap onto
// continuation lines aligned with the description's start column. CanonicalName,
// Description, and ParseError are sanitized so ANSI escapes or control bytes
// from external YAML cannot leak into the rendered output or distort width math.
func (m *Model) renderNewTabRow(entry *discovery.WorkflowEntry, isSel bool, maxWidth int, groupColor lipgloss.AdaptiveColor) string {
	var prefix string
	if isSel {
		prefix = tuistyle.ScreenMargin + cursorStyle.Render("▶") + " "
	} else {
		prefix = tuistyle.ScreenMargin + "  "
	}
	prefixWidth := lipgloss.Width(prefix)
	avail := max(10, maxWidth-prefixWidth)
	canonicalName := sanitize(entry.CanonicalName)
	nameCellWidth := runewidth.StringWidth(canonicalName)

	if entry.ParseError != "" {
		errNameStyle := tuistyle.StatusFailed
		errPart := " " + fitCell(sanitize(entry.ParseError), avail-nameCellWidth-1)
		return prefix + errNameStyle.Render(canonicalName) + errNameStyle.Render(errPart) + "\n"
	}

	var namePart string
	if isSel {
		namePart = renderSelectedName(canonicalName, m.newTab.searchText)
	} else {
		namePart = renderColoredName(canonicalName, m.newTab.searchText, groupColor)
	}

	description := sanitize(entry.Description)
	if description == "" {
		return prefix + namePart + "\n"
	}

	descAvail := avail - nameCellWidth - 1
	if descAvail < 4 {
		return prefix + namePart + "\n"
	}

	descStyle := dimStyle
	if isSel {
		descStyle = selectedStyle
	}

	lines := wrapCell(description, descAvail)
	contIndent := strings.Repeat(" ", prefixWidth+nameCellWidth+1)

	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(namePart)
	b.WriteByte(' ')
	b.WriteString(descStyle.Render(lines[0]))
	b.WriteByte('\n')
	for _, line := range lines[1:] {
		b.WriteString(contIndent)
		b.WriteString(descStyle.Render(line))
		b.WriteByte('\n')
	}
	return b.String()
}

// renderSelectedName uses the selected-text token instead of the group accent.
// On light terminals, accent+bold can be remapped to bright ANSI cyan by some
// terminal palettes.
func renderSelectedName(name, searchText string) string {
	if searchText != "" {
		return highlightMatch(name, searchText, true, tuistyle.SelectedText)
	}
	return selectedStyle.Bold(true).Render(name)
}

// renderColoredName renders a non-selected workflow name in the group color.
func renderColoredName(name, searchText string, color lipgloss.AdaptiveColor) string {
	if searchText != "" {
		return highlightMatch(name, searchText, false, color)
	}
	return lipgloss.NewStyle().Foreground(color).Render(name)
}

// highlightMatch returns the name string with the first occurrence of the search
// substring underlined, using color as the base color.
// Matching is case-insensitive via Unicode simple case-folding, working entirely
// in original-rune positions so no transformation can change index alignment.
func highlightMatch(name, filter string, selected bool, color lipgloss.AdaptiveColor) string {
	if filter == "" {
		return name
	}
	baseStyle := lipgloss.NewStyle().Foreground(color)
	if selected {
		baseStyle = baseStyle.Bold(true)
	}
	nr := []rune(name)
	fr := []rune(filter)
	idx := runeIndexFold(nr, fr)
	if idx < 0 {
		return baseStyle.Render(name)
	}
	before := string(nr[:idx])
	match := string(nr[idx : idx+len(fr)])
	after := string(nr[idx+len(fr):])

	matchStyle := baseStyle.Underline(true)
	return baseStyle.Render(before) + matchStyle.Render(match) + baseStyle.Render(after)
}

// runeIndexFold returns the rune index of the first occurrence of needle in
// haystack using Unicode simple case-folding for comparison, or -1.
// Indices refer to positions in haystack, never the lowercased form.
func runeIndexFold(haystack, needle []rune) int {
	nl := len(needle)
	if nl == 0 {
		return 0
	}
	for i := 0; i+nl <= len(haystack); i++ {
		match := true
		for j := range needle {
			if !runeEqualFold(haystack[i+j], needle[j]) {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// runeEqualFold reports whether a and b are equal under Unicode simple case folding.
func runeEqualFold(a, b rune) bool {
	if a == b {
		return true
	}
	for r := unicode.SimpleFold(a); r != a; r = unicode.SimpleFold(r) {
		if r == b {
			return true
		}
	}
	return false
}

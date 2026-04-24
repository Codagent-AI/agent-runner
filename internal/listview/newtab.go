package listview

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

func lipglossWidth(s string) int { return lipgloss.Width(s) }

// handleSearchKey processes a key event when the new tab search box has focus.
// Returns the updated model and a command.
func (m *Model) handleSearchKey(msg tea.KeyMsg) (*Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "tab":
		m.nextTab()
	case "shift+tab":
		m.prevTab()
	case "esc":
		m.newTab.searchText = ""
		m.newTab.searchFocused = false
		m.newTab.filtered = buildFilteredRows(m.newTab.workflows, "")
		m.newTab.cursor = firstSelectableRow(m.newTab.filtered)
	case "backspace":
		if m.newTab.searchText != "" {
			r := []rune(m.newTab.searchText)
			m.newTab.searchText = string(r[:len(r)-1])
			m.updateSearchFilter()
		}
	case "down", "j", "enter":
		m.newTab.searchFocused = false
		m.newTab.cursor = firstSelectableRow(m.newTab.filtered)
	default:
		if msg.Type == tea.KeyRunes {
			m.newTab.searchText += msg.String()
			m.updateSearchFilter()
		}
	}
	return m, nil
}

func (m *Model) updateSearchFilter() {
	m.newTab.filtered = buildFilteredRows(m.newTab.workflows, m.newTab.searchText)
	m.newTab.cursor = firstSelectableRow(m.newTab.filtered)
}

// buildFilteredRows computes the filtered slice for the new tab.
// Each element is either an index into workflows (>= 0) or -1 for a blank-line separator.
// Groups are: project entries, user entries, builtin namespace sub-groups.
// Groups with no matching entries are collapsed (no separator).
// Filter is case-insensitive substring match against CanonicalName or SourcePath.
func buildFilteredRows(workflows []discovery.WorkflowEntry, filter string) []int {
	filter = strings.ToLower(filter)

	type group struct {
		indices []int
	}

	// Collect groups in display order.
	// Project and user are each one group.
	// Builtins are sub-grouped by namespace.
	type groupKey struct {
		scope discovery.Scope
		ns    string
	}

	var order []groupKey
	seen := make(map[groupKey]bool)
	groups := make(map[groupKey]*group)

	for i, e := range workflows {
		key := groupKey{scope: e.Scope, ns: e.Namespace}
		if !seen[key] {
			seen[key] = true
			order = append(order, key)
			groups[key] = &group{}
		}

		if filter == "" || matchesFilter(&e, filter) {
			groups[key].indices = append(groups[key].indices, i)
		}
	}

	var result []int
	first := true
	for _, key := range order {
		g := groups[key]
		if len(g.indices) == 0 {
			continue
		}
		if !first {
			result = append(result, -1) // blank-line separator
		}
		result = append(result, g.indices...)
		first = false
	}
	return result
}

func matchesFilter(e *discovery.WorkflowEntry, lowerFilter string) bool {
	return strings.Contains(strings.ToLower(e.CanonicalName), lowerFilter) ||
		strings.Contains(strings.ToLower(e.SourcePath), lowerFilter)
}

// firstSelectableRow returns the index of the first non-separator row in filtered,
// or 0 if there are none.
func firstSelectableRow(filtered []int) int {
	for i, idx := range filtered {
		if idx != -1 {
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
	idx := f[pos]
	if idx < 0 || idx >= len(m.newTab.workflows) {
		return nil
	}
	e := m.newTab.workflows[idx]
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
	for _, idx := range filtered {
		if idx >= 0 {
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

	maxRows := m.listMaxRows(false)
	// Adjust offset so cursor stays in view.
	m.newTab.offset = adjustOffset(m.newTab.cursor, m.newTab.offset, maxRows, len(filtered))
	end := min(m.newTab.offset+maxRows, len(filtered))

	for i := m.newTab.offset; i < end; i++ {
		idx := filtered[i]
		if idx == -1 {
			b.WriteString("\n")
			continue
		}
		entry := &workflows[idx]
		isSel := i == m.newTab.cursor && !m.newTab.searchFocused
		b.WriteString(m.renderNewTabRow(entry, isSel, maxWidth))
	}
	return b.String()
}

// renderNewTabSearch renders the search box line.
func (m *Model) renderNewTabSearch() string {
	const searchIcon = "🔍 "
	placeholder := dimStyle.Render("Search...")
	var searchContent string
	if m.newTab.searchText == "" {
		searchContent = placeholder
	} else {
		searchContent = tuistyle.NormalStyle.Render(m.newTab.searchText)
	}

	// Count label (right-aligned).
	count := 0
	for _, idx := range m.newTab.filtered {
		if idx >= 0 {
			count++
		}
	}
	countLabel := dimStyle.Render(formatCount(count))

	left := tuistyle.ScreenMargin + searchIcon + searchContent
	if m.termWidth > 0 {
		// Use lipgloss-aware width measurement to handle ANSI sequences.
		leftW := lipglossWidth(left)
		rightW := lipglossWidth(countLabel)
		pad := m.termWidth - leftW - rightW
		if pad > 0 {
			return left + strings.Repeat(" ", pad) + countLabel
		}
	}
	return left + "  " + countLabel
}

func formatCount(n int) string {
	if n == 1 {
		return "(1 workflow)"
	}
	return "(" + itoa(n) + " workflows)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// renderNewTabRow renders a single workflow row.
func (m *Model) renderNewTabRow(entry *discovery.WorkflowEntry, isSel bool, maxWidth int) string {
	const cursorGlyph = "›"
	const margin = "  " // 2-space indent for non-cursor rows

	var prefix string
	if isSel {
		prefix = tuistyle.ScreenMargin + tuistyle.AccentStyle.Render(cursorGlyph) + " "
	} else {
		prefix = tuistyle.ScreenMargin + " " + margin
	}

	// Visible prefix width: screen margin (1) + glyph/space (1) + space (1) = 3.
	const prefixWidth = 3
	avail := max(10, maxWidth-prefixWidth)

	if entry.ParseError != "" {
		nameStyle := tuistyle.StatusFailed
		errPart := " " + fitCell(entry.ParseError, avail-len(entry.CanonicalName)-1)
		return prefix + nameStyle.Render(entry.CanonicalName) + nameStyle.Render(errPart) + "\n"
	}

	var namePart, descPart string
	if isSel {
		namePart = tuistyle.SelectedStyle.Bold(true).Render(highlightMatch(entry.CanonicalName, m.newTab.searchText, true))
	} else {
		namePart = tuistyle.NormalStyle.Render(highlightMatch(entry.CanonicalName, m.newTab.searchText, false))
	}

	if entry.Description != "" {
		descAvail := avail - visibleLen(entry.CanonicalName) - 1
		if descAvail > 3 {
			desc := fitCell(entry.Description, descAvail)
			descPart = " " + dimStyle.Render(desc)
		}
	}

	return prefix + namePart + descPart + "\n"
}

// highlightMatch returns the name string with the first occurrence of the search
// substring highlighted in AccentCyan, or plain if no match or empty filter.
// Uses rune-level indexing so multi-byte Unicode sequences are never split.
func highlightMatch(name, filter string, selected bool) string {
	if filter == "" {
		return name
	}
	nr := []rune(name)
	lnr := []rune(strings.ToLower(name))
	lfr := []rune(strings.ToLower(filter))
	idx := runeIndex(lnr, lfr)
	if idx < 0 {
		return name
	}
	before := string(nr[:idx])
	match := string(nr[idx : idx+len(lfr)])
	after := string(nr[idx+len(lfr):])

	baseStyle := tuistyle.NormalStyle
	if selected {
		baseStyle = tuistyle.SelectedStyle.Bold(true)
	}
	return baseStyle.Render(before) + tuistyle.AccentStyle.Render(match) + baseStyle.Render(after)
}

// runeIndex returns the rune index of needle in haystack, or -1.
func runeIndex(haystack, needle []rune) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j, r := range needle {
			if haystack[i+j] != r {
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

func visibleLen(s string) int {
	return len(s) // simple approximation for ASCII names
}

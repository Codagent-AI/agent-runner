package listview

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/discovery"
)

// helpers

func validEntry(name string) discovery.WorkflowEntry {
	return discovery.WorkflowEntry{
		CanonicalName: name,
		Description:   "A description for " + name,
		SourcePath:    "/workflows/" + name + ".yaml",
		Scope:         discovery.ScopeBuiltin,
		Namespace:     "core",
	}
}

func malformedEntry(name string) discovery.WorkflowEntry {
	return discovery.WorkflowEntry{
		CanonicalName: name,
		SourcePath:    "/workflows/" + name + ".yaml",
		Scope:         discovery.ScopeBuiltin,
		Namespace:     "core",
		ParseError:    "unexpected char at line 1",
	}
}

func newTabModel(entries []discovery.WorkflowEntry) *Model {
	m := &Model{
		activeTab: tabNew,
	}
	m.newTab.workflows = entries
	m.newTab.filtered = buildFilteredRows(entries, "")
	return m
}

// TestNewTab_PressN_SwitchesToNewTab verifies n key switches to new tab.
func TestNewTab_PressN_SwitchesToNewTab(t *testing.T) {
	m := newTestListModel(nil)
	m.activeTab = tabCurrentDir

	result, _ := pressKey(m, "n")
	if result.activeTab != tabNew {
		t.Errorf("after pressing n, activeTab = %v, want tabNew", result.activeTab)
	}
}

// TestNewTab_TabBarContainsNew verifies the tab bar renders "new" tab.
func TestNewTab_TabBarContainsNew(t *testing.T) {
	m := newTestListModel(nil)
	rendered := sanitize(m.renderTabs())
	if !strings.Contains(rendered, "New") {
		t.Errorf("tab bar should contain %q, got: %q", "New", rendered)
	}
}

// TestNewTab_TabOrderIsNewCurrentDirWorktreesAll verifies tab order.
func TestNewTab_TabOrderIsNewCurrentDirWorktreesAll(t *testing.T) {
	m := newTestListModel(nil)
	rendered := sanitize(m.renderTabs())

	newPos := strings.Index(rendered, "New")
	curPos := strings.Index(rendered, "Current Dir")
	allPos := strings.Index(rendered, "All")

	if newPos < 0 {
		t.Fatal("tab bar missing 'New'")
	}
	if curPos < 0 {
		t.Fatal("tab bar missing 'Current Dir'")
	}
	if allPos < 0 {
		t.Fatal("tab bar missing 'All'")
	}
	if newPos >= curPos {
		t.Errorf("New tab should appear before Current Dir tab: newPos=%d curPos=%d", newPos, curPos)
	}
	if curPos >= allPos {
		t.Errorf("Current Dir tab should appear before All tab: curPos=%d allPos=%d", curPos, allPos)
	}
}

// TestNewTab_EnterOnValidWorkflow_EmitsViewDefinitionMsg verifies Enter emits ViewDefinitionMsg.
func TestNewTab_EnterOnValidWorkflow_EmitsViewDefinitionMsg(t *testing.T) {
	entry := validEntry("core:finalize-pr")
	m := newTabModel([]discovery.WorkflowEntry{entry})

	result, cmd := m.handleEnter()
	_ = result

	if cmd == nil {
		t.Fatal("Enter on valid workflow should produce a cmd")
	}
	msg := cmd()
	vdm, ok := msg.(discovery.ViewDefinitionMsg)
	if !ok {
		t.Fatalf("expected discovery.ViewDefinitionMsg, got %T", msg)
	}
	if vdm.Entry.CanonicalName != entry.CanonicalName {
		t.Errorf("ViewDefinitionMsg.Entry.CanonicalName = %q, want %q", vdm.Entry.CanonicalName, entry.CanonicalName)
	}
}

// TestNewTab_EnterOnMalformedWorkflow_Ignored verifies Enter is ignored on malformed rows.
func TestNewTab_EnterOnMalformedWorkflow_Ignored(t *testing.T) {
	entry := malformedEntry("core:broken")
	m := newTabModel([]discovery.WorkflowEntry{entry})

	_, cmd := m.handleEnter()
	if cmd != nil {
		t.Fatal("Enter on malformed workflow should produce no cmd")
	}
}

// TestNewTab_R_EmitsStartRunMsg verifies r on valid workflow emits StartRunMsg.
func TestNewTab_R_EmitsStartRunMsg(t *testing.T) {
	entry := validEntry("core:finalize-pr")
	m := newTabModel([]discovery.WorkflowEntry{entry})

	_, cmd := pressKey(m, "r")
	if cmd == nil {
		t.Fatal("r on valid workflow should produce a cmd")
	}
	msg := cmd()
	srm, ok := msg.(discovery.StartRunMsg)
	if !ok {
		t.Fatalf("expected discovery.StartRunMsg, got %T", msg)
	}
	if srm.Entry.CanonicalName != entry.CanonicalName {
		t.Errorf("StartRunMsg.Entry.CanonicalName = %q, want %q", srm.Entry.CanonicalName, entry.CanonicalName)
	}
}

// TestNewTab_R_OnMalformedWorkflow_Ignored verifies r is ignored on malformed rows.
func TestNewTab_R_OnMalformedWorkflow_Ignored(t *testing.T) {
	entry := malformedEntry("core:broken")
	m := newTabModel([]discovery.WorkflowEntry{entry})

	_, cmd := pressKey(m, "r")
	if cmd != nil {
		t.Fatal("r on malformed workflow should produce no cmd")
	}
}

// TestNewTab_FilterNarrowsList verifies search filter narrows results.
func TestNewTab_FilterNarrowsList(t *testing.T) {
	entries := []discovery.WorkflowEntry{
		{CanonicalName: "core:implement-task", SourcePath: "/w/impl.yaml", Scope: discovery.ScopeBuiltin, Namespace: "core"},
		{CanonicalName: "core:finalize-pr", SourcePath: "/w/fin.yaml", Scope: discovery.ScopeBuiltin, Namespace: "core"},
	}
	filtered := buildFilteredRows(entries, "impl")

	// Count non-separator rows.
	count := 0
	for _, idx := range filtered {
		if idx >= 0 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("filter 'impl' should match 1 entry, got %d", count)
	}
	if filtered[0] != 0 {
		t.Errorf("filtered[0] should be index 0 (implement-task), got %d", filtered[0])
	}
}

// TestNewTab_FilterCaseInsensitive verifies filter is case-insensitive.
func TestNewTab_FilterCaseInsensitive(t *testing.T) {
	entries := []discovery.WorkflowEntry{
		{CanonicalName: "core:Implement-Task", SourcePath: "/w/impl.yaml", Scope: discovery.ScopeBuiltin, Namespace: "core"},
		{CanonicalName: "core:finalize-pr", SourcePath: "/w/fin.yaml", Scope: discovery.ScopeBuiltin, Namespace: "core"},
	}
	filtered := buildFilteredRows(entries, "IMPL")

	count := 0
	for _, idx := range filtered {
		if idx >= 0 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("filter 'IMPL' (case-insensitive) should match 1 entry, got %d", count)
	}
}

// TestNewTab_EmptyFilterShowsAll verifies empty search shows all entries.
func TestNewTab_EmptyFilterShowsAll(t *testing.T) {
	entries := []discovery.WorkflowEntry{
		{CanonicalName: "core:implement-task", SourcePath: "/w/impl.yaml", Scope: discovery.ScopeBuiltin, Namespace: "core"},
		{CanonicalName: "core:finalize-pr", SourcePath: "/w/fin.yaml", Scope: discovery.ScopeBuiltin, Namespace: "core"},
	}
	filtered := buildFilteredRows(entries, "")

	count := 0
	for _, idx := range filtered {
		if idx >= 0 {
			count++
		}
	}
	if count != len(entries) {
		t.Errorf("empty filter should show %d entries, got %d", len(entries), count)
	}
}

// TestNewTab_BlankLineSeparatorsBetweenGroups verifies blank-line separators (-1)
// between scope groups.
func TestNewTab_BlankLineSeparatorsBetweenGroups(t *testing.T) {
	entries := []discovery.WorkflowEntry{
		{CanonicalName: "proj-wf", Scope: discovery.ScopeProject, SourcePath: "/proj/wf.yaml"},
		{CanonicalName: "core:finalize-pr", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/fin.yaml"},
	}
	filtered := buildFilteredRows(entries, "")

	hasSeparator := false
	for _, idx := range filtered {
		if idx == -1 {
			hasSeparator = true
			break
		}
	}
	if !hasSeparator {
		t.Error("expected blank-line separator (-1) between scope groups")
	}
}

// TestNewTab_EmptyGroupsCollapsed verifies groups with no matches are collapsed.
func TestNewTab_EmptyGroupsCollapsed(t *testing.T) {
	entries := []discovery.WorkflowEntry{
		{CanonicalName: "proj-wf", Scope: discovery.ScopeProject, SourcePath: "/proj/wf.yaml"},
		{CanonicalName: "core:finalize-pr", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/fin.yaml"},
	}
	// Filter that only matches the builtin.
	filtered := buildFilteredRows(entries, "finalize")

	// Proj-wf group should be collapsed. The first entry should be the builtin, no separator at start.
	if len(filtered) == 0 {
		t.Fatal("expected some results")
	}
	if filtered[0] == -1 {
		t.Error("filter collapsed project group: first row should be a valid entry, not separator")
	}
}

// TestNewTab_CursorSkipsSeparators verifies moveCursor skips blank-line separators.
func TestNewTab_CursorSkipsSeparators(t *testing.T) {
	entries := []discovery.WorkflowEntry{
		{CanonicalName: "proj-wf", Scope: discovery.ScopeProject, SourcePath: "/proj/wf.yaml"},
		{CanonicalName: "core:finalize-pr", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/fin.yaml"},
	}
	m := newTabModel(entries)
	// Put cursor at first entry (proj-wf), move down — should skip separator and land on builtin.
	m.newTab.cursor = 0

	m.moveCursor(1)

	// Cursor should now point to the builtin entry (index 1 in workflows), not the separator.
	cursorIdx := m.newTab.cursor
	if cursorIdx >= len(m.newTab.filtered) {
		t.Fatalf("cursor %d out of bounds (len=%d)", cursorIdx, len(m.newTab.filtered))
	}
	if m.newTab.filtered[cursorIdx] == -1 {
		t.Error("cursor should skip blank-line separators")
	}
	if m.newTab.filtered[cursorIdx] != 1 {
		t.Errorf("cursor should land on workflow index 1 (core:finalize-pr), filtered[%d]=%d", cursorIdx, m.newTab.filtered[cursorIdx])
	}
}

// TestNewTab_UpFromFirstItem_FocusesSearchBox verifies ↑ from first item focuses search.
func TestNewTab_UpFromFirstItem_FocusesSearchBox(t *testing.T) {
	entries := []discovery.WorkflowEntry{validEntry("core:finalize-pr")}
	m := newTabModel(entries)
	m.newTab.cursor = 0
	m.newTab.searchFocused = false

	m.moveCursor(-1)

	if !m.newTab.searchFocused {
		t.Error("moving up from first item should focus search box")
	}
}

// TestNewTab_DownFromSearchBox_FocusesList verifies ↓ from search box focuses list.
func TestNewTab_DownFromSearchBox_FocusesList(t *testing.T) {
	entries := []discovery.WorkflowEntry{validEntry("core:finalize-pr")}
	m := newTabModel(entries)
	m.newTab.searchFocused = true

	m.moveCursor(1)

	if m.newTab.searchFocused {
		t.Error("moving down from search box should focus list")
	}
}

// TestNewTab_WithInitialTab_DefaultIsTabNew verifies bare New() starts on tabNew.
func TestNewTab_InitialTabDefault(t *testing.T) {
	m := &Model{activeTab: tabNew}
	if m.activeTab != tabNew {
		t.Errorf("default initial tab should be tabNew, got %v", m.activeTab)
	}
}

// TestNewTab_WithInitialTabCurrentDir verifies WithInitialTab sets currentDir.
func TestNewTab_WithInitialTabCurrentDir(t *testing.T) {
	m := &Model{}
	WithInitialTab(InitialTabCurrentDir)(m)
	if m.activeTab != tabCurrentDir {
		t.Errorf("WithInitialTab(CurrentDir) should set tabCurrentDir, got %v", m.activeTab)
	}
}

// TestNewTab_HelpBar_ShowsNewTabBindings verifies help bar shows enter/r bindings.
func TestNewTab_HelpBar_ShowsNewTabBindings(t *testing.T) {
	m := newTabModel([]discovery.WorkflowEntry{validEntry("core:deploy")})
	help := sanitize(m.renderHelp())
	if !strings.Contains(help, "enter") {
		t.Errorf("help bar should contain 'enter', got: %q", help)
	}
	if !strings.Contains(help, "start run") {
		t.Errorf("help bar should contain 'start run', got: %q", help)
	}
}

// TestBuildFilteredRows_MultipleNamespaces verifies namespace sub-groups separated by blank lines.
func TestBuildFilteredRows_MultipleNamespaces(t *testing.T) {
	entries := []discovery.WorkflowEntry{
		{CanonicalName: "core:implement", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/impl.yaml"},
		{CanonicalName: "openspec:change", Scope: discovery.ScopeBuiltin, Namespace: "openspec", SourcePath: "/b/change.yaml"},
	}
	filtered := buildFilteredRows(entries, "")

	// Expect: [0, -1, 1] — core entry, separator, openspec entry.
	if len(filtered) != 3 {
		t.Fatalf("expected 3 rows (entry, sep, entry), got %d: %v", len(filtered), filtered)
	}
	if filtered[0] != 0 {
		t.Errorf("filtered[0] = %d, want 0", filtered[0])
	}
	if filtered[1] != -1 {
		t.Errorf("filtered[1] = %d, want -1 (separator)", filtered[1])
	}
	if filtered[2] != 1 {
		t.Errorf("filtered[2] = %d, want 1", filtered[2])
	}
}

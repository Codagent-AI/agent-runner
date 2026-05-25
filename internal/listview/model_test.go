package listview

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/runs"
	"github.com/codagent/agent-runner/internal/settingseditor"
	"github.com/codagent/agent-runner/internal/usersettings"
)

func newTestListModel(runList []runs.RunInfo) *Model {
	return &Model{
		activeTab:   tabCurrentDir,
		currentRuns: runList,
	}
}

func inactiveRun() runs.RunInfo {
	return runs.RunInfo{
		SessionID:    "my-run-2026-04-19T10-00-00Z",
		WorkflowName: "implement",
		Status:       runs.StatusInactive,
	}
}

func activeRun() runs.RunInfo {
	return runs.RunInfo{
		SessionID:    "my-run-2026-04-19T11-00-00Z",
		WorkflowName: "implement",
		Status:       runs.StatusActive,
	}
}

func completedRun() runs.RunInfo {
	return runs.RunInfo{
		SessionID:    "my-run-2026-04-19T09-00-00Z",
		WorkflowName: "implement",
		Status:       runs.StatusCompleted,
	}
}

func pressKey(m *Model, key string) (*Model, tea.Cmd) {
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	return newModel.(*Model), cmd
}

func pressSpecialKey(m *Model, key tea.KeyType) (*Model, tea.Cmd) {
	newModel, cmd := m.Update(tea.KeyMsg{Type: key})
	return newModel.(*Model), cmd
}

type testRefreshMsg struct{}
type testPulseMsg struct{}

func installTestTickerCmds(t *testing.T) {
	t.Helper()
	oldRefresh := doRefresh
	oldPulse := doPulse
	doRefresh = func() tea.Cmd {
		return func() tea.Msg { return testRefreshMsg{} }
	}
	doPulse = func() tea.Cmd {
		return func() tea.Msg { return testPulseMsg{} }
	}
	t.Cleanup(func() {
		doRefresh = oldRefresh
		doPulse = oldPulse
	})
}

func collectCmdMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		msgs := make([]tea.Msg, 0, len(batch))
		for _, batchCmd := range batch {
			if batchCmd != nil {
				msgs = append(msgs, batchCmd())
			}
		}
		return msgs
	}
	return []tea.Msg{msg}
}

func countMessagesOfType[T any](msgs []tea.Msg) int {
	count := 0
	for _, msg := range msgs {
		if _, ok := msg.(T); ok {
			count++
		}
	}
	return count
}

func TestListModel_Init_SkipsPulseWhenVisibleRunsAreIdle(t *testing.T) {
	installTestTickerCmds(t)
	m := newTestListModel([]runs.RunInfo{inactiveRun(), completedRun()})

	msgs := collectCmdMessages(m.Init())

	if got := countMessagesOfType[testRefreshMsg](msgs); got != 1 {
		t.Fatalf("refresh commands = %d, want 1", got)
	}
	if got := countMessagesOfType[testPulseMsg](msgs); got != 0 {
		t.Fatalf("pulse commands = %d, want 0", got)
	}
}

func TestListModel_Init_SchedulesPulseWhenVisibleRunIsActive(t *testing.T) {
	installTestTickerCmds(t)
	m := newTestListModel([]runs.RunInfo{activeRun()})

	msgs := collectCmdMessages(m.Init())

	if got := countMessagesOfType[testRefreshMsg](msgs); got != 1 {
		t.Fatalf("refresh commands = %d, want 1", got)
	}
	if got := countMessagesOfType[testPulseMsg](msgs); got != 1 {
		t.Fatalf("pulse commands = %d, want 1", got)
	}
}

func TestListModel_PulseMsg_IdleVisibleRunsDoesNotRescheduleOrAdvance(t *testing.T) {
	installTestTickerCmds(t)
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.pulsePhase = 1.25

	_, cmd := m.Update(pulseMsg{})

	if cmd != nil {
		t.Fatal("idle list view should ignore stale pulse messages without rescheduling")
	}
	if m.pulsePhase != 1.25 {
		t.Fatalf("pulsePhase = %v, want 1.25", m.pulsePhase)
	}
}

// r on inactive run emits ResumeRunMsg with the model's cwd as ProjectDir.
func TestListModel_R_InactiveRun_EmitsResumeRunMsg(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.cwd = "/my/project"

	_, cmd := pressKey(m, "r")
	if cmd == nil {
		t.Fatal("r on inactive run should produce a cmd")
	}
	msg := cmd()
	rr, ok := msg.(ResumeRunMsg)
	if !ok {
		t.Fatalf("expected ResumeRunMsg, got %T", msg)
	}
	want := "my-run-2026-04-19T10-00-00Z"
	if rr.RunID != want {
		t.Fatalf("RunID = %q, want %q", rr.RunID, want)
	}
	if rr.ProjectDir != "/my/project" {
		t.Fatalf("ProjectDir = %q, want %q", rr.ProjectDir, "/my/project")
	}
}

// r on active run does nothing.
func TestListModel_R_ActiveRun_Ignored(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{activeRun()})

	_, cmd := pressKey(m, "r")
	if cmd != nil {
		t.Fatal("r on active run should produce no cmd")
	}
}

// r on completed run does nothing.
func TestListModel_R_CompletedRun_Ignored(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{completedRun()})

	_, cmd := pressKey(m, "r")
	if cmd != nil {
		t.Fatal("r on completed run should produce no cmd")
	}
}

// r on worktrees picker (not run-list) does nothing.
func TestListModel_R_WorktreePickerIgnored(t *testing.T) {
	m := newTestListModel(nil)
	m.activeTab = tabWorktrees
	m.worktreeTab.subView = subViewPicker
	m.worktreeTab.worktrees = []WorktreeEntry{{Name: "main", Path: "/repo"}}

	_, cmd := pressKey(m, "r")
	if cmd != nil {
		t.Fatal("r on worktree picker should produce no cmd")
	}
}

// r on worktrees run-list with inactive run emits ResumeRunMsg with the worktree path as ProjectDir.
func TestListModel_R_WorktreeRunList_InactiveRun_EmitsResumeRunMsg(t *testing.T) {
	r := inactiveRun()
	m := newTestListModel(nil)
	m.activeTab = tabWorktrees
	m.worktreeTab.subView = subViewRunList
	m.worktreeTab.selectedDir = "/repo"
	m.worktreeTab.worktrees = []WorktreeEntry{
		{Name: "main", Path: "/repo", Runs: []runs.RunInfo{r}},
	}

	_, cmd := pressKey(m, "r")
	if cmd == nil {
		t.Fatal("r on inactive run in worktree tab should produce a cmd")
	}
	msg := cmd()
	rr, ok := msg.(ResumeRunMsg)
	if !ok {
		t.Fatalf("expected ResumeRunMsg, got %T", msg)
	}
	if rr.RunID != r.SessionID {
		t.Fatalf("RunID = %q, want %q", rr.RunID, r.SessionID)
	}
	if rr.ProjectDir != "/repo" {
		t.Fatalf("ProjectDir = %q, want %q", rr.ProjectDir, "/repo")
	}
}

// r on all-tab run-list with inactive run emits ResumeRunMsg with the dir's path as ProjectDir.
func TestListModel_R_AllTabRunList_InactiveRun_EmitsResumeRunMsg(t *testing.T) {
	r := inactiveRun()
	m := newTestListModel(nil)
	m.activeTab = tabAll
	m.allTab.subView = subViewRunList
	m.allTab.selectedDir = "encoded-dir"
	m.allTab.dirs = []DirEntry{
		{Path: "/some/path", Encoded: "encoded-dir", Runs: []runs.RunInfo{r}},
	}

	_, cmd := pressKey(m, "r")
	if cmd == nil {
		t.Fatal("r on inactive run in all tab should produce a cmd")
	}
	msg := cmd()
	rr, ok := msg.(ResumeRunMsg)
	if !ok {
		t.Fatalf("expected ResumeRunMsg, got %T", msg)
	}
	if rr.RunID != r.SessionID {
		t.Fatalf("RunID = %q, want %q", rr.RunID, r.SessionID)
	}
	if rr.ProjectDir != "/some/path" {
		t.Fatalf("ProjectDir = %q, want %q", rr.ProjectDir, "/some/path")
	}
}

func TestListModel_QuestionMark_EmitsHelpStartRunMsg(t *testing.T) {
	m := newTestListModel(nil)
	m.activeTab = tabCurrentDir
	m.newTab.workflows = []discovery.WorkflowEntry{
		{CanonicalName: "onboarding:help", SourcePath: "builtin:onboarding/help.yaml", Namespace: "onboarding", Scope: discovery.ScopeBuiltin},
	}

	_, cmd := pressKey(m, "?")
	if cmd == nil {
		t.Fatal("? should produce a cmd")
	}
	msg := cmd()
	start, ok := msg.(discovery.StartRunMsg)
	if !ok {
		t.Fatalf("expected discovery.StartRunMsg, got %T", msg)
	}
	if start.Entry.CanonicalName != "onboarding:help" {
		t.Fatalf("CanonicalName = %q, want onboarding:help", start.Entry.CanonicalName)
	}
	if start.Entry.SourcePath != "builtin:onboarding/help.yaml" {
		t.Fatalf("SourcePath = %q, want builtin:onboarding/help.yaml", start.Entry.SourcePath)
	}
	if start.Entry.Scope != discovery.ScopeBuiltin {
		t.Fatalf("Scope = %v, want ScopeBuiltin", start.Entry.Scope)
	}
	if start.Entry.Namespace != "onboarding" {
		t.Fatalf("Namespace = %q, want onboarding", start.Entry.Namespace)
	}
}

func TestNewTab_HKeyTogglesHiddenVisibility(t *testing.T) {
	m := newTabModel([]discovery.WorkflowEntry{
		{CanonicalName: "core:visible", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/visible.yaml"},
		{CanonicalName: "core:hidden", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/hidden.yaml", Hidden: true},
	})

	m, _ = pressKey(m, "h")
	if !m.newTab.showHidden {
		t.Fatal("first h press should enable showHidden")
	}
	if got := workflowRows(m.newTab.filtered); len(got) != 2 {
		t.Fatalf("workflow rows after first h = %v, want visible and hidden", got)
	}

	m, _ = pressKey(m, "h")
	if m.newTab.showHidden {
		t.Fatal("second h press should disable showHidden")
	}
	if got := workflowRows(m.newTab.filtered); len(got) != 1 || got[0] != 0 {
		t.Fatalf("workflow rows after second h = %v, want only visible", got)
	}
}

func TestNewTab_HKeyPreservesSearchText(t *testing.T) {
	m := newTabModel([]discovery.WorkflowEntry{
		{CanonicalName: "core:visible", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/visible.yaml"},
		{CanonicalName: "core:hidden-target", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/hidden.yaml", Hidden: true},
	})
	m.newTab.searchText = "hidden"
	m.rebuildNewTabFiltered()

	m, _ = pressKey(m, "h")

	if m.newTab.searchText != "hidden" {
		t.Fatalf("search text = %q, want hidden", m.newTab.searchText)
	}
	if got := workflowRows(m.newTab.filtered); len(got) != 1 || got[0] != 1 {
		t.Fatalf("workflow rows = %v, want hidden search match", got)
	}
}

func TestNewTab_HKeyInSearchBoxAppendsInput(t *testing.T) {
	m := newTabModel([]discovery.WorkflowEntry{
		{CanonicalName: "core:hidden", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/hidden.yaml", Hidden: true},
	})
	m.newTab.searchFocused = true

	m, _ = pressKey(m, "h")

	if m.newTab.showHidden {
		t.Fatal("h while search is focused should not toggle showHidden")
	}
	if m.newTab.searchText != "h" {
		t.Fatalf("search text = %q, want h", m.newTab.searchText)
	}
}

func TestNewTab_EnteringNewTabResetsShowHidden(t *testing.T) {
	m := newTabModel([]discovery.WorkflowEntry{
		{CanonicalName: "core:visible", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/visible.yaml"},
		{CanonicalName: "core:hidden", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/hidden.yaml", Hidden: true},
	})
	m.newTab.showHidden = true
	m.newTab.filtered = buildFilteredRows(m.newTab.workflows, m.newTab.groups, "", true)
	m.activeTab = tabCurrentDir

	m, _ = pressKey(m, "n")

	if m.newTab.showHidden {
		t.Fatal("entering new tab with n should reset showHidden")
	}
	if got := workflowRows(m.newTab.filtered); len(got) != 1 || got[0] != 0 {
		t.Fatalf("workflow rows after entering new tab = %v, want only visible", got)
	}
}

func TestNewTab_TabCyclingIntoNewTabResetsShowHidden(t *testing.T) {
	m := newTabModel([]discovery.WorkflowEntry{
		{CanonicalName: "core:visible", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/visible.yaml"},
		{CanonicalName: "core:hidden", Scope: discovery.ScopeBuiltin, Namespace: "core", SourcePath: "/b/hidden.yaml", Hidden: true},
	})
	m.newTab.showHidden = true
	m.newTab.filtered = buildFilteredRows(m.newTab.workflows, m.newTab.groups, "", true)
	m.activeTab = tabAll

	m.nextTab()

	if m.activeTab != tabNew {
		t.Fatalf("active tab = %v, want tabNew", m.activeTab)
	}
	if m.newTab.showHidden {
		t.Fatal("tab cycling into new tab should reset showHidden")
	}
	if got := workflowRows(m.newTab.filtered); len(got) != 1 || got[0] != 0 {
		t.Fatalf("workflow rows after tab cycle = %v, want only visible", got)
	}
}

func TestListView_UsesSingleScreenMargin(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.cwd = "/repo/project"
	m.termWidth = 120

	help := sanitize(m.renderHelp())
	if !strings.HasPrefix(help, " ") || strings.HasPrefix(help, "  ") {
		t.Fatalf("help bar should use a single leading margin, got %q", help)
	}
}

func TestListView_RenderChromeShowsTabsAndLogoWhenWide(t *testing.T) {
	m := newTestListModel(nil)
	m.termWidth = 100

	chrome := sanitize(m.renderChrome())
	if !strings.Contains(chrome, "New") {
		t.Fatalf("chrome = %q, want tab labels", chrome)
	}
	if !strings.Contains(chrome, "█") {
		t.Fatalf("chrome = %q, want logo block chars", chrome)
	}
}

func TestListView_RenderHelpWithCwdDisplaysPath(t *testing.T) {
	m := newTestListModel(nil)
	m.cwd = "/repo/project"
	m.termWidth = 120

	helpLine := sanitize(m.renderHelpWithCwd())
	if !strings.Contains(helpLine, "/repo/project") {
		t.Fatalf("help line = %q, want cwd indicator", helpLine)
	}
	if !strings.Contains(helpLine, "q quit") {
		t.Fatalf("help line = %q, want help shortcuts", helpLine)
	}
}

func TestListView_RenderChromeDropsLogoWhenNarrow(t *testing.T) {
	m := newTestListModel(nil)
	m.termWidth = 40

	chrome := sanitize(m.renderChrome())
	if !strings.Contains(chrome, "New") {
		t.Fatalf("chrome = %q, want tabs even when narrow", chrome)
	}
	if strings.Contains(chrome, "█") {
		t.Fatalf("chrome should not contain logo at narrow width, got %q", chrome)
	}
}

func TestListView_RenderSubheaderExplainsTopLevelTabs(t *testing.T) {
	tests := []struct {
		name string
		m    *Model
		want string
	}{
		{
			name: "new",
			m: &Model{
				activeTab: tabNew,
			},
			want: "Browse and search workflow definitions. Press r to start a new run.",
		},
		{
			name: "current dir",
			m: &Model{
				activeTab: tabCurrentDir,
				cwd:       "/repo/project",
			},
			want: "All runs for /repo/project. Press enter to view a run.",
		},
		{
			name: "worktrees",
			m: &Model{
				activeTab: tabWorktrees,
				cwd:       "/repo/project",
				worktreeTab: worktreeTabState{
					repoName: "agent-runner",
				},
			},
			want: "All worktrees for agent-runner. Press enter to view runs.",
		},
		{
			name: "all",
			m: &Model{
				activeTab: tabAll,
			},
			want: "All directories with runs. Press enter to view runs.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitize(tt.m.renderSubheader())
			if !strings.Contains(got, tt.want) {
				t.Fatalf("subheader = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestListView_RenderEmptyRunScopes(t *testing.T) {
	tests := []struct {
		name string
		m    *Model
	}{
		{
			name: "current dir no runs",
			m: &Model{
				activeTab: tabCurrentDir,
			},
		},
		{
			name: "all no directories",
			m: &Model{
				activeTab: tabAll,
				allTab: allTabState{
					subView: subViewPicker,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitize(tt.m.renderBody())
			for _, want := range []string{
				"Nothing to see here yet",
				"From the new tab, select a workflow to get started",
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("empty body = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

func TestListView_RenderSubheaderExplainsRunListDrilldowns(t *testing.T) {
	tests := []struct {
		name string
		m    *Model
		want string
	}{
		{
			name: "worktree runs",
			m: &Model{
				activeTab: tabWorktrees,
				worktreeTab: worktreeTabState{
					subView:     subViewRunList,
					repoName:    "agent-runner",
					selectedDir: "/repo/wt",
					worktrees: []WorktreeEntry{
						{Name: "feature", Path: "/repo/wt"},
					},
				},
			},
			want: "All runs for agent-runner › feature. Press enter to view a run.",
		},
		{
			name: "all directory runs",
			m: &Model{
				activeTab: tabAll,
				allTab: allTabState{
					subView:     subViewRunList,
					selectedDir: "encoded",
					dirs: []DirEntry{
						{Path: "/repo/project", Encoded: "encoded"},
					},
				},
			},
			want: "All runs for /repo/project. Press enter to view a run.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitize(tt.m.renderSubheader())
			if !strings.Contains(got, tt.want) {
				t.Fatalf("subheader = %q, want to contain %q", got, tt.want)
			}
		})
	}
}

func TestListModel_S_OpensSettingsEditorOnRunList(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun(), activeRun(), completedRun()})
	m.currentDirCursor = 2
	m.currentDirOffset = 1
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{Theme: usersettings.ThemeDark}, nil
	}

	m, cmd := pressKey(m, "s")

	if cmd != nil {
		t.Fatal("opening settings should not produce a command")
	}
	if m.settingsEditor == nil {
		t.Fatal("settings editor was not opened")
	}
	if got := m.settingsEditor.SelectedTheme(); got != usersettings.ThemeDark {
		t.Fatalf("editor selected theme = %q, want dark", got)
	}
	if m.activeTab != tabCurrentDir || m.currentDirCursor != 2 || m.currentDirOffset != 1 {
		t.Fatalf("list state changed after opening editor: tab=%v cursor=%d offset=%d", m.activeTab, m.currentDirCursor, m.currentDirOffset)
	}
}

func TestListModel_S_IgnoredOnPickerSubViews(t *testing.T) {
	tests := []struct {
		name string
		m    *Model
	}{
		{
			name: "worktrees picker",
			m: &Model{
				activeTab: tabWorktrees,
				worktreeTab: worktreeTabState{
					subView:   subViewPicker,
					worktrees: []WorktreeEntry{{Name: "main", Path: "/repo"}},
				},
			},
		},
		{
			name: "all picker",
			m: &Model{
				activeTab: tabAll,
				allTab: allTabState{
					subView: subViewPicker,
					dirs:    []DirEntry{{Path: "/repo", Encoded: "repo"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loaded := false
			tt.m.loadSettings = func() (usersettings.Settings, error) {
				loaded = true
				return usersettings.Settings{Theme: usersettings.ThemeDark}, nil
			}

			next, cmd := pressKey(tt.m, "s")

			if cmd != nil {
				t.Fatal("s on picker should not produce a command")
			}
			if loaded {
				t.Fatal("s on picker should not load settings")
			}
			if next.settingsEditor != nil {
				t.Fatal("s on picker opened settings editor")
			}
		})
	}
}

func TestListModel_SettingsEditorSwallowsListKeys(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{Theme: usersettings.ThemeDark}, nil
	}
	m, _ = pressKey(m, "s")

	for _, key := range []string{"r", "n", "c", "?", "q"} {
		next, cmd := pressKey(m, key)
		m = next
		if cmd != nil {
			t.Fatalf("%q produced a command while editor is open", key)
		}
		if m.activeTab != tabCurrentDir {
			t.Fatalf("%q changed active tab to %v while editor is open", key, m.activeTab)
		}
		if m.quitting {
			t.Fatalf("%q quit while editor is open", key)
		}
		if m.settingsEditor == nil {
			t.Fatalf("%q closed the editor unexpectedly", key)
		}
	}
}

func TestListModel_SettingsEditorEnterCommitsAppliesThemeAndClosesEditor(t *testing.T) {
	var saved []usersettings.Settings
	var applied []usersettings.Theme
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.termWidth = 100
	m.termHeight = 30
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{Theme: usersettings.ThemeLight}, nil
	}
	m.saveSettings = func(settings usersettings.Settings) error {
		saved = append(saved, settings)
		return nil
	}
	m.applyTheme = func(theme usersettings.Theme) {
		applied = append(applied, theme)
	}
	m, _ = pressKey(m, "s")

	// Tab cycles the value locally without saving.
	m, cmd := pressSpecialKey(m, tea.KeyTab)
	if cmd != nil {
		t.Fatal("tab should not emit a save command; saves are deferred to Enter")
	}
	if len(saved) != 0 {
		t.Fatalf("saved count after Tab = %d, want 0", len(saved))
	}

	// Enter commits the pending change, fires SavedMsg, and the embedder closes.
	m, cmd = pressSpecialKey(m, tea.KeyEnter)
	if cmd == nil {
		t.Fatal("enter should emit a save command")
	}
	msg := cmd()
	if _, ok := msg.(settingseditor.SavedMsg); !ok {
		t.Fatalf("editor command emitted %T, want settingseditor.SavedMsg", msg)
	}

	next, rerender := m.Update(msg)
	m = next.(*Model)

	if m.settingsEditor != nil {
		t.Fatal("editor should close after Enter commits and SavedMsg is handled")
	}
	if len(saved) != 1 || saved[0].Theme != usersettings.ThemeDark {
		t.Fatalf("saved settings = %#v, want one dark save", saved)
	}
	if len(applied) != 1 || applied[0] != usersettings.ThemeDark {
		t.Fatalf("applied themes = %#v, want [dark]", applied)
	}
	if rerender == nil {
		t.Fatal("saved settings should force a render command")
	}
	if got, ok := rerender().(tea.WindowSizeMsg); !ok {
		t.Fatalf("rerender command emitted %T, want tea.WindowSizeMsg", rerender())
	} else if got.Width != 100 || got.Height != 30 {
		t.Fatalf("rerender size = %dx%d, want 100x30", got.Width, got.Height)
	}
}

func TestListModel_SettingsEditorEscDiscardsPendingChangesAndClosesEditor(t *testing.T) {
	saveCount := 0
	applyCount := 0
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{Theme: usersettings.ThemeLight}, nil
	}
	m.saveSettings = func(usersettings.Settings) error {
		saveCount++
		return nil
	}
	m.applyTheme = func(usersettings.Theme) {
		applyCount++
	}
	m, _ = pressKey(m, "s")

	// Cycle Theme locally, then Esc without committing.
	m, _ = pressSpecialKey(m, tea.KeyTab)
	m, cmd := pressSpecialKey(m, tea.KeyEsc)
	if cmd == nil {
		t.Fatal("esc should emit a cancel command")
	}
	next, closeCmd := m.Update(cmd())
	m = next.(*Model)

	if closeCmd != nil {
		t.Fatal("cancel should not force render")
	}
	if m.settingsEditor != nil {
		t.Fatal("cancelled settings editor should close")
	}
	if saveCount != 0 {
		t.Fatalf("Esc should not invoke save (got %d saves)", saveCount)
	}
	if applyCount != 0 {
		t.Fatalf("Esc should not invoke apply (got %d applies)", applyCount)
	}
}

func TestListModel_SettingsEditorEscClosesAndDoesNotInvokeSave(t *testing.T) {
	saveCount := 0
	applyCount := 0
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{Theme: usersettings.ThemeLight}, nil
	}
	m.saveSettings = func(usersettings.Settings) error {
		saveCount++
		return nil
	}
	m.applyTheme = func(usersettings.Theme) {
		applyCount++
	}
	m, _ = pressKey(m, "s")

	m, cmd := pressSpecialKey(m, tea.KeyEsc)
	if cmd == nil {
		t.Fatal("esc in settings editor should emit a cancel command")
	}
	next, closeCmd := m.Update(cmd())
	m = next.(*Model)

	if closeCmd != nil {
		t.Fatal("cancel should not force render")
	}
	if m.settingsEditor != nil {
		t.Fatal("cancelled settings editor should close")
	}
	if saveCount != 0 {
		t.Fatalf("esc should not invoke save (got %d saves)", saveCount)
	}
	if applyCount != 0 {
		t.Fatalf("esc should not invoke apply (got %d applies)", applyCount)
	}
}

func TestListModel_SettingsEditorSaveFailureStaysOpenAndDoesNotApply(t *testing.T) {
	applied := false
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{Theme: usersettings.ThemeLight}, nil
	}
	m.saveSettings = func(usersettings.Settings) error {
		return errors.New("permission denied")
	}
	m.settingsPath = func() (string, error) {
		return "/tmp/settings.yaml", nil
	}
	m.applyTheme = func(usersettings.Theme) {
		applied = true
	}
	m, _ = pressKey(m, "s")

	// Cycle the value, then Enter to commit. The save fails.
	m, _ = pressSpecialKey(m, tea.KeyTab)
	m, cmd := pressSpecialKey(m, tea.KeyEnter)

	if cmd != nil {
		t.Fatal("failed editor save should not emit SavedMsg")
	}
	if m.settingsEditor == nil {
		t.Fatal("editor should stay open on save failure")
	}
	if applied {
		t.Fatal("failed save should not apply theme")
	}
	view := m.View()
	if !strings.Contains(view, "/tmp/settings.yaml") || !strings.Contains(view, "permission denied") {
		t.Fatalf("View() missing save failure details:\n%s", view)
	}
}

func TestListModel_SettingsEditorCtrlCStillQuits(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{Theme: usersettings.ThemeDark}, nil
	}
	m, _ = pressKey(m, "s")

	m, cmd := pressSpecialKey(m, tea.KeyCtrlC)

	if !m.quitting {
		t.Fatal("ctrl+c should mark list as quitting even with editor open")
	}
	if cmd == nil {
		t.Fatal("ctrl+c should return tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c command = %T, want tea.QuitMsg", cmd())
	}
}

func TestListView_SplashOverlayRendersContentButtonsAndUnderlyingList(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.cwd = "/repo/project"
	m.termWidth = 120
	m.termHeight = 32
	m.splashVisible = true
	m.splashShown = true

	view := sanitize(m.View())

	for _, want := range []string{
		"Welcome to Agent Runner!",
		"Select a workflow and press 'r' to get started.",
		"From this screen you can also:",
		"• Browse runs by directory, worktree, or project",
		"• Press ? for help, s for settings, q to quit",
		"[ Got it ]",
		"[ Don't show again ]",
		"Current Dir",
		"implement",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q with splash visible:\n%s", want, view)
		}
	}
	if m.splashFocus != 0 {
		t.Fatalf("initial splash focus = %d, want Got it", m.splashFocus)
	}
}

func TestListModel_SplashSwallowsListKeys(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.cwd = "/repo/project"
	m.newTab.workflows = []discovery.WorkflowEntry{
		{CanonicalName: "onboarding:help", SourcePath: "builtin:onboarding/help.yaml", Namespace: "onboarding", Scope: discovery.ScopeBuiltin},
	}
	m.splashVisible = true
	m.splashShown = true
	m.loadSettings = func() (usersettings.Settings, error) {
		t.Fatal("splash should not let s open settings")
		return usersettings.Settings{}, nil
	}

	for _, key := range []string{"s", "?", "r", "q", "up", "down", "right"} {
		beforeTab := m.activeTab
		beforeCursor := m.currentDirCursor
		var cmd tea.Cmd
		if key == "up" || key == "down" || key == "right" {
			var keyType tea.KeyType
			switch key {
			case "up":
				keyType = tea.KeyUp
			case "down":
				keyType = tea.KeyDown
			case "right":
				keyType = tea.KeyRight
			}
			m, cmd = pressSpecialKey(m, keyType)
		} else {
			m, cmd = pressKey(m, key)
		}
		if cmd != nil {
			t.Fatalf("%q produced a command while splash is visible", key)
		}
		if !m.splashVisible {
			t.Fatalf("%q closed the splash unexpectedly", key)
		}
		if m.activeTab != beforeTab || m.currentDirCursor != beforeCursor {
			t.Fatalf("%q changed list state: tab %v->%v cursor %d->%d", key, beforeTab, m.activeTab, beforeCursor, m.currentDirCursor)
		}
		if m.settingsEditor != nil {
			t.Fatalf("%q opened settings while splash is visible", key)
		}
		if m.quitting {
			t.Fatalf("%q quit while splash is visible", key)
		}
	}

	m.splashFocus = 0
	m, cmd := pressSpecialKey(m, tea.KeyTab)
	if cmd != nil {
		t.Fatal("tab should not produce a command while splash is visible")
	}
	if m.splashFocus != 1 {
		t.Fatalf("splash focus after tab = %d, want Don't show again", m.splashFocus)
	}
	m, _ = pressSpecialKey(m, tea.KeyShiftTab)
	if m.splashFocus != 0 {
		t.Fatalf("splash focus after shift+tab = %d, want Got it", m.splashFocus)
	}
}

func TestListModel_SplashGotItClosesWithoutSaving(t *testing.T) {
	saveCount := 0
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.splashVisible = true
	m.splashShown = true
	m.saveSettings = func(usersettings.Settings) error {
		saveCount++
		return nil
	}

	m, cmd := pressSpecialKey(m, tea.KeyEnter)

	if cmd != nil {
		t.Fatal("Got it should not produce a command")
	}
	if m.splashVisible {
		t.Fatal("Got it should close the splash")
	}
	if saveCount != 0 {
		t.Fatalf("Got it saved settings %d times, want 0", saveCount)
	}
}

func TestListModel_SplashEscClosesWithoutSaving(t *testing.T) {
	saveCount := 0
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.splashVisible = true
	m.splashShown = true
	m.saveSettings = func(usersettings.Settings) error {
		saveCount++
		return nil
	}

	m, _ = pressSpecialKey(m, tea.KeyEsc)

	if m.splashVisible {
		t.Fatal("Esc should close the splash")
	}
	if saveCount != 0 {
		t.Fatalf("Esc saved settings %d times, want 0", saveCount)
	}
}

func TestListModel_SplashDontShowAgainPersistsDismissal(t *testing.T) {
	var saved []usersettings.Settings
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.splashVisible = true
	m.splashShown = true
	m.splashFocus = 1
	m.now = func() time.Time { return time.Date(2026, 5, 24, 12, 30, 0, 0, time.UTC) }
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{
			Theme:      usersettings.ThemeDark,
			Onboarding: usersettings.OnboardingSettings{Dismissed: "2026-05-23T00:00:00Z"},
		}, nil
	}
	m.saveSettings = func(settings usersettings.Settings) error {
		saved = append(saved, settings)
		return nil
	}

	m, cmd := pressSpecialKey(m, tea.KeyEnter)

	if cmd != nil {
		t.Fatal("Don't show again should not produce a command")
	}
	if m.splashVisible {
		t.Fatal("Don't show again should close the splash")
	}
	if len(saved) != 1 {
		t.Fatalf("saved count = %d, want 1", len(saved))
	}
	if saved[0].Splash.Dismissed != "2026-05-24T12:30:00Z" {
		t.Fatalf("Splash.Dismissed = %q, want 2026-05-24T12:30:00Z", saved[0].Splash.Dismissed)
	}
	if saved[0].Theme != usersettings.ThemeDark || saved[0].Onboarding.Dismissed != "2026-05-23T00:00:00Z" {
		t.Fatalf("saved settings lost unrelated fields: %#v", saved[0])
	}
}

func TestListModel_SplashSaveFailureClosesAndShowsInlineError(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.termWidth = 100
	m.termHeight = 30
	m.splashVisible = true
	m.splashShown = true
	m.splashFocus = 1
	m.now = func() time.Time { return time.Date(2026, 5, 24, 12, 30, 0, 0, time.UTC) }
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{}, nil
	}
	m.saveSettings = func(usersettings.Settings) error {
		return errors.New("permission denied")
	}

	m, _ = pressSpecialKey(m, tea.KeyEnter)

	if m.splashVisible {
		t.Fatal("failed Don't show again save should still close splash")
	}
	view := sanitize(m.View())
	if !strings.Contains(view, "could not save splash preference") || !strings.Contains(view, "permission denied") {
		t.Fatalf("View() missing splash save failure:\n%s", view)
	}
}

func TestListModel_SplashCtrlCQuitsWithoutSaving(t *testing.T) {
	saveCount := 0
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.splashVisible = true
	m.splashShown = true
	m.splashFocus = 1
	m.saveSettings = func(usersettings.Settings) error {
		saveCount++
		return nil
	}

	m, cmd := pressSpecialKey(m, tea.KeyCtrlC)

	if !m.quitting {
		t.Fatal("ctrl+c should mark list as quitting")
	}
	if cmd == nil {
		t.Fatal("ctrl+c should return tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c command = %T, want tea.QuitMsg", cmd())
	}
	if saveCount != 0 {
		t.Fatalf("ctrl+c saved settings %d times, want 0", saveCount)
	}
}

func TestListView_OnboardingFailureOverlayRendersContentButtonsAndUnderlyingList(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.cwd = "/repo/project"
	m.termWidth = 120
	m.termHeight = 32
	WithOnboardingFailure("/tmp/onboarding-run", "validator failed: missing config")(m)

	view := sanitize(m.View())

	for _, want := range []string{
		"Onboarding failed unexpectedly",
		"validator failed: missing config",
		"[ Debug now ]",
		"[ Skip ]",
		"Current Dir",
		"implement",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q with onboarding failure visible:\n%s", want, view)
		}
	}
	if m.onboardingFailureFocus != 0 {
		t.Fatalf("initial onboarding failure focus = %d, want Debug now", m.onboardingFailureFocus)
	}
}

func TestListModel_OnboardingFailureTogglesFocus(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	WithOnboardingFailure("/tmp/onboarding-run", "failed")(m)

	m, cmd := pressSpecialKey(m, tea.KeyTab)
	if cmd != nil {
		t.Fatal("tab should not produce a command while onboarding failure modal is visible")
	}
	if m.onboardingFailureFocus != 1 {
		t.Fatalf("focus after tab = %d, want Skip", m.onboardingFailureFocus)
	}
	m, _ = pressSpecialKey(m, tea.KeyLeft)
	if m.onboardingFailureFocus != 0 {
		t.Fatalf("focus after left = %d, want Debug now", m.onboardingFailureFocus)
	}
	m, _ = pressSpecialKey(m, tea.KeyRight)
	if m.onboardingFailureFocus != 1 {
		t.Fatalf("focus after right = %d, want Skip", m.onboardingFailureFocus)
	}
	m, _ = pressSpecialKey(m, tea.KeyShiftTab)
	if m.onboardingFailureFocus != 0 {
		t.Fatalf("focus after shift+tab = %d, want Debug now", m.onboardingFailureFocus)
	}
}

func TestListModel_OnboardingFailureDebugNowLaunchesDebugWithoutSaving(t *testing.T) {
	saveCount := 0
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	WithOnboardingFailure("/tmp/onboarding-run", "failed")(m)
	m.saveSettings = func(usersettings.Settings) error {
		saveCount++
		return nil
	}

	m, cmd := pressSpecialKey(m, tea.KeyEnter)

	if m.onboardingFailureVisible {
		t.Fatal("Debug now should close the onboarding failure modal")
	}
	if saveCount != 0 {
		t.Fatalf("Debug now saved settings %d times, want 0", saveCount)
	}
	if cmd == nil {
		t.Fatal("Debug now should produce a start-run command")
	}
	msg := cmd()
	start, ok := msg.(discovery.StartRunMsg)
	if !ok {
		t.Fatalf("Debug now command = %T, want discovery.StartRunMsg", msg)
	}
	if start.Entry.CanonicalName != "core:debug" {
		t.Fatalf("CanonicalName = %q, want core:debug", start.Entry.CanonicalName)
	}
	if got := start.Params["failed_session_dir"]; got != "/tmp/onboarding-run" {
		t.Fatalf("failed_session_dir = %q, want /tmp/onboarding-run", got)
	}
}

func TestListModel_OnboardingFailureSpaceActivatesFocusedSkip(t *testing.T) {
	var saved []usersettings.Settings
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	WithOnboardingFailure("/tmp/onboarding-run", "failed")(m)
	m.onboardingFailureFocus = 1
	m.now = func() time.Time { return time.Date(2026, 5, 24, 12, 30, 0, 0, time.UTC) }
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{Theme: usersettings.ThemeDark}, nil
	}
	m.saveSettings = func(settings usersettings.Settings) error {
		saved = append(saved, settings)
		return nil
	}

	m, cmd := pressSpecialKey(m, tea.KeySpace)

	if cmd != nil {
		t.Fatal("Skip should not produce a command")
	}
	if m.onboardingFailureVisible {
		t.Fatal("Skip should close the onboarding failure modal")
	}
	if len(saved) != 1 {
		t.Fatalf("saved count = %d, want 1", len(saved))
	}
	if saved[0].Onboarding.Dismissed != "2026-05-24T12:30:00Z" {
		t.Fatalf("Onboarding.Dismissed = %q, want 2026-05-24T12:30:00Z", saved[0].Onboarding.Dismissed)
	}
	if saved[0].Theme != usersettings.ThemeDark {
		t.Fatalf("saved settings lost unrelated fields: %#v", saved[0])
	}
}

func TestListModel_OnboardingFailureEscActsAsSkip(t *testing.T) {
	saveCount := 0
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	WithOnboardingFailure("/tmp/onboarding-run", "failed")(m)
	m.loadSettings = func() (usersettings.Settings, error) { return usersettings.Settings{}, nil }
	m.saveSettings = func(usersettings.Settings) error {
		saveCount++
		return nil
	}

	m, cmd := pressSpecialKey(m, tea.KeyEsc)

	if cmd != nil {
		t.Fatal("Esc-as-Skip should not produce a command")
	}
	if m.onboardingFailureVisible {
		t.Fatal("Esc should close the onboarding failure modal")
	}
	if saveCount != 1 {
		t.Fatalf("Esc saved settings %d times, want 1", saveCount)
	}
}

func TestListModel_OnboardingFailureSaveFailureClosesAndShowsInlineError(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.termWidth = 100
	m.termHeight = 30
	WithOnboardingFailure("/tmp/onboarding-run", "failed")(m)
	m.onboardingFailureFocus = 1
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{}, nil
	}
	m.saveSettings = func(usersettings.Settings) error {
		return errors.New("permission denied")
	}

	m, _ = pressSpecialKey(m, tea.KeyEnter)

	if m.onboardingFailureVisible {
		t.Fatal("failed Skip save should still close onboarding failure modal")
	}
	view := sanitize(m.View())
	if !strings.Contains(view, "could not save onboarding dismissal preference") || !strings.Contains(view, "permission denied") {
		t.Fatalf("View() missing onboarding dismissal save failure:\n%s", view)
	}
}

func TestListModel_OnboardingFailureCtrlCQuitsWithoutSaving(t *testing.T) {
	saveCount := 0
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	WithOnboardingFailure("/tmp/onboarding-run", "failed")(m)
	m.saveSettings = func(usersettings.Settings) error {
		saveCount++
		return nil
	}

	m, cmd := pressSpecialKey(m, tea.KeyCtrlC)

	if !m.quitting {
		t.Fatal("ctrl+c should mark list as quitting")
	}
	if cmd == nil {
		t.Fatal("ctrl+c should return tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c command = %T, want tea.QuitMsg", cmd())
	}
	if saveCount != 0 {
		t.Fatalf("ctrl+c saved settings %d times, want 0", saveCount)
	}
}

func TestListModel_OnboardingFailureSwallowsListKeys(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.cwd = "/repo/project"
	m.newTab.workflows = []discovery.WorkflowEntry{
		{CanonicalName: "onboarding:help", SourcePath: "builtin:onboarding/help.yaml", Namespace: "onboarding", Scope: discovery.ScopeBuiltin},
	}
	WithOnboardingFailure("/tmp/onboarding-run", "failed")(m)
	m.loadSettings = func() (usersettings.Settings, error) {
		t.Fatal("onboarding failure modal should not let s open settings")
		return usersettings.Settings{}, nil
	}

	for _, key := range []string{"s", "?", "r", "q", "up", "down"} {
		beforeTab := m.activeTab
		beforeCursor := m.currentDirCursor
		var cmd tea.Cmd
		if key == "up" || key == "down" {
			keyType := tea.KeyUp
			if key == "down" {
				keyType = tea.KeyDown
			}
			m, cmd = pressSpecialKey(m, keyType)
		} else {
			m, cmd = pressKey(m, key)
		}
		if cmd != nil {
			t.Fatalf("%q produced a command while onboarding failure modal is visible", key)
		}
		if !m.onboardingFailureVisible {
			t.Fatalf("%q closed the onboarding failure modal unexpectedly", key)
		}
		if m.activeTab != beforeTab || m.currentDirCursor != beforeCursor {
			t.Fatalf("%q changed list state: tab %v->%v cursor %d->%d", key, beforeTab, m.activeTab, beforeCursor, m.currentDirCursor)
		}
		if m.settingsEditor != nil {
			t.Fatalf("%q opened settings while onboarding failure modal is visible", key)
		}
		if m.quitting {
			t.Fatalf("%q quit while onboarding failure modal is visible", key)
		}
	}
}

func TestListView_SettingsOverlayKeepsUnderlyingListVisible(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.cwd = "/repo/project"
	m.termWidth = 100
	m.termHeight = 30
	m.settingsEditor = settingseditor.New(usersettings.Settings{Theme: usersettings.ThemeDark})

	view := sanitize(m.View())

	// The editor's compact view shows each field's current value only (not all
	// options). The test asserts both the underlying list ("implement",
	// "Current Dir") and the editor's content (field labels + current values)
	// are visible simultaneously.
	for _, want := range []string{"Current Dir", "implement", "Theme", "Autonomous Backend", "Dark"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q with settings editor open:\n%s", want, view)
		}
	}
}

func TestListView_HelpAdvertisesSettingsOnRunLists(t *testing.T) {
	tests := []struct {
		name string
		m    *Model
	}{
		{name: "new", m: &Model{activeTab: tabNew}},
		{name: "current dir empty", m: newTestListModel(nil)},
		{name: "current dir with runs", m: newTestListModel([]runs.RunInfo{inactiveRun()})},
		{
			name: "worktree run list",
			m: &Model{
				activeTab: tabWorktrees,
				worktreeTab: worktreeTabState{
					subView:     subViewRunList,
					selectedDir: "/repo",
					worktrees:   []WorktreeEntry{{Path: "/repo", Runs: []runs.RunInfo{inactiveRun()}}},
				},
			},
		},
		{
			name: "all run list",
			m: &Model{
				activeTab: tabAll,
				allTab: allTabState{
					subView:     subViewRunList,
					selectedDir: "repo",
					dirs:        []DirEntry{{Encoded: "repo", Runs: []runs.RunInfo{inactiveRun()}}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitize(tt.m.renderHelp()); !strings.Contains(got, "s settings") {
				t.Fatalf("help = %q, want s settings", got)
			}
		})
	}

	for _, tt := range []struct {
		name string
		m    *Model
	}{
		{name: "worktree picker", m: &Model{activeTab: tabWorktrees, worktreeTab: worktreeTabState{subView: subViewPicker, worktrees: []WorktreeEntry{{Path: "/repo"}}}}},
		{name: "all picker", m: &Model{activeTab: tabAll, allTab: allTabState{subView: subViewPicker, dirs: []DirEntry{{Encoded: "repo"}}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitize(tt.m.renderHelp()); strings.Contains(got, "s settings") {
				t.Fatalf("help = %q, did not expect s settings", got)
			}
		})
	}
}

package listview

import (
	"errors"
	"strings"
	"testing"

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

func TestListView_UsesSingleScreenMargin(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.cwd = "/repo/project"

	header := sanitize(m.renderHeader())
	if !strings.HasPrefix(header, " Agent Runner") || strings.HasPrefix(header, "  Agent Runner") {
		t.Fatalf("header should use a single leading margin, got %q", header)
	}

	help := sanitize(m.renderHelp())
	if !strings.HasPrefix(help, " ") || strings.HasPrefix(help, "  ") {
		t.Fatalf("help bar should use a single leading margin, got %q", help)
	}
}

func TestListView_RenderHeaderDisplaysVersionAndCWD(t *testing.T) {
	m := newTestListModel(nil)
	m.cwd = "/repo/project"
	m.termWidth = 60
	WithVersion("0.7.0")(m)

	header := sanitize(m.renderHeader())
	if !strings.HasPrefix(header, " Agent Runner v0.7.0") {
		t.Fatalf("header = %q, want title followed by version", header)
	}
	if !strings.Contains(header, "/repo/project") {
		t.Fatalf("header = %q, want cwd indicator", header)
	}
}

func TestListView_RenderHeaderDisplaysDevVersion(t *testing.T) {
	m := newTestListModel(nil)
	WithVersion("dev")(m)

	header := sanitize(m.renderHeader())
	if !strings.HasPrefix(header, " Agent Runner vdev") {
		t.Fatalf("header = %q, want dev version", header)
	}
}

func TestListView_RenderHeaderDropsCWDButKeepsVersionWhenNarrow(t *testing.T) {
	m := newTestListModel(nil)
	m.cwd = "/repo/project"
	m.termWidth = len(" Agent Runner v0.7.0") + 2
	WithVersion("0.7.0")(m)

	header := sanitize(m.renderHeader())
	if header != " Agent Runner v0.7.0" {
		t.Fatalf("header = %q, want only title and version", header)
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

func TestListModel_SettingsEditorSaveAppliesThemeClosesAndForcesRender(t *testing.T) {
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
	m, _ = pressSpecialKey(m, tea.KeyRight)

	m, cmd := pressSpecialKey(m, tea.KeyEnter)
	if cmd == nil {
		t.Fatal("enter in settings editor should emit a save command")
	}
	msg := cmd()
	if _, ok := msg.(settingseditor.SavedMsg); !ok {
		t.Fatalf("editor command emitted %T, want settingseditor.SavedMsg", msg)
	}

	next, rerender := m.Update(msg)
	m = next.(*Model)

	if m.settingsEditor != nil {
		t.Fatal("saved settings editor should close")
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

func TestListModel_SettingsEditorCancelClosesWithoutSavingOrApplying(t *testing.T) {
	saved := false
	applied := false
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.loadSettings = func() (usersettings.Settings, error) {
		return usersettings.Settings{Theme: usersettings.ThemeLight}, nil
	}
	m.saveSettings = func(usersettings.Settings) error {
		saved = true
		return nil
	}
	m.applyTheme = func(usersettings.Theme) {
		applied = true
	}
	m, _ = pressKey(m, "s")
	m, _ = pressSpecialKey(m, tea.KeyRight)

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
	if saved {
		t.Fatal("cancel should not save")
	}
	if applied {
		t.Fatal("cancel should not apply theme")
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
	m, _ = pressSpecialKey(m, tea.KeyRight)

	m, cmd := pressSpecialKey(m, tea.KeyEnter)

	if cmd != nil {
		t.Fatal("failed editor save should not emit completion command")
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

func TestListView_SettingsOverlayKeepsUnderlyingListVisible(t *testing.T) {
	m := newTestListModel([]runs.RunInfo{inactiveRun()})
	m.cwd = "/repo/project"
	m.termWidth = 100
	m.termHeight = 30
	m.settingsEditor = settingseditor.New(usersettings.Settings{Theme: usersettings.ThemeDark})

	view := sanitize(m.View())

	for _, want := range []string{"Agent Runner", "Current Dir", "implement", "Theme", "Light", "Dark"} {
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

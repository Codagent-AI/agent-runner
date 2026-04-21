package listview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/runs"
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

package runview

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/discovery"
)

func TestModel_HelpBar_FromDefinition_ShowsStartRunBinding(t *testing.T) {
	entry := testDefinitionEntry()
	m := buildFromDefinitionModel(&entry, "")

	help := m.renderHelpBar()
	if !containsString(help, "r start run") {
		t.Fatalf("help bar should show 'r start run' in definition mode: %q", help)
	}
	if containsString(help, "r resume") {
		t.Fatalf("help bar should not show 'r resume' in definition mode: %q", help)
	}
}

func TestModel_Esc_FromLiveRunTerminal_EmitsResumeListMsg(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = false
	m.liveResult = "success"

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc after a live run finishes should produce a cmd")
	}
	if _, ok := cmd().(ResumeListMsg); !ok {
		t.Fatalf("expected ResumeListMsg, got %T", cmd())
	}
}

func TestModel_Esc_FromListCompletedRun_EmitsResumeListMsg(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusSuccess
	m := newTestModel(tree, FromList)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc on a completed run should produce a cmd")
	}
	if _, ok := cmd().(ResumeListMsg); !ok {
		t.Fatalf("expected ResumeListMsg, got %T", cmd())
	}
}

func TestModel_Esc_FromInspectTerminal_StillEmitsExitMsg(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusSuccess
	m := newTestModel(tree, FromInspect)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Esc in inspect mode should produce a cmd")
	}
	if _, ok := cmd().(ExitMsg); !ok {
		t.Fatalf("expected ExitMsg, got %T", cmd())
	}
}

func testDefinitionEntry() discovery.WorkflowEntry {
	return discovery.WorkflowEntry{
		CanonicalName: "core:finalize-pr",
		SourcePath:    "builtin:core/finalize-pr.yaml",
		Scope:         discovery.ScopeBuiltin,
	}
}

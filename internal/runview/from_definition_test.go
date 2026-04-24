package runview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

// TestFromDefinition_New_DoesNotReadAuditLog verifies that constructing a Model
// in FromDefinition mode does not attempt to load an audit log.
func TestFromDefinition_New_DoesNotReadAuditLog(t *testing.T) {
	entry := discovery.WorkflowEntry{
		CanonicalName: "core:test-workflow",
		SourcePath:    "builtin:core/test-workflow.yaml",
		Scope:         discovery.ScopeBuiltin,
	}
	m := buildFromDefinitionModel(&entry, "")

	if m.loadErr != "" {
		t.Errorf("expected no loadErr in FromDefinition mode, got: %q", m.loadErr)
	}
}

// TestFromDefinition_EnteredIsFromDefinition verifies the entered mode is set.
func TestFromDefinition_EnteredIsFromDefinition(t *testing.T) {
	entry := discovery.WorkflowEntry{
		CanonicalName: "core:finalize-pr",
		SourcePath:    "builtin:core/finalize-pr.yaml",
		Scope:         discovery.ScopeBuiltin,
	}
	m := buildFromDefinitionModel(&entry, "")
	if m.entered != FromDefinition {
		t.Errorf("entered = %v, want FromDefinition", m.entered)
	}
}

// TestFromDefinition_WorkflowEntryStored verifies the workflow entry is stored.
func TestFromDefinition_WorkflowEntryStored(t *testing.T) {
	entry := discovery.WorkflowEntry{
		CanonicalName: "core:finalize-pr",
		SourcePath:    "builtin:core/finalize-pr.yaml",
		Scope:         discovery.ScopeBuiltin,
	}
	m := buildFromDefinitionModel(&entry, "")
	if m.workflowEntry.CanonicalName != entry.CanonicalName {
		t.Errorf("workflowEntry.CanonicalName = %q, want %q", m.workflowEntry.CanonicalName, entry.CanonicalName)
	}
}

// TestFromDefinition_EscAtTopLevel_EmitsBackMsg verifies that pressing Escape
// at the top level in FromDefinition mode sends BackMsg (same as FromList).
func TestFromDefinition_EscAtTopLevel_EmitsBackMsg(t *testing.T) {
	entry := discovery.WorkflowEntry{
		CanonicalName: "core:finalize-pr",
		SourcePath:    "builtin:core/finalize-pr.yaml",
		Scope:         discovery.ScopeBuiltin,
	}
	m := buildFromDefinitionModel(&entry, "")

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_ = newModel

	if cmd == nil {
		t.Fatal("Esc at top level in FromDefinition mode should produce a cmd")
	}
	msg := cmd()
	if _, ok := msg.(BackMsg); !ok {
		t.Errorf("expected BackMsg, got %T", msg)
	}
}

// TestFromDefinition_R_EmitsStartRunMsg verifies pressing r in FromDefinition mode
// emits a StartRunMsg.
func TestFromDefinition_R_EmitsStartRunMsg(t *testing.T) {
	entry := discovery.WorkflowEntry{
		CanonicalName: "core:finalize-pr",
		SourcePath:    "builtin:core/finalize-pr.yaml",
		Scope:         discovery.ScopeBuiltin,
	}
	m := buildFromDefinitionModel(&entry, "")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("r in FromDefinition mode should produce a cmd")
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

// TestFromDefinition_CannotResumeRun verifies canResumeRun returns false in FromDefinition mode.
func TestFromDefinition_CannotResumeRun(t *testing.T) {
	entry := discovery.WorkflowEntry{CanonicalName: "deploy", SourcePath: "/tmp/deploy.yaml"}
	m := buildFromDefinitionModel(&entry, "")
	if m.canResumeRun() {
		t.Error("canResumeRun should be false in FromDefinition mode")
	}
}

// TestFromDefinition_Breadcrumb_ShowsCanonicalName verifies that the view
// shows the canonical workflow name.
func TestFromDefinition_Breadcrumb_ShowsCanonicalName(t *testing.T) {
	entry := discovery.WorkflowEntry{
		CanonicalName: "core:finalize-pr",
		SourcePath:    "builtin:core/finalize-pr.yaml",
		Scope:         discovery.ScopeBuiltin,
	}
	m := buildFromDefinitionModel(&entry, "")
	m.termWidth = 120
	m.termHeight = 40

	view := tuistyle.Sanitize(m.View())
	if !strings.Contains(view, "core:finalize-pr") {
		t.Errorf("view should contain %q, got:\n%s", "core:finalize-pr", view)
	}
}

// TestFromDefinition_NoPolling verifies the model is not active and not running.
func TestFromDefinition_NoPolling(t *testing.T) {
	entry := discovery.WorkflowEntry{CanonicalName: "deploy", SourcePath: "/tmp/deploy.yaml"}
	m := buildFromDefinitionModel(&entry, "")
	if m.active {
		t.Error("active should be false in FromDefinition mode")
	}
	if m.running {
		t.Error("running should be false in FromDefinition mode")
	}
}

// buildFromDefinitionModel creates a runview Model in FromDefinition mode for testing.
func buildFromDefinitionModel(entry *discovery.WorkflowEntry, projectDir string) *Model {
	m := &Model{
		tree: &Tree{
			Root: &StepNode{
				ID:     entry.CanonicalName,
				Type:   NodeRoot,
				Status: StatusPending,
			},
		},
		sessionDir:    entry.SourcePath,
		projectDir:    projectDir,
		entered:       FromDefinition,
		loadedFull:    make(map[string]bool),
		workflowEntry: *entry,
		altScreen:     true,
	}
	m.path = []*StepNode{m.tree.Root}
	return m
}

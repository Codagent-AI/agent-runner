package paramform_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/discovery"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/paramform"
)

func boolPtr(b bool) *bool { return &b }

func entryWithParams(params []model.Param) discovery.WorkflowEntry {
	return discovery.WorkflowEntry{
		CanonicalName: "my-workflow",
		Description:   "A test workflow",
		Params:        params,
	}
}

// TestNew_InputsCreatedForEachParam verifies one input per param in declaration order.
func TestNew_InputsCreatedForEachParam(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "task_file"},
		{Name: "branch"},
		{Name: "tag"},
	})
	m := paramform.New(&entry)
	if m.InputCount() != 3 {
		t.Fatalf("InputCount() = %d, want 3", m.InputCount())
	}
	if m.InputName(0) != "task_file" {
		t.Errorf("InputName(0) = %q, want %q", m.InputName(0), "task_file")
	}
	if m.InputName(1) != "branch" {
		t.Errorf("InputName(1) = %q, want %q", m.InputName(1), "branch")
	}
	if m.InputName(2) != "tag" {
		t.Errorf("InputName(2) = %q, want %q", m.InputName(2), "tag")
	}
}

// TestNew_DefaultPrePopulated verifies params with Default have their input pre-populated.
func TestNew_DefaultPrePopulated(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "branch", Default: "main"},
		{Name: "tag"},
	})
	m := paramform.New(&entry)
	if m.InputValue(0) != "main" {
		t.Errorf("InputValue(0) = %q, want %q", m.InputValue(0), "main")
	}
	if m.InputValue(1) != "" {
		t.Errorf("InputValue(1) = %q, want empty", m.InputValue(1))
	}
}

// TestNew_FirstInputFocused verifies the first input starts focused.
func TestNew_FirstInputFocused(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "a"},
		{Name: "b"},
	})
	m := paramform.New(&entry)
	if m.FocusedIndex() != 0 {
		t.Errorf("FocusedIndex() = %d, want 0", m.FocusedIndex())
	}
}

// TestTab_MovesForward verifies Tab moves focus to the next input.
func TestTab_MovesForward(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
	})
	m := paramform.New(&entry)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(*paramform.Model)
	if m.FocusedIndex() != 1 {
		t.Errorf("after Tab: FocusedIndex() = %d, want 1", m.FocusedIndex())
	}
}

// TestTab_WrapsToStartButton verifies Tab wraps from last input to Start button (-1).
func TestTab_WrapsToStartButton(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "a"},
		{Name: "b"},
	})
	m := paramform.New(&entry)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(*paramform.Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(*paramform.Model)
	if m.FocusedIndex() != -1 {
		t.Errorf("after Tab past all inputs: FocusedIndex() = %d, want -1 (Start button)", m.FocusedIndex())
	}
}

// TestTab_WrapsFromStartButtonToFirst verifies Tab wraps from Start button back to first input.
func TestTab_WrapsFromStartButtonToFirst(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "a"},
	})
	m := paramform.New(&entry)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(*paramform.Model)
	if m.FocusedIndex() != -1 {
		t.Fatalf("expected Start button focus, got %d", m.FocusedIndex())
	}
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(*paramform.Model)
	if m.FocusedIndex() != 0 {
		t.Errorf("after Tab from Start: FocusedIndex() = %d, want 0", m.FocusedIndex())
	}
}

// TestShiftTab_MovesBackward verifies Shift+Tab moves focus backward.
func TestShiftTab_MovesBackward(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "a"},
		{Name: "b"},
	})
	m := paramform.New(&entry)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(*paramform.Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = m2.(*paramform.Model)
	if m.FocusedIndex() != 0 {
		t.Errorf("after Shift+Tab: FocusedIndex() = %d, want 0", m.FocusedIndex())
	}
}

// TestEnter_OnLastInput_Submits verifies Enter on the last input triggers submission.
func TestEnter_OnLastInput_Submits(t *testing.T) {
	required := true
	entry := entryWithParams([]model.Param{
		{Name: "task_file", Required: &required},
	})
	m := paramform.New(&entry)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("my-task.md")})
	m = m2.(*paramform.Model)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = m2
	if cmd == nil {
		t.Fatal("Enter on last input with required fields filled should produce a cmd")
	}
	msg := cmd()
	submitted, ok := msg.(paramform.SubmittedMsg)
	if !ok {
		t.Fatalf("expected SubmittedMsg, got %T", msg)
	}
	if submitted["task_file"] != "my-task.md" {
		t.Errorf("submitted[task_file] = %q, want %q", submitted["task_file"], "my-task.md")
	}
}

// TestEnter_OnStartButton_Submits verifies Enter on the Start button triggers submission.
func TestEnter_OnStartButton_Submits(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "branch", Default: "main"},
	})
	m := paramform.New(&entry)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(*paramform.Model)
	if m.FocusedIndex() != -1 {
		t.Fatalf("expected Start button focus, got %d", m.FocusedIndex())
	}
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = m2
	if cmd == nil {
		t.Fatal("Enter on Start button should produce a cmd")
	}
	msg := cmd()
	if _, ok := msg.(paramform.SubmittedMsg); !ok {
		t.Fatalf("expected SubmittedMsg, got %T", msg)
	}
}

// TestSubmit_MissingRequired_ShowsError verifies missing required param prevents submission.
func TestSubmit_MissingRequired_ShowsError(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "task_file", Required: boolPtr(true)},
	})
	m := paramform.New(&entry)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(*paramform.Model)
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(paramform.SubmittedMsg); ok {
			t.Fatal("should not emit SubmittedMsg when required field is empty")
		}
	}
	if m.InputError(0) == "" {
		t.Error("expected error for empty required field, got empty string")
	}
}

// TestSubmit_NilRequired_TreatedAsRequired verifies nil Required defaults to required.
func TestSubmit_NilRequired_TreatedAsRequired(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "task_file"},
	})
	m := paramform.New(&entry)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(*paramform.Model)
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(paramform.SubmittedMsg); ok {
			t.Fatal("nil Required should default to required; should not submit empty")
		}
	}
	if m.InputError(0) == "" {
		t.Error("expected error for nil-required field left empty")
	}
}

// TestSubmit_OptionalEmpty_Passes verifies optional param left empty does not block submission.
func TestSubmit_OptionalEmpty_Passes(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "tag", Required: boolPtr(false)},
	})
	m := paramform.New(&entry)
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = m2
	if cmd == nil {
		t.Fatal("optional empty field should allow submission")
	}
	msg := cmd()
	submitted, ok := msg.(paramform.SubmittedMsg)
	if !ok {
		t.Fatalf("expected SubmittedMsg, got %T", msg)
	}
	if submitted["tag"] != "" {
		t.Errorf("submitted[tag] = %q, want empty", submitted["tag"])
	}
}

// TestSubmit_DefaultNotEdited_UsesDefault verifies default value is used when field is not edited.
func TestSubmit_DefaultNotEdited_UsesDefault(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "branch", Default: "main"},
	})
	m := paramform.New(&entry)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("should submit since default is present")
	}
	msg := cmd()
	submitted, ok := msg.(paramform.SubmittedMsg)
	if !ok {
		t.Fatalf("expected SubmittedMsg, got %T", msg)
	}
	if submitted["branch"] != "main" {
		t.Errorf("submitted[branch] = %q, want %q", submitted["branch"], "main")
	}
}

// TestEscape_EmitsCancelledMsg verifies Escape emits CancelledMsg.
func TestEscape_EmitsCancelledMsg(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "task_file"},
	})
	m := paramform.New(&entry)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("Escape should produce a cmd")
	}
	msg := cmd()
	if _, ok := msg.(paramform.CancelledMsg); !ok {
		t.Fatalf("expected CancelledMsg, got %T", msg)
	}
}

// TestView_RendersWorkflowName verifies the view shows the workflow canonical name.
func TestView_RendersWorkflowName(t *testing.T) {
	entry := entryWithParams([]model.Param{{Name: "task_file"}})
	m := paramform.New(&entry).WithWidth(120)
	v := m.View()
	if v == "" {
		t.Fatal("View() should return non-empty string")
	}
	if !contains(v, "my-workflow") {
		t.Errorf("View() does not contain workflow name %q:\n%s", "my-workflow", v)
	}
}

// TestView_RendersParamLabels verifies the view shows param labels.
func TestView_RendersParamLabels(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "task_file"},
		{Name: "branch"},
	})
	m := paramform.New(&entry).WithWidth(120)
	v := m.View()
	if !contains(v, "task_file") {
		t.Errorf("View() missing label %q", "task_file")
	}
	if !contains(v, "branch") {
		t.Errorf("View() missing label %q", "branch")
	}
}

// TestView_RendersHelpBar verifies the view shows navigation hints.
func TestView_RendersHelpBar(t *testing.T) {
	entry := entryWithParams([]model.Param{{Name: "task_file"}})
	m := paramform.New(&entry).WithWidth(120)
	v := m.View()
	if !contains(v, "tab") {
		t.Errorf("View() help bar missing 'tab' hint:\n%s", v)
	}
	if !contains(v, "esc") {
		t.Errorf("View() help bar missing 'esc' hint:\n%s", v)
	}
}

// TestView_RequiredMarker verifies required params show a required marker (*).
func TestView_RequiredMarker(t *testing.T) {
	entry := entryWithParams([]model.Param{
		{Name: "task_file", Required: boolPtr(true)},
		{Name: "tag", Required: boolPtr(false)},
	})
	m := paramform.New(&entry).WithWidth(120)
	v := m.View()
	if !contains(v, "*") {
		t.Errorf("View() should show * required marker for required param:\n%s", v)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || s != "" && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := range s {
		if s[i:] != "" && len(s)-i >= len(sub) && s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

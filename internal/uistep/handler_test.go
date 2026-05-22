package uistep

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

func TestUIModelRendersInputsAndActionsTogether(t *testing.T) {
	m := newModel(adapterRequest())

	view := tuistyle.Sanitize(m.View())

	for _, want := range []string{"CLI adapter", "claude", "codex"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Continue") {
		t.Fatalf("simple picker should not render a Continue button:\n%s", view)
	}
	if strings.Contains(view, "Option") || strings.Contains(view, "Select") {
		t.Fatalf("View() should not render keyboard hints inline:\n%s", view)
	}
}

func TestNewHandlerNilRequestReturnsError(t *testing.T) {
	handler := NewHandler(nil, nil)

	_, err := handler(nil)
	if err == nil || !strings.Contains(err.Error(), "ui step request is nil") {
		t.Fatalf("expected nil request error, got %v", err)
	}
}

func TestUIModelArrowKeysKeepInputFocusedAndTabFocusesAction(t *testing.T) {
	m := newModel(cancelableAdapterRequest())

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(*uiModel)
	view := tuistyle.Sanitize(m.View())
	if !strings.Contains(view, "▶ codex") {
		t.Fatalf("input highlight should remain on codex after arrow down, got:\n%s", view)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*uiModel)
	view = tuistyle.Sanitize(m.View())
	if !strings.Contains(view, "[ Continue ]") {
		t.Fatalf("focused action should render as a button, got:\n%s", view)
	}
}

func TestUIModelLeftRightMoveFocusedInputSelection(t *testing.T) {
	m := newModel(cancelableAdapterRequest())

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(*uiModel)
	view := tuistyle.Sanitize(m.View())
	if !strings.Contains(view, "▶ codex") {
		t.Fatalf("input highlight should move to codex after right, got:\n%s", view)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(*uiModel)
	view = tuistyle.Sanitize(m.View())
	if !strings.Contains(view, "▶ claude") {
		t.Fatalf("input highlight should move to claude after left, got:\n%s", view)
	}
}

func TestUIModelHelpPartsReflectFocus(t *testing.T) {
	m := newModel(cancelableAdapterRequest())
	if got := strings.Join(m.HelpParts(), " "); !strings.Contains(got, "↑↓ option") || strings.Contains(got, "←→ action") {
		t.Fatalf("input help should show option controls only, got %q", got)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = next.(*uiModel)
	if got := strings.Join(m.HelpParts(), " "); strings.Contains(got, "↑↓ option") || !strings.Contains(got, "←→ action") {
		t.Fatalf("action help should show action controls and omit option controls, got %q", got)
	}
}

func TestUIModelActionsRenderHorizontallyAndLeftRightMovesFocus(t *testing.T) {
	m := newModel(&model.UIStepRequest{
		StepID: "welcome",
		Title:  "Welcome",
		Actions: []model.UIAction{
			{Label: "Continue", Outcome: "continue"},
			{Label: "Not now", Outcome: "not_now"},
			{Label: "Dismiss", Outcome: "dismiss"},
		},
	})

	view := tuistyle.Sanitize(m.View())
	if !strings.Contains(view, "[ Continue ]  [ Not now ]  [ Dismiss ]") {
		t.Fatalf("actions should render on one row, got:\n%s", view)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(*uiModel)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected enter on focused action to quit")
	}
	if m.result.Outcome != "not_now" {
		t.Fatalf("outcome = %q, want not_now", m.result.Outcome)
	}
}

func TestUIModelBodyWrapsSoftLineBreaksAsParagraphs(t *testing.T) {
	m := newModel(&model.UIStepRequest{
		StepID: "explain-tutor",
		Title:  "Tutorial Step",
		Body: "The next step opens a separate tutorial agent session. It will review the\n" +
			"plan, answer questions in context, and preview how the headless implementor\n" +
			"will execute the task.",
		Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
	})
	m.SetWidth(76)

	view := tuistyle.Sanitize(m.View())
	lines := strings.Split(view, "\n")
	for _, line := range lines {
		if line == "review the" || line == "implementor" {
			t.Fatalf("body should not orphan YAML source-line tails as standalone wrapped lines:\n%s", view)
		}
		if strings.HasSuffix(line, "   ") {
			t.Fatalf("body lines should not be padded by a width box:\n%q\n\n%s", line, view)
		}
	}
	if !strings.Contains(view, "implementor will") {
		t.Fatalf("soft line break should keep adjacent words together when wrapping, got:\n%s", view)
	}
}

func TestUIModelBodyPreservesIndentedHardLines(t *testing.T) {
	m := newModel(&model.UIStepRequest{
		StepID: "intro-ui",
		Title:  "Live Workflow Demo",
		Body: "Try navigating now:\n" +
			"  ↑/↓  move between steps\n" +
			"  d    drill into a sub-workflow\n" +
			"  j/k  scroll this pane",
		Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
	})
	m.SetWidth(90)

	view := tuistyle.Sanitize(m.View())
	for _, want := range []string{
		"Try navigating now:",
		"  ↑/↓  move between steps",
		"  d    drill into a sub-workflow",
		"  j/k  scroll this pane",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("body should preserve indented hard line %q, got:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Try navigating now: ↑/↓") {
		t.Fatalf("body should not flatten indented help text into prose, got:\n%s", view)
	}
}

func TestUIModelEnterOnFocusedActionFiresOutcomeWithHighlightedInput(t *testing.T) {
	m := newModel(adapterRequest())
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyTab})

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*uiModel)
	if cmd == nil {
		t.Fatal("expected enter on action to quit")
	}
	if m.result.Outcome != "continue" {
		t.Fatalf("outcome = %q, want continue", m.result.Outcome)
	}
	if got := m.result.Inputs["cli"]; got != "codex" {
		t.Fatalf("input cli = %q, want codex", got)
	}
}

func TestUIModelEnterOnSimplePickerSubmitsImmediately(t *testing.T) {
	m := newModel(adapterRequest())

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(*uiModel)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(*uiModel)

	if cmd == nil {
		t.Fatal("expected enter on simple picker to submit")
	}
	if !m.done {
		t.Fatal("expected simple picker to be done")
	}
	if m.result.Outcome != "continue" {
		t.Fatalf("outcome = %q, want continue", m.result.Outcome)
	}
	if got := m.result.Inputs["cli"]; got != "codex" {
		t.Fatalf("input cli = %q, want codex", got)
	}
}

func adapterRequest() *model.UIStepRequest {
	return &model.UIStepRequest{
		StepID: "pick-cli",
		Title:  "Pick CLI",
		Body:   "Choose the CLI adapter.",
		Inputs: []model.UIInput{{
			Kind:    "single_select",
			ID:      "cli",
			Prompt:  "CLI adapter",
			Options: []string{"claude", "codex"},
		}},
		Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
	}
}

func cancelableAdapterRequest() *model.UIStepRequest {
	req := adapterRequest()
	req.Actions = []model.UIAction{
		{Label: "Continue", Outcome: "continue"},
		{Label: "Cancel", Outcome: "cancel"},
	}
	return req
}

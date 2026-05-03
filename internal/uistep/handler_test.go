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

	for _, want := range []string{"CLI adapter", "claude", "codex", "Continue"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q:\n%s", want, view)
		}
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
	m := newModel(adapterRequest())

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

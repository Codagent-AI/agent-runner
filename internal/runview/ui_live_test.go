package runview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

func TestLiveUIRequestRendersInsideRunViewChromeAndReturnsAction(t *testing.T) {
	root := &StepNode{ID: "onboarding-welcome", Type: NodeRoot, Status: StatusInProgress}
	ui := &StepNode{ID: "welcome", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{ui}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true

	reply := make(chan model.UIStepResult, 1)
	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID: "welcome",
			Title:  "Welcome to Agent Runner",
			Body:   "Choose how to start.",
			Actions: []model.UIAction{
				{Label: "Continue", Outcome: "continue"},
				{Label: "Not now", Outcome: "not_now"},
				{Label: "Dismiss", Outcome: "dismiss"},
			},
		},
		Reply: reply,
	})
	m = updated.(*Model)

	view := tuistyle.Sanitize(m.View())
	for _, want := range []string{"Agent Runner", "onboarding-welcome", "welcome", "Welcome to Agent Runner"} {
		if !strings.Contains(view, want) {
			t.Fatalf("run view missing %q while rendering live UI:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "[ Continue ]  [ Not now ]  [ Dismiss ]") {
		t.Fatalf("actions should render inside the detail pane on one row:\n%s", view)
	}
	if strings.Contains(view, "↑↓ step") || strings.Contains(view, "q quit") {
		t.Fatalf("run view should not show step-list shortcuts while live UI has focus:\n%s", view)
	}
	if !strings.Contains(view, "←→ action") || !strings.Contains(view, "enter select") || !strings.Contains(view, "esc cancel") {
		t.Fatalf("run view should show live UI shortcuts in the footer:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(*Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	select {
	case got := <-reply:
		if got.Outcome != "not_now" {
			t.Fatalf("outcome = %q, want not_now", got.Outcome)
		}
	default:
		t.Fatal("expected live UI result to be sent")
	}
	if m.liveUI != nil {
		t.Fatal("live UI state should clear after action")
	}
}

func TestLiveUIRequestAutoFollowsInProgressTopLevelStep(t *testing.T) {
	root := &StepNode{ID: "onboarding-welcome", Type: NodeRoot, Status: StatusInProgress}
	welcome := &StepNode{ID: "welcome", Type: NodeUI, Status: StatusSuccess, Parent: root}
	dismissed := &StepNode{ID: "set-dismissed", Type: NodeShell, Status: StatusSkipped, Parent: root}
	setup := &StepNode{ID: "setup", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root}
	completed := &StepNode{ID: "set-completed", Type: NodeShell, Status: StatusPending, Parent: root}
	pickScope := &StepNode{ID: "pick-scope", Type: NodeUI, Status: StatusInProgress, Parent: setup}
	setup.Children = []*StepNode{pickScope}
	root.Children = []*StepNode{welcome, dismissed, setup, completed}

	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true
	m.autoFollow = false
	m.cursor = 0

	reply := make(chan model.UIStepResult, 1)
	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "pick-scope",
			Title:   "Config Scope",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: reply,
	})
	m = updated.(*Model)

	if !m.autoFollow {
		t.Fatal("live UI request should re-enable auto-follow")
	}
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (setup)", m.cursor)
	}
	if got := m.selectedNode(); got != setup {
		t.Fatalf("selected node = %v, want setup", got)
	}
}

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
	if !strings.Contains(view, "↑↓ step") {
		t.Fatalf("run view should show step navigation while live UI is visible:\n%s", view)
	}
	if !strings.Contains(view, "q quit") {
		t.Fatalf("run view should keep quit shortcut visible while live UI has focus:\n%s", view)
	}
	if !strings.Contains(view, "←→ action") || !strings.Contains(view, "enter select") {
		t.Fatalf("run view should show live UI shortcuts in the footer:\n%s", view)
	}
	if strings.Contains(view, "esc cancel") {
		t.Fatalf("live run view should not advertise esc cancel:\n%s", view)
	}
	if !strings.Contains(view, "esc quit") {
		t.Fatalf("top-level live run view should advertise esc quit:\n%s", view)
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

func TestLiveUIRequestUsesRunViewSpecificInputHelp(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	pick := &StepNode{ID: "pick-scope", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{pick}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true

	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID: "pick-scope",
			Title:  "Pick Scope",
			Inputs: []model.UIInput{{
				Kind:    "single_select",
				ID:      "scope",
				Prompt:  "Scope",
				Options: []string{"local", "global"},
			}},
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: make(chan model.UIStepResult, 1),
	})
	m = updated.(*Model)

	view := tuistyle.Sanitize(m.View())
	if !strings.Contains(view, "↑↓ step") || !strings.Contains(view, "j/k scroll") {
		t.Fatalf("live UI help should show run-view step and scroll navigation:\n%s", view)
	}
	if !strings.Contains(view, "←→ option") {
		t.Fatalf("live UI help should show left/right option navigation:\n%s", view)
	}
	if strings.Contains(view, "↑↓ option") || strings.Contains(view, "pgup") || strings.Contains(view, "pgdn") {
		t.Fatalf("live UI help should not claim arrows move options or page keys scroll in run view:\n%s", view)
	}
}

func TestLiveUIRequestJKKeysScrollText(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	pick := &StepNode{ID: "intro-ui", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{pick}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true
	m.termHeight = 12

	body := strings.Join([]string{
		"line 01",
		"line 02",
		"line 03",
		"line 04",
		"line 05",
		"line 06",
		"line 07",
		"line 08",
		"line 09",
		"line 10",
	}, "\n")
	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "intro-ui",
			Title:   "Intro",
			Body:    body,
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: make(chan model.UIStepResult, 1),
	})
	m = updated.(*Model)

	before := m.logOffset
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(*Model)
	if m.logOffset <= before {
		t.Fatalf("j should increase live UI scroll offset: before=%d after=%d", before, m.logOffset)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(*Model)
	if m.logOffset != 0 {
		t.Fatalf("k should scroll live UI back to top, got offset %d", m.logOffset)
	}
}

func TestLiveUIRequestManualScrollSurvivesRefresh(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	pick := &StepNode{ID: "intro-ui", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{pick}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true
	m.running = true
	m.termHeight = 12

	body := strings.Join([]string{
		"line 01",
		"line 02",
		"line 03",
		"line 04",
		"line 05",
		"line 06",
		"line 07",
		"line 08",
		"line 09",
		"line 10",
	}, "\n")
	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "intro-ui",
			Title:   "Intro",
			Body:    body,
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: make(chan model.UIStepResult, 1),
	})
	m = updated.(*Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(*Model)
	scrolled := m.logOffset
	if scrolled == 0 {
		t.Fatal("j should scroll the live UI before refresh")
	}

	m.handleRefreshMsg()

	if m.logOffset != scrolled {
		t.Fatalf("refresh should preserve manual live UI scroll: before=%d after=%d", scrolled, m.logOffset)
	}
}

func TestLiveUIRequestQUsesRunViewQuitConfirmation(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	pick := &StepNode{ID: "intro-ui", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{pick}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true
	m.running = true

	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "intro-ui",
			Title:   "Intro",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: make(chan model.UIStepResult, 1),
	})
	m = updated.(*Model)

	view := tuistyle.Sanitize(m.View())
	if !strings.Contains(view, "q quit") {
		t.Fatalf("live UI help should show q quit:\n%s", view)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("q should show quit confirmation before quitting")
	}
	if !m.quitConfirming {
		t.Fatal("q should enter quit confirmation while live UI is active")
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("y should confirm quit while live UI is active")
	}
}

func TestLiveUIRequestEscUsesRunViewQuitConfirmationAtTopLevel(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	pick := &StepNode{ID: "intro-ui", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{pick}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true
	m.running = true

	reply := make(chan model.UIStepResult, 1)
	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "intro-ui",
			Title:   "Intro",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: reply,
	})
	m = updated.(*Model)

	view := tuistyle.Sanitize(m.View())
	if strings.Contains(view, "esc cancel") {
		t.Fatalf("live UI help should not advertise esc cancel inside run view:\n%s", view)
	}
	if !strings.Contains(view, "esc quit") {
		t.Fatalf("top-level live UI help should advertise esc quit:\n%s", view)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("esc should show quit confirmation before quitting")
	}
	if !m.quitConfirming {
		t.Fatal("esc should enter quit confirmation while live UI is active at top level")
	}
	if m.liveUI == nil {
		t.Fatal("esc should not resolve the live UI step")
	}
	select {
	case got := <-reply:
		t.Fatalf("esc should not resolve UI step, got %+v", got)
	default:
	}
}

func TestLiveUIRequestKeepsStepNavigationKeysActive(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	pick := &StepNode{ID: "pick-scope", Type: NodeUI, Status: StatusInProgress, Parent: root}
	build := &StepNode{ID: "build", Type: NodeSubWorkflow, Status: StatusPending, Parent: root}
	buildChild := &StepNode{ID: "compile", Type: NodeShell, Status: StatusPending, Parent: build}
	build.Children = []*StepNode{buildChild}
	root.Children = []*StepNode{pick, build}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true

	reply := make(chan model.UIStepResult, 1)
	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "pick-scope",
			Title:   "Pick Scope",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: reply,
	})
	m = updated.(*Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)

	if got := m.selectedNode(); got != build {
		t.Fatalf("selected node = %v, want build", got)
	}
	if m.liveUI == nil {
		t.Fatal("live UI should remain pending after step navigation")
	}
	view := tuistyle.Sanitize(m.View())
	if strings.Contains(view, "Pick Scope") {
		t.Fatalf("selected non-UI step should show normal run-view details, not the live UI form:\n%s", view)
	}
	if !strings.Contains(view, "enter drill") {
		t.Fatalf("run-view navigation help should be restored away from active UI step:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if got := m.currentContainer(); got != build {
		t.Fatalf("enter should drill into selected sub-workflow while live UI is pending, got %v", got)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if got := m.currentContainer(); got != root {
		t.Fatalf("esc should navigate back while selected away from live UI, got %v", got)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*Model)
	if got := m.selectedNode(); got != pick {
		t.Fatalf("selected node = %v, want pick after navigating back to live UI", got)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)

	select {
	case got := <-reply:
		if got.Outcome != "continue" {
			t.Fatalf("outcome = %q, want continue", got.Outcome)
		}
	default:
		t.Fatal("expected live UI result after returning to active UI step")
	}
	if m.liveUI != nil {
		t.Fatal("live UI should clear after action")
	}
}

func TestNestedLiveUIRequestUsesRunViewNavigationOutsideActiveAncestor(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	setup := &StepNode{ID: "setup", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root}
	done := &StepNode{ID: "done", Type: NodeShell, Status: StatusPending, Parent: root}
	pick := &StepNode{ID: "pick-scope", Type: NodeUI, Status: StatusInProgress, Parent: setup}
	afterPick := &StepNode{ID: "after-pick", Type: NodeShell, Status: StatusPending, Parent: setup}
	setup.Children = []*StepNode{pick, afterPick}
	root.Children = []*StepNode{setup, done}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true

	reply := make(chan model.UIStepResult, 1)
	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "pick-scope",
			Title:   "Pick Scope",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: reply,
	})
	m = updated.(*Model)

	if got := m.selectedNode(); got != setup {
		t.Fatalf("selected node = %v, want setup ancestor for nested UI", got)
	}
	if view := tuistyle.Sanitize(m.View()); !strings.Contains(view, "Pick Scope") {
		t.Fatalf("active UI should render while its current-level ancestor is selected:\n%s", view)
	}
	if view := tuistyle.Sanitize(m.View()); !strings.Contains(view, "d drill down") {
		t.Fatalf("active UI ancestor should advertise the live UI drill shortcut:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(*Model)
	if got := m.currentContainer(); got != setup {
		t.Fatalf("d should drill into selected sub-workflow while live UI is active, got %v", got)
	}
	if m.liveUI == nil {
		t.Fatal("live UI should remain pending after drilling into its parent")
	}
	if view := tuistyle.Sanitize(m.View()); !strings.Contains(view, "Pick Scope") {
		t.Fatalf("active UI should remain visible after drilling into its parent:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if got := m.currentContainer(); got != root {
		t.Fatalf("esc should navigate back after selecting away from nested live UI, got %v", got)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	if got := m.selectedNode(); got != done {
		t.Fatalf("selected node = %v, want done", got)
	}
	if view := tuistyle.Sanitize(m.View()); strings.Contains(view, "Pick Scope") {
		t.Fatalf("nested live UI should not keep owning detail pane after navigating away:\n%s", view)
	}

	select {
	case got := <-reply:
		t.Fatalf("UI step resolved unexpectedly: %+v", got)
	default:
	}
}

func TestNestedLiveUIRequestEscDrillsOutWhenVisible(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	setup := &StepNode{ID: "setup", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root}
	pick := &StepNode{ID: "pick-scope", Type: NodeUI, Status: StatusInProgress, Parent: setup}
	afterPick := &StepNode{ID: "after-pick", Type: NodeShell, Status: StatusPending, Parent: setup}
	setup.Children = []*StepNode{pick, afterPick}
	root.Children = []*StepNode{setup}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true

	reply := make(chan model.UIStepResult, 1)
	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "pick-scope",
			Title:   "Pick Scope",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: reply,
	})
	m = updated.(*Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = updated.(*Model)
	if got := m.currentContainer(); got != setup {
		t.Fatalf("d should drill into setup, got %v", got)
	}
	view := tuistyle.Sanitize(m.View())
	if !strings.Contains(view, "esc back") {
		t.Fatalf("drilled live UI help should show esc back:\n%s", view)
	}
	if strings.Contains(view, "esc cancel") {
		t.Fatalf("drilled live UI help should not show esc cancel:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if got := m.currentContainer(); got != root {
		t.Fatalf("esc should drill out to root, got %v", got)
	}
	if m.liveUI == nil {
		t.Fatal("esc drill-out should leave live UI pending")
	}
	select {
	case got := <-reply:
		t.Fatalf("esc drill-out should not resolve UI step, got %+v", got)
	default:
	}
}

func TestLiveUIRequestLFollowReturnsToActiveUIAcrossDrillDepth(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	setup := &StepNode{ID: "setup", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root}
	other := &StepNode{ID: "other", Type: NodeSubWorkflow, Status: StatusPending, Parent: root}
	pick := &StepNode{ID: "pick-scope", Type: NodeUI, Status: StatusInProgress, Parent: setup}
	afterPick := &StepNode{ID: "after-pick", Type: NodeShell, Status: StatusPending, Parent: setup}
	otherChild := &StepNode{ID: "other-child", Type: NodeShell, Status: StatusPending, Parent: other}
	setup.Children = []*StepNode{pick, afterPick}
	other.Children = []*StepNode{otherChild}
	root.Children = []*StepNode{setup, other}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true

	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "pick-scope",
			Title:   "Pick Scope",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: make(chan model.UIStepResult, 1),
	})
	m = updated.(*Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if got := m.currentContainer(); got != other {
		t.Fatalf("test setup should drill into other workflow, got %v", got)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(*Model)

	if got := m.selectedNode(); got != pick {
		t.Fatalf("l should select active UI step, got %v", got)
	}
	if got := m.currentContainer(); got != setup {
		t.Fatalf("l should drill to active UI parent, got %v", got)
	}
	if !m.autoFollow {
		t.Fatal("l should re-enable auto-follow")
	}
	if view := tuistyle.Sanitize(m.View()); !strings.Contains(view, "Pick Scope") {
		t.Fatalf("l should show active live UI again:\n%s", view)
	}
}

func TestLiveUIRequestLFollowWorksWhileUIVisible(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	pick := &StepNode{ID: "pick-scope", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{pick}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true
	m.autoFollow = false

	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "pick-scope",
			Title:   "Pick Scope",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: make(chan model.UIStepResult, 1),
	})
	m = updated.(*Model)
	m.autoFollow = false

	if view := tuistyle.Sanitize(m.View()); !strings.Contains(view, "l follow") {
		t.Fatalf("live UI help should show l follow when auto-follow is off:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(*Model)
	if !m.autoFollow {
		t.Fatal("l should re-enable auto-follow while live UI is visible")
	}
}

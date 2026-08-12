package runview

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

// forwardingLiveProgram exercises the same Coordinator-to-Bubble-Tea message
// boundary as a live run while keeping the integration fixture deterministic.
type forwardingLiveProgram struct {
	model *Model
	sent  chan tea.Msg
}

func (p *forwardingLiveProgram) ReleaseTerminal() error { return nil }
func (p *forwardingLiveProgram) RestoreTerminal() error { return nil }
func (p *forwardingLiveProgram) Send(msg tea.Msg) {
	updated, _ := p.model.Update(msg)
	p.model = updated.(*Model)
	if p.sent != nil {
		p.sent <- msg
	}
}

func TestINT002LiveCoordinatorPreservesOutputAndUIOwnership(t *testing.T) {
	sessionDir := t.TempDir()
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	parent := &StepNode{ID: "parent", Type: NodeHeadlessAgent, Status: StatusInProgress, Parent: root}
	work := &StepNode{ID: "work", Type: NodeShell, Status: StatusInProgress, Parent: parent}
	call := &StepNode{ID: "call", Type: NodeAgentCall, Status: StatusInProgress, Parent: parent, CallID: "call-1"}
	ui := &StepNode{ID: "choose", Type: NodeUI, Status: StatusInProgress, Parent: parent}
	parent.Children = []*StepNode{work, call, ui}
	root.Children = []*StepNode{parent}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.sessionDir = sessionDir
	m.running = true
	m.altScreen = true
	m.followActive = true
	m.followTail = true

	program := &forwardingLiveProgram{model: m, sent: make(chan tea.Msg, 32)}
	coordinator := liverun.NewCoordinator(program, sessionDir)
	runner := coordinator.TUIProcessRunner(nil)
	prefixes, ok := runner.(liverun.PrefixSetter)
	if !ok {
		t.Fatal("live process runner must expose prefix ownership")
	}

	prefix := "[parent, work]"
	prefixes.SetPrefix(prefix)
	started := time.Now()
	if _, err := runner.RunShell("printf '\\033[31mfirst'; sleep 0.02; printf ' second'; printf 'warning' >&2", true, ""); err != nil {
		t.Fatalf("deterministic shell fixture: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("first-byte fixture exceeded 100ms target: %s", elapsed)
	}
	m = program.model
	if strings.Contains(m.selectedNode().Stdout, "\x1b") || !strings.Contains(m.selectedNode().Stdout, "first second") {
		t.Fatalf("display output was not sanitized: %q", m.selectedNode().Stdout)
	}
	raw, err := os.ReadFile(filepath.Join(sessionDir, "output", liverun.SanitizeOutputPrefix(prefix)+".out"))
	if err != nil || !strings.Contains(string(raw), "\x1b[31mfirst") {
		t.Fatalf("raw output was not persisted at escaped path: %q, %v", raw, err)
	}

	callPrefix := "[parent, call:call-1]"
	if _, err := runner.RunAgent(&iexec.AgentProcessOptions{
		Context: context.Background(), Args: []string{"sh", "-c", "printf 'call output'"}, CaptureStdout: true, Prefix: callPrefix,
	}); err != nil {
		t.Fatalf("deterministic call fixture: %v", err)
	}
	if call.Stdout != "call output" || parent.Stdout != "" {
		t.Fatalf("parent/call output ownership leaked: parent=%q call=%q", parent.Stdout, call.Stdout)
	}

	result := make(chan model.UIStepResult, 1)
	go func() {
		got, err := coordinator.HandleUIStep(&model.UIStepRequest{StepID: "choose", Title: "Choose", Body: strings.Repeat("scroll ", 80), Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}}})
		if err != nil {
			t.Errorf("HandleUIStep: %v", err)
		}
		result <- got
	}()
	deadline := time.After(time.Second)
	for {
		select {
		case msg := <-program.sent:
			if _, ok := msg.(*liverun.UIRequestMsg); ok {
				goto uiForwarded
			}
		case <-deadline:
			t.Fatal("coordinator did not forward UI request")
		}
	}

uiForwarded:
	m = program.model
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*Model)
	if m.liveUI == nil || m.selectedNode() == ui {
		t.Fatal("tree navigation should leave the UI request pending")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(*Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if m.liveUI != nil {
		t.Fatal("selected form action should resolve the embedded UI")
	}
	select {
	case got := <-result:
		if got.Outcome != "continue" {
			t.Fatalf("UI outcome = %q", got.Outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("resolved UI did not release coordinator")
	}

	if err := coordinator.NotifyDone("success", nil); err != nil {
		t.Fatalf("NotifyDone success: %v", err)
	}
	if program.model.showSummary {
		t.Fatal("successful coordinator completion should retain the detailed view")
	}

	failedRoot := &StepNode{ID: "failed-workflow", Type: NodeRoot, Status: StatusInProgress}
	nested := &StepNode{ID: "nested", Type: NodeSubWorkflow, Status: StatusFailed, Parent: failedRoot}
	failedLeaf := &StepNode{ID: "failed-leaf", Type: NodeShell, Status: StatusFailed, Parent: nested}
	nested.Children = []*StepNode{failedLeaf}
	failedRoot.Children = []*StepNode{nested}
	failedModel := newTestModel(&Tree{Root: failedRoot}, FromLiveRun)
	failedModel.running = true
	failedProgram := &forwardingLiveProgram{model: failedModel}
	if err := liverun.NewCoordinator(failedProgram, t.TempDir()).NotifyDone("failed", nil); err != nil {
		t.Fatalf("NotifyDone failure: %v", err)
	}
	if got := failedProgram.model.selectedNode(); got != failedLeaf || len(failedProgram.model.path) != 1 {
		t.Fatalf("failed completion should select nested failed leaf at root: selected=%v path=%d", got, len(failedProgram.model.path))
	}
}

func TestLiveUIRequestRendersInsideRunViewChromeAndReturnsAction(t *testing.T) {
	root := &StepNode{ID: "onboarding-welcome", Type: NodeRoot, Status: StatusInProgress}
	ui := &StepNode{ID: "welcome", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{ui}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true
	m.followActive = true
	m.followTail = true

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
	for _, want := range []string{"Agent Runner", "onboarding-welcome", "welcome", "Current form", "Welcome to Agent Runner"} {
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
	m.followActive = true
	m.followTail = true
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

	if !m.followActive || !m.followTail {
		t.Fatal("live UI request should preserve engaged auto-follow")
	}
	if got := m.selectedNode(); got != pickScope {
		t.Fatalf("selected node = %v, want active UI leaf", got)
	}
}

func TestLiveUIRequestPreservesPausedSelectionUntilJumpToLive(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	setup := &StepNode{ID: "setup", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root, SubLoaded: true}
	intro := &StepNode{ID: "intro", Type: NodeShell, Status: StatusInProgress, Parent: setup}
	pick := &StepNode{ID: "pick-scope", Type: NodeUI, Status: StatusInProgress, Parent: setup}
	setup.Children = []*StepNode{intro, pick}
	root.Children = []*StepNode{setup}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true
	m.path = []*StepNode{root, setup}
	m.setSelected(intro)
	m.followActive = false
	m.followTail = false

	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "pick-scope",
			Title:   "Pick Scope",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: make(chan model.UIStepResult, 1),
	})
	m = updated.(*Model)

	if got := m.selectedNode(); got != intro {
		t.Fatalf("pending UI stole paused selection: got %v, want %v", got, intro)
	}
	if m.followActive || m.followTail {
		t.Fatalf("pending UI re-engaged paused follow: active=%v tail=%v", m.followActive, m.followTail)
	}
	if len(m.path) != 2 || m.path[1] != setup {
		t.Fatalf("pending UI changed manual scope: %#v", m.path)
	}
}

func TestLiveUIFollowSurvivesRefreshWithStaleActiveStepPrefix(t *testing.T) {
	root := &StepNode{ID: "onboarding", Type: NodeRoot, Status: StatusInProgress}
	autonomousDemo := &StepNode{ID: "autonomous-demo", Type: NodeHeadlessAgent, Status: StatusSuccess, Parent: root}
	reviewAutonomous := &StepNode{ID: "review-autonomous", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{autonomousDemo, reviewAutonomous}

	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true
	m.followActive = false
	m.cursor = 0
	m.activeStepPrefix = "[autonomous-demo]"

	reply := make(chan model.UIStepResult, 1)
	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "review-autonomous",
			Title:   "Review Autonomous",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: reply,
	})
	m = updated.(*Model)
	m.followActive = false
	m.cursor = 0

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if got := m.selectedNode(); got != reviewAutonomous {
		t.Fatalf("after l selected node = %v, want review-autonomous", got)
	}

	m.Update(tuistyle.RefreshMsg{})

	if got := m.selectedNode(); got != reviewAutonomous {
		t.Fatalf("after refresh selected node = %v, want review-autonomous", got)
	}
}

func TestLiveUIRequestPreservesManualDrillForSiblingSubWorkflow(t *testing.T) {
	root := &StepNode{ID: "onboarding", Type: NodeRoot, Status: StatusInProgress}
	guided := &StepNode{ID: "guided-workflow", Type: NodeSubWorkflow, Status: StatusSuccess, Parent: root, SubLoaded: true}
	final := &StepNode{ID: "summary", Type: NodeUI, Status: StatusSuccess, Parent: guided}
	guided.Children = []*StepNode{final}
	validator := &StepNode{ID: "validator", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root, SubLoaded: true}
	intro := &StepNode{ID: "intro-ui", Type: NodeUI, Status: StatusInProgress, Parent: validator}
	validator.Children = []*StepNode{intro}
	root.Children = []*StepNode{guided, validator}

	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true
	m.path = []*StepNode{root, guided}
	m.cursor = 0
	m.followActive = false

	reply := make(chan model.UIStepResult, 1)
	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "intro-ui",
			Title:   "Agent Validator",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: reply,
	})
	m = updated.(*Model)

	if m.followActive {
		t.Fatal("pending UI should not re-enable paused active follow")
	}
	if len(m.path) != 2 || m.path[1] != guided {
		t.Fatalf("auto-follow should preserve manual scope, got %d segments", len(m.path))
	}
	if got := m.selectedNode(); got != final {
		t.Fatalf("selected node = %v, want scoped selection", got)
	}
	if m.liveUIVisible() {
		t.Fatal("out-of-scope live UI should not replace scoped detail")
	}
}

func TestLiveUIRequestUsesRunViewSpecificInputHelp(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	pick := &StepNode{ID: "pick-scope", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{pick}
	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.altScreen = true
	m.followActive = true
	m.followTail = true

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

func TestLiveUIRequestLeavesInapplicableKeysWithRunViewChrome(t *testing.T) {
	root := &StepNode{ID: "workflow", Type: NodeRoot, Status: StatusInProgress}
	pick := &StepNode{ID: "pick-scope", Type: NodeUI, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{pick}
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

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = updated.(*Model)
	if !m.showLegend {
		t.Fatal("inapplicable form key should remain available to run-view chrome")
	}
	if m.liveUI == nil {
		t.Fatal("inapplicable form key should not resolve the pending UI step")
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

	before := m.detailOffset
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(*Model)
	if m.detailOffset <= before {
		t.Fatalf("j should increase live UI scroll offset: before=%d after=%d", before, m.detailOffset)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(*Model)
	if m.detailOffset != 0 {
		t.Fatalf("k should scroll live UI back to top, got offset %d", m.detailOffset)
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
	scrolled := m.detailOffset
	if scrolled == 0 {
		t.Fatal("j should scroll the live UI before refresh")
	}

	m.handleRefreshMsg()

	if m.detailOffset != scrolled {
		t.Fatalf("refresh should preserve manual live UI scroll: before=%d after=%d", scrolled, m.detailOffset)
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

func TestLiveUIRequestCtrlCExitsImmediately(t *testing.T) {
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

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("ctrl+c should quit immediately while live UI is active")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected ctrl+c command to quit, got %T", cmd())
	}
	if m.quitConfirming {
		t.Fatal("ctrl+c should not enter quit confirmation while live UI is active")
	}
	if !m.ExitRequested() {
		t.Fatal("ctrl+c should mark exit requested")
	}
	if m.liveUI == nil {
		t.Fatal("ctrl+c should not cancel the live UI step")
	}
	select {
	case got := <-reply:
		t.Fatalf("ctrl+c should not resolve UI step, got %+v", got)
	default:
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
	m.followActive = true
	m.followTail = true

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

	if got := m.selectedNode(); got != pick {
		t.Fatalf("selected node = %v, want nested active UI leaf", got)
	}
	if view := tuistyle.Sanitize(m.View()); !strings.Contains(view, "Pick Scope") {
		t.Fatalf("active UI should render while its current-level ancestor is selected:\n%s", view)
	}
	if view := tuistyle.Sanitize(m.View()); strings.Contains(view, "d drill") {
		t.Fatalf("live UI help must not advertise d as drill:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if got := m.currentContainer(); got != setup {
		t.Fatalf("enter should drill into selected sub-workflow while live UI is active, got %v", got)
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
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
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

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(*Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if got := m.currentContainer(); got != setup {
		t.Fatalf("enter should drill into setup, got %v", got)
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
	m.followActive = true
	m.followTail = true

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
	if got := m.currentContainer(); got != root {
		t.Fatalf("l should return to root manual scope, got %v", got)
	}
	if !m.followActive || !m.followTail {
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
	m.followActive = false

	updated, _ := m.Update(&liverun.UIRequestMsg{
		Request: model.UIStepRequest{
			StepID:  "pick-scope",
			Title:   "Pick Scope",
			Actions: []model.UIAction{{Label: "Continue", Outcome: "continue"}},
		},
		Reply: make(chan model.UIStepResult, 1),
	})
	m = updated.(*Model)
	m.followActive = false

	if view := tuistyle.Sanitize(m.View()); !strings.Contains(view, "l follow") {
		t.Fatalf("live UI help should show l follow when auto-follow is off:\n%s", view)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	m = updated.(*Model)
	if !m.followActive || !m.followTail {
		t.Fatal("l should re-enable auto-follow while live UI is visible")
	}
}

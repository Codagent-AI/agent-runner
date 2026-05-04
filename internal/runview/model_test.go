package runview

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

func newTestModel(tree *Tree, entered Entered) *Model {
	return &Model{
		tree:       tree,
		entered:    entered,
		path:       []*StepNode{tree.Root},
		loadedFull: make(map[string]bool),
		termWidth:  120,
		termHeight: 40,
		altScreen:  entered != FromLiveRun,
	}
}

func simpleTree() *Tree {
	root := &StepNode{
		ID:     "test-workflow",
		Type:   NodeRoot,
		Status: StatusInProgress,
	}
	shell := &StepNode{
		ID:            "build",
		Type:          NodeShell,
		Status:        StatusSuccess,
		Parent:        root,
		StaticCommand: "go build ./...",
		ExitCode:      intPtr(0),
	}
	agent := &StepNode{
		ID:          "implement",
		Type:        NodeInteractiveAgent,
		Status:      StatusInProgress,
		Parent:      root,
		StaticAgent: "implementor",
		AgentCLI:    "claude",
		SessionID:   "session-abc-123",
	}
	loop := &StepNode{
		ID:                  "tasks",
		Type:                NodeLoop,
		Status:              StatusInProgress,
		Parent:              root,
		StaticLoopOver:      "tasks/*.md",
		StaticLoopAs:        "task_file",
		IterationsCompleted: 2,
		LoopMatches:         []string{"tasks/a.md", "tasks/b.md", "tasks/c.md"},
	}
	iter1 := &StepNode{
		ID:             "tasks",
		Type:           NodeIteration,
		Status:         StatusSuccess,
		Parent:         loop,
		IterationIndex: 0,
		BindingValue:   "tasks/a.md",
	}
	iter1child := &StepNode{
		ID:     "run-task",
		Type:   NodeShell,
		Status: StatusSuccess,
		Parent: iter1,
	}
	iter1.Children = []*StepNode{iter1child}
	iter2 := &StepNode{
		ID:             "tasks",
		Type:           NodeIteration,
		Status:         StatusInProgress,
		Parent:         loop,
		IterationIndex: 1,
		BindingValue:   "tasks/b.md",
	}
	loop.Children = []*StepNode{iter1, iter2}
	subwf := &StepNode{
		ID:             "verify",
		Type:           NodeSubWorkflow,
		Status:         StatusPending,
		Parent:         root,
		StaticWorkflow: "verify-task.yaml",
	}
	root.Children = []*StepNode{shell, agent, loop, subwf}
	return &Tree{Root: root}
}

func intPtr(v int) *int { return &v }

func TestModel_Navigation_UpDown(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)

	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("after down: cursor = %d, want 1", m.cursor)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("after up: cursor = %d, want 0", m.cursor)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Fatalf("after up at 0: cursor = %d, want 0", m.cursor)
	}
}

func TestModel_Navigation_UpDownDoesNotClearScreen(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		t.Fatal("down navigation should redraw through Bubble Tea diffing without requesting a screen clear")
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if cmd != nil {
		t.Fatal("up navigation should redraw through Bubble Tea diffing without requesting a screen clear")
	}
}

func TestModel_Init_InactiveRunDoesNotSchedulePollingOrPulse(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.active = false
	m.running = false

	if cmd := m.Init(); cmd != nil {
		t.Fatal("inactive run view should not schedule refresh or pulse timers")
	}
}

func TestModel_Update_InactiveRefreshDoesNotReschedule(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.active = false
	m.running = false

	_, cmd := m.Update(tuistyle.RefreshMsg{})
	if cmd != nil {
		t.Fatal("inactive run view should ignore stale refresh messages without rescheduling")
	}
}

func TestModel_Update_InactivePulseDoesNotRescheduleOrAdvance(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.active = false
	m.running = false
	m.pulsePhase = 1.25

	_, cmd := m.Update(tuistyle.PulseMsg{})
	if cmd != nil {
		t.Fatal("inactive run view should ignore stale pulse messages without rescheduling")
	}
	if m.pulsePhase != 1.25 {
		t.Fatalf("pulsePhase = %v, want 1.25", m.pulsePhase)
	}
}

func TestModel_Navigation_ExpansionRowsRemainReadOnly(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	setup := &StepNode{ID: "setup", Type: NodeShell, Status: StatusSuccess, Parent: root}
	review := &StepNode{ID: "review", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root}
	cleanup := &StepNode{ID: "cleanup", Type: NodeShell, Status: StatusPending, Parent: root}
	root.Children = []*StepNode{setup, review, cleanup}

	verify := &StepNode{ID: "verify", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: review}
	review.Children = []*StepNode{verify}
	summarize := &StepNode{ID: "summarize", Type: NodeHeadlessAgent, Status: StatusInProgress, Parent: verify}
	verify.Children = []*StepNode{summarize}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 1

	rows := m.buildStepRows(root.Children)
	if len(rows) <= len(root.Children) {
		t.Fatalf("expected expansion rows to be present, got %d rows for %d children", len(rows), len(root.Children))
	}

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 2 {
		t.Fatalf("after down: cursor = %d, want 2", m.cursor)
	}
	if got := m.selectedNode(); got != cleanup {
		t.Fatalf("after down: selected node = %v, want cleanup", got)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 1 {
		t.Fatalf("after up: cursor = %d, want 1", m.cursor)
	}
	if got := m.selectedNode(); got != review {
		t.Fatalf("after up: selected node = %v, want review", got)
	}
}

// j and k scroll the log pane offset; with empty stepRanges the cursor stays put.
func TestModel_JK_ScrollsLogPane(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.cursor = 1
	initial := m.logOffset

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.logOffset <= initial {
		t.Fatal("j should increase logOffset")
	}
	// j clears autoFollow
	if m.autoFollow {
		t.Error("j should clear autoFollow")
	}

	scrolled := m.logOffset
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if m.logOffset >= scrolled {
		t.Fatal("k should decrease logOffset")
	}
	if m.autoFollow {
		t.Error("k should clear autoFollow")
	}
}

// PgUp and PgDown are not bound; they must be no-ops.
func TestModel_PgUpPgDown_NoOp(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.logOffset = 0

	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.logOffset != 0 {
		t.Fatal("PgDown should not change logOffset (unbound)")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.logOffset != 0 {
		t.Fatal("PgUp should not change logOffset (unbound)")
	}
}

func TestModel_EndKey_NoOp(t *testing.T) {
	m := newLiveModelWithFlags()

	initial := m.logOffset
	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if m.logOffset != initial {
		t.Error("End key should be a no-op")
	}
}

func TestModel_GKey_NoOp(t *testing.T) {
	m := newLiveModelWithFlags()
	initial := m.logOffset

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.logOffset != initial {
		t.Error("G key should be a no-op (not bound)")
	}
}

func TestModel_C_CopiesSelectedStepDetail(t *testing.T) {
	tree := simpleTree()
	tree.Root.Children[0].Stdout = "build ok\nnext line"
	m := newTestModel(tree, FromList)
	m.cursor = 0
	m.originCwd = "/repo/project"

	var copied string
	oldWrite := writeClipboard
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() { writeClipboard = oldWrite }()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd == nil {
		t.Fatal("copy should schedule success notice clearing")
	}

	if copied == "" {
		t.Fatal("expected selected step detail to be copied")
	}
	for _, want := range []string{"directory: /repo/project", "breadcrumb:", "test-workflow", "build", "$ go build ./...", "stdout:", "build ok", "next line"} {
		if !strings.Contains(copied, want) {
			t.Fatalf("copied detail missing %q:\n%s", want, copied)
		}
	}
	if strings.Contains(copied, "\x1b[") {
		t.Fatalf("copied detail should be plain text, got ANSI escape in %q", copied)
	}
	if m.notice != "copied selected step detail" {
		t.Fatalf("notice = %q, want copy success notice", m.notice)
	}
	if help := m.renderHelpBar(); !strings.Contains(help, "c copy") {
		t.Fatalf("help bar should advertise copy key, got %q", help)
	}
}

func TestModel_CopyNoticeExpiresOnlyWhenStillCurrent(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.notice = "copied selected step detail"
	m.copyNoticeSeq = 2

	m.Update(copyNoticeExpiredMsg{seq: 1})
	if m.notice != "copied selected step detail" {
		t.Fatalf("notice = %q, want stale copy success notice preserved", m.notice)
	}

	m.Update(copyNoticeExpiredMsg{seq: 2})
	if m.notice != "" {
		t.Fatalf("notice = %q, want cleared copy success notice", m.notice)
	}

	m.notice = "copy failed: clipboard unavailable"
	m.Update(copyNoticeExpiredMsg{seq: 2})
	if m.notice != "copy failed: clipboard unavailable" {
		t.Fatalf("notice = %q, want unrelated notice preserved", m.notice)
	}
}

func TestModel_C_ShowsNoticeWhenClipboardWriteFails(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)

	oldWrite := writeClipboard
	writeClipboard = func(string) error {
		return errors.New("clipboard unavailable")
	}
	defer func() { writeClipboard = oldWrite }()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd != nil {
		t.Fatalf("failed copy should not schedule success notice clearing, got %v", cmd)
	}

	if !strings.Contains(m.notice, "copy failed: clipboard unavailable") {
		t.Fatalf("notice = %q, want clipboard failure notice", m.notice)
	}
}

// r on an inactive run emits ResumeRunMsg.
func TestModel_R_InactiveRun_EmitsResumeRunMsg(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusInProgress
	m := newTestModel(tree, FromList)
	m.sessionDir = "/runs/my-run-id"

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("r on inactive run should produce a cmd")
	}
	msg := cmd()
	rr, ok := msg.(ResumeRunMsg)
	if !ok {
		t.Fatalf("expected ResumeRunMsg, got %T", msg)
	}
	if rr.RunID != "my-run-id" {
		t.Fatalf("RunID = %q, want %q", rr.RunID, "my-run-id")
	}
}

// r works at any drill depth.
func TestModel_R_WorksAtAnyDrillDepth(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusInProgress
	m := newTestModel(tree, FromList)
	m.sessionDir = "/runs/my-run-id"

	// Drill into loop (index 2).
	m.cursor = 2
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.path) != 2 {
		t.Fatalf("expected drilled path len 2, got %d", len(m.path))
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("r at drill depth should produce a cmd")
	}
	msg := cmd()
	if _, ok := msg.(ResumeRunMsg); !ok {
		t.Fatalf("expected ResumeRunMsg at drill depth, got %T", msg)
	}
}

// r is ignored while a workflow is running live.
func TestModel_R_IgnoredWhileRunning(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true
	m.sessionDir = "/runs/my-run-id"

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil {
		t.Fatalf("r while running should be no-op, got cmd %v", cmd)
	}
}

// r is ignored on an active run (lock held by another process).
func TestModel_R_IgnoredOnActiveRun(t *testing.T) {
	tree := simpleTree()
	m := newTestModel(tree, FromList)
	m.sessionDir = "/runs/my-run-id"
	m.active = true

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil {
		t.Fatalf("r on active run should be no-op, got cmd %v", cmd)
	}
}

func TestModel_R_CompletedRun_SelectedAgent_EmitsResumeMsg(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusSuccess
	tree.Root.Children[1].Status = StatusSuccess
	m := newTestModel(tree, FromList)
	m.sessionDir = "/runs/my-run-id"
	m.cursor = 1

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("r on selected agent in completed run should produce a cmd")
	}
	msg := cmd()
	resume, ok := msg.(ResumeMsg)
	if !ok {
		t.Fatalf("expected ResumeMsg, got %T", msg)
	}
	if resume.SessionID != "session-abc-123" {
		t.Fatalf("SessionID = %q, want session-abc-123", resume.SessionID)
	}
}

func TestModel_R_CompletedRun_NonAgent_ResumesLastAgentInCurrentWorkflow(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	firstAgent := &StepNode{
		ID:        "proposal",
		Type:      NodeInteractiveAgent,
		Status:    StatusSuccess,
		Parent:    root,
		AgentCLI:  "claude",
		SessionID: "session-first",
	}
	shell := &StepNode{ID: "archive", Type: NodeShell, Status: StatusSuccess, Parent: root}
	lastAgent := &StepNode{
		ID:        "design",
		Type:      NodeInteractiveAgent,
		Status:    StatusSuccess,
		Parent:    root,
		AgentCLI:  "codex",
		SessionID: "session-last",
	}
	root.Children = []*StepNode{firstAgent, shell, lastAgent}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 1

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("r on non-agent in completed run should produce a cmd")
	}
	msg := cmd()
	resume, ok := msg.(ResumeMsg)
	if !ok {
		t.Fatalf("expected ResumeMsg, got %T", msg)
	}
	if resume.SessionID != "session-last" {
		t.Fatalf("SessionID = %q, want session-last", resume.SessionID)
	}
	if resume.AgentCLI != "codex" {
		t.Fatalf("AgentCLI = %q, want codex", resume.AgentCLI)
	}
}

func TestModel_R_CompletedRun_NonAgent_ResumesLastAgentInsideSubWorkflow(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	rootAgent := &StepNode{
		ID:        "proposal",
		Type:      NodeInteractiveAgent,
		Status:    StatusSuccess,
		Parent:    root,
		AgentCLI:  "claude",
		SessionID: "session-root",
	}
	subwf := &StepNode{ID: "review-flow", Type: NodeSubWorkflow, Status: StatusSuccess, Parent: root, SubLoaded: true}
	nestedAgent := &StepNode{
		ID:        "review",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    subwf,
		AgentCLI:  "codex",
		SessionID: "session-subworkflow",
	}
	subwf.Children = []*StepNode{nestedAgent}
	shell := &StepNode{ID: "archive", Type: NodeShell, Status: StatusSuccess, Parent: root}
	root.Children = []*StepNode{rootAgent, subwf, shell}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 2

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("r on non-agent in completed run should produce a cmd")
	}
	msg := cmd()
	resume, ok := msg.(ResumeMsg)
	if !ok {
		t.Fatalf("expected ResumeMsg, got %T", msg)
	}
	if resume.SessionID != "session-subworkflow" {
		t.Fatalf("SessionID = %q, want session-subworkflow", resume.SessionID)
	}
	if resume.AgentCLI != "codex" {
		t.Fatalf("AgentCLI = %q, want codex", resume.AgentCLI)
	}
}

func TestModel_R_CompletedRun_SelectedSubWorkflow_ResumesLastAgentInSelectedWorkflow(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	rootAgent := &StepNode{
		ID:        "root-agent",
		Type:      NodeInteractiveAgent,
		Status:    StatusSuccess,
		Parent:    root,
		AgentCLI:  "claude",
		SessionID: "session-root",
	}
	subwf := &StepNode{
		ID:        "review-flow",
		Type:      NodeSubWorkflow,
		Status:    StatusSuccess,
		Parent:    root,
		SubLoaded: true,
	}
	nestedShell := &StepNode{ID: "precheck", Type: NodeShell, Status: StatusSuccess, Parent: subwf}
	nestedAgent := &StepNode{
		ID:        "review",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    subwf,
		AgentCLI:  "codex",
		SessionID: "session-nested",
	}
	subwf.Children = []*StepNode{nestedShell, nestedAgent}
	root.Children = []*StepNode{rootAgent, subwf}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 1

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("r on selected sub-workflow in completed run should produce a cmd")
	}
	msg := cmd()
	resume, ok := msg.(ResumeMsg)
	if !ok {
		t.Fatalf("expected ResumeMsg, got %T", msg)
	}
	if resume.SessionID != "session-nested" {
		t.Fatalf("SessionID = %q, want session-nested", resume.SessionID)
	}
}

func TestModel_R_CompletedRun_NoAgentSession_NoOp(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	root.Children = []*StepNode{{ID: "archive", Type: NodeShell, Status: StatusSuccess, Parent: root}}
	m := newTestModel(&Tree{Root: root}, FromList)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil {
		t.Fatalf("r on completed run with no agent session should be no-op, got cmd %v", cmd)
	}
}

// r is ignored on failed runs with no resumable agent session.
func TestModel_R_IgnoredOnFailedRunWithNoAgentSession(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusFailed}
	root.Children = []*StepNode{{ID: "archive", Type: NodeShell, Status: StatusFailed, Parent: root}}
	tree := &Tree{Root: root}
	tree.Root.Status = StatusFailed
	m := newTestModel(tree, FromList)
	m.sessionDir = "/runs/my-run-id"

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil {
		t.Fatalf("r on failed run should be no-op, got cmd %v", cmd)
	}
}

// r is ignored when liveResult is set (just-finished live run, not inactive).
func TestModel_R_IgnoredAfterLiveRunFinishes(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = false
	m.liveResult = "success"
	m.sessionDir = "/runs/my-run-id"

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil {
		t.Fatalf("r after live run completes should be no-op, got cmd %v", cmd)
	}
}

func TestModel_Breadcrumb_ShowsAffordance_WhenInactive(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusInProgress
	m := newTestModel(tree, FromList)

	bc := m.renderBreadcrumb()
	if !containsString(bc, "r to resume") {
		t.Errorf("breadcrumb should show '(r to resume)' for inactive run: %q", bc)
	}
}

func TestModel_Breadcrumb_ShowsStartRun_WhenFromDefinition(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusInProgress
	m := newTestModel(tree, FromDefinition)

	bc := m.renderBreadcrumb()
	if !containsString(bc, "r to start run") {
		t.Errorf("breadcrumb should show '(r to start run)' for definition view: %q", bc)
	}
	if containsString(bc, "r to resume") {
		t.Errorf("breadcrumb should not show '(r to resume)' for definition view: %q", bc)
	}
}

func TestModel_Breadcrumb_HidesAffordance_WhenCompleted(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusSuccess
	m := newTestModel(tree, FromList)

	bc := m.renderBreadcrumb()
	if containsString(bc, "r to resume") {
		t.Errorf("breadcrumb should not show '(r to resume)' for completed run: %q", bc)
	}
}

func TestModel_Breadcrumb_HidesAffordance_WhenFailed(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusFailed
	m := newTestModel(tree, FromList)

	bc := m.renderBreadcrumb()
	if containsString(bc, "r to resume") {
		t.Errorf("breadcrumb should not show '(r to resume)' for failed run: %q", bc)
	}
}

func TestModel_Breadcrumb_HidesAffordance_WhenRunning(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true

	bc := m.renderBreadcrumb()
	if containsString(bc, "r to resume") {
		t.Errorf("breadcrumb should not show '(r to resume)' while running: %q", bc)
	}
}

func TestModel_Breadcrumb_RunningLabelDoesNotBlinkOff(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true
	m.pulsePhase = 1.5 * math.Pi

	bc := stripANSI(m.renderBreadcrumb())
	if !strings.Contains(bc, "running") {
		t.Fatalf("breadcrumb should keep showing running while step-list dot blinks, got %q", bc)
	}
}

func TestModel_Breadcrumb_ActiveLabelDoesNotBlinkOff(t *testing.T) {
	tree := simpleTree()
	m := newTestModel(tree, FromList)
	m.active = true
	m.pulsePhase = 1.5 * math.Pi

	bc := stripANSI(m.renderBreadcrumb())
	if !strings.Contains(bc, "active") {
		t.Fatalf("breadcrumb should keep showing active while step-list dot blinks, got %q", bc)
	}
}

func TestFormatLiveElapsed(t *testing.T) {
	start := time.Date(2026, 4, 27, 20, 48, 0, 0, time.UTC)
	now := start.Add(18*time.Minute + 40*time.Second)

	if got := formatLiveElapsed(start, now); got != "elapsed 18m 40s" {
		t.Fatalf("formatLiveElapsed() = %q, want %q", got, "elapsed 18m 40s")
	}
}

func TestModel_Breadcrumb_RunningShowsElapsedInsteadOfStarted(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true
	m.startTime = time.Now().Add(-(18*time.Minute + 40*time.Second))

	bc := stripANSI(m.renderBreadcrumb())
	if !strings.Contains(bc, "elapsed 18m") {
		t.Fatalf("breadcrumb should show live elapsed time while running, got %q", bc)
	}
	if strings.Contains(bc, "started") {
		t.Fatalf("breadcrumb should not show started time while running, got %q", bc)
	}
}

func TestModel_Breadcrumb_ActiveShowsElapsedInsteadOfStarted(t *testing.T) {
	tree := simpleTree()
	m := newTestModel(tree, FromList)
	m.active = true
	m.startTime = time.Now().Add(-(2*time.Minute + 3*time.Second))

	bc := stripANSI(m.renderBreadcrumb())
	if !strings.Contains(bc, "elapsed 2m") {
		t.Fatalf("breadcrumb should show live elapsed time while active, got %q", bc)
	}
	if strings.Contains(bc, "started") {
		t.Fatalf("breadcrumb should not show started time while active, got %q", bc)
	}
}

func TestModel_HelpBar_ShowsRBinding_WhenInactive(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusInProgress
	m := newTestModel(tree, FromList)
	m.sessionDir = "/runs/my-run-id"

	help := m.renderHelpBar()
	if !containsString(help, "r resume") {
		t.Errorf("help bar should show 'r resume' for inactive run: %q", help)
	}
}

func TestModel_HelpBar_ShowsRBinding_WhenCompletedRunHasAgentSession(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusSuccess
	m := newTestModel(tree, FromList)

	help := m.renderHelpBar()
	if !containsString(help, "r resume") {
		t.Errorf("help bar should show 'r resume' for completed run with agent session: %q", help)
	}
}

func TestModel_HelpBar_HidesRBinding_WhenCompletedRunHasNoAgentSession(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	root.Children = []*StepNode{{ID: "archive", Type: NodeShell, Status: StatusSuccess, Parent: root}}
	m := newTestModel(&Tree{Root: root}, FromList)

	help := m.renderHelpBar()
	if containsString(help, "r resume") {
		t.Errorf("help bar should not show 'r resume' for completed run with no agent session: %q", help)
	}
}

func TestModel_HelpBar_ShowsJKScroll(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)

	help := m.renderHelpBar()
	if !containsString(help, "j/k scroll") {
		t.Errorf("help bar should show 'j/k scroll': %q", help)
	}
	if containsString(help, "pgup") || containsString(help, "pgdn") {
		t.Errorf("help bar should not mention pgup/pgdn: %q", help)
	}
}

// t key is removed; help bar must not mention "t tail".
func TestModel_HelpBar_NoTailHint(t *testing.T) {
	m := newLiveModelWithFlags()

	help := m.renderHelpBar()
	if containsString(help, "t tail") {
		t.Errorf("help bar should not show 't tail' (key removed): %q", help)
	}
}

func TestModel_DrillIntoLoop(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)

	// Move to loop step (index 2)
	m.cursor = 2

	// Enter drills into loop
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.path) != 2 {
		t.Fatalf("path len = %d, want 2", len(m.path))
	}
	if m.path[1].Type != NodeLoop {
		t.Fatalf("path[1] type = %d, want NodeLoop", m.path[1].Type)
	}
	children := m.currentChildren()
	if len(children) != 2 {
		t.Fatalf("loop children = %d, want 2", len(children))
	}
	if children[0].Type != NodeIteration {
		t.Fatalf("first child type = %d, want NodeIteration", children[0].Type)
	}
}

func TestModel_DrillIntoIterationChildren(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.cursor = 2
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // into loop

	// Enter on iter 1 drills into its children
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.path) != 3 {
		t.Fatalf("path len = %d, want 3", len(m.path))
	}
	children := m.currentChildren()
	if len(children) != 1 || children[0].ID != "run-task" {
		t.Fatalf("iter children: got %v, want [run-task]", children)
	}
}

func TestModel_DrillOut_Esc(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.cursor = 2
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // into loop

	if len(m.path) != 2 {
		t.Fatalf("path len after drill in = %d, want 2", len(m.path))
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.path) != 1 {
		t.Fatalf("path len after esc = %d, want 1", len(m.path))
	}
	// Cursor should be on the loop step (index 2)
	if m.cursor != 2 {
		t.Fatalf("cursor after drill out = %d, want 2", m.cursor)
	}
}

func TestModel_Esc_AtTop_FromList_EmitsBackMsg(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc at top level should produce a cmd")
	}
	msg := cmd()
	if _, ok := msg.(BackMsg); !ok {
		t.Fatalf("expected BackMsg, got %T", msg)
	}
}

func TestModel_Esc_AtTop_FromInspect_EmitsExitMsg(t *testing.T) {
	m := newTestModel(simpleTree(), FromInspect)

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc at top level should produce a cmd")
	}
	msg := cmd()
	if _, ok := msg.(ExitMsg); !ok {
		t.Fatalf("expected ExitMsg, got %T", msg)
	}
}

func TestModel_Q_EmitsExitMsg(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.cursor = 2
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill into loop

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q should produce a cmd")
	}
	msg := cmd()
	if _, ok := msg.(ExitMsg); !ok {
		t.Fatalf("expected ExitMsg, got %T", msg)
	}
}

// In the live-run path runview.Model is the top-level bubbletea model (no
// switcher wrap), so it must self-quit when it receives its own ExitMsg.
func TestModel_ExitMsg_ReturnsQuit(t *testing.T) {
	m := newTestModel(simpleTree(), FromLiveRun)

	_, cmd := m.Update(ExitMsg{})
	if cmd == nil {
		t.Fatal("ExitMsg should produce a cmd that quits the program")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from cmd, got %T", cmd())
	}
}

func TestModel_Enter_AgentStep_EmitsResumeMsg(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.cursor = 1 // agent step with SessionID

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on agent step should produce a cmd")
	}
	msg := cmd()
	resume, ok := msg.(ResumeMsg)
	if !ok {
		t.Fatalf("expected ResumeMsg, got %T", msg)
	}
	if resume.SessionID != "session-abc-123" {
		t.Fatalf("session ID = %q, want %q", resume.SessionID, "session-abc-123")
	}
	if resume.AgentCLI != "claude" {
		t.Fatalf("agent CLI = %q, want %q", resume.AgentCLI, "claude")
	}
}

func TestModel_Enter_AgentStep_LiveRun_NoResume(t *testing.T) {
	m := newTestModel(simpleTree(), FromLiveRun)
	m.running = true
	m.cursor = 1 // agent step with SessionID

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter on agent step during live run should be no-op (agent session is owned by the runner)")
	}
}

func TestModel_Enter_AgentStep_ActiveRun_NoResume(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.active = true
	m.tree.Root.Children[1].Status = StatusSuccess
	m.cursor = 1 // agent step with SessionID

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter on agent step in active run should be no-op")
	}
}

func TestModel_Enter_AgentStep_NoSessionID_NoOp(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	// Modify agent to have no session ID
	m.tree.Root.Children[1].SessionID = ""
	m.cursor = 1

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter on agent step without session ID should be no-op")
	}
}

func TestModel_Enter_AgentStep_InSubWorkflow_EmitsResumeMsg(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	subwf := &StepNode{
		ID:             "impl",
		Type:           NodeSubWorkflow,
		Status:         StatusSuccess,
		Parent:         root,
		StaticWorkflow: "impl.yaml",
		SubLoaded:      true,
	}
	agent := &StepNode{
		ID:        "generate-code",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    subwf,
		AgentCLI:  "claude",
		SessionID: "nested-session-1",
	}
	subwf.Children = []*StepNode{agent}
	root.Children = []*StepNode{subwf}

	m := newTestModel(&Tree{Root: root}, FromList)
	// Drill into the sub-workflow
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Now inside the sub-workflow; cursor should be on agent step
	sel := m.selectedNode()
	if sel == nil || sel.ID != "generate-code" {
		t.Fatalf("expected selected node 'generate-code', got %v", sel)
	}

	// Press Enter on the nested agent step
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on nested agent step should produce a ResumeMsg cmd")
	}
	msg := cmd()
	resume, ok := msg.(ResumeMsg)
	if !ok {
		t.Fatalf("expected ResumeMsg, got %T", msg)
	}
	if resume.SessionID != "nested-session-1" {
		t.Fatalf("session ID = %q, want %q", resume.SessionID, "nested-session-1")
	}
}

func TestModel_Enter_AgentStep_InSubWorkflow_HelpBar(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	subwf := &StepNode{
		ID:             "impl",
		Type:           NodeSubWorkflow,
		Status:         StatusSuccess,
		Parent:         root,
		StaticWorkflow: "impl.yaml",
		SubLoaded:      true,
	}
	agent := &StepNode{
		ID:        "generate-code",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    subwf,
		AgentCLI:  "claude",
		SessionID: "nested-session-1",
	}
	subwf.Children = []*StepNode{agent}
	root.Children = []*StepNode{subwf}

	m := newTestModel(&Tree{Root: root}, FromList)
	// Drill into the sub-workflow
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	help := m.renderHelpBar()
	if !containsString(help, "enter resume") {
		t.Errorf("help bar should show 'enter resume' for nested agent step: %q", help)
	}
}

func TestModel_Enter_AgentStep_InSubWorkflow_DetailPane(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	subwf := &StepNode{
		ID:             "impl",
		Type:           NodeSubWorkflow,
		Status:         StatusSuccess,
		Parent:         root,
		StaticWorkflow: "impl.yaml",
		SubLoaded:      true,
	}
	agent := &StepNode{
		ID:        "generate-code",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    subwf,
		AgentCLI:  "claude",
		SessionID: "nested-session-1",
	}
	subwf.Children = []*StepNode{agent}
	root.Children = []*StepNode{subwf}

	m := newTestModel(&Tree{Root: root}, FromList)
	// Drill into the sub-workflow
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := m.View()
	if !containsString(view, "resume session") {
		t.Errorf("detail pane should show 'resume session' for nested agent step")
	}
}

func TestModel_Enter_AgentStep_InSubWorkflow_LiveRunAfterCompletion(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	subwf := &StepNode{
		ID:             "impl",
		Type:           NodeSubWorkflow,
		Status:         StatusSuccess,
		Parent:         root,
		StaticWorkflow: "impl.yaml",
		SubLoaded:      true,
	}
	agent := &StepNode{
		ID:        "generate-code",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    subwf,
		AgentCLI:  "claude",
		SessionID: "nested-session-1",
	}
	subwf.Children = []*StepNode{agent}
	root.Children = []*StepNode{subwf}

	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true

	// Simulate run completion
	m.Update(liverun.ExecDoneMsg{Result: "success"})

	// Drill into the sub-workflow
	m.cursor = 0 // subwf is the only child at root level
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	sel := m.selectedNode()
	if sel == nil || sel.ID != "generate-code" {
		t.Fatalf("expected selected node 'generate-code', got %v", sel)
	}

	// Press Enter on the nested agent step
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on nested agent step after live run completion should produce ResumeMsg")
	}
	msg := cmd()
	resume, ok := msg.(ResumeMsg)
	if !ok {
		t.Fatalf("expected ResumeMsg, got %T", msg)
	}
	if resume.SessionID != "nested-session-1" {
		t.Fatalf("session ID = %q, want %q", resume.SessionID, "nested-session-1")
	}
}

func TestModel_Enter_CompletedAgentStep_InSubWorkflow_DuringLiveRun_NoResume(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	subwf := &StepNode{
		ID:             "impl",
		Type:           NodeSubWorkflow,
		Status:         StatusSuccess,
		Parent:         root,
		StaticWorkflow: "impl.yaml",
		SubLoaded:      true,
	}
	agent := &StepNode{
		ID:        "generate-code",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    subwf,
		AgentCLI:  "claude",
		SessionID: "nested-session-1",
	}
	subwf.Children = []*StepNode{agent}
	nextStep := &StepNode{
		ID:     "finalize",
		Type:   NodeShell,
		Status: StatusInProgress,
		Parent: root,
	}
	root.Children = []*StepNode{subwf, nextStep}

	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true

	// Drill into the completed sub-workflow
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	sel := m.selectedNode()
	if sel == nil || sel.ID != "generate-code" {
		t.Fatalf("expected selected node 'generate-code', got %v", sel)
	}

	// Press Enter on the completed agent step. Even though this step is done,
	// the live runner still owns the workflow terminal/session boundary.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter on completed agent step during live run should be no-op")
	}
}

func TestModel_Enter_CompletedAgentStep_DuringLiveRun_HelpBar(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	subwf := &StepNode{
		ID:             "impl",
		Type:           NodeSubWorkflow,
		Status:         StatusSuccess,
		Parent:         root,
		StaticWorkflow: "impl.yaml",
		SubLoaded:      true,
	}
	agent := &StepNode{
		ID:        "generate-code",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    subwf,
		AgentCLI:  "claude",
		SessionID: "nested-session-1",
	}
	subwf.Children = []*StepNode{agent}
	nextStep := &StepNode{
		ID:     "finalize",
		Type:   NodeShell,
		Status: StatusInProgress,
		Parent: root,
	}
	root.Children = []*StepNode{subwf, nextStep}

	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true

	// Drill into the completed sub-workflow
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	help := m.renderHelpBar()
	if containsString(help, "enter resume") {
		t.Errorf("help bar should hide 'enter resume' for completed agent step during live run: %q", help)
	}
}

func TestModel_Enter_CompletedAgentStep_DuringLiveRun_DetailPane(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	subwf := &StepNode{
		ID:             "impl",
		Type:           NodeSubWorkflow,
		Status:         StatusSuccess,
		Parent:         root,
		StaticWorkflow: "impl.yaml",
		SubLoaded:      true,
	}
	agent := &StepNode{
		ID:        "generate-code",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    subwf,
		AgentCLI:  "claude",
		SessionID: "nested-session-1",
	}
	subwf.Children = []*StepNode{agent}
	nextStep := &StepNode{
		ID:     "finalize",
		Type:   NodeShell,
		Status: StatusInProgress,
		Parent: root,
	}
	root.Children = []*StepNode{subwf, nextStep}

	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true
	m.altScreen = true

	// Drill into the completed sub-workflow
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	view := m.View()
	if containsString(view, "resume session") {
		t.Errorf("detail pane should hide 'resume session' for completed agent step during live run")
	}
}

func TestModel_Enter_InProgressAgentStep_DuringLiveRun_NoResume(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	agent := &StepNode{
		ID:        "implement",
		Type:      NodeHeadlessAgent,
		Status:    StatusInProgress,
		Parent:    root,
		AgentCLI:  "claude",
		SessionID: "session-in-progress",
	}
	root.Children = []*StepNode{agent}

	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter on in-progress agent step during live run should be no-op " +
			"(session is owned by the runner)")
	}
}

func TestModel_Enter_ShellStep_NoOp(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.cursor = 0 // shell step

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter on shell step should be no-op")
	}
}

func TestModel_LegendToggle(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)

	if m.showLegend {
		t.Fatal("legend should start hidden")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if !m.showLegend {
		t.Fatal("? should show legend")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if m.showLegend {
		t.Fatal("? again should hide legend")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.showLegend {
		t.Fatal("esc should hide legend")
	}
}

func TestModel_View_Renders(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	output := m.View()

	if output == "" {
		t.Fatal("View should produce output")
	}

	checks := []string{"Agent Runner", "test-workflow", "build", "implement", "tasks"}
	for _, check := range checks {
		if !containsString(output, check) {
			t.Errorf("View output missing %q", check)
		}
	}
}

func TestModel_View_LegendOverlay(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.showLegend = true
	output := m.View()

	checks := []string{"Legend", "running", "pending", "success", "failed", "skipped", "shell", "headless agent"}
	for _, check := range checks {
		if !containsString(output, check) {
			t.Errorf("Legend overlay missing %q", check)
		}
	}
}

func TestModel_AutoFlatten(t *testing.T) {
	root := &StepNode{
		ID:     "wf",
		Type:   NodeRoot,
		Status: StatusInProgress,
	}
	loop := &StepNode{
		ID:          "my-loop",
		Type:        NodeLoop,
		Status:      StatusInProgress,
		Parent:      root,
		AutoFlatten: true,
	}
	subwf := &StepNode{
		ID:                 "impl",
		Type:               NodeSubWorkflow,
		Status:             StatusInProgress,
		Parent:             nil, // set below
		StaticWorkflow:     "impl.yaml",
		StaticWorkflowPath: "/repo/workflows/impl.yaml",
		SubLoaded:          true,
	}
	subChild := &StepNode{
		ID:     "step-a",
		Type:   NodeShell,
		Status: StatusPending,
		Parent: subwf,
	}
	subwf.Children = []*StepNode{subChild}

	iter := &StepNode{
		ID:             "my-loop",
		Type:           NodeIteration,
		Status:         StatusInProgress,
		Parent:         loop,
		IterationIndex: 0,
		BindingValue:   "foo.md",
		FlattenTarget:  subwf,
		Children:       []*StepNode{subwf},
	}
	subwf.Parent = iter
	loop.Children = []*StepNode{iter}
	root.Children = []*StepNode{loop}

	tree := &Tree{Root: root}
	m := newTestModel(tree, FromList)

	// Drill into loop
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.path) != 2 {
		t.Fatalf("after entering loop: path len = %d, want 2", len(m.path))
	}

	// Enter on iteration should auto-flatten past sub-workflow
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.path) != 3 {
		t.Fatalf("after entering iteration: path len = %d, want 3", len(m.path))
	}

	// Current container should be the sub-workflow (FlattenTarget)
	container := m.currentContainer()
	if container.Type != NodeSubWorkflow {
		t.Fatalf("current container = %d, want NodeSubWorkflow", container.Type)
	}
	children := m.currentChildren()
	if len(children) != 1 || children[0].ID != "step-a" {
		t.Fatalf("children = %v, want [step-a]", children)
	}

	// Breadcrumb should NOT contain the sub-workflow step
	breadcrumb := m.renderBreadcrumb()
	if containsString(breadcrumb, "impl") {
		t.Error("breadcrumb should not contain the flattened sub-workflow name")
	}
	if !containsString(breadcrumb, "iter 1") {
		t.Error("breadcrumb should contain iter 1")
	}
}

func TestModel_SubWorkflowHeader(t *testing.T) {
	root := &StepNode{
		ID:     "wf",
		Type:   NodeRoot,
		Status: StatusInProgress,
	}
	subwf := &StepNode{
		ID:                 "verify",
		Type:               NodeSubWorkflow,
		Status:             StatusInProgress,
		Parent:             root,
		StaticWorkflow:     "verify.yaml",
		StaticWorkflowPath: "/repo/workflows/verify.yaml",
		SubLoaded:          true,
		InterpolatedParams: map[string]string{"task_file": "task.md"},
	}
	child := &StepNode{
		ID:     "check",
		Type:   NodeShell,
		Status: StatusPending,
		Parent: subwf,
	}
	subwf.Children = []*StepNode{child}
	root.Children = []*StepNode{subwf}
	tree := &Tree{Root: root}
	m := newTestModel(tree, FromList)

	// Drill into sub-workflow
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	header := m.renderSubWorkflowHeader()
	if header == "" {
		t.Fatal("sub-workflow header should be shown when drilled into a sub-workflow")
	}
	if !containsString(header, "workflow:") {
		t.Error("header should contain 'workflow:'")
	}
	if !containsString(header, "task_file") {
		t.Error("header should contain param name")
	}
}

func TestOutput_SanitizeUTF8(t *testing.T) {
	valid := "hello world"
	if got := sanitizeUTF8(valid); got != valid {
		t.Errorf("sanitizeUTF8(%q) = %q, want %q", valid, got, valid)
	}

	invalid := "hello\x80world"
	got := sanitizeUTF8(invalid)
	want := "hello\uFFFDworld"
	if got != want {
		t.Errorf("sanitizeUTF8 with invalid byte: got %q, want %q", got, want)
	}
}

func TestOutput_TruncateOutput(t *testing.T) {
	small := "line1\nline2\nline3"
	result := truncateOutput(small)
	if result.Truncated {
		t.Error("small output should not be truncated")
	}
	if result.TotalLines != 3 {
		t.Errorf("total lines = %d, want 3", result.TotalLines)
	}

	// Generate large output
	var lines []string
	for i := 0; i < 3000; i++ {
		lines = append(lines, "line")
	}
	large := ""
	for _, l := range lines {
		large += l + "\n"
	}
	result = truncateOutput(large)
	if !result.Truncated {
		t.Error("large output should be truncated")
	}
	if len(result.Lines) != tailLines {
		t.Errorf("shown lines = %d, want %d", len(result.Lines), tailLines)
	}
	banner := result.banner()
	if banner == "" {
		t.Error("banner should not be empty for truncated output")
	}
	if !containsString(banner, "press g to load all") {
		t.Error("banner should contain load hint")
	}
}

func TestModel_LoadFull(t *testing.T) {
	tree := simpleTree()
	// Give the shell step some output
	tree.Root.Children[0].Stdout = generateLargeOutput(3000)
	m := newTestModel(tree, FromList)
	m.cursor = 0

	step := tree.Root.Children[0]
	if m.loadedFull[step.NodeKey()] {
		t.Fatal("should not be loaded full initially")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if !m.loadedFull[step.NodeKey()] {
		t.Fatal("g should mark step as loaded full")
	}
}

// TestModel_LoadFull_DoesNotCrossContaminateByID verifies that pressing g on one
// step does not mark a different step with the same ID as loaded-full.
// Iteration nodes reuse the loop ID (ensureIteration sets ID: loop.ID), so this
// was a real bug when loadedFull was keyed by node.ID instead of *StepNode.
func TestModel_LoadFull_DoesNotCrossContaminateByID(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	loop := &StepNode{ID: "tasks", Type: NodeLoop, Status: StatusInProgress, Parent: root}

	// Two shell steps in different iterations but with the same ID — just like
	// cloneTemplate produces when seeding iteration children from the loop body.
	step0 := &StepNode{
		ID:     "run", // same ID as step1
		Type:   NodeShell,
		Status: StatusSuccess,
		Stdout: generateLargeOutput(3000),
	}
	step1 := &StepNode{
		ID:     "run", // same ID as step0
		Type:   NodeShell,
		Status: StatusSuccess,
	}
	iter0 := &StepNode{ID: "tasks", Type: NodeIteration, Status: StatusSuccess, Parent: loop, IterationIndex: 0}
	iter1 := &StepNode{ID: "tasks", Type: NodeIteration, Status: StatusSuccess, Parent: loop, IterationIndex: 1}
	step0.Parent = iter0
	step1.Parent = iter1
	iter0.Children = []*StepNode{step0}
	iter1.Children = []*StepNode{step1}
	loop.Children = []*StepNode{iter0, iter1}
	root.Children = []*StepNode{loop}

	m := newTestModel(&Tree{Root: root}, FromList)

	// Drill into the loop, then into iter0, select step0 and press g.
	m.cursor = 0 // loop
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.cursor = 0 // iter0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.cursor = 0 // step0
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})

	if !m.loadedFull[step0.NodeKey()] {
		t.Error("step0 should be marked loadedFull")
	}
	if m.loadedFull[step1.NodeKey()] {
		t.Error("step1 should NOT be marked loadedFull (same ID, different pointer)")
	}
}

func TestModel_LoadFull_PersistsAcrossEquivalentTreeRebuild(t *testing.T) {
	build := func() (*Tree, *StepNode, *StepNode, *StepNode) {
		root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
		loop := &StepNode{ID: "tasks", Type: NodeLoop, Status: StatusInProgress, Parent: root}
		iter := &StepNode{ID: "tasks", Type: NodeIteration, Status: StatusSuccess, Parent: loop, IterationIndex: 0}
		step := &StepNode{
			ID:     "run",
			Type:   NodeShell,
			Status: StatusSuccess,
			Parent: iter,
			Stdout: generateLargeOutput(3000),
		}
		iter.Children = []*StepNode{step}
		loop.Children = []*StepNode{iter}
		root.Children = []*StepNode{loop}
		return &Tree{Root: root}, loop, iter, step
	}

	tree1, _, _, step1 := build()
	m := newTestModel(tree1, FromList)
	m.cursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.cursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.cursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})

	if !m.loadedFull[step1.NodeKey()] {
		t.Fatal("step should be marked loaded full before tree rebuild")
	}

	tree2, loop2, iter2, _ := build()
	m.tree = tree2
	m.path = []*StepNode{tree2.Root, loop2, iter2}
	m.cursor = 0

	if m.selectedNodeHasTruncatedOutput() {
		t.Fatal("loaded-full state should survive an equivalent tree rebuild")
	}
}

// TestModel_KScroll_AfterAutoScroll_IsEffective verifies that pressing k after
// an auto-scroll (which sets logOffset to math.MaxInt32) produces a meaningful
// decrease in logOffset — not just MaxInt32 − 1.
func TestModel_KScroll_AfterAutoScroll_IsEffective(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	step := &StepNode{
		ID:     "build",
		Type:   NodeShell,
		Status: StatusSuccess,
		Parent: root,
		Stdout: generateLargeOutput(100), // enough lines to make maxOffset > 0
	}
	root.Children = []*StepNode{step}
	m := newTestModel(&Tree{Root: root}, FromList)
	m.termHeight = 20 // small height so maxOffset > 0

	// Simulate the auto-scroll sentinel (as set by handleOutputChunkMsg).
	m.logOffset = math.MaxInt32
	lineCount := m.rebuildRanges()
	m.clampLogOffset(lineCount) // fix should ensure this runs in the real code path too

	preK := m.logOffset
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})

	if m.logOffset >= preK {
		t.Fatalf("k did not decrease logOffset: before=%d after=%d", preK, m.logOffset)
	}
	// After fix the offset should be well below the MaxInt32 range.
	if m.logOffset > 10_000 {
		t.Fatalf("logOffset still huge after k (%d); MaxInt32 sentinel was not clamped", m.logOffset)
	}
}

func TestModel_SelectedNodeHasTruncatedOutput_StderrOnly(t *testing.T) {
	tree := simpleTree()
	tree.Root.Children[0].Stdout = ""
	tree.Root.Children[0].Stderr = generateLargeOutput(3000)

	m := newTestModel(tree, FromList)
	m.cursor = 0

	if !m.selectedNodeHasTruncatedOutput() {
		t.Fatal("stderr-only truncated output should advertise the g key")
	}
}

// ---- Live-run tests ----

func liveTree() *Tree {
	root := &StepNode{
		ID:     "live-workflow",
		Type:   NodeRoot,
		Status: StatusInProgress,
	}
	shell := &StepNode{
		ID:            "build",
		Type:          NodeShell,
		Status:        StatusInProgress,
		Parent:        root,
		StaticCommand: "make",
	}
	root.Children = []*StepNode{shell}
	return &Tree{Root: root}
}

func newLiveModel() *Model {
	tree := liveTree()
	return &Model{
		tree:       tree,
		entered:    FromLiveRun,
		path:       []*StepNode{tree.Root},
		loadedFull: make(map[string]bool),
		termWidth:  120,
		termHeight: 40,
		running:    true,
		altScreen:  true,
	}
}

func TestModel_LiveRun_OutputChunk(t *testing.T) {
	m := newLiveModel()
	shell := m.tree.Root.Children[0]

	// Audit prefix for a top-level step "build" is "[build]"
	m.Update(liverun.OutputChunkMsg{StepPrefix: "[build]", Stream: "stdout", Bytes: []byte("hello\n")})
	m.Update(liverun.OutputChunkMsg{StepPrefix: "[build]", Stream: "stdout", Bytes: []byte("world\n")})

	if !containsString(shell.Stdout, "hello") {
		t.Errorf("stdout missing 'hello': %q", shell.Stdout)
	}
	if !containsString(shell.Stdout, "world") {
		t.Errorf("stdout missing 'world': %q", shell.Stdout)
	}
}

func TestModel_LiveRun_ExecDone_Success(t *testing.T) {
	m := newLiveModel()
	if !m.running {
		t.Fatal("expected running=true before ExecDoneMsg")
	}

	m.Update(liverun.ExecDoneMsg{Result: "success"})
	if m.running {
		t.Error("expected running=false after ExecDoneMsg")
	}
	if m.liveResult != "success" {
		t.Errorf("liveResult = %q, want 'success'", m.liveResult)
	}

	// Breadcrumb should show "completed"
	bc := m.renderBreadcrumb()
	if !containsString(bc, "completed") {
		t.Errorf("breadcrumb missing 'completed': %q", bc)
	}
}

func TestModel_LiveRun_ExecDone_Failed(t *testing.T) {
	m := newLiveModel()
	m.Update(liverun.ExecDoneMsg{Result: "failed"})

	if m.running {
		t.Error("expected running=false after ExecDoneMsg")
	}
	bc := m.renderBreadcrumb()
	if !containsString(bc, "failed") {
		t.Errorf("breadcrumb missing 'failed': %q", bc)
	}
}

func TestModel_LiveRun_QuitConfirm_Shown(t *testing.T) {
	m := newLiveModel()

	// q while running should open confirmation modal
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = m2.(*Model)
	if !m.quitConfirming {
		t.Fatal("expected quitConfirming=true after q mid-run")
	}
	// View should render the confirmation text
	v := m.View()
	if !containsString(v, "still running") {
		t.Errorf("quit confirm view missing 'still running': %q", v)
	}
}

func TestModel_LiveRun_QuitConfirm_CtrlC(t *testing.T) {
	m := newLiveModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = m2.(*Model)
	if !m.quitConfirming {
		t.Fatal("expected quitConfirming=true after Ctrl+C mid-run")
	}
}

func TestModel_LiveRun_QuitConfirm_EscAtTopLevel(t *testing.T) {
	m := newLiveModel()
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(*Model)
	if !m.quitConfirming {
		t.Fatal("expected quitConfirming=true after Esc at top level mid-run")
	}
}

func TestModel_LiveRun_QuitConfirm_EscDrillOut(t *testing.T) {
	m := newLiveModel()
	// Drill into loop to leave top level
	loop := &StepNode{ID: "tasks", Type: NodeLoop, Status: StatusInProgress, Parent: m.tree.Root}
	m.tree.Root.Children = append(m.tree.Root.Children, loop)
	m.path = append(m.path, loop)

	// Esc while drilled in should drill out, not confirm
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = m2.(*Model)
	if m.quitConfirming {
		t.Error("Esc while drilled in should drill out, not open quit confirm")
	}
	if len(m.path) != 1 {
		t.Errorf("path len = %d, want 1 after drill-out", len(m.path))
	}
}

func TestModel_LiveRun_QuitConfirm_Decline(t *testing.T) {
	m := newLiveModel()
	m.quitConfirming = true

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = m2.(*Model)
	if m.quitConfirming {
		t.Error("expected quitConfirming=false after n")
	}
	if !m.running {
		t.Error("expected running=true after declining quit")
	}
}

func TestModel_LiveRun_QuitAfterDone_NoConfirm(t *testing.T) {
	m := newLiveModel()
	m.running = false
	m.liveResult = "success"

	// q after done should exit immediately (emit ExitMsg) without confirmation
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.quitConfirming {
		t.Error("quitConfirming should not be set after run is done")
	}
	if cmd == nil {
		t.Error("expected an exit command after q on completed run")
	}
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

func generateLargeOutput(lines int) string {
	var b []byte
	for i := 0; i < lines; i++ {
		b = append(b, "output line content here\n"...)
	}
	return string(b)
}

// ---- autoFollow / navigateToNode / lockout tests ----

func newLiveModelWithFlags() *Model {
	tree := liveTree()
	return &Model{
		tree:       tree,
		entered:    FromLiveRun,
		path:       []*StepNode{tree.Root},
		loadedFull: make(map[string]bool),
		termWidth:  120,
		termHeight: 40,
		running:    true,
		autoFollow: true,
		altScreen:  true,
	}
}

func TestModel_FromLiveRun_DefaultFlags(t *testing.T) {
	sessionDir := t.TempDir()
	// Write a minimal workflow file so the loader can build a tree.
	wfPath := sessionDir + "/workflow.yaml"
	wfContent := "name: live-workflow\nsteps:\n  - id: build\n    command: make\n"
	if err := os.WriteFile(wfPath, []byte(wfContent), 0o644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}

	m, err := New(sessionDir, "", FromLiveRun)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !m.autoFollow {
		t.Error("autoFollow should be true in FromLiveRun")
	}
	if !m.running {
		t.Error("running should be true in FromLiveRun")
	}
}

func TestModel_FromList_DefaultFlags(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	if m.autoFollow {
		t.Error("autoFollow should be false in FromList")
	}
}

func TestModel_NavigateToNode_TopLevel(t *testing.T) {
	tree := simpleTree()
	m := newTestModel(tree, FromList)

	// Navigate to the third child (loop, index 2)
	target := tree.Root.Children[2]
	m.navigateToNode(target)

	if len(m.path) != 1 {
		t.Fatalf("path len = %d, want 1 for top-level node", len(m.path))
	}
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.cursor)
	}
}

func TestModel_NavigateToNode_InsideIteration(t *testing.T) {
	tree := simpleTree()
	m := newTestModel(tree, FromList)

	// iter1 (index 0 in loop.Children) has one child: iter1child
	loop := tree.Root.Children[2] // NodeLoop
	iter1 := loop.Children[0]     // NodeIteration (index 0)
	target := iter1.Children[0]   // "run-task" shell step

	m.navigateToNode(target)

	// path should be [root, loop, iter1]
	if len(m.path) != 3 {
		t.Fatalf("path len = %d, want 3 for nested node", len(m.path))
	}
	if m.path[1] != loop {
		t.Error("path[1] should be loop")
	}
	if m.path[2] != iter1 {
		t.Error("path[2] should be iter1")
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
}

func TestModel_NavigateToNode_AutoFlatten(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	loop := &StepNode{ID: "my-loop", Type: NodeLoop, Status: StatusInProgress, Parent: root, AutoFlatten: true}
	subwf := &StepNode{
		ID: "impl", Type: NodeSubWorkflow, Status: StatusInProgress,
		StaticWorkflowPath: "/repo/workflows/impl.yaml", SubLoaded: true,
	}
	subChild := &StepNode{ID: "step-a", Type: NodeShell, Status: StatusPending, Parent: subwf}
	subwf.Children = []*StepNode{subChild}
	iter := &StepNode{
		ID: "my-loop", Type: NodeIteration, Status: StatusInProgress, Parent: loop,
		IterationIndex: 0, FlattenTarget: subwf, Children: []*StepNode{subwf},
	}
	subwf.Parent = iter
	loop.Children = []*StepNode{iter}
	root.Children = []*StepNode{loop}
	tree := &Tree{Root: root}
	m := newTestModel(tree, FromList)

	// Navigate to subChild (inside auto-flattened iter)
	m.navigateToNode(subChild)

	// path = [root, loop, iter] — subwf is NOT in the path (it's FlattenTarget)
	if len(m.path) != 3 {
		t.Fatalf("path len = %d, want 3", len(m.path))
	}
	if m.path[1] != loop {
		t.Error("path[1] should be loop")
	}
	if m.path[2] != iter {
		t.Error("path[2] should be iter (FlattenTarget skipped)")
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
}

func TestModel_StepStateMsg_AutoFollow_NavigatesCursor(t *testing.T) {
	tree := simpleTree()
	m := newLiveModelWithFlags()
	m.tree = tree
	m.path = []*StepNode{tree.Root}

	// Simulate StepStateMsg for the second child (agent, index 1, ID "implement")
	m.Update(liverun.StepStateMsg{ActiveStepPrefix: "[implement]"})

	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 after auto-follow StepStateMsg", m.cursor)
	}
	if m.activeStepPrefix != "[implement]" {
		t.Fatalf("activeStepPrefix = %q, want [implement]", m.activeStepPrefix)
	}
}

// TestModel_StepStateMsg_AutoFollow_NoDrillIn verifies that when the active step
// is nested inside a loop iteration, auto-follow moves the cursor to the
// top-level ancestor (the loop) but does NOT drill into the loop.
func TestModel_StepStateMsg_AutoFollow_NoDrillIn(t *testing.T) {
	tree := simpleTree()
	m := newLiveModelWithFlags()
	m.tree = tree
	m.path = []*StepNode{tree.Root}
	m.cursor = 0

	// The prefix [tasks:0, run-task] refers to run-task inside iter1 inside tasks loop.
	m.Update(liverun.StepStateMsg{ActiveStepPrefix: "[tasks:0, run-task]"})

	// cursor should be 2 (the loop "tasks" at index 2 in root.Children)
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (tasks loop index)", m.cursor)
	}
	// path must remain at root — no drill-in
	if len(m.path) != 1 {
		t.Fatalf("path len = %d, want 1 (no drill-in)", len(m.path))
	}
}

func TestModel_StepStateMsg_ActiveRunAutoScrollsLog(t *testing.T) {
	tree := simpleTree()
	m := newLiveModelWithFlags()
	m.tree = tree
	m.path = []*StepNode{tree.Root}
	m.logOffset = 7

	m.Update(liverun.StepStateMsg{ActiveStepPrefix: "[implement]"})

	// After the fix, logOffset is clamped to [0, maxOffset] immediately after
	// rebuild, so it should be well below the MaxInt32 sentinel value.
	if m.logOffset == 7 {
		t.Fatal("logOffset should have changed (auto-scroll should fire)")
	}
	if m.logOffset > 10_000 {
		t.Fatalf("logOffset too large (%d); auto-scroll should clamp to real maxOffset", m.logOffset)
	}
}

func TestModel_StepStateMsg_AutoFollowOff_NoNavigation(t *testing.T) {
	tree := simpleTree()
	m := newLiveModelWithFlags()
	m.tree = tree
	m.path = []*StepNode{tree.Root}
	m.cursor = 0
	m.autoFollow = false

	m.Update(liverun.StepStateMsg{ActiveStepPrefix: "[implement]"})

	// cursor should stay at 0 since autoFollow is off
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 when autoFollow is disabled", m.cursor)
	}
}

func TestModel_ManualNavigation_ClearsAutoFollow(t *testing.T) {
	m := newLiveModelWithFlags()

	// up/down arrow keys, j, and k all clear autoFollow.
	for _, key := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyUp},
		tea.KeyMsg{Type: tea.KeyDown},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")},
	} {
		m.autoFollow = true
		m.Update(key)
		if m.autoFollow {
			t.Errorf("key %v should clear autoFollow", key)
		}
	}
}

func TestModel_ArrowToPendingStep_RebuildsGhostRangeAndSyncsLog(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	done := &StepNode{
		ID:            "setup",
		Type:          NodeShell,
		Status:        StatusSuccess,
		Parent:        root,
		StaticCommand: "echo setup",
		ExitCode:      intPtr(0),
	}
	pending := &StepNode{
		ID:            "deploy",
		Type:          NodeShell,
		Status:        StatusPending,
		Parent:        root,
		StaticCommand: "echo deploy",
	}
	root.Children = []*StepNode{done, pending}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.rebuildRanges()

	m.Update(tea.KeyMsg{Type: tea.KeyDown})

	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	if len(m.stepRanges) != 2 {
		t.Fatalf("expected ghost range to be added, got %d ranges", len(m.stepRanges))
	}
	if m.stepRanges[1].node != pending {
		t.Fatalf("stepRanges[1] = %v, want pending node", m.stepRanges[1].node)
	}
	if m.logOffset != m.stepRanges[1].startLine {
		t.Fatalf("logOffset = %d, want ghost startLine %d", m.logOffset, m.stepRanges[1].startLine)
	}
}

func TestModel_EnterEsc_ClearAutoFollow(t *testing.T) {
	m := newLiveModelWithFlags()
	// Add a loop to drill into to avoid the quit-confirm path on Esc at top level
	loop := &StepNode{ID: "tasks", Type: NodeLoop, Parent: m.tree.Root}
	m.tree.Root.Children = append(m.tree.Root.Children, loop)
	m.cursor = len(m.tree.Root.Children) - 1 // select loop

	// Enter should clear autoFollow
	m.autoFollow = true
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.autoFollow {
		t.Error("Enter should clear autoFollow")
	}

	// Esc should clear autoFollow
	m.autoFollow = true
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.autoFollow {
		t.Error("Esc should clear autoFollow")
	}
}

func TestModel_LKey_ReengagesAutoFollow(t *testing.T) {
	tree := simpleTree()
	m := newLiveModelWithFlags()
	m.tree = tree
	m.path = []*StepNode{tree.Root}
	m.autoFollow = false
	m.activeStepPrefix = "[implement]" // agent step at index 1

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})

	if !m.autoFollow {
		t.Error("l key should re-engage autoFollow")
	}
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 after l key", m.cursor)
	}
}

// TestModel_LKey_NoDrillIn verifies that pressing l re-engages auto-follow
// at the current drill level without drilling into sub-workflows or loops.
func TestModel_LKey_NoDrillIn(t *testing.T) {
	tree := simpleTree()
	m := newLiveModelWithFlags()
	m.tree = tree
	m.path = []*StepNode{tree.Root}
	m.autoFollow = false
	// Active step is nested inside the tasks loop iteration
	m.activeStepPrefix = "[tasks:0, run-task]"

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})

	if !m.autoFollow {
		t.Error("l key should re-engage autoFollow")
	}
	// cursor should land on "tasks" (index 2), not drill in
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (tasks loop)", m.cursor)
	}
	if len(m.path) != 1 {
		t.Fatalf("path len = %d, want 1 (no drill-in)", len(m.path))
	}
}

func TestModel_MouseWheelUp_ClearsAutoFollow(t *testing.T) {
	m := newLiveModelWithFlags()
	m.autoFollow = true

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})

	if m.autoFollow {
		t.Error("mouse wheel up should clear autoFollow")
	}
}

func TestModel_MouseWheelDown_ClearsAutoFollow(t *testing.T) {
	m := newLiveModelWithFlags()
	m.autoFollow = true

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})

	if m.autoFollow {
		t.Error("mouse wheel down should clear autoFollow")
	}
}

func TestModel_MouseWheelDown_ChangesLogOffset(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.logLineCount = 100
	initial := m.logOffset

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.logOffset <= initial {
		t.Fatalf("mouse wheel down should increase logOffset: got %d, want > %d", m.logOffset, initial)
	}
}

func TestModel_MouseWheelUp_ChangesLogOffset(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.logLineCount = 100
	m.logOffset = 10

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if m.logOffset >= 10 {
		t.Fatalf("mouse wheel up should decrease logOffset: got %d, want < 10", m.logOffset)
	}
}

func TestModel_LiveRun_OutputChunk_DoesNotTailWhenAutoFollowOff(t *testing.T) {
	m := newLiveModelWithFlags()
	m.termHeight = 10
	shell := m.tree.Root.Children[0]
	shell.Stdout = generateLargeOutput(100)
	lineCount := m.rebuildRanges()
	m.clampLogOffset(lineCount)
	m.logOffset = m.maxLogOffset()

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if m.autoFollow {
		t.Fatal("test setup: mouse wheel up should clear autoFollow")
	}
	scrolledOffset := m.logOffset

	m.Update(liverun.OutputChunkMsg{StepPrefix: "[build]", Stream: "stdout", Bytes: []byte("new output\n")})

	if m.logOffset != scrolledOffset {
		t.Fatalf("output chunk should not tail when autoFollow is off: before=%d after=%d", scrolledOffset, m.logOffset)
	}
}

func TestModel_ResumedMsg_ReEnablesMouse(t *testing.T) {
	m := newLiveModelWithFlags()
	_, cmd := m.Update(liverun.ResumedMsg{})
	if cmd == nil {
		t.Fatal("ResumedMsg should return a command to re-enable mouse")
	}
	msg := cmd()
	if _, ok := msg.(tea.MouseMsg); ok {
		t.Fatal("expected enableMouseCellMotionMsg, not MouseMsg")
	}
	got := fmt.Sprintf("%T", msg)
	if got != "tea.enableMouseCellMotionMsg" {
		t.Fatalf("ResumedMsg should return EnableMouseCellMotion cmd, got %s", got)
	}
}

func TestModel_ExecDone_Failed_JumpsToFailedStep(t *testing.T) {
	tree := simpleTree()
	// Mark the first step (shell "build") as failed
	tree.Root.Children[0].Status = StatusFailed

	m := newLiveModelWithFlags()
	m.tree = tree
	m.path = []*StepNode{tree.Root}
	m.cursor = 3 // start somewhere else

	m.Update(liverun.ExecDoneMsg{Result: "failed"})

	// Cursor should jump to the failed step (index 0, "build")
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (index of failed step)", m.cursor)
	}
}

func TestModel_ExecDone_Failed_NoFailedStep_NoChange(t *testing.T) {
	m := newLiveModelWithFlags()
	m.cursor = 0

	// No step has StatusFailed — navigateToNode should be a no-op
	m.Update(liverun.ExecDoneMsg{Result: "failed"})

	if m.cursor != 0 {
		t.Fatalf("cursor changed to %d, want 0 (no failed step)", m.cursor)
	}
}

func TestModel_ExecDone_Success_JumpsToLastTopLevelStep(t *testing.T) {
	tree := simpleTree()
	for _, c := range tree.Root.Children {
		c.Status = StatusSuccess
	}

	m := newLiveModelWithFlags()
	m.tree = tree
	m.path = []*StepNode{tree.Root}
	m.cursor = 1 // somewhere other than the last child

	m.Update(liverun.ExecDoneMsg{Result: "success"})

	want := len(tree.Root.Children) - 1
	if m.cursor != want {
		t.Fatalf("cursor = %d, want %d (last top-level step)", m.cursor, want)
	}
	if len(m.path) != 1 || m.path[0] != tree.Root {
		t.Fatalf("path should remain at root, got %d segments", len(m.path))
	}
}

func TestModel_HelpBar_ShowsLiveHint_WhenAutoFollowOff(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true
	m.autoFollow = false

	help := m.renderHelpBar()
	if !containsString(help, "l follow") {
		t.Errorf("help bar missing 'l follow' hint: %q", help)
	}
}

func TestModel_HelpBar_HidesLiveHint_WhenAutoFollowOn(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true
	m.autoFollow = true

	help := m.renderHelpBar()
	if containsString(help, "l follow") {
		t.Errorf("help bar should not show 'l follow' when autoFollow is on: %q", help)
	}
}

func TestModel_Legend_ContainsLiveNavKeys(t *testing.T) {
	m := newLiveModelWithFlags()
	m.showLegend = true

	legend := m.View()
	for _, want := range []string{"l", "Live Navigation"} {
		if !containsString(legend, want) {
			t.Errorf("legend missing %q", want)
		}
	}
	// t key is removed from legend
	if containsString(legend, "t  jump") || containsString(legend, "t tail") {
		t.Errorf("legend should not mention removed 't' tail key")
	}
}

func TestModel_HelpBar_Live_HidesEscBackAndEnterResume(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true

	help := m.renderHelpBar()
	if containsString(help, "esc back") {
		t.Errorf("help bar should hide 'esc back' while running: %q", help)
	}
	if containsString(help, "enter resume") {
		t.Errorf("help bar should hide 'enter resume' while running: %q", help)
	}
}

func TestModel_HelpBar_Live_ShowsEscBackAndEnterResume_AfterExecDone(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true

	// Replace the default single-shell tree with one containing a completed
	// agent step so renderHelpBar has a resume-able selection to advertise.
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	agent := &StepNode{
		ID:        "implement",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    root,
		AgentCLI:  "claude",
		SessionID: "sess-1",
	}
	root.Children = []*StepNode{agent}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}
	m.cursor = 0

	m.Update(liverun.ExecDoneMsg{Result: "success"})

	help := m.renderHelpBar()
	if !containsString(help, "esc back") {
		t.Errorf("help bar should show 'esc back' after run completes: %q", help)
	}
	if !containsString(help, "enter resume") {
		t.Errorf("help bar should show 'enter resume' after run completes: %q", help)
	}
}

func TestModel_LiveRun_ResumeMsg_StoresInfoAndQuits(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = false

	_, cmd := m.Update(ResumeMsg{AgentCLI: "claude", SessionID: "sess-xyz"})
	if cmd == nil {
		t.Fatal("ResumeMsg should produce a cmd that quits the program")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}
	if m.ResumeAgentCLI() != "claude" || m.ResumeSessionID() != "sess-xyz" {
		t.Fatalf("resume info not stored: cli=%q, session=%q", m.ResumeAgentCLI(), m.ResumeSessionID())
	}
}

// ---- NewForReentry tests ----

func TestNewForReentry_HasCorrectState(t *testing.T) {
	sessionDir := t.TempDir()
	m, err := NewForReentry(sessionDir, "", FromLiveRun, nil)
	if err != nil {
		t.Fatalf("NewForReentry: %v", err)
	}
	if m.running {
		t.Error("running should be false in NewForReentry model")
	}
	if m.autoFollow {
		t.Error("autoFollow should be false in NewForReentry model")
	}
	if m.entered != FromLiveRun {
		t.Errorf("entered = %d, want FromLiveRun (%d)", m.entered, FromLiveRun)
	}
}

func TestNewForReentry_PreservesProvidedEntered(t *testing.T) {
	for _, entered := range []Entered{FromList, FromInspect, FromLiveRun} {
		sessionDir := t.TempDir()
		m, err := NewForReentry(sessionDir, "", entered, nil)
		if err != nil {
			t.Fatalf("NewForReentry(%d): %v", entered, err)
		}
		if m.entered != entered {
			t.Errorf("entered = %d, want %d", m.entered, entered)
		}
		if m.Entered() != entered {
			t.Errorf("Entered() = %d, want %d", m.Entered(), entered)
		}
		if m.SessionDir() != sessionDir {
			t.Errorf("SessionDir() = %q, want %q", m.SessionDir(), sessionDir)
		}
	}
}

func TestNewForReentry_SpawnError_ShowsInView(t *testing.T) {
	sessionDir := t.TempDir()
	m, err := NewForReentry(sessionDir, "", FromLiveRun, errors.New("spawn failed: claude: not found in PATH"))
	if err != nil {
		t.Fatalf("NewForReentry: %v", err)
	}
	m.termWidth = 120
	m.termHeight = 40
	v := m.View()
	if !containsString(v, "not found") {
		t.Errorf("view should surface spawn error; got: %q", v)
	}
}

func TestNewForReentry_NoSpawnError_NoNotice(t *testing.T) {
	sessionDir := t.TempDir()
	m, err := NewForReentry(sessionDir, "", FromLiveRun, nil)
	if err != nil {
		t.Fatalf("NewForReentry: %v", err)
	}
	if m.notice != "" {
		t.Errorf("notice should be empty when no spawn error; got %q", m.notice)
	}
}

func TestNewForReentry_Enter_AgentStep_EmitsResumeMsg(t *testing.T) {
	sessionDir := t.TempDir()
	m, err := NewForReentry(sessionDir, "", FromLiveRun, nil)
	if err != nil {
		t.Fatalf("NewForReentry: %v", err)
	}
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	agent := &StepNode{
		ID:        "implement",
		Type:      NodeHeadlessAgent,
		Status:    StatusSuccess,
		Parent:    root,
		AgentCLI:  "claude",
		SessionID: "sess-reentry",
	}
	root.Children = []*StepNode{agent}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}
	m.cursor = 0

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on agent step should produce a cmd")
	}
	msg := cmd()
	resume, ok := msg.(ResumeMsg)
	if !ok {
		t.Fatalf("expected ResumeMsg, got %T", msg)
	}
	if resume.SessionID != "sess-reentry" {
		t.Fatalf("session ID = %q, want %q", resume.SessionID, "sess-reentry")
	}
}

// TestModel_StatusGlyph_BlinkOffHidesDot verifies that the in-progress
// indicator on an active step disappears during the off-half of the blink
// cycle — rather than being recolored.
func TestModel_StatusGlyph_BlinkOffHidesDot(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true

	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	step := &StepNode{
		ID:     "running-step",
		Type:   NodeShell,
		Status: StatusInProgress,
		Parent: root,
	}
	root.Children = []*StepNode{step}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}

	// On-phase (sin(0) = 0, which BlinkOn treats as on): a "●" must be present.
	m.pulsePhase = 0
	if on := m.statusGlyph(step); !strings.Contains(on, "●") {
		t.Errorf("on-phase glyph should render '●', got %q", on)
	}
	// Off-phase (sin(3π/2) = -1): the "●" must NOT appear — rendered invisible.
	m.pulsePhase = 1.5 * math.Pi
	off := m.statusGlyph(step)
	if strings.Contains(off, "●") {
		t.Errorf("off-phase glyph should hide '●', got %q", off)
	}
	// Width must still be preserved so the step-list columns don't jump.
	if lipgloss.Width(off) != lipgloss.Width("●") {
		t.Errorf("off-phase width = %d, want %d (preserve column alignment)",
			lipgloss.Width(off), lipgloss.Width("●"))
	}
}

func TestModel_StatusGlyph_UIInProgressDoesNotBlink(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true

	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	step := &StepNode{
		ID:     "pick-scope",
		Type:   NodeUI,
		Status: StatusInProgress,
		Parent: root,
	}
	root.Children = []*StepNode{step}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}

	m.pulsePhase = 0
	on := m.statusGlyph(step)
	m.pulsePhase = 1.5 * math.Pi
	off := m.statusGlyph(step)

	if !strings.Contains(on, "●") || !strings.Contains(off, "●") {
		t.Fatalf("ui in-progress glyph should stay visible, got on=%q off=%q", on, off)
	}
	if on != off {
		t.Fatalf("ui in-progress glyph should not blink, got on=%q off=%q", on, off)
	}
}

func TestFindFailedLeaf(t *testing.T) {
	t.Run("finds failed shell step", func(t *testing.T) {
		root := &StepNode{ID: "root", Type: NodeRoot}
		shell := &StepNode{ID: "build", Type: NodeShell, Status: StatusFailed, Parent: root}
		root.Children = []*StepNode{shell}

		found := findFailedLeaf(root)
		if found != shell {
			t.Fatalf("expected %v, got %v", shell, found)
		}
	})

	t.Run("returns nil when no failed step", func(t *testing.T) {
		root := &StepNode{ID: "root", Type: NodeRoot}
		shell := &StepNode{ID: "build", Type: NodeShell, Status: StatusSuccess, Parent: root}
		root.Children = []*StepNode{shell}

		found := findFailedLeaf(root)
		if found != nil {
			t.Fatalf("expected nil, got %v", found)
		}
	})

	t.Run("finds deepest failed step in nested tree", func(t *testing.T) {
		root := &StepNode{ID: "root", Type: NodeRoot, Status: StatusFailed}
		loop := &StepNode{ID: "loop", Type: NodeLoop, Status: StatusFailed, Parent: root}
		iter := &StepNode{ID: "loop", Type: NodeIteration, Status: StatusFailed, Parent: loop}
		shell := &StepNode{ID: "step", Type: NodeShell, Status: StatusFailed, Parent: iter}
		iter.Children = []*StepNode{shell}
		loop.Children = []*StepNode{iter}
		root.Children = []*StepNode{loop}

		found := findFailedLeaf(root)
		if found != shell {
			t.Fatalf("expected deepest failed shell %v, got %v", shell, found)
		}
	})
}

// ---- Scroll sync tests ----

func makeRanges(nodes []*StepNode, lineSize int) []stepLineRange {
	ranges := make([]stepLineRange, len(nodes))
	for i, n := range nodes {
		ranges[i] = stepLineRange{node: n, startLine: i * lineSize, endLine: (i + 1) * lineSize}
	}
	return ranges
}

func TestScrollSync_SyncLogToSelection_SetsOffset(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	steps := []*StepNode{
		{ID: "a", Type: NodeShell, Status: StatusSuccess, Parent: root},
		{ID: "b", Type: NodeShell, Status: StatusSuccess, Parent: root},
		{ID: "c", Type: NodeShell, Status: StatusSuccess, Parent: root},
	}
	root.Children = steps
	tree := &Tree{Root: root}
	m := newTestModel(tree, FromList)
	m.cursor = 1
	// Set up stepRanges with 10 lines per step.
	m.stepRanges = makeRanges(steps, 10)

	m.syncLogToSelection()

	// logOffset should jump to start of step "b" (index 1 → startLine=10)
	if m.logOffset != 10 {
		t.Fatalf("logOffset = %d, want 10", m.logOffset)
	}
	if m.logAnchor.stepKey != steps[1].NodeKey() {
		t.Fatalf("logAnchor.stepKey = %q, want %q", m.logAnchor.stepKey, steps[1].NodeKey())
	}
}

func TestScrollSync_SyncSelectionToLog_PicksLatestInViewport(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	steps := []*StepNode{
		{ID: "a", Type: NodeShell, Status: StatusSuccess, Parent: root},
		{ID: "b", Type: NodeShell, Status: StatusSuccess, Parent: root},
		{ID: "c", Type: NodeShell, Status: StatusSuccess, Parent: root},
	}
	root.Children = steps
	tree := &Tree{Root: root}
	m := newTestModel(tree, FromList)
	m.termHeight = 40 // bodyHeight ≈ 30
	// logOffset = 5, bodyH ≈ 30 → viewport [5, 35)
	// Step a: [0,10) — overlaps [5,35), startLine=0
	// Step b: [10,20) — overlaps [5,35), startLine=10
	// Step c: [20,30) — overlaps [5,35), startLine=20
	// Winner = c (latest startLine)
	m.stepRanges = makeRanges(steps, 10)
	m.logOffset = 5

	m.syncSelectionToLog()

	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (step c has latest startLine in viewport)", m.cursor)
	}
}

func TestScrollSync_NestedWinner_MapsToAncestor(t *testing.T) {
	// Build a tree: root → loop → iter → child
	root := &StepNode{ID: "root", Type: NodeRoot}
	loop := &StepNode{ID: "loop", Type: NodeLoop, Status: StatusInProgress, Parent: root}
	iter := &StepNode{ID: "loop", Type: NodeIteration, Status: StatusInProgress, Parent: loop}
	child := &StepNode{ID: "child-step", Type: NodeShell, Status: StatusInProgress, Parent: iter}
	iter.Children = []*StepNode{child}
	loop.Children = []*StepNode{iter}
	root.Children = []*StepNode{loop}
	tree := &Tree{Root: root}

	m := newTestModel(tree, FromList)
	m.termHeight = 40

	// stepRanges has entries for loop, iter, and child (as buildLogLines would produce).
	m.stepRanges = []stepLineRange{
		{node: loop, startLine: 0, endLine: 30},
		{node: iter, startLine: 1, endLine: 25},
		{node: child, startLine: 2, endLine: 20},
	}
	m.logOffset = 5

	m.syncSelectionToLog()

	// All three ranges overlap [5, 35). Winner is loop (startLine=0 < iter/child start lines).
	// Wait, actually syncSelectionToLog picks the LATEST startLine in viewport.
	// So winner should be child (startLine=2 > iter startLine=1 > loop startLine=0).
	// ancestor-at-current-level(child): child.Parent=iter, iter.Parent=loop, loop.Parent=root
	// root == m.currentContainer() → return loop
	// So cursor should be 0 (loop at index 0 in root.Children).
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (loop is the top-level ancestor of child)", m.cursor)
	}
}

func TestScrollSync_BuildLogLinesChildViewport_SelectsChildAncestor(t *testing.T) {
	root := &StepNode{ID: "root", Type: NodeRoot}
	loop := &StepNode{ID: "loop", Type: NodeLoop, Status: StatusInProgress, Parent: root}
	iter := &StepNode{ID: "loop", Type: NodeIteration, Status: StatusInProgress, Parent: loop, IterationIndex: 0}
	child := &StepNode{
		ID:            "child-step",
		Type:          NodeShell,
		Status:        StatusSuccess,
		Parent:        iter,
		StaticCommand: "echo hi",
		ExitCode:      intPtr(0),
	}
	iter.Children = []*StepNode{child}
	loop.Children = []*StepNode{iter}
	root.Children = []*StepNode{loop}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.rebuildRanges()
	if len(m.stepRanges) != 3 {
		t.Fatalf("expected 3 ranges, got %d", len(m.stepRanges))
	}

	childRange := m.stepRanges[2]
	m.logOffset = childRange.startLine
	m.syncSelectionToLog()

	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 (loop is current-level ancestor of child)", m.cursor)
	}
	if m.logAnchor.stepKey != child.NodeKey() {
		t.Fatalf("logAnchor.stepKey = %q, want %q", m.logAnchor.stepKey, child.NodeKey())
	}
}

func TestHandleWindowSize_ReanchorsAcrossEquivalentTreeRebuild(t *testing.T) {
	build := func() (*Tree, []*StepNode) {
		root := &StepNode{ID: "root", Type: NodeRoot, Status: StatusInProgress}
		step0 := &StepNode{
			ID:            "setup",
			Type:          NodeShell,
			Status:        StatusSuccess,
			Parent:        root,
			StaticCommand: "echo " + strings.Repeat("very-long-command ", 12),
			Stdout:        generateLargeOutput(40),
			ExitCode:      intPtr(0),
		}
		step1 := &StepNode{
			ID:            "deploy",
			Type:          NodeShell,
			Status:        StatusSuccess,
			Parent:        root,
			StaticCommand: "echo done",
			ExitCode:      intPtr(0),
		}
		root.Children = []*StepNode{step0, step1}
		return &Tree{Root: root}, root.Children
	}

	tree1, steps1 := build()
	m := newTestModel(tree1, FromList)
	m.termWidth = 160
	m.termHeight = 24
	m.cursor = 1
	m.rebuildRanges()
	m.syncLogToSelection()

	if m.logAnchor.stepKey != steps1[1].NodeKey() {
		t.Fatalf("logAnchor.stepKey = %q, want %q before rebuild", m.logAnchor.stepKey, steps1[1].NodeKey())
	}

	tree2, steps2 := build()
	m.tree = tree2
	m.path = []*StepNode{tree2.Root}
	m.cursor = 1

	m.handleWindowSize(tea.WindowSizeMsg{Width: 80, Height: 24})

	expected := -1
	for _, r := range m.stepRanges {
		if r.node.NodeKey() == steps2[1].NodeKey() {
			expected = min(r.startLine, m.maxLogOffset())
			break
		}
	}
	if expected < 0 {
		t.Fatal("expected to find rebuilt range for selected step")
	}
	if m.logOffset != expected {
		t.Fatalf("logOffset = %d, want %d after re-anchoring rebuilt tree", m.logOffset, expected)
	}
}

// ---- Deferred alt-screen tests ----

func TestLiveRun_ViewEmptyBeforeAltScreen(t *testing.T) {
	m := newTestModel(simpleTree(), FromLiveRun)
	// FromLiveRun starts with altScreen=false
	if m.altScreen {
		t.Fatal("expected altScreen=false for FromLiveRun")
	}
	if got := m.View(); got != "" {
		t.Fatalf("expected empty view before alt-screen, got %q", got)
	}
}

func TestNonLiveRun_AltScreenImmediate(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	if !m.altScreen {
		t.Fatal("expected altScreen=true for FromList")
	}
	if got := m.View(); got == "" {
		t.Fatal("expected non-empty view for FromList")
	}
}

func TestDeferredAltScreen_EntersOnTimer(t *testing.T) {
	m := newTestModel(simpleTree(), FromLiveRun)
	m.termWidth = 120
	m.termHeight = 40

	updated, cmd := m.Update(deferredAltScreenMsg{})
	m = updated.(*Model)

	if !m.altScreen {
		t.Fatal("expected altScreen=true after deferred timer")
	}
	// cmd should include EnterAltScreen
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
}

func TestDeferredAltScreen_SuppressedBySuspend(t *testing.T) {
	m := newTestModel(simpleTree(), FromLiveRun)

	// SuspendedMsg arrives before the timer
	updated, _ := m.Update(liverun.SuspendedMsg{})
	m = updated.(*Model)

	if !m.suppressAltScreen {
		t.Fatal("expected suppressAltScreen=true after SuspendedMsg")
	}

	// Timer fires — should be suppressed
	updated, cmd := m.Update(deferredAltScreenMsg{})
	m = updated.(*Model)

	if m.altScreen {
		t.Fatal("expected altScreen to remain false after suppressed timer")
	}
	if cmd != nil {
		t.Fatal("expected nil cmd when timer is suppressed")
	}
}

func TestShowTUIMsg_EntersAltScreen(t *testing.T) {
	m := newTestModel(simpleTree(), FromLiveRun)
	m.suppressAltScreen = true // was previously suppressed

	updated, cmd := m.Update(liverun.ShowTUIMsg{})
	m = updated.(*Model)

	if !m.altScreen {
		t.Fatal("expected altScreen=true after ShowTUIMsg")
	}
	if m.suppressAltScreen {
		t.Fatal("expected suppressAltScreen cleared by ShowTUIMsg")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd for EnterAltScreen")
	}
}

func TestShowTUIMsg_NoOpWhenAlreadyInAltScreen(t *testing.T) {
	m := newTestModel(simpleTree(), FromList) // altScreen=true already

	_, cmd := m.Update(liverun.ShowTUIMsg{})

	if cmd != nil {
		t.Fatal("expected nil cmd when already in alt-screen")
	}
}

// TestSuspendedMsg_PopsDrillInWhenActiveOutside reproduces the disorientation
// bug where a user drilled into a sub-workflow stayed pinned to it after the
// sub-workflow completed and the parent workflow advanced into an interactive
// step elsewhere. On SuspendedMsg, when the active step lives outside the
// drilled container, the path pops back to root and autoFollow re-enables, so
// the resumed TUI shows the running step.
func TestSuspendedMsg_PopsDrillInWhenActiveOutside(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	stepA := &StepNode{ID: "stepA", Type: NodeShell, Status: StatusSuccess, Parent: root}
	subwf := &StepNode{ID: "subwf", Type: NodeSubWorkflow, Status: StatusSuccess, Parent: root, SubLoaded: true}
	stepB := &StepNode{ID: "stepB", Type: NodeShell, Status: StatusSuccess, Parent: subwf}
	subwf.Children = []*StepNode{stepB}
	stepC := &StepNode{ID: "stepC", Type: NodeInteractiveAgent, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{stepA, subwf, stepC}

	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true
	m.path = []*StepNode{root, subwf}
	m.cursor = 0
	m.autoFollow = false
	m.activeStepPrefix = "[stepC]"

	m.Update(liverun.SuspendedMsg{})

	if len(m.path) != 1 {
		t.Fatalf("path len = %d, want 1 (popped back to root)", len(m.path))
	}
	if !m.autoFollow {
		t.Fatal("autoFollow should be re-enabled on suspend")
	}
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2 (stepC index)", m.cursor)
	}
}

// TestSuspendedMsg_KeepsDrillInWhenActiveInside verifies the looser policy:
// if the active step lives inside the drilled container, the drill-in is
// preserved (autoFollow re-enables and the cursor follows within the
// container).
func TestSuspendedMsg_KeepsDrillInWhenActiveInside(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	stepA := &StepNode{ID: "stepA", Type: NodeShell, Status: StatusSuccess, Parent: root}
	subwf := &StepNode{ID: "subwf", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root, SubLoaded: true}
	stepB1 := &StepNode{ID: "stepB1", Type: NodeShell, Status: StatusSuccess, Parent: subwf}
	stepB2 := &StepNode{ID: "stepB2", Type: NodeInteractiveAgent, Status: StatusInProgress, Parent: subwf}
	subwf.Children = []*StepNode{stepB1, stepB2}
	root.Children = []*StepNode{stepA, subwf}

	m := newTestModel(&Tree{Root: root}, FromLiveRun)
	m.running = true
	m.path = []*StepNode{root, subwf}
	m.cursor = 0
	m.autoFollow = false
	m.activeStepPrefix = "[subwf, stepB2]"

	m.Update(liverun.SuspendedMsg{})

	if len(m.path) != 2 {
		t.Fatalf("path len = %d, want 2 (drill-in preserved when active is inside)", len(m.path))
	}
	if m.path[1] != subwf {
		t.Fatal("path[1] should still be subwf")
	}
	if !m.autoFollow {
		t.Fatal("autoFollow should be re-enabled on suspend")
	}
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (stepB2 index inside subwf)", m.cursor)
	}
}

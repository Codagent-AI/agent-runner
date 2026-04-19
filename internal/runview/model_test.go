package runview

import (
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

func newTestModel(tree *Tree, entered Entered) *Model {
	return &Model{
		tree:       tree,
		entered:    entered,
		path:       []*StepNode{tree.Root},
		loadedFull: make(map[*StepNode]bool),
		termWidth:  120,
		termHeight: 40,
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

// j and k scroll the detail pane; they must NOT move the cursor.
func TestModel_JK_ScrollDetailPane(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.cursor = 1
	initial := m.detailOffset

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.cursor != 1 {
		t.Fatalf("j moved cursor to %d, want 1", m.cursor)
	}
	if m.detailOffset <= initial {
		t.Fatal("j should increase detailOffset")
	}

	scrolled := m.detailOffset
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if m.cursor != 1 {
		t.Fatalf("k moved cursor to %d, want 1", m.cursor)
	}
	if m.detailOffset >= scrolled {
		t.Fatal("k should decrease detailOffset")
	}
}

// PgUp and PgDown are no longer bound; they must be no-ops.
func TestModel_PgUpPgDown_NoOp(t *testing.T) {
	m := newTestModel(simpleTree(), FromList)
	m.detailOffset = 0

	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.detailOffset != 0 {
		t.Fatal("PgDown should not change detailOffset (unbound)")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.detailOffset != 0 {
		t.Fatal("PgUp should not change detailOffset (unbound)")
	}
}

// t re-engages tail-follow; End and G are no longer bound.
func TestModel_TKey_ReengagesTailFollow(t *testing.T) {
	m := newLiveModelWithFlags()
	m.tailFollow = false

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})

	if !m.tailFollow {
		t.Error("t key should re-engage tailFollow")
	}
	if m.detailOffset <= 0 {
		t.Errorf("detailOffset = %d, expected large value after t", m.detailOffset)
	}
}

func TestModel_EndKey_NoOp(t *testing.T) {
	m := newLiveModelWithFlags()
	m.tailFollow = false

	m.Update(tea.KeyMsg{Type: tea.KeyEnd})

	if m.tailFollow {
		t.Error("End key should not re-engage tailFollow (unbound)")
	}
}

func TestModel_GKey_NoOp(t *testing.T) {
	m := newLiveModelWithFlags()
	m.tailFollow = false

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})

	if m.tailFollow {
		t.Error("G key should not re-engage tailFollow (unbound)")
	}
}

func TestModel_K_ClearsTailFollow(t *testing.T) {
	m := newLiveModelWithFlags()
	m.tailFollow = true
	m.detailOffset = 50

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})

	if m.tailFollow {
		t.Error("k should clear tailFollow")
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

// r is ignored on completed runs.
func TestModel_R_IgnoredOnCompletedRun(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusSuccess
	m := newTestModel(tree, FromList)
	m.sessionDir = "/runs/my-run-id"

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil {
		t.Fatalf("r on completed run should be no-op, got cmd %v", cmd)
	}
}

// r is ignored on failed runs.
func TestModel_R_IgnoredOnFailedRun(t *testing.T) {
	tree := simpleTree()
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

func TestModel_HelpBar_HidesRBinding_WhenCompleted(t *testing.T) {
	tree := simpleTree()
	tree.Root.Status = StatusSuccess
	m := newTestModel(tree, FromList)

	help := m.renderHelpBar()
	if containsString(help, "r resume") {
		t.Errorf("help bar should not show 'r resume' for completed run: %q", help)
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
// Without this, q-after-completion emits ExitMsg into the void and the TUI
// appears frozen.
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

	if m.loadedFull[tree.Root.Children[0]] {
		t.Fatal("should not be loaded full initially")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if !m.loadedFull[tree.Root.Children[0]] {
		t.Fatal("g should mark step as loaded full")
	}
}

// TestModel_PageUpDown is superseded by TestModel_PgUpPgDown_NoOp above.

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
		loadedFull: make(map[*StepNode]bool),
		termWidth:  120,
		termHeight: 40,
		running:    true,
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

// ---- autoFollow / tailFollow / navigateToNode / lockout tests ----

func newLiveModelWithFlags() *Model {
	tree := liveTree()
	return &Model{
		tree:       tree,
		entered:    FromLiveRun,
		path:       []*StepNode{tree.Root},
		loadedFull: make(map[*StepNode]bool),
		termWidth:  120,
		termHeight: 40,
		running:    true,
		autoFollow: true,
		tailFollow: true,
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
	if !m.tailFollow {
		t.Error("tailFollow should be true in FromLiveRun")
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
	if m.tailFollow {
		t.Error("tailFollow should be false in FromList")
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

	// Only up/down cursor keys clear autoFollow; j/k scroll the detail pane
	// and do not affect the cursor or autoFollow.
	for _, key := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyUp},
		tea.KeyMsg{Type: tea.KeyDown},
	} {
		m.autoFollow = true
		m.Update(key)
		if m.autoFollow {
			t.Errorf("key %v should clear autoFollow", key)
		}
	}
}

func TestModel_JK_DoNotClearAutoFollow(t *testing.T) {
	m := newLiveModelWithFlags()
	m.autoFollow = true

	for _, key := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")},
	} {
		m.autoFollow = true
		m.Update(key)
		if !m.autoFollow {
			t.Errorf("key %v should not clear autoFollow (it scrolls detail pane, not cursor)", key)
		}
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

func TestModel_TailFollow_PinsOnOutputChunk(t *testing.T) {
	tree := liveTree()
	m := newLiveModelWithFlags()
	m.tree = tree
	m.path = []*StepNode{tree.Root}
	m.cursor = 0 // shell step "build" is selected

	// Send output chunk for the selected step
	m.Update(liverun.OutputChunkMsg{StepPrefix: "[build]", Stream: "stdout", Bytes: []byte("line\n")})

	// detailOffset should be pinned to a large value
	if m.detailOffset <= 0 {
		t.Errorf("detailOffset = %d, expected large value for tail-pin", m.detailOffset)
	}
}

func TestModel_TailFollow_IgnoresOtherStep(t *testing.T) {
	tree := simpleTree()
	m := newLiveModelWithFlags()
	m.tree = tree
	m.path = []*StepNode{tree.Root}
	m.cursor = 0 // "build" step selected

	// Send output chunk for a DIFFERENT step ("implement", index 1)
	m.Update(liverun.OutputChunkMsg{StepPrefix: "[implement]", Stream: "stdout", Bytes: []byte("line\n")})

	// detailOffset should NOT be changed (still 0)
	if m.detailOffset != 0 {
		t.Errorf("detailOffset = %d, expected 0 (unselected step should not pin tail)", m.detailOffset)
	}
}

// TestModel_PgUp_ClearsTailFollow, TestModel_EndKey_ReengagesTailFollow, and
// TestModel_GKey_ReengagesTailFollow are superseded by TestModel_K_ClearsTailFollow
// and TestModel_TKey_ReengagesTailFollow / TestModel_EndKey_NoOp / TestModel_GKey_NoOp above.

func TestModel_MouseWheelUp_ClearsTailFollow(t *testing.T) {
	m := newLiveModelWithFlags()
	m.tailFollow = true

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})

	if m.tailFollow {
		t.Error("mouse wheel up should clear tailFollow")
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
	// A live run can leave autoFollow stuck on an intermediate step whose
	// audit prefix resolves to a tree node while later steps (e.g. loop
	// iterations) arrive before their tree nodes are created by audit
	// replay. On clean success the cursor should land on the final
	// top-level step so the user sees the workflow's "end state" without
	// having to navigate manually.
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

func TestModel_HelpBar_ShowsLiveHint_WhenRunningAndAutoFollowOff(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true
	m.autoFollow = false

	help := m.renderHelpBar()
	if !containsString(help, "l live") {
		t.Errorf("help bar missing 'l live' hint: %q", help)
	}
}

func TestModel_HelpBar_HidesLiveHint_WhenAutoFollowOn(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true
	m.autoFollow = true

	help := m.renderHelpBar()
	if containsString(help, "l live") {
		t.Errorf("help bar should not show 'l live' when autoFollow is on: %q", help)
	}
}

func TestModel_HelpBar_ShowsTailHint_WhenTailFollowOff(t *testing.T) {
	m := newLiveModelWithFlags()
	m.tailFollow = false

	help := m.renderHelpBar()
	if !containsString(help, "t tail") {
		t.Errorf("help bar missing 't tail' hint: %q", help)
	}
}

func TestModel_HelpBar_HidesTailHint_WhenTailFollowOn(t *testing.T) {
	m := newLiveModelWithFlags()
	m.tailFollow = true

	help := m.renderHelpBar()
	if containsString(help, "t tail") {
		t.Errorf("help bar should not show 't tail' when tailFollow is on: %q", help)
	}
}

func TestModel_Legend_ContainsLiveNavKeys(t *testing.T) {
	m := newLiveModelWithFlags()
	m.showLegend = true

	legend := m.View()
	for _, want := range []string{"l", "t", "Live Navigation"} {
		if !containsString(legend, want) {
			t.Errorf("legend missing %q", want)
		}
	}
	for _, absent := range []string{"End", "G  jump"} {
		if containsString(legend, absent) {
			t.Errorf("legend should not mention %q (key removed)", absent)
		}
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

func TestModel_Detail_Live_HidesResumeHint_WhileRunning(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true

	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	agent := &StepNode{
		ID:        "implement",
		Type:      NodeHeadlessAgent,
		Status:    StatusInProgress,
		Parent:    root,
		AgentCLI:  "claude",
		SessionID: "sess-1",
	}
	root.Children = []*StepNode{agent}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}
	m.cursor = 0

	detail := m.renderDetail(agent)
	if containsString(detail, "resume session") {
		t.Errorf("detail should hide 'resume session' hint while running: %q", detail)
	}
}

func TestModel_Detail_Live_ShowsResumeHint_AfterCompletion(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = false

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

	detail := m.renderDetail(agent)
	if !containsString(detail, "resume session") {
		t.Errorf("detail should show 'resume session' hint after run completes: %q", detail)
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

func TestModel_HeadlessDetail_NoOutput_WhileRunning_ShowsSpinner(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = true

	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	agent := &StepNode{
		ID:       "headless-new-session",
		Type:     NodeHeadlessAgent,
		Status:   StatusInProgress,
		Parent:   root,
		AgentCLI: "claude",
	}
	root.Children = []*StepNode{agent}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}
	m.cursor = 0

	detail := m.renderDetail(agent)
	if containsString(detail, "stdout") {
		t.Errorf("detail should not show 'stdout' label for headless agent: %q", detail)
	}
	if containsString(detail, "stderr") {
		t.Errorf("detail should not show 'stderr' label for headless agent: %q", detail)
	}
	if !containsString(detail, "agent") {
		t.Errorf("detail should show 'agent' label: %q", detail)
	}
	// The spinner is drawn as a 3-row × 2-dot grid using "●" for lit
	// cells and spaces for empty cells — one character per dot so the
	// animation is visible at normal font size.
	if !strings.Contains(detail, "●") {
		t.Errorf("detail should contain a spinner dot glyph: %q", detail)
	}
	if strings.Contains(detail, "\x1b#3") || strings.Contains(detail, "\x1b#4") {
		t.Errorf("detail should not emit DECDHL escapes: %q", detail)
	}
	agentIdx := strings.Index(detail, "agent:")
	spinnerIdx := strings.Index(detail, "●")
	if agentIdx < 0 || spinnerIdx < 0 || spinnerIdx < agentIdx {
		t.Fatalf("expected spinner to appear after 'agent:' label in: %q", detail)
	}
	if !strings.Contains(detail[agentIdx:spinnerIdx], "\n") {
		t.Errorf("expected a newline between 'agent:' label and spinner: %q", detail)
	}
}

func TestModel_HeadlessDetail_StdoutOnly_NoStreamLabel_NoGutter(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = false

	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	agent := &StepNode{
		ID:       "headless-new-session",
		Type:     NodeHeadlessAgent,
		Status:   StatusSuccess,
		Parent:   root,
		AgentCLI: "claude",
		Stdout:   "Octopuses have three hearts.\n",
	}
	root.Children = []*StepNode{agent}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}
	m.cursor = 0
	m.detailWidth = 60

	detail := m.renderDetail(agent)
	if containsString(detail, "stdout") {
		t.Errorf("detail should not label the single output stream as 'stdout': %q", detail)
	}
	if containsString(detail, "stderr") {
		t.Errorf("detail should not show 'stderr' label: %q", detail)
	}
	if !containsString(detail, "agent") {
		t.Errorf("detail should show 'agent' label: %q", detail)
	}
	if containsString(detail, "| Octopuses") {
		t.Errorf("detail should not prefix agent output with '| ' gutter: %q", detail)
	}
	if !containsString(detail, "Octopuses have three hearts.") {
		t.Errorf("detail missing expected output text: %q", detail)
	}
}

func TestModel_HeadlessDetail_StdoutAndStderr_LabelsBoth(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = false

	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	agent := &StepNode{
		ID:       "headless",
		Type:     NodeHeadlessAgent,
		Status:   StatusSuccess,
		Parent:   root,
		AgentCLI: "claude",
		Stdout:   "OUT-TEXT\n",
		Stderr:   "ERR-TEXT\n",
	}
	root.Children = []*StepNode{agent}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}
	m.cursor = 0
	m.detailWidth = 60

	detail := m.renderDetail(agent)
	if !containsString(detail, "OUT-TEXT") || !containsString(detail, "ERR-TEXT") {
		t.Fatalf("detail missing output or error: %q", detail)
	}
	// When both streams are present, a disambiguating label is required.
	if !containsString(detail, "stdout") || !containsString(detail, "stderr") {
		t.Errorf("detail should label both streams when both present: %q", detail)
	}
}

func TestModel_HeadlessDetail_LongLine_Wraps(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = false

	long := strings.Repeat("abcdef ", 30) // ~210 visual columns
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	agent := &StepNode{
		ID:       "headless",
		Type:     NodeHeadlessAgent,
		Status:   StatusSuccess,
		Parent:   root,
		AgentCLI: "claude",
		Stdout:   long + "\n",
	}
	root.Children = []*StepNode{agent}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}
	m.cursor = 0
	m.detailWidth = 40

	detail := m.renderDetail(agent)
	// Truncation-ellipsis indicates the old behavior; wrapping must not
	// leave one overflowing line with a trailing ellipsis.
	if strings.Contains(detail, "…") {
		t.Errorf("long output should be wrapped, not truncated with ellipsis: %q", detail)
	}
	// Wrapping should split the text across multiple visual lines.
	outLines := strings.Count(detail, "abcdef")
	if outLines < 2 {
		t.Errorf("expected multiple wrapped segments containing 'abcdef', got %d: %q", outLines, detail)
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
	if m.tailFollow {
		t.Error("tailFollow should be false in NewForReentry model")
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
// cycle — rather than being recolored. Recoloring has proven fragile across
// terminal themes (lipgloss's background detection can misresolve adaptive
// whites to near-black inside bubbletea's alt-screen), so the off-phase
// simply hides the glyph by emitting width-matched spaces.
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

func TestModel_ShellDetail_LongStdout_Wraps_NoGutter(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = false

	long := strings.Repeat("abcdef ", 30) // ~210 visual columns
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	shell := &StepNode{
		ID:            "build",
		Type:          NodeShell,
		Status:        StatusSuccess,
		Parent:        root,
		StaticCommand: "echo hi",
		Stdout:        long + "\n",
		ExitCode:      intPtr(0),
	}
	root.Children = []*StepNode{shell}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}
	m.cursor = 0
	m.detailWidth = 40

	detail := m.renderDetail(shell)
	if strings.Contains(detail, "| ") {
		t.Errorf("shell stdout should not use '| ' gutter: %q", detail)
	}
	if strings.Contains(detail, "…") {
		t.Errorf("long shell stdout should wrap, not truncate: %q", detail)
	}
	if strings.Count(detail, "abcdef") < 2 {
		t.Errorf("expected wrapped shell stdout segments containing 'abcdef', got: %q", detail)
	}
}

func TestModel_ShellDetail_LongStderr_Wraps_NoGutter(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = false

	long := strings.Repeat("abcdef ", 30)
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusFailed}
	shell := &StepNode{
		ID:            "check",
		Type:          NodeShell,
		Status:        StatusFailed,
		Parent:        root,
		StaticCommand: "agent-validator detect",
		Stderr:        long + "\n",
		ExitCode:      intPtr(1),
	}
	root.Children = []*StepNode{shell}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}
	m.cursor = 0
	m.detailWidth = 40

	detail := m.renderDetail(shell)
	if strings.Contains(detail, "| ") {
		t.Errorf("shell stderr should not use '| ' gutter: %q", detail)
	}
	if strings.Contains(detail, "…") {
		t.Errorf("long shell stderr should wrap, not truncate: %q", detail)
	}
	if strings.Count(detail, "abcdef") < 2 {
		t.Errorf("expected wrapped shell stderr segments containing 'abcdef', got: %q", detail)
	}
}

func TestModel_ShellDetail_LongCommand_Wraps(t *testing.T) {
	m := newLiveModelWithFlags()
	m.running = false

	longCmd := "agent-validator detect; status=$?; if [ $status -eq 2 ]; then echo needs-validation; else echo ok; fi && exit $status"
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusSuccess}
	shell := &StepNode{
		ID:                  "check-clean",
		Type:                NodeShell,
		Status:              StatusSuccess,
		Parent:              root,
		InterpolatedCommand: longCmd,
		ExitCode:            intPtr(0),
	}
	root.Children = []*StepNode{shell}
	m.tree = &Tree{Root: root}
	m.path = []*StepNode{root}
	m.cursor = 0
	m.detailWidth = 40

	detail := m.renderDetail(shell)
	// Every emitted line must fit within detailWidth so the downstream
	// fitDetailLine does not need to truncate with an ellipsis.
	for _, line := range strings.Split(detail, "\n") {
		plain := tuistyle.Sanitize(line)
		if runewidth.StringWidth(plain) > m.detailWidth {
			t.Errorf("shell command line exceeds detailWidth=%d: width=%d line=%q",
				m.detailWidth, runewidth.StringWidth(plain), plain)
		}
	}
	if !strings.Contains(detail, "agent-validator") {
		t.Errorf("detail missing expected command text: %q", detail)
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

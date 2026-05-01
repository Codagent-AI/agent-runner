package runview

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/tuistyle"
)

// TestFitDetailLine_PreservesANSIWhenFitting is a regression against
// truncating detail-pane lines based on raw byte width (which includes ANSI
// escape bytes) rather than visible width. Any line that fits visibly SHALL
// be returned verbatim so styling renders intact.
func TestFitDetailLine_PreservesANSIWhenFitting(t *testing.T) {
	styled := tuistyle.DimStyle.Render("outcome: ") + tuistyle.NormalStyle.Render("success")
	// Visible width is len("outcome: success") == 16. Pick a width that is
	// comfortably above visible width but below raw byte length (ANSI adds
	// ~20+ bytes).
	width := 40
	if lipgloss.Width(styled) > width {
		t.Fatalf("test setup: visible width %d > target %d", lipgloss.Width(styled), width)
	}
	got := fitDetailLine(styled, width)
	if got != styled {
		t.Errorf("fitDetailLine corrupted a line that already fits:\n got=%q\nwant=%q", got, styled)
	}
	// Visible text must still contain the full value.
	if !strings.Contains(tuistyle.Sanitize(got), "success") {
		t.Errorf("fit line missing \"success\": %q", tuistyle.Sanitize(got))
	}
}

// TestFitDetailLine_TruncatesWithoutManglingEscape verifies that when a line
// genuinely overflows, the truncation does not split an ANSI escape
// sequence mid-stream (which would bleed styles into later output).
func TestFitDetailLine_TruncatesWithoutManglingEscape(t *testing.T) {
	styled := tuistyle.DimStyle.Render("| ") + tuistyle.NormalStyle.Render("Before doing anything else, ask the user to describe the change")
	width := 20
	got := fitDetailLine(styled, width)
	if lipgloss.Width(got) > width {
		t.Fatalf("visible width %d exceeds %d: %q", lipgloss.Width(got), width, got)
	}
	// Must not contain a raw escape byte — stripping ANSI before truncating
	// guarantees the output is plain text.
	if strings.Contains(got, "\x1b") {
		t.Errorf("truncated output still contains ANSI escape bytes: %q", got)
	}
	// Ellipsis appended on overflow.
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected trailing ellipsis, got %q", got)
	}
}

// TestFitDetailLine_ZeroWidth handles the degenerate case so renderTwoColumn
// can collapse the detail pane without crashing when the terminal is very
// narrow.
func TestFitDetailLine_ZeroWidth(t *testing.T) {
	if got := fitDetailLine("anything", 0); got != "" {
		t.Errorf("fitDetailLine at width 0 = %q, want empty", got)
	}
	if got := fitDetailLine("anything", -5); got != "" {
		t.Errorf("fitDetailLine at negative width = %q, want empty", got)
	}
}

// TestRenderTwoColumn_PromptWraps verifies long prompt lines are word-wrapped
// to the detail-pane width instead of truncated, and that no `| ` quote prefix
// is emitted.
func TestRenderTwoColumn_PromptWraps(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	longLine := "A substantially longer second line that will overflow the column width and require wrapping across several rows."
	agent := &StepNode{
		ID:                 "proposal",
		Type:               NodeInteractiveAgent,
		Status:             StatusInProgress,
		Parent:             root,
		StaticAgent:        "planner",
		StaticCLI:          "claude",
		InterpolatedPrompt: "Short line.\n" + longLine + "\n\nAnother short one.",
	}
	root.Children = []*StepNode{agent}
	tree := &Tree{Root: root}
	m := newTestModel(tree, FromList)
	m.cursor = 0
	m.termWidth = 70

	out := m.View()
	plain := tuistyle.Sanitize(out)

	if strings.Contains(plain, "| ") {
		t.Errorf("prompt should not use `| ` quote notation, got:\n%s", plain)
	}
	// The long line must be present uncut (all words appear) because wrapping
	// preserves the content where truncation would drop the tail.
	for _, word := range []string{"substantially", "wrapping", "several", "rows."} {
		if !strings.Contains(plain, word) {
			t.Errorf("expected wrapped prompt to contain %q, missing from output:\n%s", word, plain)
		}
	}
}

func TestBuildStepRows_SelectedStepShowsDirectChildrenOnly(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	setup := &StepNode{ID: "setup", Type: NodeShell, Status: StatusSuccess, Parent: root}
	review := &StepNode{ID: "review", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root}
	cleanup := &StepNode{ID: "cleanup", Type: NodeShell, Status: StatusPending, Parent: root}
	root.Children = []*StepNode{setup, review, cleanup}

	gather := &StepNode{ID: "gather", Type: NodeShell, Status: StatusSuccess, Parent: review}
	fanout := &StepNode{
		ID:                  "fanout",
		Type:                NodeLoop,
		Status:              StatusSuccess,
		Parent:              review,
		IterationsCompleted: 1,
		LoopMatches:         []string{"tasks/a.md", "tasks/b.md"},
	}
	review.Children = []*StepNode{gather, fanout}

	iter1 := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusSuccess,
		Parent:         fanout,
		IterationIndex: 0,
		BindingValue:   "tasks/a.md",
	}
	iter2 := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusInProgress,
		Parent:         fanout,
		IterationIndex: 1,
		BindingValue:   "tasks/b.md",
	}
	fanout.Children = []*StepNode{iter1, iter2}

	verify := &StepNode{ID: "verify", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: iter2}
	iter2.Children = []*StepNode{verify}

	summarize := &StepNode{ID: "summarize", Type: NodeHeadlessAgent, Status: StatusInProgress, Parent: verify}
	verify.Children = []*StepNode{summarize}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 1

	rows := m.buildStepRows(root.Children)
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows including direct-child expansion, got %d", len(rows))
	}

	plain := make([]string, len(rows))
	for i, row := range rows {
		plain[i] = stripANSI(row)
	}

	if !strings.Contains(plain[0], "setup") {
		t.Fatalf("row 0 should be the setup sibling, got %q", plain[0])
	}
	if !strings.Contains(plain[1], "review") {
		t.Fatalf("row 1 should be the selected step, got %q", plain[1])
	}
	if !regexp.MustCompile(`^\s{2,}\$ {2}gather`).MatchString(plain[2]) {
		t.Fatalf("row 2 should show the first direct child with positive indent, got %q", plain[2])
	}
	if !regexp.MustCompile(`^\s{2,}↺ {2}fanout \(1/2\)`).MatchString(plain[3]) {
		t.Fatalf("row 3 should show the direct loop child with its glyph and counter, got %q", plain[3])
	}
	if strings.Contains(strings.Join(plain, "\n"), "iter 2") {
		t.Fatalf("expansion should not recurse into iterations, got rows:\n%s", strings.Join(plain, "\n"))
	}
	if strings.Contains(strings.Join(plain, "\n"), "summarize") {
		t.Fatalf("expansion should not recurse into deeper descendants, got rows:\n%s", strings.Join(plain, "\n"))
	}
	if !strings.Contains(plain[4], "cleanup") {
		t.Fatalf("row 4 should be the final top-level sibling, got %q", plain[4])
	}
}

func TestBuildStepRows_SelectedLoopShowsIterationsWithoutBindingValues(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	loop := &StepNode{
		ID:                  "fanout",
		Type:                NodeLoop,
		Status:              StatusInProgress,
		Parent:              root,
		IterationsCompleted: 1,
		LoopMatches:         []string{"tasks/a.md", "tasks/b.md"},
	}
	root.Children = []*StepNode{loop}
	iter1 := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusSuccess,
		Parent:         loop,
		IterationIndex: 0,
		BindingValue:   "tasks/a.md",
	}
	iter2 := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusInProgress,
		Parent:         loop,
		IterationIndex: 1,
		BindingValue:   "tasks/b.md",
	}
	loop.Children = []*StepNode{iter1, iter2}

	m := newTestModel(&Tree{Root: root}, FromList)
	rows := m.buildStepRows(root.Children)
	if len(rows) != 3 {
		t.Fatalf("expected loop row plus 2 iteration expansion rows, got %d", len(rows))
	}

	joined := strings.Join([]string{
		stripANSI(rows[0]),
		stripANSI(rows[1]),
		stripANSI(rows[2]),
	}, "\n")
	if !strings.Contains(joined, "↺") {
		t.Fatalf("loop row should show a loop glyph, got:\n%s", joined)
	}
	if !strings.Contains(joined, "fanout (1/2)") {
		t.Fatalf("loop row should show the iteration counter, got:\n%s", joined)
	}
	if !strings.Contains(joined, "iter 1") || !strings.Contains(joined, "iter 2") {
		t.Fatalf("loop expansion should list each iteration, got:\n%s", joined)
	}
	if strings.Contains(joined, "tasks/a.md") || strings.Contains(joined, "tasks/b.md") {
		t.Fatalf("iteration rows must not show binding values, got:\n%s", joined)
	}
}

// TestBuildStepRows_SelectedContainerWithActiveChildSuppressesOwnIndicator
// verifies that when a selected container step (sub-workflow, loop, iteration)
// has at least one in-progress child in its expansion rows, the parent's own
// "●" indicator is suppressed so only one blinking indicator is rendered.
func TestBuildStepRows_SelectedContainerWithActiveChildSuppressesOwnIndicator(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	review := &StepNode{ID: "review", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{review}
	active := &StepNode{ID: "impl", Type: NodeHeadlessAgent, Status: StatusInProgress, Parent: review}
	review.Children = []*StepNode{active}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 0
	rows := m.buildStepRows(root.Children)
	if len(rows) != 2 {
		t.Fatalf("expected selected sub-workflow row plus one expansion row, got %d", len(rows))
	}

	parent := stripANSI(rows[0])
	child := stripANSI(rows[1])
	if strings.Contains(parent, "●") {
		t.Fatalf("selected in-progress container with active child should hide its own '●' indicator, got parent=%q", parent)
	}
	if !strings.Contains(child, "●") {
		t.Fatalf("in-progress expansion child should show a '●' indicator, got child=%q", child)
	}

	// Column alignment must be preserved: the parent row's visible width must
	// remain stable (as if the indicator were still there).
	withActive := lipgloss.Width(rows[0])
	active.Status = StatusPending
	rowsNoActive := m.buildStepRows(root.Children)
	withoutActive := lipgloss.Width(rowsNoActive[0])
	if withActive != withoutActive {
		t.Fatalf("parent row width should not change when indicator is suppressed: active=%d, pending=%d", withActive, withoutActive)
	}
}

// TestBuildStepRows_SelectedLoopWithActiveIterationSuppressesOwnIndicator
// verifies the same "only one in-progress indicator" rule applies when the
// selected container is a loop whose active child is an iteration.
func TestBuildStepRows_SelectedLoopWithActiveIterationSuppressesOwnIndicator(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	loop := &StepNode{
		ID:                  "fanout",
		Type:                NodeLoop,
		Status:              StatusInProgress,
		Parent:              root,
		IterationsCompleted: 1,
		LoopMatches:         []string{"a.md", "b.md"},
	}
	root.Children = []*StepNode{loop}
	iter1 := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusSuccess,
		Parent:         loop,
		IterationIndex: 0,
	}
	iter2 := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusInProgress,
		Parent:         loop,
		IterationIndex: 1,
	}
	loop.Children = []*StepNode{iter1, iter2}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 0
	rows := m.buildStepRows(root.Children)
	if len(rows) != 3 {
		t.Fatalf("expected loop row plus 2 iteration expansion rows, got %d", len(rows))
	}

	parent := stripANSI(rows[0])
	iter2Row := stripANSI(rows[2])
	if strings.Contains(parent, "●") {
		t.Fatalf("selected in-progress loop with active iteration should hide its own '●', got parent=%q", parent)
	}
	if !strings.Contains(iter2Row, "●") {
		t.Fatalf("active iteration expansion row should show '●', got iter2=%q", iter2Row)
	}
}

// TestBuildStepRows_SelectedContainerWithoutActiveChildKeepsOwnIndicator
// verifies that when a selected container has no in-progress child among its
// expansion rows, its own "●" indicator is rendered normally.
func TestBuildStepRows_SelectedContainerWithoutActiveChildKeepsOwnIndicator(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	loop := &StepNode{
		ID:                  "fanout",
		Type:                NodeLoop,
		Status:              StatusInProgress,
		Parent:              root,
		IterationsCompleted: 1,
		LoopMatches:         []string{"a.md", "b.md"},
	}
	root.Children = []*StepNode{loop}
	// No iteration currently in progress — prior one completed, next not started.
	iter1 := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusSuccess,
		Parent:         loop,
		IterationIndex: 0,
	}
	iter2 := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusPending,
		Parent:         loop,
		IterationIndex: 1,
	}
	loop.Children = []*StepNode{iter1, iter2}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 0
	rows := m.buildStepRows(root.Children)
	if len(rows) < 1 {
		t.Fatalf("expected at least the parent row, got %d", len(rows))
	}
	parent := stripANSI(rows[0])
	if !strings.Contains(parent, "●") {
		t.Fatalf("selected in-progress container with no active child should still show its own '●', got parent=%q", parent)
	}
}

// TestIterationRowRendersTypeGlyph verifies that iteration rows, like other
// step types, carry a type glyph in the step list for visual consistency with
// shell/agent/sub-workflow/loop rows.
func TestIterationRowRendersTypeGlyph(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	loop := &StepNode{
		ID:     "fanout",
		Type:   NodeLoop,
		Status: StatusInProgress,
		Parent: root,
	}
	root.Children = []*StepNode{loop}
	iter := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusInProgress,
		Parent:         loop,
		IterationIndex: 0,
	}
	loop.Children = []*StepNode{iter}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 0
	rows := m.buildStepRows(root.Children)
	if len(rows) != 2 {
		t.Fatalf("expected loop row plus 1 iteration expansion row, got %d", len(rows))
	}
	iterRow := stripANSI(rows[1])
	if !strings.Contains(iterRow, "»") {
		t.Fatalf("iteration row should include a type glyph (»), got %q", iterRow)
	}
}

// TestLegendListsIterationGlyph ensures the legend overlay documents the
// iteration type glyph alongside the other step-type glyphs.
func TestLegendListsIterationGlyph(t *testing.T) {
	m := newTestModel(&Tree{Root: &StepNode{ID: "wf", Type: NodeRoot}}, FromList)
	legend := stripANSI(m.renderLegend())
	if !strings.Contains(legend, "»") {
		t.Fatalf("legend should include iteration type glyph, got:\n%s", legend)
	}
	if !strings.Contains(legend, "iteration") {
		t.Fatalf("legend should label the iteration glyph, got:\n%s", legend)
	}
}

func TestRenderLegend_StatusGlyphsUseRunScreenColors(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := newTestModel(&Tree{Root: &StepNode{ID: "wf", Type: NodeRoot}}, FromList)

	legend := m.renderLegend()
	wants := []string{
		tuistyle.StatusSuccess.Render("●") + "  running",
		tuistyle.StatusInactive.Render("○") + "  pending",
		tuistyle.StatusSuccess.Render("✓") + "  success",
		tuistyle.StatusFailed.Render("✗") + "  failed",
		tuistyle.StatusDone.Render("⇥") + "  skipped",
	}
	for _, want := range wants {
		if !strings.Contains(legend, want) {
			t.Errorf("legend should contain styled status glyph %q, got:\n%q", want, legend)
		}
	}
}

func TestRenderLegend_TypeGlyphsUseRunScreenColors(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := newTestModel(&Tree{Root: &StepNode{ID: "wf", Type: NodeRoot}}, FromList)

	legend := m.renderLegend()
	wants := []string{
		typeGlyph(NodeShell) + "  shell",
		typeGlyph(NodeHeadlessAgent) + "  headless agent",
		typeGlyph(NodeInteractiveAgent) + "  interactive agent",
		typeGlyph(NodeSubWorkflow) + "  sub-workflow",
		typeGlyph(NodeLoop) + "  loop",
		typeGlyph(NodeIteration) + "  iteration",
	}
	for _, want := range wants {
		if !strings.Contains(legend, want) {
			t.Errorf("legend should contain styled type glyph %q, got:\n%q", want, legend)
		}
	}
}

func TestView_HeaderShowsOriginCwdShortened(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projectDir := filepath.Join(t.TempDir(), "projects", "encoded")
	sessionDir := filepath.Join(projectDir, "runs", "wf-2026-04-26T12-00-00Z")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	originCwd := filepath.Join(home, "src", "agent-runner")
	writeMeta(t, projectDir, originCwd)

	m, err := New(sessionDir, projectDir, FromList)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.termWidth = 100
	m.termHeight = 30

	view := stripANSI(m.View())
	if !strings.Contains(view, "~/src/agent-runner") {
		t.Fatalf("run view header should show shortened origin cwd, got:\n%s", view)
	}
	if strings.Contains(view, projectDir) {
		t.Fatalf("run view should not show internal project dir %q, got:\n%s", projectDir, view)
	}
}

// TestBuildStepRows_SelectedLoopIterationNestsUnderParent verifies iteration
// expansion rows are visibly indented to the right of the selected parent
// loop's label, so the step list conveys hierarchy.
func TestBuildStepRows_SelectedLoopIterationNestsUnderParent(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	loop := &StepNode{
		ID:                  "fanout",
		Type:                NodeLoop,
		Status:              StatusInProgress,
		Parent:              root,
		IterationsCompleted: 1,
		LoopMatches:         []string{"tasks/a.md"},
	}
	root.Children = []*StepNode{loop}
	iter := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusInProgress,
		Parent:         loop,
		IterationIndex: 0,
	}
	loop.Children = []*StepNode{iter}

	m := newTestModel(&Tree{Root: root}, FromList)
	rows := m.buildStepRows(root.Children)
	if len(rows) != 2 {
		t.Fatalf("expected loop row plus 1 iteration row, got %d", len(rows))
	}

	parent := stripANSI(rows[0])
	child := stripANSI(rows[1])
	parentParts := strings.SplitN(parent, "fanout", 2)
	childParts := strings.SplitN(child, "iter 1", 2)
	if len(parentParts) != 2 || len(childParts) != 2 {
		t.Fatalf("missing expected labels:\nparent=%q\nchild=%q", parent, child)
	}
	parentLabelWidth := lipgloss.Width(parentParts[0])
	childLabelWidth := lipgloss.Width(childParts[0])
	if childLabelWidth <= parentLabelWidth {
		t.Fatalf("iteration label should be indented to the right of the parent label, got parent=%d child=%d\nparent=%q\nchild=%q", parentLabelWidth, childLabelWidth, parent, child)
	}
}

// TestBuildStepRows_SelectedSubWorkflowNestsChildrenUnderParent verifies that
// the inline expansion of a selected sub-workflow renders its direct children
// visibly indented to the right of the parent row, so nesting is apparent.
// Regression against a flat-render bug where the sub-workflow parent and its
// child rows rendered at the same column.
func TestBuildStepRows_SelectedSubWorkflowNestsChildrenUnderParent(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	parentSub := &StepNode{ID: "agent-steps", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root}
	root.Children = []*StepNode{parentSub}

	resume := &StepNode{ID: "agent-resume", Type: NodeHeadlessAgent, Status: StatusSuccess, Parent: parentSub}
	shellFail := &StepNode{ID: "shell-fail", Type: NodeShell, Status: StatusFailed, Parent: parentSub}
	agentAfter := &StepNode{ID: "agent-after-fail", Type: NodeHeadlessAgent, Status: StatusSuccess, Parent: parentSub}
	parentSub.Children = []*StepNode{resume, shellFail, agentAfter}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 0
	rows := m.buildStepRows(root.Children)
	if len(rows) != 4 {
		t.Fatalf("expected parent row plus 3 expansion rows, got %d", len(rows))
	}

	parent := stripANSI(rows[0])
	parentParts := strings.SplitN(parent, "agent-steps", 2)
	if len(parentParts) != 2 {
		t.Fatalf("parent row missing label: %q", parent)
	}
	parentLabelOffset := lipgloss.Width(parentParts[0])

	for i, childID := range []string{"agent-resume", "shell-fail", "agent-after-fail"} {
		child := stripANSI(rows[i+1])
		childParts := strings.SplitN(child, childID, 2)
		if len(childParts) != 2 {
			t.Fatalf("child row %d missing label %q: %q", i, childID, child)
		}
		childLabelOffset := lipgloss.Width(childParts[0])
		if childLabelOffset <= parentLabelOffset {
			t.Fatalf("child %q should be indented past the parent label (parent offset=%d, child offset=%d)\nparent=%q\nchild=%q",
				childID, parentLabelOffset, childLabelOffset, parent, child)
		}
	}
}

func TestBuildStepRows_SelectedAutoFlattenedIterationShowsSubWorkflowChildren(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	loop := &StepNode{ID: "fanout", Type: NodeLoop, Status: StatusInProgress, Parent: root, AutoFlatten: true}
	subwf := &StepNode{
		ID: "impl", Type: NodeSubWorkflow, Status: StatusInProgress,
		StaticWorkflowPath: "/repo/workflows/impl.yaml", SubLoaded: true,
	}
	prepare := &StepNode{ID: "prepare", Type: NodeShell, Status: StatusSuccess, Parent: subwf}
	summarize := &StepNode{ID: "summarize", Type: NodeHeadlessAgent, Status: StatusPending, Parent: subwf}
	subwf.Children = []*StepNode{prepare, summarize}
	iter := &StepNode{
		ID: "fanout", Type: NodeIteration, Status: StatusInProgress, Parent: loop,
		IterationIndex: 0, FlattenTarget: subwf, Children: []*StepNode{subwf},
	}
	subwf.Parent = iter
	loop.Children = []*StepNode{iter}
	root.Children = []*StepNode{loop}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.path = []*StepNode{root, loop}

	rows := m.buildStepRows(loop.Children)
	joined := stripANSI(strings.Join(rows, "\n"))

	if !strings.Contains(joined, "prepare") {
		t.Fatalf("iteration expansion should list auto-flattened sub-workflow's direct children, got:\n%s", joined)
	}
	if !strings.Contains(joined, "summarize") {
		t.Fatalf("iteration expansion should list all direct children of auto-flattened target, got:\n%s", joined)
	}
	if strings.Contains(joined, "impl") {
		t.Fatalf("iteration expansion must not surface the auto-flattened sub-workflow row, got:\n%s", joined)
	}
}

func TestBuildStepRows_SelectedPendingSubWorkflowLoadsDirectChildren(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusPending}
	review := &StepNode{
		ID:             "review",
		Type:           NodeSubWorkflow,
		Status:         StatusPending,
		Parent:         root,
		StaticWorkflow: "review.yaml",
	}
	root.Children = []*StepNode{review}
	tree := &Tree{
		Root:         root,
		WorkflowPath: "/repo/workflows/root.yaml",
		SubWorkflowLoader: func(path string) (model.Workflow, error) {
			if path != "/repo/workflows/review.yaml" {
				t.Fatalf("unexpected sub-workflow path %q", path)
			}
			return model.Workflow{
				Name: "review",
				Steps: []model.Step{
					{ID: "prepare", Command: "echo prepare"},
					{ID: "summarize", Prompt: "Summarize"},
				},
			}, nil
		},
	}

	m := newTestModel(tree, FromList)
	rows := m.buildStepRows(root.Children)
	if len(rows) != 3 {
		t.Fatalf("expected sub-workflow row plus 2 pending child rows, got %d", len(rows))
	}
	if !review.SubLoaded {
		t.Fatal("selected sub-workflow should lazy-load its children for inline expansion")
	}

	joined := strings.Join([]string{
		stripANSI(rows[0]),
		stripANSI(rows[1]),
		stripANSI(rows[2]),
	}, "\n")
	for _, want := range []string{"prepare", "summarize"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected inline expansion to include %q, got:\n%s", want, joined)
		}
	}
}

func TestBuildStepRows_FailedPendingSubWorkflowExpansionDoesNotRetryEveryRender(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusPending}
	review := &StepNode{
		ID:             "review",
		Type:           NodeSubWorkflow,
		Status:         StatusPending,
		Parent:         root,
		StaticWorkflow: "review.yaml",
	}
	root.Children = []*StepNode{review}
	loadCalls := 0
	tree := &Tree{
		Root:         root,
		WorkflowPath: "/repo/workflows/root.yaml",
		SubWorkflowLoader: func(path string) (model.Workflow, error) {
			loadCalls++
			return model.Workflow{}, errors.New("load failed")
		},
	}

	m := newTestModel(tree, FromList)
	first := m.buildStepRows(root.Children)
	second := m.buildStepRows(root.Children)

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("failed inline expansion should leave only the selected row, got %d and %d rows", len(first), len(second))
	}
	if loadCalls != 1 {
		t.Fatalf("failed pending sub-workflow load should be attempted once per selection until explicit retry, got %d calls", loadCalls)
	}
	if review.ErrorMessage != "load failed" {
		t.Fatalf("expected cached load error, got %q", review.ErrorMessage)
	}
}

func TestBuildRenderedStepRows_TracksRenderedIndexAfterExpansion(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	setup := &StepNode{ID: "setup", Type: NodeShell, Status: StatusSuccess, Parent: root}
	review := &StepNode{ID: "review", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root}
	cleanup := &StepNode{ID: "cleanup", Type: NodeShell, Status: StatusPending, Parent: root}
	root.Children = []*StepNode{setup, review, cleanup}

	review.Children = []*StepNode{
		{ID: "one", Type: NodeShell, Status: StatusSuccess, Parent: review},
		{ID: "two", Type: NodeShell, Status: StatusSuccess, Parent: review},
		{ID: "three", Type: NodeShell, Status: StatusSuccess, Parent: review},
		{ID: "four", Type: NodeShell, Status: StatusSuccess, Parent: review},
	}

	m := newTestModel(&Tree{Root: root}, FromList)
	m.cursor = 1

	rendered := m.buildRenderedStepRows(root.Children)
	if got := renderedRowIndexForNode(rendered, cleanup); got != 6 {
		t.Fatalf("cleanup rendered row index = %d, want 6 after inserted expansion rows", got)
	}
	if got := leftPaneOffset(renderedRowIndexForNode(rendered, cleanup), len(rendered), 5); got != 2 {
		t.Fatalf("left pane offset = %d, want 2 for a 5-line viewport", got)
	}
}

func TestStepRowParts_IterationOmitsBindingValue(t *testing.T) {
	m := newTestModel(&Tree{Root: &StepNode{ID: "wf", Type: NodeRoot}}, FromList)
	_, label, _ := m.stepRowParts(&StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusSuccess,
		IterationIndex: 1,
		BindingValue:   "tasks/really/long/path.md",
	})
	if label != "iter 2" {
		t.Fatalf("iteration label = %q, want %q", label, "iter 2")
	}
}

func TestStepRowParts_LoopShowsGlyphAndCounter(t *testing.T) {
	m := newTestModel(&Tree{Root: &StepNode{ID: "wf", Type: NodeRoot}}, FromList)
	typeCol, label, _ := m.stepRowParts(&StepNode{
		ID:                  "fanout",
		Type:                NodeLoop,
		Status:              StatusInProgress,
		IterationsCompleted: 3,
		LoopMatches:         []string{"a", "b", "c", "d"},
	})
	if !strings.Contains(stripANSI(typeCol), "↺") {
		t.Fatalf("loop type column should contain a loop glyph, got %q", stripANSI(typeCol))
	}
	if label != "fanout (3/4)" {
		t.Fatalf("loop label = %q, want %q", label, "fanout (3/4)")
	}
}

func TestStepRowParts_TruncatesLongSidebarName(t *testing.T) {
	m := newTestModel(&Tree{Root: &StepNode{ID: "wf", Type: NodeRoot}}, FromList)
	_, label, _ := m.stepRowParts(&StepNode{
		ID:     "abcdefghijklmnopqrstuvw",
		Type:   NodeShell,
		Status: StatusPending,
	})
	if label != "abcdefghijklmnopq…" {
		t.Fatalf("truncated label = %q, want %q", label, "abcdefghijklmnopq…")
	}
}

// TestRenderStepRow_NonSelectedUsesDefaultTextColor verifies that step-list
// rows are rendered in the default body text color rather than the dim grey
// used for secondary UI elements.
func TestRenderStepRow_NonSelectedUsesDefaultTextColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	step := &StepNode{ID: "build", Type: NodeShell, Status: StatusSuccess, Parent: root}
	root.Children = []*StepNode{step}

	m := newTestModel(&Tree{Root: root}, FromList)
	rendered := m.renderStepRow(step, false, false)

	normalLabel := tuistyle.NormalStyle.Render("build")
	dimLabel := tuistyle.DimStyle.Render("build")
	if normalLabel == dimLabel {
		t.Fatalf("test setup: NormalStyle and DimStyle produced identical output — color profile not forced")
	}
	if !strings.Contains(rendered, normalLabel) {
		t.Errorf("expected non-selected label to use NormalStyle, got:\n%q", rendered)
	}
	if strings.Contains(rendered, dimLabel) {
		t.Errorf("non-selected label should not use DimStyle, got:\n%q", rendered)
	}
}

// TestRenderStepRow_SelectedIsGreenAndBold verifies the cursor-selected row
// renders its label in green + bold to stand out from siblings.
func TestRenderStepRow_SelectedIsGreenAndBold(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	step := &StepNode{ID: "build", Type: NodeShell, Status: StatusSuccess, Parent: root}
	root.Children = []*StepNode{step}

	m := newTestModel(&Tree{Root: root}, FromList)
	rendered := m.renderStepRow(step, true, false)

	want := lipgloss.NewStyle().Foreground(tuistyle.SuccessGreen).Bold(true).Render("build")
	if !strings.Contains(rendered, want) {
		t.Errorf("expected selected label rendered green+bold (%q), got:\n%q", want, rendered)
	}
}

// TestRenderExpansionRow_UsesDefaultTextColor verifies children surfaced
// beneath a selected container are rendered in the default body text color.
func TestRenderExpansionRow_UsesDefaultTextColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	child := &StepNode{ID: "prepare", Type: NodeShell, Status: StatusSuccess, Parent: root}

	m := newTestModel(&Tree{Root: root}, FromList)
	rendered := m.renderExpansionRow(child, 1)

	normalLabel := tuistyle.NormalStyle.Render("prepare")
	dimLabel := tuistyle.DimStyle.Render("prepare")
	if normalLabel == dimLabel {
		t.Fatalf("test setup: NormalStyle and DimStyle produced identical output — color profile not forced")
	}
	if !strings.Contains(rendered, normalLabel) {
		t.Errorf("expected expansion-row label to use NormalStyle, got:\n%q", rendered)
	}
	if strings.Contains(rendered, dimLabel) {
		t.Errorf("expansion-row label should not use DimStyle, got:\n%q", rendered)
	}
}

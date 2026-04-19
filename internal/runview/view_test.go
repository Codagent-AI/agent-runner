package runview

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

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

func TestBuildStepRows_SelectedStepShowsRecursiveExpansion(t *testing.T) {
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
	if len(rows) != 7 {
		t.Fatalf("expected 7 rows including expansion, got %d", len(rows))
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
	if !regexp.MustCompile(`^\s{2,}fanout`).MatchString(plain[2]) {
		t.Fatalf("row 2 should show the loop expansion with depth-1 indent, got %q", plain[2])
	}
	if !regexp.MustCompile(`^\s{4,}iter 2`).MatchString(plain[3]) {
		t.Fatalf("row 3 should show the active iteration with deeper indent, got %q", plain[3])
	}
	if !regexp.MustCompile(`^\s{6,}↳ {2}verify`).MatchString(plain[4]) {
		t.Fatalf("row 4 should show the nested sub-workflow, got %q", plain[4])
	}
	if !regexp.MustCompile(`^\s{8,}⚙ {2}summarize`).MatchString(plain[5]) {
		t.Fatalf("row 5 should show the deepest active descendant, got %q", plain[5])
	}
	if !strings.Contains(plain[6], "cleanup") {
		t.Fatalf("row 6 should be the final top-level sibling, got %q", plain[6])
	}
}

func TestBuildRenderedStepRows_TracksRenderedIndexAfterExpansion(t *testing.T) {
	root := &StepNode{ID: "wf", Type: NodeRoot, Status: StatusInProgress}
	setup := &StepNode{ID: "setup", Type: NodeShell, Status: StatusSuccess, Parent: root}
	review := &StepNode{ID: "review", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: root}
	cleanup := &StepNode{ID: "cleanup", Type: NodeShell, Status: StatusPending, Parent: root}
	root.Children = []*StepNode{setup, review, cleanup}

	loop := &StepNode{
		ID:                  "fanout",
		Type:                NodeLoop,
		Status:              StatusInProgress,
		Parent:              review,
		IterationsCompleted: 1,
		LoopMatches:         []string{"tasks/a.md", "tasks/b.md"},
	}
	review.Children = []*StepNode{loop}

	iter := &StepNode{
		ID:             "fanout",
		Type:           NodeIteration,
		Status:         StatusInProgress,
		Parent:         loop,
		IterationIndex: 1,
		BindingValue:   "tasks/b.md",
	}
	loop.Children = []*StepNode{iter}

	verify := &StepNode{ID: "verify", Type: NodeSubWorkflow, Status: StatusInProgress, Parent: iter}
	iter.Children = []*StepNode{verify}

	summarize := &StepNode{ID: "summarize", Type: NodeHeadlessAgent, Status: StatusInProgress, Parent: verify}
	verify.Children = []*StepNode{summarize}

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

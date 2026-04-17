package runview

import (
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

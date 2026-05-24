package tuistyle

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mattn/go-runewidth"
)

func TestWrapCell_ShortStringFits(t *testing.T) {
	got := WrapCell("hello world", 20)
	want := []string{"hello world"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("WrapCell short string mismatch (-want +got):\n%s", diff)
	}
}

func TestWrapCell_WrapsAtWordBoundary(t *testing.T) {
	got := WrapCell("alpha beta gamma delta", 12)
	want := []string{"alpha beta", "gamma delta"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("WrapCell word boundary mismatch (-want +got):\n%s", diff)
	}
}

func TestWrapCell_BreaksOverlongWordsByRune(t *testing.T) {
	got := WrapCell("abcdefghij short", 5)
	want := []string{"abcde", "fghij", "short"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("WrapCell character break mismatch (-want +got):\n%s", diff)
	}
}

func TestWrapCell_EmptyInputReturnsSingleEmptyLine(t *testing.T) {
	got := WrapCell("", 10)
	want := []string{""}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("WrapCell empty input mismatch (-want +got):\n%s", diff)
	}
}

func TestWrapCell_NonPositiveWidthReturnsSingleEmptyLine(t *testing.T) {
	for _, w := range []int{0, -1, -100} {
		got := WrapCell("anything", w)
		want := []string{""}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("WrapCell width=%d mismatch (-want +got):\n%s", w, diff)
		}
	}
}

func TestWrapCell_EveryLineFitsWidth(t *testing.T) {
	for _, c := range []struct {
		name  string
		input string
		width int
	}{
		{"narrow paragraph", strings.Repeat("alpha beta ", 20), 14},
		{"long word among short", "ant " + strings.Repeat("z", 60) + " bee", 10},
		{"single character width", strings.Repeat("ab ", 10), 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, line := range WrapCell(c.input, c.width) {
				if w := runewidth.StringWidth(line); w > c.width {
					t.Errorf("line %q width %d exceeds %d", line, w, c.width)
				}
			}
		})
	}
}

// TestWrapCell_OverWideRuneDoesNotEmitEmptyChunk guards against a regression in
// chunkRunes where a rune wider than the available width would cause an empty
// leading chunk before the rune was emitted. The expected behaviour is to emit
// the over-wide rune as its own line without preceding empty lines.
func TestWrapCell_OverWideRuneDoesNotEmitEmptyChunk(t *testing.T) {
	// "漢" has cell width 2 in runewidth; with width=1 the rune exceeds budget.
	got := WrapCell("漢漢", 1)
	for _, line := range got {
		if line == "" {
			t.Fatalf("WrapCell over-wide rune produced empty chunk: %#v", got)
		}
	}
	if len(got) == 0 {
		t.Fatalf("WrapCell over-wide rune produced no lines")
	}
}

package tuistyle

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestBlinkHidden verifies that BlinkHidden returns a blank string of the
// same visual width as the input — no ANSI escapes, no printable glyphs.
// This is the "off" half of a blink cycle: rather than recoloring the glyph
// (which is fragile because lipgloss background detection misfires inside
// bubbletea's alt-screen and resolves adaptive whites to near-black on some
// light themes), we simply hide the glyph by emitting width-matched spaces.
func TestBlinkHidden(t *testing.T) {
	cases := []struct {
		in        string
		wantWidth int
	}{
		{"●", 1},
		{"running", 7},
		{"active", 6},
		{"", 0},
	}
	for _, c := range cases {
		got := BlinkHidden(c.in)
		if got != strings.Repeat(" ", c.wantWidth) {
			t.Errorf("BlinkHidden(%q) = %q, want %d spaces", c.in, got, c.wantWidth)
		}
		if strings.ContainsRune(got, '\x1b') {
			t.Errorf("BlinkHidden(%q) contains ANSI escape: %q", c.in, got)
		}
		if lipgloss.Width(got) != c.wantWidth {
			t.Errorf("BlinkHidden(%q) width = %d, want %d", c.in, lipgloss.Width(got), c.wantWidth)
		}
	}
}

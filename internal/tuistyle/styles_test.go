package tuistyle

import (
	"math"
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

func TestRenderRule_UsesSingleScreenMargin(t *testing.T) {
	got := RenderRule(20)
	if !strings.HasPrefix(got, ScreenMargin) || strings.HasPrefix(got, ScreenMargin+ScreenMargin) {
		t.Fatalf("RenderRule should start with exactly one screen-margin prefix, got %q", got)
	}
	if lipgloss.Width(got) != 19 {
		t.Fatalf("RenderRule width = %d, want %d", lipgloss.Width(got), 19)
	}
}

func TestSecondaryTextColorsHaveReadableContrast(t *testing.T) {
	const (
		darkTerminalBackground  = "#111827"
		lightTerminalBackground = "#ffffff"
		minReadableContrast     = 4.5
	)
	tokens := map[string]lipgloss.AdaptiveColor{
		"CompletedGray": CompletedGray,
		"DimText":       DimText,
	}

	for name, token := range tokens {
		if got := contrastRatio(token.Dark, darkTerminalBackground); got < minReadableContrast {
			t.Errorf("%s dark contrast = %.2f, want >= %.1f", name, got, minReadableContrast)
		}
		if got := contrastRatio(token.Light, lightTerminalBackground); got < minReadableContrast {
			t.Errorf("%s light contrast = %.2f, want >= %.1f", name, got, minReadableContrast)
		}
	}
}

func contrastRatio(foreground, background string) float64 {
	frontLum := relativeLuminance(foreground)
	backLum := relativeLuminance(background)
	light, dark := math.Max(frontLum, backLum), math.Min(frontLum, backLum)
	return (light + 0.05) / (dark + 0.05)
}

func relativeLuminance(hex string) float64 {
	r, g, b := ParseHex(hex)
	return 0.2126*linearRGB(r) + 0.7152*linearRGB(g) + 0.0722*linearRGB(b)
}

func linearRGB(component uint8) float64 {
	channel := float64(component) / 255
	if channel <= 0.03928 {
		return channel / 12.92
	}
	return math.Pow((channel+0.055)/1.055, 2.4)
}

func TestSpinnerGlyph_IsSingleCellBraille(t *testing.T) {
	got := SpinnerGlyph(0)
	if got != "⠋" {
		t.Fatalf("SpinnerGlyph(0) = %q, want %q", got, "⠋")
	}
	if lipgloss.Width(got) != 1 {
		t.Fatalf("SpinnerGlyph width = %d, want 1", lipgloss.Width(got))
	}
}

package tuistyle

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// AdjustOffset returns an offset such that cursor is visible within a window of
// maxRows, preferring to keep the previous offset when possible.
func AdjustOffset(cursor, offset, maxRows, length int) int {
	if maxRows <= 0 || length <= maxRows {
		return 0
	}
	if cursor < offset {
		offset = cursor
	} else if cursor >= offset+maxRows {
		offset = cursor - maxRows + 1
	}
	if offset+maxRows > length {
		offset = length - maxRows
	}
	return max(0, offset)
}

// FitCellLeft is like FitCell but truncates from the start, prepending an
// ellipsis. Useful for paths where the tail is more meaningful than the head.
func FitCellLeft(s string, n int) string {
	if n <= 0 {
		return ""
	}
	w := runewidth.StringWidth(s)
	if w > n {
		budget := max(n-1, 0)
		runes := []rune(s)
		taken := 0
		i := len(runes)
		for i > 0 {
			rw := runewidth.RuneWidth(runes[i-1])
			if taken+rw > budget {
				break
			}
			taken += rw
			i--
		}
		s = "…" + string(runes[i:])
		w = runewidth.StringWidth(s)
	}
	if w < n {
		s += strings.Repeat(" ", n-w)
	}
	return s
}

// WrapCell wraps s into lines whose cell width does not exceed width, breaking
// at whitespace where possible. Words wider than width are character-broken.
// Always returns at least one line; an empty input yields []string{""}.
func WrapCell(s string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if runewidth.StringWidth(s) <= width {
		return []string{s}
	}

	var lines []string
	var cur strings.Builder
	curWidth := 0
	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
		curWidth = 0
	}

	for _, word := range strings.Fields(s) {
		ww := runewidth.StringWidth(word)
		if ww > width {
			if curWidth > 0 {
				flush()
			}
			lines = append(lines, chunkRunes(word, width)...)
			continue
		}
		need := ww
		if curWidth > 0 {
			need++ // space separator
		}
		if curWidth+need > width {
			flush()
		}
		if curWidth > 0 {
			cur.WriteByte(' ')
			curWidth++
		}
		cur.WriteString(word)
		curWidth += ww
	}
	if curWidth > 0 {
		flush()
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func chunkRunes(word string, width int) []string {
	var out []string
	var cur strings.Builder
	curWidth := 0
	for _, r := range word {
		rw := runewidth.RuneWidth(r)
		if curWidth+rw > width {
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				curWidth = 0
			}
			if rw > width {
				out = append(out, string(r))
				continue
			}
		}
		cur.WriteRune(r)
		curWidth += rw
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// FitCell truncates s to width n (adding an ellipsis if needed) and pads with
// spaces to reach exactly n visible columns.
func FitCell(s string, n int) string {
	if n <= 0 {
		return ""
	}
	w := runewidth.StringWidth(s)
	if w > n {
		s = runewidth.Truncate(s, n, "…")
		w = runewidth.StringWidth(s)
	}
	if w < n {
		s += strings.Repeat(" ", n-w)
	}
	return s
}

// ShortenPath replaces the user's home directory prefix with ~.
func ShortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	sep := string(os.PathSeparator)
	if strings.HasPrefix(p, home+sep) {
		return "~" + p[len(home):]
	}
	return p
}

// FormatTime renders a time suitable for timestamps displayed to users.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	local := t.In(now.Location())
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04") + " today"
	}
	if local.Year() == now.Year() {
		return local.Format("Jan 02")
	}
	return local.Format("Jan 02 2006")
}

// LerpColor linearly interpolates between two hex color strings.
func LerpColor(hex1, hex2 string, t float64) string {
	r1, g1, b1 := ParseHex(hex1)
	r2, g2, b2 := ParseHex(hex2)

	r := uint8(float64(r1) + t*(float64(r2)-float64(r1)))
	g := uint8(float64(g1) + t*(float64(g2)-float64(g1)))
	b := uint8(float64(b1) + t*(float64(b2)-float64(b1)))

	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// spinnerFrames is the classic 10-frame rotating braille spinner rendered
// at a larger scale: each frame occupies 3 rows × 2 dot-columns of real
// terminal cells, where a lit dot is "●" and an empty dot is a space.
// The lit dots trace clockwise around the border of a 3-row, 2-column
// grid — the same motion as the U+280B..U+280F braille cells, just drawn
// one character per dot so the animation is visible at normal font size.
var spinnerFrames = [][]string{
	{"● ●", "●  ", "   "}, // ⠋ dots 1,2,4
	{"● ●", "  ●", "   "}, // ⠙ dots 1,4,5
	{"● ●", "  ●", "  ●"}, // ⠹ dots 1,4,5,6
	{"  ●", "  ●", "  ●"}, // ⠸ dots 4,5,6
	{"  ●", "  ●", "● ●"}, // ⠼ dots 3,4,5,6
	{"   ", "  ●", "● ●"}, // ⠴ dots 3,5,6
	{"   ", "●  ", "● ●"}, // ⠦ dots 2,3,6
	{"●  ", "●  ", "● ●"}, // ⠧ dots 1,2,3,6
	{"●  ", "●  ", "●  "}, // ⠇ dots 1,2,3
	{"● ●", "●  ", "●  "}, // ⠏ dots 1,2,3,4
}

var spinnerGlyphFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerFrameIndex(phase float64, count int) int {
	idx := int(math.Floor(phase*1.5)) % count
	if idx < 0 {
		idx += count
	}
	return idx
}

// SpinnerFrame returns the three lines of the current spinner frame.
// Each line is 3 columns wide and each frame is 3 lines tall, so callers
// should print them on consecutive rows.
func SpinnerFrame(phase float64) []string {
	return spinnerFrames[spinnerFrameIndex(phase, len(spinnerFrames))]
}

// SpinnerGlyph returns the single-cell braille spinner glyph for the current phase.
func SpinnerGlyph(phase float64) string {
	return spinnerGlyphFrames[spinnerFrameIndex(phase, len(spinnerGlyphFrames))]
}

// BlinkOn returns true during the "on" half of each pulse cycle and false
// during the "off" half. Callers pair this with conditional styling to
// render a clear on/off blink — typically an accent color when on and the
// terminal default foreground (no color) when off, which keeps the blink
// visible regardless of the terminal's background theme.
func BlinkOn(phase float64) bool {
	return math.Sin(phase) >= 0
}

// ParseHex parses a #RRGGBB or RRGGBB hex color string into its components.
func ParseHex(hex string) (r, g, b uint8) {
	hex = strings.TrimPrefix(hex, "#")
	_, _ = fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\][^\x1b]*\x1b\\`)

// Sanitize strips ANSI escape sequences and non-printable runes, replacing
// tabs with single spaces so column widths measured via runewidth remain
// accurate.
func Sanitize(s string) string {
	s = ansiEscapeRe.ReplaceAllString(s, "")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\t':
			b.WriteByte(' ')
		case '\r':
			continue
		case '\n':
			b.WriteByte('\n')
		default:
			if unicode.IsPrint(r) && !unicode.Is(unicode.Co, r) {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

package tui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mattn/go-runewidth"

	"github.com/codagent/agent-runner/internal/runs"
)

// adjustOffset returns an offset such that cursor is visible within a window of
// maxRows, preferring to keep the previous offset when possible.
func adjustOffset(cursor, offset, maxRows, length int) int {
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

// fitCellLeft is like fitCell but truncates from the start, prepending an
// ellipsis. Useful for paths where the tail is more meaningful than the head.
func fitCellLeft(s string, n int) string {
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

// fitCell truncates s to width n (adding an ellipsis if needed) and pads with
// spaces to reach exactly n visible columns.
func fitCell(s string, n int) string {
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

func shortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04") + " today"
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 02")
	}
	return t.Format("Jan 02 2006")
}

func runSummary(runList []runs.RunInfo) string {
	total := len(runList)
	if total == 0 {
		return "no runs"
	}

	active := 0
	for i := range runList {
		if runList[i].Status == runs.StatusActive {
			active++
		}
	}

	label := "runs"
	if total == 1 {
		label = "run"
	}

	if active > 0 {
		return fmt.Sprintf("%d %s  ● %d active", total, label, active)
	}
	return fmt.Sprintf("%d %s", total, label)
}

func lerpColor(hex1, hex2 string, t float64) string {
	r1, g1, b1 := parseHex(hex1)
	r2, g2, b2 := parseHex(hex2)

	r := uint8(float64(r1) + t*(float64(r2)-float64(r1)))
	g := uint8(float64(g1) + t*(float64(g2)-float64(g1)))
	b := uint8(float64(b1) + t*(float64(b2)-float64(b1)))

	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func parseHex(hex string) (r, g, b uint8) {
	hex = strings.TrimPrefix(hex, "#")
	_, _ = fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\][^\x1b]*\x1b\\`)

func sanitize(s string) string {
	s = ansiEscapeRe.ReplaceAllString(s, "")
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\t' || (unicode.IsPrint(r) && !unicode.Is(unicode.Co, r)) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

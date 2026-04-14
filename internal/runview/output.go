package runview

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxOutputLines = 2000
	maxOutputBytes = 256 * 1024 // 256 KB
	tailLines      = 35
)

type truncatedOutput struct {
	Lines      []string
	TotalLines int
	Truncated  bool
}

func truncateOutput(output string) truncatedOutput {
	if output == "" {
		return truncatedOutput{}
	}
	lines := strings.Split(output, "\n")
	total := len(lines)
	if total <= maxOutputLines && len(output) <= maxOutputBytes {
		return truncatedOutput{Lines: lines, TotalLines: total}
	}
	// Enforce the byte cap: keep only the last maxOutputBytes of the output,
	// then split into lines, so that a few very large lines don't bypass truncation.
	tail := output
	if len(output) > maxOutputBytes {
		tail = output[len(output)-maxOutputBytes:]
		// Drop the first (likely partial) line after byte-slicing.
		if idx := strings.IndexByte(tail, '\n'); idx >= 0 {
			tail = tail[idx+1:]
		}
	}
	tailLns := strings.Split(tail, "\n")
	shown := min(tailLines, len(tailLns))
	return truncatedOutput{
		Lines:      tailLns[len(tailLns)-shown:],
		TotalLines: total,
		Truncated:  true,
	}
}

func (t truncatedOutput) banner() string {
	if !t.Truncated {
		return ""
	}
	return fmt.Sprintf("[%d lines total · showing last %d — press g to load all]", t.TotalLines, len(t.Lines))
}

func sanitizeUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			b.WriteRune('\uFFFD')
			i++
		} else {
			b.WriteRune(r)
			i += size
		}
	}
	return b.String()
}

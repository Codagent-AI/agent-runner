package pty

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteEnterShellAltScreen(t *testing.T) {
	var buf bytes.Buffer
	writeEnterShellAltScreen(&buf)

	out := buf.String()
	if !strings.Contains(out, "\x1b[?1049h") {
		t.Errorf("expected alt-screen enter sequence, got %q", out)
	}
	if !strings.Contains(out, "\x1b[2J") {
		t.Errorf("expected screen-clear sequence, got %q", out)
	}
	if !strings.Contains(out, "\x1b[H") {
		t.Errorf("expected cursor-home sequence, got %q", out)
	}

	enter := strings.Index(out, "\x1b[?1049h")
	clearPos := strings.Index(out, "\x1b[2J")
	home := strings.Index(out, "\x1b[H")
	if enter >= clearPos || clearPos >= home {
		t.Errorf("expected order enter<clear<home, got positions %d,%d,%d in %q",
			enter, clearPos, home, out)
	}
}

func TestWriteExitShellAltScreen(t *testing.T) {
	var buf bytes.Buffer
	writeExitShellAltScreen(&buf)

	out := buf.String()
	if out != "\x1b[?1049l" {
		t.Errorf("expected only the alt-screen exit sequence, got %q", out)
	}
}

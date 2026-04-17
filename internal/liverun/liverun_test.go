package liverun

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// ---- ANSI stripper tests ----

func strip(input string) string {
	var buf bytes.Buffer
	s := NewANSIStripper(&buf)
	_, _ = s.Write([]byte(input))
	return buf.String()
}

func TestANSIStripper_PlainText(t *testing.T) {
	got := strip("hello world\n")
	if got != "hello world\n" {
		t.Errorf("plain text modified: %q", got)
	}
}

func TestANSIStripper_SGR(t *testing.T) {
	// ESC[31m is red color; ESC[0m resets
	input := "\x1b[31mhello\x1b[0m world"
	got := strip(input)
	if got != "hello world" {
		t.Errorf("SGR not stripped: %q", got)
	}
}

func TestANSIStripper_OSC(t *testing.T) {
	// OSC terminated by BEL (0x07)
	input := "before\x1b]0;title\x07after"
	got := strip(input)
	if got != "beforeafter" {
		t.Errorf("OSC not stripped: %q", got)
	}
}

func TestANSIStripper_OSC_StringTerminator(t *testing.T) {
	// OSC terminated by ESC \
	input := "x\x1b]0;title\x1b\\y"
	got := strip(input)
	if got != "xy" {
		t.Errorf("OSC with ST not stripped: %q", got)
	}
}

func TestANSIStripper_PartialSequence(t *testing.T) {
	// Split a CSI sequence across two writes
	var buf bytes.Buffer
	s := NewANSIStripper(&buf)

	// Write ESC [ separately from the rest of the sequence
	_, _ = s.Write([]byte("start\x1b"))
	_, _ = s.Write([]byte("[32mgreen\x1b[0m end"))

	got := buf.String()
	if got != "startgreen end" {
		t.Errorf("partial CSI not handled: %q", got)
	}
}

func TestANSIStripper_MoveCursor(t *testing.T) {
	// ESC[1;1H is a cursor-move CSI sequence (final byte 'H' in 0x40-0x7E)
	input := "a\x1b[1;1Hb"
	got := strip(input)
	if got != "ab" {
		t.Errorf("cursor-move CSI not stripped: %q", got)
	}
}

// ---- sanitizePrefix tests ----

func TestSanitizePrefix_Simple(t *testing.T) {
	got := sanitizePrefix("my-step")
	if got != "my-step" {
		t.Errorf("sanitizePrefix(%q) = %q, want %q", "my-step", got, "my-step")
	}
}

func TestSanitizePrefix_SlashAndColon(t *testing.T) {
	// '/' and ':' both become '_'
	got := sanitizePrefix("loop-b:2/step-c")
	want := "loop-b_2_step-c"
	if got != want {
		t.Errorf("sanitizePrefix = %q, want %q", got, want)
	}
}

func TestSanitizePrefix_Brackets(t *testing.T) {
	// Audit prefix format "[build]" → "_build_"
	got := sanitizePrefix("[build]")
	want := "_build_"
	if got != want {
		t.Errorf("sanitizePrefix = %q, want %q", got, want)
	}
}

func TestSanitizePrefix_Spaces(t *testing.T) {
	got := sanitizePrefix("step one")
	if strings.Contains(got, " ") {
		t.Errorf("sanitizePrefix should not contain spaces: %q", got)
	}
}

// ---- chunkWriter tests ----

// nullCoord is a Coordinator with a nil program; send() is a no-op (guarded in coordinator.go).
func nullCoord() *Coordinator { return &Coordinator{program: nil, sessionDir: ""} }

func TestChunkWriter_FlushClearsBuffer(t *testing.T) {
	cw := newChunkWriter(nullCoord(), "[step]", "stdout")
	_, _ = cw.Write([]byte("hello"))
	cw.Flush()
	if len(cw.buf) != 0 {
		t.Errorf("buf not empty after Flush: len=%d", len(cw.buf))
	}
}

func TestChunkWriter_SizeFlush(t *testing.T) {
	// Writing >= chunkMaxBytes should flush immediately (buf empty after Write returns).
	cw := newChunkWriter(nullCoord(), "[step]", "stdout")
	large := strings.Repeat("x", chunkMaxBytes+1)
	_, _ = cw.Write([]byte(large))
	cw.mu.Lock()
	remaining := len(cw.buf)
	cw.mu.Unlock()
	if remaining != 0 {
		t.Errorf("buf should be empty after size-triggered flush: len=%d", remaining)
	}
}

func TestChunkWriter_IdleFlush(t *testing.T) {
	cw := newChunkWriter(nullCoord(), "[step]", "stdout")
	_, _ = cw.Write([]byte("data"))
	// Wait longer than the idle flush duration.
	time.Sleep(chunkIdleFlush + 20*time.Millisecond)
	cw.mu.Lock()
	remaining := len(cw.buf)
	cw.mu.Unlock()
	if remaining != 0 {
		t.Errorf("buf not flushed after idle timeout: len=%d", remaining)
	}
}

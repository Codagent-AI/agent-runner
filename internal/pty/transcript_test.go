package pty

import (
	"bytes"
	"strings"
	"testing"
)

func TestForwardOutputRawWritesToBothSinks(t *testing.T) {
	src := strings.NewReader("hello world")
	var stdout, transcript bytes.Buffer

	forwardOutputRaw(src, &stdout, &transcript)

	if stdout.String() != "hello world" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "hello world")
	}
	if transcript.String() != "hello world" {
		t.Errorf("transcript = %q, want %q", transcript.String(), "hello world")
	}
}

func TestForwardOutputRawNilTranscriptStillWritesStdout(t *testing.T) {
	src := strings.NewReader("data")
	var stdout bytes.Buffer

	forwardOutputRaw(src, &stdout, nil)

	if stdout.String() != "data" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "data")
	}
}

func TestStripTranscriptRemovesANSIEscapes(t *testing.T) {
	raw := "\x1b[32mgreen\x1b[0m plain \x1b[1;31mbold red\x1b[0m"
	got := stripTranscript(raw)
	want := "green plain bold red"
	if got != want {
		t.Errorf("stripTranscript(%q) = %q, want %q", raw, got, want)
	}
}

// shortWriter returns n=1 per Write call, exercising the short-write branch of
// the io.Writer contract. forwardOutputRaw must keep writing until the whole
// chunk is accepted.
type shortWriter struct {
	got []byte
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.got = append(w.got, p[0])
	return 1, nil
}

func TestForwardOutputRawHandlesShortWrites(t *testing.T) {
	src := strings.NewReader("hello")
	stdout := &shortWriter{}
	transcript := &shortWriter{}

	forwardOutputRaw(src, stdout, transcript)

	if string(stdout.got) != "hello" {
		t.Errorf("stdout dropped bytes: got %q, want %q", stdout.got, "hello")
	}
	if string(transcript.got) != "hello" {
		t.Errorf("transcript dropped bytes: got %q, want %q", transcript.got, "hello")
	}
}

func TestTailBufferKeepsTailUnderCap(t *testing.T) {
	b := newTailBuffer(4)
	_, _ = b.Write([]byte("abcdefgh"))
	got := b.String()
	if !strings.HasSuffix(got, "efgh") {
		t.Errorf("tailBuffer does not end with tail bytes: got %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("tailBuffer truncation marker missing: got %q", got)
	}
}

func TestTailBufferDoesNotTruncateBelowCap(t *testing.T) {
	b := newTailBuffer(16)
	_, _ = b.Write([]byte("hello "))
	_, _ = b.Write([]byte("world"))
	if got := b.String(); got != "hello world" {
		t.Errorf("tailBuffer = %q, want %q", got, "hello world")
	}
}

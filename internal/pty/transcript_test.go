package pty

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestForwardOutputRawWritesToBothSinks(t *testing.T) {
	raw := "hello /next\x1b[31m red\x1b[0m\x1b[?1002h\x1b[<0;4;5M"
	src := strings.NewReader(raw)
	var stdout, transcript bytes.Buffer

	if err := forwardOutputRaw(src, &stdout, &transcript); err != nil {
		t.Fatalf("forward output: %v", err)
	}

	if stdout.String() != raw {
		t.Errorf("stdout = %q, want exact bytes %q", stdout.String(), raw)
	}
	if transcript.String() != raw {
		t.Errorf("transcript = %q, want exact bytes %q", transcript.String(), raw)
	}
}

func TestForwardOutputRawNilTranscriptStillWritesStdout(t *testing.T) {
	src := strings.NewReader("data")
	var stdout bytes.Buffer

	if err := forwardOutputRaw(src, &stdout, nil); err != nil {
		t.Fatalf("forward output: %v", err)
	}

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

	if err := forwardOutputRaw(src, stdout, transcript); err != nil {
		t.Fatalf("forward output: %v", err)
	}

	if string(stdout.got) != "hello" {
		t.Errorf("stdout dropped bytes: got %q, want %q", stdout.got, "hello")
	}
	if string(transcript.got) != "hello" {
		t.Errorf("transcript dropped bytes: got %q, want %q", transcript.got, "hello")
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestForwardOutputRawPropagatesWriterError(t *testing.T) {
	wantErr := errors.New("terminal unavailable")
	err := forwardOutputRaw(strings.NewReader("data"), failingWriter{err: wantErr}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("forwardOutputRaw error = %v, want %v", err, wantErr)
	}
}

func TestInputBytesAreForwardedWithoutInterpretation(t *testing.T) {
	raw := []byte("/next\r\x1b[1;5D\x1b[<64;10;12M")
	var destination bytes.Buffer
	if err := writeFull(&destination, raw); err != nil {
		t.Fatalf("forward input: %v", err)
	}
	if !bytes.Equal(destination.Bytes(), raw) {
		t.Fatalf("input bytes = %q, want exact bytes %q", destination.Bytes(), raw)
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

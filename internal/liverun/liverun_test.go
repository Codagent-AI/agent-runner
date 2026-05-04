package liverun

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	iexec "github.com/codagent/agent-runner/internal/exec"
)

type captureProgram struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (p *captureProgram) ReleaseTerminal() error { return nil }
func (p *captureProgram) RestoreTerminal() error { return nil }
func (p *captureProgram) Send(msg tea.Msg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.msgs = append(p.msgs, msg)
}

func (p *captureProgram) messages() []tea.Msg {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]tea.Msg, len(p.msgs))
	copy(out, p.msgs)
	return out
}

type closeFlushWriter struct {
	downstream io.Writer
	buf        bytes.Buffer
}

func (w *closeFlushWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func (w *closeFlushWriter) Close() error {
	_, err := w.downstream.Write(w.buf.Bytes())
	return err
}

type unusedRunner struct{}

func (unusedRunner) RunShell(string, bool, string) (iexec.ProcessResult, error) {
	return iexec.ProcessResult{}, nil
}

func (unusedRunner) RunAgent([]string, bool, string) (iexec.ProcessResult, error) {
	return iexec.ProcessResult{}, nil
}

func (unusedRunner) RunScript(string, []byte, bool, string) (iexec.ProcessResult, error) {
	return iexec.ProcessResult{}, nil
}

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
	// '/' → "--" (nesting), ':' → "_" (iteration), '_' passes through.
	got := sanitizePrefix("loop-b:2/step-c")
	want := "loop-b_2--step-c"
	if got != want {
		t.Errorf("sanitizePrefix = %q, want %q", got, want)
	}
}

// TestSanitizePrefix_NestingVsIterationDisambiguation guards against the
// collision where the nested step 'b' under loop 'a' (audit prefix "a/b")
// would match iteration 'b' of loop 'a' (audit prefix "a:b") if both
// separators mapped to the same replacement.
func TestSanitizePrefix_NestingVsIterationDisambiguation(t *testing.T) {
	nested := sanitizePrefix("a/b")
	iter := sanitizePrefix("a:b")
	if nested == iter {
		t.Errorf("sanitizePrefix collision: %q == %q", nested, iter)
	}
}

// TestSanitizePrefix_UnderscoreInStepID verifies that step IDs containing
// '_' do not collide with the nesting separator. Regression against the
// earlier '__' nesting separator, where a prefix like "a__b/c" would
// collide with "a/b__c" because both produced "a__b__c".
func TestSanitizePrefix_UnderscoreInStepID(t *testing.T) {
	nestedWithUnderscore := sanitizePrefix("a__b/c")
	slashBetweenUnderscores := sanitizePrefix("a/b__c")
	if nestedWithUnderscore == slashBetweenUnderscores {
		t.Errorf("sanitizePrefix collision on '_' vs '/': %q == %q", nestedWithUnderscore, slashBetweenUnderscores)
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

func TestCompositeWriter_ClosesStdoutWrapperBeforeChunkFlush(t *testing.T) {
	program := &captureProgram{}
	runner := NewCoordinator(program, "").TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)
	runner.SetPrefix("[step]")
	runner.SetStdoutWrapper(func(w io.Writer) io.Writer {
		return &closeFlushWriter{downstream: w}
	})

	w, cleanup := runner.compositeWriter("stdout", "out", nil)
	_, _ = w.Write([]byte("hello"))
	cleanup()

	var got string
	for _, msg := range program.messages() {
		if chunk, ok := msg.(OutputChunkMsg); ok {
			got += string(chunk.Bytes)
		}
	}
	if got != "hello" {
		t.Fatalf("streamed stdout = %q, want %q", got, "hello")
	}
}

func TestCompositeWriter_AppliesStderrWrapperBeforeChunkFlush(t *testing.T) {
	program := &captureProgram{}
	runner := NewCoordinator(program, "").TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)
	runner.SetPrefix("[step]")
	runner.SetStderrWrapper(func(w io.Writer) io.Writer {
		return &closeFlushWriter{downstream: w}
	})

	w, cleanup := runner.compositeWriter("stderr", "err", nil)
	_, _ = w.Write([]byte("warning"))
	cleanup()

	var got string
	for _, msg := range program.messages() {
		if chunk, ok := msg.(OutputChunkMsg); ok {
			got += string(chunk.Bytes)
		}
	}
	if got != "warning" {
		t.Fatalf("streamed stderr = %q, want %q", got, "warning")
	}
}

func TestTUIProcessRunner_SetScriptPrefixDelaysStepState(t *testing.T) {
	program := &captureProgram{}
	runner := NewCoordinator(program, "").TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)

	runner.SetScriptPrefix("[script]", 50*time.Millisecond)
	if hasStepState(program.messages(), "[script]") {
		t.Fatal("script step state should not be sent immediately")
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if hasStepState(program.messages(), "[script]") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("script step state was not sent after delay")
}

func TestTUIProcessRunner_RunScriptCancelsDelayedStepState(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/quick.sh"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf ok\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	program := &captureProgram{}
	runner := NewCoordinator(program, "").TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)
	runner.SetScriptPrefix("[script]", time.Hour)
	if _, err := runner.RunScript(script, nil, true, ""); err != nil {
		t.Fatalf("RunScript returned error: %v", err)
	}

	if hasStepState(program.messages(), "[script]") {
		t.Fatal("quick script should cancel delayed step state")
	}
}

func hasStepState(messages []tea.Msg, prefix string) bool {
	for _, msg := range messages {
		if state, ok := msg.(StepStateMsg); ok && state.ActiveStepPrefix == prefix {
			return true
		}
	}
	return false
}

func TestTUIProcessRunner_RunAgentDoesNotInheritStdin(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString("leaked\n"); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	runner := NewCoordinator(&captureProgram{}, "").TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)
	result, err := runner.RunAgent([]string{"sh", "-c", `if read x; then printf "read:%s" "$x"; else printf "eof"; fi`}, true, "")
	if err != nil {
		t.Fatalf("RunAgent returned error: %v", err)
	}
	if result.Stdout != "eof" {
		t.Fatalf("RunAgent inherited stdin, stdout = %q", result.Stdout)
	}
}

func TestChunkWriter_SizeFlush(t *testing.T) {
	// Writing more than chunkMaxBytes should flush complete chunks immediately.
	// Any bytes that don't fill a complete chunk remain buffered.
	cw := newChunkWriter(nullCoord(), "[step]", "stdout")
	large := strings.Repeat("x", chunkMaxBytes+1)
	_, _ = cw.Write([]byte(large))
	cw.mu.Lock()
	remaining := len(cw.buf)
	cw.mu.Unlock()
	// After writing chunkMaxBytes+1 bytes, exactly 1 byte remains buffered
	// (the last byte that didn't fill a complete chunk).
	if remaining >= chunkMaxBytes {
		t.Errorf("buf should have been flushed below chunkMaxBytes: len=%d", remaining)
	}
}

func TestChunkWriter_IdleFlush(t *testing.T) {
	cw := newChunkWriter(nullCoord(), "[step]", "stdout")
	_, _ = cw.Write([]byte("data"))
	// Poll until the buffer drains (idle timer fires) or the deadline expires.
	deadline := time.Now().Add(5 * chunkIdleFlush)
	for time.Now().Before(deadline) {
		cw.mu.Lock()
		remaining := len(cw.buf)
		cw.mu.Unlock()
		if remaining == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	cw.mu.Lock()
	remaining := len(cw.buf)
	cw.mu.Unlock()
	if remaining != 0 {
		t.Errorf("buf not flushed after idle timeout: len=%d", remaining)
	}
}

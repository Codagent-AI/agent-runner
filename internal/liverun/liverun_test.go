package liverun

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

func (unusedRunner) RunAgent(*iexec.AgentProcessOptions) (iexec.ProcessResult, error) {
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
	// '/' → "__" (nesting), ':' → "_" (iteration), '_' passes through.
	got := sanitizePrefix("loop-b:2/step-c")
	want := "loop-b_2__step-c"
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

func TestSanitizePrefix_EscapesLiteralUnderscoresWithoutCollidingWithNesting(t *testing.T) {
	nested := sanitizePrefix("a__b/c")
	slashBetweenUnderscores := sanitizePrefix("a/b__c")
	if nested == slashBetweenUnderscores {
		t.Errorf("sanitizePrefix collision: %q == %q", nested, slashBetweenUnderscores)
	}
	if got, want := nested, "a%5F%5Fb__c"; got != want {
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

func TestTUIProcessRunner_RunScriptRetainsFailureDiagnosticsWithoutCapture(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'archive conflict\\n'\nprintf 'details\\n' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	runner := NewCoordinator(&captureProgram{}, "").TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)
	result, err := runner.RunScript(script, nil, false, "")
	if err != nil {
		t.Fatalf("RunScript returned error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("exit code = %d, want 1", result.ExitCode)
	}
	if result.Stdout != "archive conflict\n" {
		t.Fatalf("stdout = %q, want failure diagnostics", result.Stdout)
	}
	if result.Stderr != "details\n" {
		t.Fatalf("stderr = %q, want failure diagnostics", result.Stderr)
	}
}

func TestTailBufferBoundsFailureDiagnostics(t *testing.T) {
	buffer := &tailBuffer{}
	prefix := strings.Repeat("x", maxFailureDiagnosticBytes+1024)
	if _, err := buffer.Write([]byte(prefix + "final diagnostic")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if got := len(buffer.String()); got != maxFailureDiagnosticBytes {
		t.Fatalf("buffer length = %d, want %d", got, maxFailureDiagnosticBytes)
	}
	if !strings.HasSuffix(buffer.String(), "final diagnostic") {
		t.Fatalf("buffer did not retain diagnostic tail")
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
	result, err := runner.RunAgent(&iexec.AgentProcessOptions{
		Context: context.Background(), Args: []string{"sh", "-c", `if read x; then printf "read:%s" "$x"; else printf "eof"; fi`}, CaptureStdout: true,
	})
	if err != nil {
		t.Fatalf("RunAgent returned error: %v", err)
	}
	if result.Stdout != "eof" {
		t.Fatalf("RunAgent inherited stdin, stdout = %q", result.Stdout)
	}
}

func TestTUIProcessRunner_RunAgentCanceledBeforeStartIsNotLaunched(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := NewCoordinator(&captureProgram{}, "").TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)

	result, err := runner.RunAgent(&iexec.AgentProcessOptions{
		Context: ctx, Args: []string{"sh", "-c", "exit 0"}, CaptureStdout: true,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunAgent() error = %v, want context canceled", err)
	}
	if result.Started {
		t.Fatalf("RunAgent() result = %#v, want Started=false", result)
	}
}

func TestTUIProcessRunner_RunAgentRetainsOutputAndWaitDelayForLingeringPipe(t *testing.T) {
	runner := NewCoordinator(&captureProgram{}, "").TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)

	result, err := runner.RunAgent(&iexec.AgentProcessOptions{
		Context: context.Background(),
		Args: []string{
			"sh", "-c",
			`printf complete; sh -c 'sleep 0.2' &`,
		},
		CaptureStdout: true,
		Supervision: iexec.AgentProcessSupervision{
			ProcessGroup:     true,
			TerminationGrace: 10 * time.Millisecond,
		},
	})
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("RunAgent() error = %v, want exec.ErrWaitDelay", err)
	}
	if result.Stdout != "complete" {
		t.Fatalf("RunAgent() result = %#v, want retained output", result)
	}
}

func TestTUIProcessRunnerPersistsRepeatedAgentCallOutputByCallIdentity(t *testing.T) {
	sessionDir := t.TempDir()
	runner := NewCoordinator(&captureProgram{}, sessionDir).TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)

	for _, call := range []struct {
		id, output string
	}{
		{id: "call-1", output: "first child response"},
		{id: "call-2", output: "second child response"},
	} {
		prefix := "[parent, call:" + call.id + "]"
		result, err := runner.RunAgent(&iexec.AgentProcessOptions{
			Context: context.Background(), Args: []string{"sh", "-c", "printf '%s' \"$1\"", "agent", call.output},
			CaptureStdout: true, Prefix: prefix,
		})
		if err != nil {
			t.Fatalf("RunAgent(%s): %v", call.id, err)
		}
		if result.Stdout != call.output {
			t.Fatalf("RunAgent(%s) stdout = %q, want %q", call.id, result.Stdout, call.output)
		}
		path := filepath.Join(sessionDir, "output", sanitizePrefix(prefix)+".out")
		persisted, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s output: %v", call.id, err)
		}
		if string(persisted) != call.output {
			t.Fatalf("persisted %s output = %q, want %q", call.id, persisted, call.output)
		}
	}

	entries, err := os.ReadDir(filepath.Join(sessionDir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	var stdoutFiles int
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".out") {
			stdoutFiles++
		}
	}
	if stdoutFiles != 2 {
		t.Fatalf("persisted stdout files = %d, want one per call; entries=%v", stdoutFiles, entries)
	}
}

func TestTUIProcessRunnerRoutesRepositoryOutputDirectory(t *testing.T) {
	sessionDir := t.TempDir()
	outputDir := filepath.Join(sessionDir, "output", "repositories", "backend")
	runner := NewCoordinator(&captureProgram{}, sessionDir).TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)
	runner.SetOutputDirectory(outputDir)
	runner.SetPrefix("[backend-step]")
	if _, err := runner.RunShell("printf backend", true, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, sanitizePrefix("[backend-step]")+".out")); err != nil {
		t.Fatalf("repository output was not written beneath its directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, "output", sanitizePrefix("[backend-step]")+".out")); !os.IsNotExist(err) {
		t.Fatalf("repository output leaked into workspace output: %v", err)
	}
}

func TestTUIProcessRunnerPreservesOutputWhenPrefixIsReplayed(t *testing.T) {
	sessionDir := t.TempDir()
	runner := NewCoordinator(&captureProgram{}, sessionDir).TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)
	prefix := "[implement, generate-code]"

	runner.SetPrefix(prefix)
	first, firstCleanup := runner.compositeWriter("stdout", "out", nil)
	if _, err := first.Write([]byte("interrupted attempt")); err != nil {
		t.Fatalf("write first attempt: %v", err)
	}

	second, secondCleanup := runner.compositeWriter("stdout", "out", nil)
	if _, err := second.Write([]byte("resumed attempt")); err != nil {
		t.Fatalf("write resumed attempt: %v", err)
	}
	if _, err := first.Write([]byte(" still exiting")); err != nil {
		t.Fatalf("write exiting first attempt: %v", err)
	}
	firstCleanup()
	secondCleanup()

	currentPath := filepath.Join(sessionDir, "output", sanitizePrefix(prefix)+".out")
	current, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("read current output: %v", err)
	}
	if got, want := string(current), "resumed attempt"; got != want {
		t.Fatalf("current output = %q, want %q", got, want)
	}

	archives, err := filepath.Glob(currentPath + ".bak-*")
	if err != nil {
		t.Fatalf("glob archived output: %v", err)
	}
	if len(archives) != 1 {
		t.Fatalf("archived outputs = %v, want one preserved attempt", archives)
	}
	archived, err := os.ReadFile(archives[0])
	if err != nil {
		t.Fatalf("read archived output: %v", err)
	}
	if got, want := string(archived), "interrupted attempt still exiting"; got != want {
		t.Fatalf("archived output = %q, want %q", got, want)
	}
}

func TestTUIProcessRunnerBoundsReplayedOutputArchives(t *testing.T) {
	const wantArchives = 8

	sessionDir := t.TempDir()
	runner := NewCoordinator(&captureProgram{}, sessionDir).TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)
	prefix := "[implement, generate-code]"
	runner.SetPrefix(prefix)

	for attempt := range wantArchives + 4 {
		writer, cleanup := runner.compositeWriter("stdout", "out", nil)
		if _, err := fmt.Fprintf(writer, "attempt %d", attempt); err != nil {
			t.Fatalf("write attempt %d: %v", attempt, err)
		}
		cleanup()
	}

	currentPath := filepath.Join(sessionDir, "output", sanitizePrefix(prefix)+".out")
	archives, err := filepath.Glob(currentPath + ".bak-*")
	if err != nil {
		t.Fatalf("glob archived output: %v", err)
	}
	if len(archives) != wantArchives {
		t.Fatalf("archived outputs = %d, want bounded retention of %d", len(archives), wantArchives)
	}
}

func TestTUIProcessRunnerKeepsCurrentOutputWhenArchivePruningFails(t *testing.T) {
	sessionDir := t.TempDir()
	program := &captureProgram{}
	runner := NewCoordinator(program, sessionDir).TUIProcessRunner(unusedRunner{}).(*tuiProcessRunner)
	prefix := "[implement, generate-code]"
	currentPath := filepath.Join(sessionDir, "output", sanitizePrefix(prefix)+".out")
	if err := os.MkdirAll(filepath.Dir(currentPath), 0o750); err != nil {
		t.Fatalf("create output directory: %v", err)
	}
	if err := os.WriteFile(currentPath, []byte("interrupted"), 0o600); err != nil {
		t.Fatalf("write current output: %v", err)
	}

	unremovable := currentPath + ".bak-0000"
	if err := os.MkdirAll(unremovable, 0o750); err != nil {
		t.Fatalf("create unremovable archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(unremovable, "child"), []byte("keep"), 0o600); err != nil {
		t.Fatalf("write unremovable archive child: %v", err)
	}
	for i := 1; i < maxArchivedOutputAttempts; i++ {
		archive := fmt.Sprintf("%s.bak-%04d", currentPath, i)
		if err := os.WriteFile(archive, []byte("old"), 0o600); err != nil {
			t.Fatalf("write archive %d: %v", i, err)
		}
	}

	runner.SetPrefix(prefix)
	writer, cleanup := runner.compositeWriter("stdout", "out", nil)
	if _, err := writer.Write([]byte("resumed")); err != nil {
		t.Fatalf("write resumed output: %v", err)
	}
	cleanup()

	current, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("read resumed output: %v", err)
	}
	if got, want := string(current), "resumed"; got != want {
		t.Fatalf("current output = %q, want %q", got, want)
	}

	var warning string
	for _, msg := range program.messages() {
		if chunk, ok := msg.(OutputChunkMsg); ok && chunk.Stream == "stderr" {
			warning += string(chunk.Bytes)
		}
	}
	if !strings.Contains(warning, "could not prune archived output") {
		t.Fatalf("persistence warning = %q, want archive pruning failure", warning)
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

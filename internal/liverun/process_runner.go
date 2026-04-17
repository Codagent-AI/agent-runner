package liverun

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	iexec "github.com/codagent/agent-runner/internal/exec"
)

// PrefixSetter is implemented by TUIProcessRunner. The exec package uses this
// interface (via type assertion) to set the audit-log step prefix before each
// RunShell / RunAgent call so output chunks and files can be labeled correctly.
type PrefixSetter interface {
	SetPrefix(prefix string)
}

// tuiProcessRunner implements exec.ProcessRunner with TUI streaming.
type tuiProcessRunner struct {
	base       iexec.ProcessRunner
	coord      *Coordinator
	stepPrefix string // set via SetPrefix before each step
}

// SetPrefix updates the step prefix that labels output chunks and output files,
// and notifies the TUI that a new step has become active. Called by exec/shell.go
// and exec/agent.go via a type assertion before each run.
func (r *tuiProcessRunner) SetPrefix(prefix string) {
	r.stepPrefix = prefix
	r.coord.NotifyStepChange(prefix)
}

// sanitizePrefix converts an audit-log prefix into a safe filesystem name.
// Maps '/' → "__" and ':' → "_" per the spec (so loop-b:2/step-c becomes
// loop-b_2__step-c, distinguishing nesting from iteration suffixes). Any
// other character outside the allowlist [A-Za-z0-9._-] is replaced with
// a single '_'. Separator replacement blocks path traversal on every
// platform (including '\' on Windows); the containment check in
// openOutputFile rejects any residual '..' substring.
func sanitizePrefix(prefix string) string {
	var b strings.Builder
	for _, ch := range prefix {
		switch {
		case ch >= 'A' && ch <= 'Z',
			ch >= 'a' && ch <= 'z',
			ch >= '0' && ch <= '9',
			ch == '.' || ch == '-':
			b.WriteRune(ch)
		case ch == '/':
			b.WriteString("__")
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// openOutputFile creates (or truncates) an output file under
// <sessionDir>/output/<sanitizedPrefix>.<ext>. Returns nil on any error —
// callers treat a nil file as "no persistence" and continue without it.
func (r *tuiProcessRunner) openOutputFile(ext string) *os.File {
	if r.coord.sessionDir == "" || r.stepPrefix == "" {
		return nil
	}
	dir := filepath.Join(r.coord.sessionDir, "output")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil
	}
	base := sanitizePrefix(r.stepPrefix)
	// Defense in depth: reject any residual traversal tokens even after
	// allowlist sanitization, and verify the resolved path stays under dir.
	if base == "" || base == "." || base == ".." || strings.Contains(base, "..") {
		return nil
	}
	name := filepath.Clean(filepath.Join(dir, base+"."+ext))
	cleanDir := filepath.Clean(dir)
	if !strings.HasPrefix(name, cleanDir+string(filepath.Separator)) {
		return nil
	}
	// #nosec G304 — name is allowlist-sanitized and containment-checked above.
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil
	}
	return f
}

// compositeWriter builds the three-way tee:
//
//	raw → io.MultiWriter(
//	       ANSIStripper → chunkWriter → p.Send(OutputChunkMsg),
//	       outputFile (raw bytes),
//	       *bytes.Buffer (for ProcessResult.Stdout/Stderr),
//	     )
func (r *tuiProcessRunner) compositeWriter(stream, ext string, buf *bytes.Buffer) (w io.Writer, cleanup func()) {
	chunk := newChunkWriter(r.coord, r.stepPrefix, stream)
	stripped := NewANSIStripper(chunk)

	f := r.openOutputFile(ext)

	writers := []io.Writer{stripped}
	if f != nil {
		writers = append(writers, f)
	}
	if buf != nil {
		writers = append(writers, buf)
	}

	w = io.MultiWriter(writers...)
	cleanup = func() {
		chunk.Flush()
		if f != nil {
			_ = f.Close()
		}
	}
	return w, cleanup
}

// RunShell runs a shell command, streaming stdout and stderr to the TUI and
// persisting raw bytes to output files. Behaves identically to realProcessRunner
// from the caller's perspective; ProcessResult is always populated.
func (r *tuiProcessRunner) RunShell(cmd string, captureStdout bool, workdir string) (iexec.ProcessResult, error) {
	c := exec.Command("sh", "-c", cmd) // #nosec G204
	c.Stdin = os.Stdin
	if workdir != "" {
		c.Dir = filepath.Clean(workdir) // #nosec G304
	}

	var stdoutBuf, stderrBuf bytes.Buffer

	stdoutW, stdoutCleanup := r.compositeWriter("stdout", "out", &stdoutBuf)
	stderrW, stderrCleanup := r.compositeWriter("stderr", "err", &stderrBuf)
	defer stdoutCleanup()
	defer stderrCleanup()

	c.Stdout = stdoutW
	c.Stderr = stderrW

	err := c.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return iexec.ProcessResult{}, err
		}
	}

	stdout := strings.TrimSpace(stdoutBuf.String())
	if !captureStdout {
		stdout = ""
	}

	return iexec.ProcessResult{
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   strings.TrimSpace(stderrBuf.String()),
	}, nil
}

// RunAgent runs an agent CLI process, streaming output to the TUI and persisting
// raw bytes to output files. Interactive steps bypass this path entirely (they
// go through pty.RunInteractive).
func (r *tuiProcessRunner) RunAgent(args []string, captureStdout bool, workdir string) (iexec.ProcessResult, error) {
	c := exec.Command(args[0], args[1:]...) // #nosec G204
	c.Stdin = os.Stdin
	if workdir != "" {
		c.Dir = filepath.Clean(workdir) // #nosec G304
	}

	var stdoutBuf, stderrBuf bytes.Buffer

	stdoutW, stdoutCleanup := r.compositeWriter("stdout", "out", &stdoutBuf)
	stderrW, stderrCleanup := r.compositeWriter("stderr", "err", &stderrBuf)
	defer stdoutCleanup()
	defer stderrCleanup()

	c.Stdout = stdoutW
	c.Stderr = stderrW

	err := c.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return iexec.ProcessResult{}, err
		}
	}

	return iexec.ProcessResult{
		ExitCode: exitCode,
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
	}, nil
}

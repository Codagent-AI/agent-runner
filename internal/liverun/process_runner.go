package liverun

import (
	"bytes"
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

// SetPrefix updates the step prefix that labels output chunks and output files.
// Called by exec/shell.go and exec/agent.go via a type assertion before each run.
func (r *tuiProcessRunner) SetPrefix(prefix string) {
	r.stepPrefix = prefix
}

// sanitizePrefix converts an audit-log prefix into a safe filesystem name:
// '/' → "__", ':' → "_", whitespace → "_".
func sanitizePrefix(prefix string) string {
	var b strings.Builder
	for _, ch := range prefix {
		switch {
		case ch == '/':
			b.WriteString("__")
		case ch == ':' || ch == ' ' || ch == '\t':
			b.WriteByte('_')
		default:
			b.WriteRune(ch)
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
	name := filepath.Join(dir, sanitizePrefix(r.stepPrefix)+"."+ext)
	// #nosec G304 — prefix is sanitized; no path traversal possible
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
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
func (r *tuiProcessRunner) compositeWriter(stream, ext string, buf *bytes.Buffer) (io.Writer, func()) {
	chunk := newChunkWriter(r.coord, r.stepPrefix, stream)
	stripped := NewANSIStripper(chunk)

	f := r.openOutputFile(ext)

	var writers []io.Writer
	writers = append(writers, stripped)
	if f != nil {
		writers = append(writers, f)
	}
	if buf != nil {
		writers = append(writers, buf)
	}

	cleanup := func() {
		chunk.Flush()
		if f != nil {
			_ = f.Close()
		}
	}

	return io.MultiWriter(writers...), cleanup
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
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			err = nil
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
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			err = nil
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

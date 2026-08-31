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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	base          iexec.ProcessRunner
	coord         *Coordinator
	stepPrefix    string // set via SetPrefix before each step
	stdoutWrapper func(w io.Writer) io.Writer
	stderrWrapper func(w io.Writer) io.Writer

	mu                sync.Mutex
	delayedStepPrefix string
	delayedStepTimer  *time.Timer
}

type outputScope struct {
	prefix        string
	stdoutWrapper func(io.Writer) io.Writer
	stderrWrapper func(io.Writer) io.Writer
}

var outputArchiveSequence atomic.Uint64

const maxArchivedOutputAttempts = 8
const maxFailureDiagnosticBytes = 64 * 1024

type textBuffer interface {
	io.Writer
	String() string
}

type tailBuffer struct {
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	written := len(p)
	if len(p) >= maxFailureDiagnosticBytes {
		b.data = append(b.data[:0], p[len(p)-maxFailureDiagnosticBytes:]...)
		return written, nil
	}
	overflow := len(b.data) + len(p) - maxFailureDiagnosticBytes
	if overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
	}
	b.data = append(b.data, p...)
	return written, nil
}

func (b *tailBuffer) String() string { return string(b.data) }

func diagnosticBuffer(full bool) textBuffer {
	if full {
		return &bytes.Buffer{}
	}
	return &tailBuffer{}
}

func (r *tuiProcessRunner) NotifyAgentCallAccepted(call *iexec.AgentCallAccepted) {
	r.coord.NotifyStepChange(call.Prefix)
}

func (r *tuiProcessRunner) NotifyAgentCallFinished(call *iexec.AgentCallAccepted) {
	r.coord.NotifyStepChange(call.ParentPrefix)
}

// SetStdoutWrapper sets a function that wraps the TUI stdout writer. When set,
// the wrapper filters structured output (e.g. JSONL) before display.
func (r *tuiProcessRunner) SetStdoutWrapper(fn func(w io.Writer) io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stdoutWrapper = fn
}

// SetStderrWrapper sets a function that wraps the TUI stderr writer. When set,
// the wrapper filters known diagnostics before display.
func (r *tuiProcessRunner) SetStderrWrapper(fn func(w io.Writer) io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stderrWrapper = fn
}

// SetPrefix updates the step prefix that labels output chunks and output files,
// and notifies the TUI that a new step has become active. Called by exec/shell.go
// and exec/agent.go via a type assertion before each run.
func (r *tuiProcessRunner) SetPrefix(prefix string) {
	r.mu.Lock()
	r.cancelDelayedStepLocked()
	r.stepPrefix = prefix
	r.mu.Unlock()
	r.coord.NotifyStepChange(prefix)
}

func (r *tuiProcessRunner) SetScriptPrefix(prefix string, delay time.Duration) {
	r.mu.Lock()
	r.cancelDelayedStepLocked()
	r.stepPrefix = prefix
	r.delayedStepPrefix = prefix
	r.delayedStepTimer = time.AfterFunc(delay, func() {
		r.mu.Lock()
		if r.delayedStepPrefix != prefix {
			r.mu.Unlock()
			return
		}
		r.delayedStepPrefix = ""
		r.delayedStepTimer = nil
		r.mu.Unlock()
		r.coord.NotifyStepChange(prefix)
	})
	r.mu.Unlock()
}

func (r *tuiProcessRunner) cancelDelayedStepLocked() {
	if r.delayedStepTimer != nil {
		r.delayedStepTimer.Stop()
		r.delayedStepTimer = nil
	}
	r.delayedStepPrefix = ""
}

// sanitizePrefix converts an audit-log prefix into a safe filesystem name.
// Maps '/' → "__" (nesting) and ':' → "_" (iteration). Example: audit
// prefix loop-b:2/step-c becomes
// loop-b_2__step-c. Literal underscores are percent-escaped so they cannot
// collide with the nesting separator. Any other character outside the
// allowlist [A-Za-z0-9.\-] is replaced with a single '_'. Separator replacement
// blocks path traversal on every platform (including '\' on Windows);
// the containment check in openOutputFile rejects any residual '..'
// substring.
func sanitizePrefix(prefix string) string {
	return SanitizeOutputPrefix(prefix)
}

// SanitizeOutputPrefix returns the stable filesystem-safe basename used for
// persisted stdout and stderr. Run inspection uses the same mapping to load
// execution-specific output without consulting audit metadata.
func SanitizeOutputPrefix(prefix string) string {
	return sanitizeOutputPrefix(prefix, true)
}

// LegacySanitizeOutputPrefix returns the ambiguous output basename used before
// literal underscores were escaped. It exists only so run inspection can read
// established artifacts; new output must use SanitizeOutputPrefix.
func LegacySanitizeOutputPrefix(prefix string) string {
	return sanitizeOutputPrefix(prefix, false)
}

func sanitizeOutputPrefix(prefix string, escapeLiteralUnderscores bool) string {
	var b strings.Builder
	for _, ch := range prefix {
		switch {
		case ch >= 'A' && ch <= 'Z',
			ch >= 'a' && ch <= 'z',
			ch >= '0' && ch <= '9',
			ch == '.' || ch == '-':
			b.WriteRune(ch)
		case ch == '_' && escapeLiteralUnderscores:
			b.WriteString("%5F")
		case ch == '_':
			b.WriteRune(ch)
		case ch == '/':
			b.WriteString("__")
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// openOutputFile creates an output file under
// <sessionDir>/output/<sanitizedPrefix>.<ext>. When a replay or resume reuses a
// prefix, the previous file is renamed first so neither its evidence nor writes
// from a still-exiting process are truncated. Returns nil on any error — callers
// treat a nil file as "no persistence" and continue without it.
func (r *tuiProcessRunner) openOutputFile(prefix, ext string) *os.File {
	if r.coord.sessionDir == "" || prefix == "" {
		return nil
	}
	dir := filepath.Join(r.coord.sessionDir, "output")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil
	}
	base := sanitizePrefix(prefix)
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
	archivedExisting := false
	for range 16 {
		// #nosec G304 — name is allowlist-sanitized and containment-checked above.
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if archivedExisting {
				if err := pruneOutputArchives(name); err != nil {
					r.reportOutputPersistenceWarning(prefix, err)
				}
			}
			return f
		}
		if !errors.Is(err, os.ErrExist) {
			return nil
		}

		archive := fmt.Sprintf(
			"%s.bak-%s-%d-%020d",
			name,
			time.Now().UTC().Format("20060102T150405.000000000Z"),
			os.Getpid(),
			outputArchiveSequence.Add(1),
		)
		if err := os.Rename(name, archive); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil
		}
		archivedExisting = true
	}
	return nil
}

func (r *tuiProcessRunner) reportOutputPersistenceWarning(prefix string, err error) {
	message := fmt.Sprintf("agent-runner: warning: could not prune archived output for %s: %v", prefix, err)
	r.coord.send(OutputChunkMsg{StepPrefix: prefix, Stream: "stderr", Bytes: []byte(message + "\n")})

	if r.coord.sessionDir == "" {
		return
	}
	warningPath := filepath.Join(r.coord.sessionDir, "output", "persistence-warnings.log")
	// #nosec G304 — warningPath is constructed from the internally managed session directory.
	f, openErr := os.OpenFile(warningPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if openErr != nil {
		return
	}
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339Nano), message)
	_ = f.Close()
}

func pruneOutputArchives(name string) error {
	archives, err := filepath.Glob(name + ".bak-*")
	if err != nil {
		return err
	}
	if len(archives) <= maxArchivedOutputAttempts {
		return nil
	}

	sort.Strings(archives)
	for _, archive := range archives[:len(archives)-maxArchivedOutputAttempts] {
		if err := os.Remove(archive); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// compositeWriter builds the three-way tee:
//
//	raw → io.MultiWriter(
//	       ANSIStripper → [stream wrapper?] → chunkWriter → p.Send(OutputChunkMsg),
//	       outputFile (raw bytes),
//	       *bytes.Buffer (for ProcessResult.Stdout/Stderr),
//	     )
//
// When a stream wrapper is set, it is inserted between the ANSI stripper and
// the chunk writer so adapters can filter output before display.
func (r *tuiProcessRunner) compositeWriter(stream, ext string, buf io.Writer) (w io.Writer, cleanup func()) {
	r.mu.Lock()
	scope := outputScope{prefix: r.stepPrefix, stdoutWrapper: r.stdoutWrapper, stderrWrapper: r.stderrWrapper}
	r.mu.Unlock()
	return r.compositeWriterFor(scope, stream, ext, buf)
}

func (r *tuiProcessRunner) compositeWriterFor(scope outputScope, stream, ext string, buf io.Writer) (w io.Writer, cleanup func()) {
	chunk := newChunkWriter(r.coord, scope.prefix, stream)

	var tuiTarget io.Writer = chunk
	var wrapperClosers []io.Closer
	if stream == "stdout" && scope.stdoutWrapper != nil {
		tuiTarget = scope.stdoutWrapper(chunk)
		if c, ok := tuiTarget.(io.Closer); ok {
			wrapperClosers = append(wrapperClosers, c)
		}
	}
	if stream == "stderr" && scope.stderrWrapper != nil {
		tuiTarget = scope.stderrWrapper(chunk)
		if c, ok := tuiTarget.(io.Closer); ok {
			wrapperClosers = append(wrapperClosers, c)
		}
	}
	stripped := NewANSIStripper(tuiTarget)

	f := r.openOutputFile(scope.prefix, ext)

	writers := []io.Writer{stripped}
	if f != nil {
		writers = append(writers, f)
	}
	if buf != nil {
		writers = append(writers, buf)
	}

	w = io.MultiWriter(writers...)
	cleanup = func() {
		for _, c := range wrapperClosers {
			_ = c.Close()
		}
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

	stdoutBuf := diagnosticBuffer(captureStdout)
	stderrBuf := diagnosticBuffer(false)

	stdoutW, stdoutCleanup := r.compositeWriter("stdout", "out", stdoutBuf)
	stderrW, stderrCleanup := r.compositeWriter("stderr", "err", stderrBuf)
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
// raw bytes to output files. Interactive steps bypass this path entirely and
// hand the user's terminal directly to the agent CLI.
func (r *tuiProcessRunner) RunAgent(options *iexec.AgentProcessOptions) (iexec.ProcessResult, error) {
	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	c := exec.CommandContext(ctx, options.Args[0], options.Args[1:]...) // #nosec G204
	iexec.ConfigureAgentCommand(c, options.Supervision)
	c.Env = iexec.BuildAgentEnvironment(os.Environ(), options.DropEnv, options.Env)
	if options.Workdir != "" {
		c.Dir = filepath.Clean(options.Workdir) // #nosec G304
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	scope := outputScope{prefix: options.Prefix, stdoutWrapper: options.StdoutWrapper, stderrWrapper: options.StderrWrapper}
	if options.Prefix != "" {
		r.coord.NotifyStepChange(options.Prefix)
	}
	stdoutW, stdoutCleanup := r.compositeWriterFor(scope, "stdout", "out", &stdoutBuf)
	stderrW, stderrCleanup := r.compositeWriterFor(scope, "stderr", "err", &stderrBuf)
	defer stdoutCleanup()
	defer stderrCleanup()

	c.Stdout = stdoutW
	c.Stderr = stderrW

	if err := c.Start(); err != nil {
		return iexec.ProcessResult{}, err
	}
	options.NotifyStarted()
	err := c.Wait()
	if errors.Is(err, exec.ErrWaitDelay) {
		if c.Cancel != nil {
			_ = c.Cancel()
		}
	}
	result := iexec.ProcessResult{Started: true, ExitCode: -1, Stdout: stdoutBuf.String(), Stderr: stderrBuf.String()}
	exitCode := 0
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return result, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return result, err
		}
	}

	stdout := stdoutBuf.String()
	if !options.CaptureStdout {
		stdout = ""
	}

	return iexec.ProcessResult{
		Started:  true,
		ExitCode: exitCode,
		Stdout:   stdout,
		Stderr:   stderrBuf.String(),
	}, nil
}

func (r *tuiProcessRunner) RunScript(path string, stdin []byte, captureStdout bool, workdir string) (iexec.ProcessResult, error) {
	return r.runScript(path, stdin, captureStdout, workdir, nil)
}

func (r *tuiProcessRunner) RunScriptWithEnv(path string, stdin []byte, captureStdout bool, workdir string, environment []string) (iexec.ProcessResult, error) {
	return r.runScript(path, stdin, captureStdout, workdir, environment)
}

func (r *tuiProcessRunner) runScript(path string, stdin []byte, captureStdout bool, workdir string, environment []string) (iexec.ProcessResult, error) {
	defer func() {
		r.mu.Lock()
		r.cancelDelayedStepLocked()
		r.mu.Unlock()
	}()

	c := exec.Command(path) // #nosec G204
	c.Stdin = bytes.NewReader(stdin)
	c.Env = iexec.BuildAgentEnvironment(os.Environ(), nil, append(environment, "AGENT_RUNNER_BUNDLE_DIR="+scriptBundleDir(path)))
	if workdir != "" {
		c.Dir = filepath.Clean(workdir) // #nosec G304
	}

	stdoutBuf := diagnosticBuffer(captureStdout)
	stderrBuf := diagnosticBuffer(false)
	stdoutW, stdoutCleanup := r.compositeWriter("stdout", "out", stdoutBuf)
	stderrW, stderrCleanup := r.compositeWriter("stderr", "err", stderrBuf)
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

	stdout := stdoutBuf.String()
	if !captureStdout && exitCode == 0 {
		stdout = ""
	}
	return iexec.ProcessResult{ExitCode: exitCode, Stdout: stdout, Stderr: stderrBuf.String()}, nil
}

func scriptBundleDir(scriptPath string) string {
	clean := filepath.Clean(scriptPath)
	sep := string(filepath.Separator)
	marker := sep + "bundled" + sep
	if idx := strings.Index(clean, marker); idx >= 0 {
		rest := clean[idx+len(marker):]
		if namespace, _, ok := strings.Cut(rest, sep); ok && namespace != "" {
			return clean[:idx+len(marker)+len(namespace)]
		}
	}
	return filepath.Dir(clean)
}

// Package cli provides CLI adapter abstractions for invoking different agent backends.
package cli

import (
	"bytes"
	"fmt"
	"io"
	"time"
)

// lineBufferedWriter is an io.WriteCloser that buffers input until a newline,
// then dispatches each newline-terminated line to onLine. Adapters use it to
// transform JSONL streams without re-implementing the buffer/scan/short-write
// bookkeeping. The trailing partial line (if any) is flushed on Close.
type lineBufferedWriter struct {
	downstream io.Writer
	onLine     func(line []byte) error
	buf        []byte
	err        error
}

func (f *lineBufferedWriter) Write(p []byte) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	n := len(p)
	f.buf = append(f.buf, p...)
	for {
		idx := bytes.IndexByte(f.buf, '\n')
		if idx < 0 {
			break
		}
		line := f.buf[:idx]
		f.buf = f.buf[idx+1:]
		if err := f.onLine(line); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (f *lineBufferedWriter) Close() error {
	if f.err != nil {
		return f.err
	}
	if len(f.buf) > 0 {
		if err := f.onLine(f.buf); err != nil {
			return err
		}
		f.buf = nil
	}
	return nil
}

func (f *lineBufferedWriter) writeDownstream(p []byte) error {
	n, err := f.downstream.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		f.err = err
	}
	return err
}

// BuildArgsInput provides the parameters needed to construct CLI invocation args.
type BuildArgsInput struct {
	Prompt          string
	SystemPrompt    string // Content to deliver as a system prompt (for adapters that support it)
	SessionID       string // Session ID to pass to the CLI (pre-generated for new, or existing for resume)
	Resume          bool   // True when resuming an existing session, false for fresh sessions
	Model           string
	Effort          string // Effort level (low, medium, high, xhigh) — empty means unset
	Headless        bool
	DisallowedTools []string // Tool names to block (e.g. "AskUserQuestion"); adapter translates to CLI flags where supported
}

// Adapter abstracts CLI invocation for a specific agent backend.
type Adapter interface {
	// BuildArgs constructs the full command and args for invoking the CLI.
	BuildArgs(input *BuildArgsInput) []string

	// DiscoverSessionID returns a session ID after the CLI process exits.
	// For some adapters this is deterministic (e.g. a pre-generated UUID),
	// for others it requires parsing output or scanning the filesystem.
	DiscoverSessionID(opts *DiscoverOptions) string

	// SupportsSystemPrompt reports whether this adapter can deliver content
	// as a native system prompt (e.g. via --append-system-prompt).
	SupportsSystemPrompt() bool

	// ProbeModel performs the lightest available acceptance check for a
	// model/effort pair without spawning an agent invocation.
	ProbeModel(model, effort string) (ProbeStrength, error)
}

// ProbeStrength describes how strongly ProbeModel verified a model/effort
// pair.
type ProbeStrength int

const (
	// BinaryOnly means only the CLI binary was confirmed present.
	BinaryOnly ProbeStrength = iota
	// SyntaxOnly means adapter-side syntax was checked, without consulting the CLI.
	SyntaxOnly
	// Verified means the underlying CLI accepted the pair through a probe surface.
	Verified
)

func (s ProbeStrength) String() string {
	switch s {
	case BinaryOnly:
		return "BinaryOnly"
	case SyntaxOnly:
		return "SyntaxOnly"
	case Verified:
		return "Verified"
	default:
		return fmt.Sprintf("ProbeStrength(%d)", int(s))
	}
}

// DiscoverOptions provides context for session ID discovery after a CLI process exits.
type DiscoverOptions struct {
	SpawnTime     time.Time
	PresetID      string // Pre-generated session ID (used by Claude adapter)
	ProcessOutput string // Captured stdout/stderr from the CLI process (used by Codex headless)
	Headless      bool
	Workdir       string // Effective working directory of the CLI process (for Copilot filesystem discovery)
}

// OutputFilter is an optional interface adapters may implement when the CLI
// produces structured output (e.g. JSONL) and the runner needs to extract
// the plain-text response for capture variables and display.
type OutputFilter interface {
	FilterOutput(stdout string) string
}

// HeadlessResultFilter is an optional interface adapters may implement when a
// CLI can report a non-zero exit after a completed headless turn for a known
// non-fatal bookkeeping error.
type HeadlessResultFilter interface {
	FilterHeadlessResult(exitCode int, stdout, stderr string) (filteredExitCode int, filteredStderr string)
}

// StdoutWrapper is an optional interface adapters may implement to wrap the
// stdout io.Writer used by the process runner. This allows adapters that
// produce structured output (e.g. JSONL) to filter bytes in-flight so the
// TUI displays only the plain-text response.
type StdoutWrapper interface {
	WrapStdout(downstream io.Writer) io.Writer
}

// StderrWrapper is an optional interface adapters may implement to wrap the
// stderr io.Writer used for live TUI display. Raw stderr remains available to
// process capture and output files; this only filters what the user sees live.
type StderrWrapper interface {
	WrapStderr(downstream io.Writer) io.Writer
}

// InteractiveRejector is an optional interface adapters may implement to refuse
// interactive mode at runtime with a descriptive error.
type InteractiveRejector interface {
	InteractiveModeError() error
}

// SessionStore is an optional interface adapters may implement when the
// existence of a persisted session ID can be verified (e.g. via a file on
// disk). Used by the runner to decide whether to resume an existing session
// or re-establish a fresh one with the same deterministic ID — for example
// when a session:new step's ID was flushed to state before the CLI actually
// created the session (pre-spawn crash).
type SessionStore interface {
	SessionExists(sessionID, workdir string) bool
}

// registry holds the known CLI adapters, populated at init time.
var registry = map[string]Adapter{
	"claude":   &ClaudeAdapter{},
	"codex":    &CodexAdapter{},
	"copilot":  &CopilotAdapter{},
	"cursor":   &CursorAdapter{},
	"opencode": &OpenCodeAdapter{},
}

// Get returns the adapter for the given CLI name, or an error if unknown.
func Get(name string) (Adapter, error) {
	adapter, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown CLI adapter: %q", name)
	}
	return adapter, nil
}

// KnownCLIs returns the list of registered CLI adapter names.
func KnownCLIs() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

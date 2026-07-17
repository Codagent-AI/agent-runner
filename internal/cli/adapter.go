// Package cli provides CLI adapter abstractions for invoking different agent backends.
package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/usersettings"
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
			f.err = err
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
			f.err = err
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
	Context         InvocationContext
	PermissionMode  usersettings.AutonomousPermissionMode
	DisallowedTools []string // Tool names to block (e.g. "AskUserQuestion"); adapter translates to CLI flags where supported
	// Workdir is the step's working directory ("" means the runner's own cwd).
	// Adapters use it to discover project-level CLI configuration such as
	// Cursor's <project>/.cursor/cli.json permissions.
	Workdir string
	// CompletionCommand is the in-session control client. Adapters only use
	// it when it names the absolute-path, fixed `step complete` command.
	CompletionCommand *CompletionCommand
}

// CompletionCommand describes the only runner command an interactive CLI may
// pre-approve. Keeping the executable and argv separate prevents adapters from
// accepting shell fragments supplied as a single opaque string.
type CompletionCommand struct {
	Executable string
	Args       []string
}

// Valid reports whether the descriptor is the spec-bounded completion client.
func (c CompletionCommand) Valid() bool {
	return filepath.IsAbs(c.Executable) && slices.Equal(c.Args, []string{"step", "complete"})
}

// ShellCommand renders the validated command as shell-safe words.
func (c CompletionCommand) ShellCommand() string {
	if !c.Valid() {
		return ""
	}
	parts := make([]string, 0, 1+len(c.Args))
	parts = append(parts, shellQuote(c.Executable))
	for _, arg := range c.Args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func (c CompletionCommand) hookCommand() string {
	if !c.Valid() {
		return ""
	}
	return strings.Join([]string{shellQuote(c.Executable), "internal", "turn-committed"}, " ")
}

func shellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return false
		default:
			return !strings.ContainsRune("_@%+=:,./-", r)
		}
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

type InvocationContext string

const (
	ContextInteractive           InvocationContext = "interactive"
	ContextAutonomousHeadless    InvocationContext = "autonomous-headless"
	ContextAutonomousInteractive InvocationContext = "autonomous-interactive"
)

func (c InvocationContext) IsInteractive() bool {
	return c == ContextInteractive
}

func (c InvocationContext) IsAutonomous() bool {
	return c != ContextInteractive
}

func (c InvocationContext) IsHeadless() bool {
	return c == ContextAutonomousHeadless
}

func (input *BuildArgsInput) InvocationContext() InvocationContext {
	if input.Context != "" {
		return input.Context
	}
	return ContextInteractive
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

// Checkpoint is opaque adapter-owned state captured when completion is
// accepted. Artifact identifies the native store inspected on failure.
type Checkpoint struct {
	Artifact string
	Offset   int64
	Marker   string
}

// TurnDurabilityProbe confirms that a completed assistant turn was persisted
// after completion acceptance. Implementations must use semantic records, not
// mtimes, quiet periods, or successful file writes. Both methods must honor
// the caller's context, including any subprocesses they spawn: a hung
// inspection tool must never stall completion acknowledgement or shutdown.
type TurnDurabilityProbe interface {
	Checkpoint(ctx context.Context, sessionID string) (Checkpoint, error)
	WaitForCommittedTurn(ctx context.Context, sessionID string, after Checkpoint) error
}

// ReceiptTurnDurabilityProbe is an optional extension for adapters whose
// native store records no terminal committed-turn marker. The receipt is the
// acknowledged completion request's ID, printed to stdout by the completion
// client after the control server accepted the completion. Finding the exact
// receipt persisted after the checkpoint proves the store committed the
// completion exchange (and everything before it) — causal evidence that never
// converts elapsed time into success. The durability orchestrator prefers
// this method whenever the probe implements it and a receipt is available.
type ReceiptTurnDurabilityProbe interface {
	TurnDurabilityProbe
	WaitForCommittedTurnWithReceipt(ctx context.Context, sessionID string, after Checkpoint, receipt string) error
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

// ArgsBuilderWithError is an optional interface adapters may implement when
// constructing invocation args can fail — for example when a required
// completion integration must be materialized on disk. BuildInvocationArgs
// prefers this path so a failure surfaces before the CLI is spawned instead
// of silently degrading the invocation.
type ArgsBuilderWithError interface {
	BuildArgsWithError(input *BuildArgsInput) ([]string, error)
}

// BuildInvocationArgs constructs the invocation args for an adapter,
// surfacing construction errors from adapters that implement
// ArgsBuilderWithError. Adapters without a fallible path never return an
// error.
func BuildInvocationArgs(adapter Adapter, input *BuildArgsInput) ([]string, error) {
	if builder, ok := adapter.(ArgsBuilderWithError); ok {
		return builder.BuildArgsWithError(input)
	}
	return adapter.BuildArgs(input), nil
}

// SpawnEnvSanitizer is an optional interface adapters may implement when the
// CLI changes behavior under environment variables inherited from an
// enclosing session of the same CLI. DropSpawnEnvVars returns the names of
// variables the runner must remove from the spawned process environment so
// the child runs as a clean top-level session.
type SpawnEnvSanitizer interface {
	DropSpawnEnvVars() []string
}

// DropSpawnEnvVars returns the environment variable names the adapter needs
// removed from spawned process environments, or nil for adapters without
// sanitization requirements.
func DropSpawnEnvVars(adapter Adapter) []string {
	if sanitizer, ok := adapter.(SpawnEnvSanitizer); ok {
		return sanitizer.DropSpawnEnvVars()
	}
	return nil
}

// SpawnEnvContributor is an optional interface adapters may implement when a
// spawned invocation needs process-local environment variables — for example
// a private, per-invocation configuration directory. The entries apply only
// to the spawned CLI process, never to the runner's own environment. The
// runner currently applies contributed entries to interactive-backend spawns;
// adapters must return nil for contexts that need none.
type SpawnEnvContributor interface {
	SpawnEnv(input *BuildArgsInput) ([]string, error)
}

// SpawnEnvForInvocation returns the adapter's process-local environment
// entries for an invocation, or nil for adapters without any.
func SpawnEnvForInvocation(adapter Adapter, input *BuildArgsInput) ([]string, error) {
	if contributor, ok := adapter.(SpawnEnvContributor); ok {
		return contributor.SpawnEnv(input)
	}
	return nil, nil
}

// InteractiveRejector is an optional interface adapters may implement to refuse
// interactive mode at runtime with a descriptive error.
type InteractiveRejector interface {
	InteractiveModeError() error
}

// ExecutableNamer is implemented by adapters whose logical CLI name differs
// from the binary that should be launched or probed on PATH.
type ExecutableNamer interface {
	ExecutableName() string
}

// ExecutableName returns the binary name used to launch an adapter.
func ExecutableName(logicalName string, adapter Adapter) string {
	if e, ok := adapter.(ExecutableNamer); ok {
		if name := e.ExecutableName(); name != "" {
			return name
		}
	}
	return logicalName
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

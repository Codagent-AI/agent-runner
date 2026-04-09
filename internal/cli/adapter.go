// Package cli provides CLI adapter abstractions for invoking different agent backends.
package cli

import (
	"fmt"
	"time"
)

// BuildArgsInput provides the parameters needed to construct CLI invocation args.
type BuildArgsInput struct {
	Prompt       string
	SystemPrompt string // Content to deliver as a system prompt (for adapters that support it)
	SessionID    string // Session ID to pass to the CLI (pre-generated for new, or existing for resume)
	Resume       bool   // True when resuming an existing session, false for fresh sessions
	Model        string
	Headless     bool
}

// Adapter abstracts CLI invocation for a specific agent backend.
type Adapter interface {
	// BuildArgs constructs the full command and args for invoking the CLI.
	BuildArgs(input *BuildArgsInput) []string

	// DiscoverSessionID returns a session ID after the CLI process exits.
	// For some adapters this is deterministic (e.g. a pre-generated UUID),
	// for others it requires parsing output or scanning the filesystem.
	DiscoverSessionID(opts DiscoverOptions) string

	// SupportsSystemPrompt reports whether this adapter can deliver content
	// as a native system prompt (e.g. via --append-system-prompt).
	SupportsSystemPrompt() bool
}

// DiscoverOptions provides context for session ID discovery after a CLI process exits.
type DiscoverOptions struct {
	SpawnTime     time.Time
	PresetID      string // Pre-generated session ID (used by Claude adapter)
	ProcessOutput string // Captured stdout/stderr from the CLI process (used by Codex headless)
	Headless      bool
}

// registry holds the known CLI adapters, populated at init time.
var registry = map[string]Adapter{
	"claude": &ClaudeAdapter{},
	"codex":  &CodexAdapter{},
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

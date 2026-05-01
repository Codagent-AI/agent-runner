package cli

import (
	"os"
	"path/filepath"
	"regexp"
)

// ClaudeAdapter constructs invocation args for the Claude CLI.
type ClaudeAdapter struct{}

// BuildArgs constructs Claude CLI args.
//
// Patterns:
//   - Fresh interactive:  claude --session-id <uuid> <prompt>
//   - Fresh headless:     claude --session-id <uuid> -p <prompt>
//   - Resume interactive: claude --resume <uuid> <prompt>
//   - Resume headless:    claude --resume <uuid> -p <prompt>
//   - Model override:     appends --model <m> (fresh sessions only)
//
// --session-id is reserved for fresh sessions — Claude CLI rejects it when
// the UUID already exists on disk ("Session ID ... is already in use").
// --resume works for both interactive and headless continuations.
//
// --model is intentionally omitted on resume: a Claude session keeps the
// model it was started with, and passing a different --model on resume
// would be silently ignored at best or rejected at worst. The profile's
// model is honored on fresh sessions and inherited thereafter.
func (a *ClaudeAdapter) BuildArgs(input *BuildArgsInput) []string {
	args := []string{"claude"}

	if input.SessionID != "" {
		if input.Resume {
			args = append(args, "--resume", input.SessionID)
		} else {
			args = append(args, "--session-id", input.SessionID)
		}
	}

	if input.Model != "" && !input.Resume {
		args = append(args, "--model", input.Model)
	}

	if input.Effort != "" {
		args = append(args, "--effort", input.Effort)
	}

	if input.Headless {
		args = append(args, "-p")
	}

	for _, tool := range input.DisallowedTools {
		args = append(args, "--disallowedTools", tool)
	}

	if input.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", input.SystemPrompt)
	}

	if input.Prompt != "" {
		// Use "--" to terminate flags before the positional prompt. Without
		// this, variadic flags like --disallowedTools consume the trailing
		// prompt as an additional flag value.
		args = append(args, "--", input.Prompt)
	}
	return args
}

// SupportsSystemPrompt returns true — Claude CLI supports --append-system-prompt.
func (a *ClaudeAdapter) SupportsSystemPrompt() bool {
	return true
}

func (a *ClaudeAdapter) ProbeModel(model, effort string) (ProbeStrength, error) {
	return BinaryOnly, nil
}

// DiscoverSessionID returns the pre-generated session ID.
// The Claude adapter uses a deterministic approach: the runner generates a UUID
// upfront and passes it via --session-id; the adapter returns this same UUID.
func (a *ClaudeAdapter) DiscoverSessionID(opts *DiscoverOptions) string {
	return opts.PresetID
}

var claudePathUnsafeRe = regexp.MustCompile(`[/._]`)

// SessionExists reports whether the Claude CLI has a transcript on disk for
// sessionID. Claude stores transcripts at
// ~/.claude/projects/<encoded-cwd>/<session-id>.jsonl, where the encoding
// replaces /, ., and _ with dashes. When workdir is empty the caller's
// current directory is used.
func (a *ClaudeAdapter) SessionExists(sessionID, workdir string) bool {
	if sessionID == "" {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	dir := workdir
	if dir == "" {
		dir, err = os.Getwd()
		if err != nil {
			return false
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	encoded := claudePathUnsafeRe.ReplaceAllString(abs, "-")
	path := filepath.Join(home, ".claude", "projects", encoded, sessionID+".jsonl")
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

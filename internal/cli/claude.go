package cli

// ClaudeAdapter constructs invocation args for the Claude CLI.
type ClaudeAdapter struct{}

// BuildArgs constructs Claude CLI args.
//
// Patterns:
//   - Fresh interactive:  claude --session-id <uuid> <prompt>
//   - Fresh headless:     claude --session-id <uuid> -p <prompt>
//   - Resume interactive: claude --resume <uuid> <prompt>
//   - Resume headless:    claude --session-id <uuid> -p <prompt>
//   - Model override:     appends --model <m>
//
// Headless resume uses --session-id instead of --resume because --resume
// requires a deferred tool marker in the session. When transitioning from
// an interactive step that completed normally, no marker exists and --resume
// fails. Using --session-id sends a new message to the existing session.
func (a *ClaudeAdapter) BuildArgs(input *BuildArgsInput) []string {
	args := []string{"claude"}

	if input.SessionID != "" {
		if input.Resume && !input.Headless {
			args = append(args, "--resume", input.SessionID)
		} else {
			args = append(args, "--session-id", input.SessionID)
		}
	}

	if input.Model != "" {
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
		args = append(args, input.Prompt)
	}
	return args
}

// SupportsSystemPrompt returns true — Claude CLI supports --append-system-prompt.
func (a *ClaudeAdapter) SupportsSystemPrompt() bool {
	return true
}

// DiscoverSessionID returns the pre-generated session ID.
// The Claude adapter uses a deterministic approach: the runner generates a UUID
// upfront and passes it via --session-id; the adapter returns this same UUID.
func (a *ClaudeAdapter) DiscoverSessionID(opts DiscoverOptions) string {
	return opts.PresetID
}

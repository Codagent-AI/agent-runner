package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

// CursorAdapter constructs invocation args for the Cursor agent CLI.
type CursorAdapter struct{}

// BuildArgs constructs Cursor CLI args for headless mode.
//
// Patterns:
//   - Fresh headless:  agent -p --output-format stream-json --force --trust [--model <m>] <prompt>
//   - Resume headless: agent -p --output-format stream-json --force --trust --resume=<id> <prompt>
//
// Cursor has no native system-prompt, effort, or disallowed-tools flags. Those
// inputs are intentionally ignored here. --model is omitted on resume because a
// resumed Cursor chat keeps the model it was started with.
func (a *CursorAdapter) BuildArgs(input *BuildArgsInput) []string {
	args := []string{"agent", "-p", "--output-format", "stream-json", "--force", "--trust"}

	if input.Resume && input.SessionID != "" {
		args = append(args, "--resume="+input.SessionID)
	} else if input.Model != "" {
		args = append(args, "--model", input.Model)
	}

	args = append(args, input.Prompt)
	return args
}

// SupportsSystemPrompt returns false — Cursor CLI has no native system prompt flag.
func (a *CursorAdapter) SupportsSystemPrompt() bool {
	return false
}

// DiscoverSessionID returns the session ID after a Cursor process exits by
// parsing stream-json output for the first event containing a session_id field.
func (a *CursorAdapter) DiscoverSessionID(opts *DiscoverOptions) string {
	if opts.PresetID != "" {
		return opts.PresetID
	}
	return discoverCursorSessionID(opts.ProcessOutput)
}

// InteractiveModeError returns an error indicating interactive mode is not supported.
func (a *CursorAdapter) InteractiveModeError() error {
	return fmt.Errorf("interactive mode is not supported for the cursor CLI")
}

func discoverCursorSessionID(output string) string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var event struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.SessionID != "" {
			return event.SessionID
		}
	}
	if err := scanner.Err(); err != nil {
		return ""
	}
	return ""
}

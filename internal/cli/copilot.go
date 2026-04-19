package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

// CopilotAdapter constructs invocation args for the GitHub Copilot CLI.
type CopilotAdapter struct{}

// BuildArgs constructs Copilot CLI args for headless mode.
//
// Patterns:
//   - Fresh headless:  copilot -p <prompt> --allow-all-tools --output-format json [--model <m>] [--reasoning-effort <e>]
//   - Resume headless: copilot -p <prompt> --allow-all-tools --output-format json --resume=<id>
//
// --model and --reasoning-effort are omitted on resume: a resumed copilot thread
// keeps the model and effort it was started with.
func (a *CopilotAdapter) BuildArgs(input *BuildArgsInput) []string {
	args := []string{"copilot", "-p", input.Prompt, "--allow-all-tools", "--output-format", "json"}

	resuming := input.Resume

	if !resuming {
		if input.Model != "" {
			args = append(args, "--model", input.Model)
		}
		if input.Effort != "" {
			args = append(args, "--reasoning-effort", input.Effort)
		}
	}

	if resuming && input.SessionID != "" {
		args = append(args, "--resume="+input.SessionID)
	}

	for _, tool := range input.DisallowedTools {
		if tool == "AskUserQuestion" {
			args = append(args, "--no-ask-user")
			break
		}
	}

	return args
}

// SupportsSystemPrompt returns false — Copilot CLI has no native system prompt flag.
func (a *CopilotAdapter) SupportsSystemPrompt() bool {
	return false
}

// DiscoverSessionID parses stdout as JSONL and returns the sessionId from the
// first line whose type is "result". Returns empty string if not found.
func (a *CopilotAdapter) DiscoverSessionID(opts DiscoverOptions) string {
	return discoverCopilotSession(opts.ProcessOutput)
}

// InteractiveModeError returns an error indicating interactive mode is not supported.
// This implements the optional cli.InteractiveRejector interface.
func (a *CopilotAdapter) InteractiveModeError() error {
	return fmt.Errorf("interactive mode is not supported for the copilot CLI")
}

// discoverCopilotSession parses the sessionId from the first JSONL line with type "result".
// Uses bufio.Reader.ReadBytes to avoid the fixed token limit of bufio.Scanner.
func discoverCopilotSession(output string) string {
	reader := bufio.NewReader(strings.NewReader(output))
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var event struct {
				Type      string `json:"type"`
				SessionID string `json:"sessionId"`
			}
			if jsonErr := json.Unmarshal(line, &event); jsonErr == nil {
				if event.Type == "result" && event.SessionID != "" {
					return event.SessionID
				}
			}
		}
		if err != nil {
			break
		}
	}
	return ""
}

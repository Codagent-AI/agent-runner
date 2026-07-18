package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/usersettings"
)

// ClaudeAdapter constructs invocation args for the Claude CLI.
type ClaudeAdapter struct {
	prepareCompletionPlugin func(CompletionCommand) (string, error) // test seam; nil uses prepareNextCommandPlugin
}

// BuildArgs constructs Claude CLI args.
//
// Patterns:
//   - Fresh interactive:  claude --session-id <uuid> <prompt>
//   - Fresh headless:     claude --session-id <uuid> --permission-mode acceptEdits -p --output-format stream-json --verbose <prompt>
//   - Resume interactive: claude --resume <uuid> <prompt>
//   - Resume headless:    claude --resume <uuid> -p --output-format stream-json --verbose <prompt>
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
	args, _ := a.BuildArgsWithError(input)
	return args
}

// BuildArgsWithError constructs Claude args and fails before spawn if its
// process-local completion command cannot be materialized.
func (a *ClaudeAdapter) BuildArgsWithError(input *BuildArgsInput) ([]string, error) {
	args := []string{"claude"}
	context := input.InvocationContext()

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

	if context.IsAutonomous() {
		permissionMode := "acceptEdits"
		if usersettings.EffectiveAutonomousPermissionMode(input.PermissionMode) == usersettings.PermissionModeYOLO {
			permissionMode = "bypassPermissions"
		}
		args = append(args, "--permission-mode", permissionMode)
	}

	if context.IsHeadless() {
		args = append(args, "-p", "--output-format", "stream-json", "--verbose")
	}

	for _, tool := range input.DisallowedTools {
		args = append(args, "--disallowedTools", tool)
	}

	if input.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", input.SystemPrompt)
	}

	if !context.IsHeadless() && input.CompletionCommand != nil && input.CompletionCommand.Valid() {
		command := input.CompletionCommand.ShellCommand()
		args = append(args, "--allowedTools", "Bash("+command+")")
		settings, _ := json.Marshal(map[string]any{
			"hooks": map[string]any{
				"Stop": []any{map[string]any{
					"hooks": []any{map[string]string{
						"type":    "command",
						"command": input.CompletionCommand.hookCommand(),
					}},
				}},
			},
		})
		args = append(args, "--settings", string(settings))
		prepare := a.prepareCompletionPlugin
		if prepare == nil {
			prepare = prepareNextCommandPlugin
		}
		pluginDir, err := prepare(*input.CompletionCommand)
		if err != nil {
			return nil, fmt.Errorf("claude: create completion plugin: %w", err)
		}
		args = append(args, "--plugin-dir", pluginDir)
	}

	if input.Prompt != "" {
		// Use "--" to terminate flags before the positional prompt. Without
		// this, variadic flags like --disallowedTools consume the trailing
		// prompt as an additional flag value.
		args = append(args, "--", input.Prompt)
	}
	return args, nil
}

// SupportsSystemPrompt returns true — Claude CLI supports --append-system-prompt.
func (a *ClaudeAdapter) SupportsSystemPrompt() bool {
	return true
}

func (a *ClaudeAdapter) ProbeModel(modelName, effort string) (ProbeStrength, error) {
	return BinaryOnly, nil
}

// claudeEnclosingSessionEnvVars mark a process as living inside an enclosing
// Claude Code session. A spawned CLI must not inherit them: Claude Code
// (observed on 2.1.212) treats CLAUDE_CODE_CHILD_SESSION as "this is a child
// session" and silently skips persisting the interactive session transcript,
// which breaks session resume; the other variables describe the enclosing
// session's identity, not the spawned one's. User configuration such as
// CLAUDE_CODE_USE_BEDROCK is deliberately left untouched.
var claudeEnclosingSessionEnvVars = []string{
	"CLAUDECODE",
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_SESSION_ID",
}

// DropSpawnEnvVars implements SpawnEnvSanitizer: spawned Claude processes run
// as clean top-level sessions even when the runner itself was launched from
// inside a Claude Code session.
func (a *ClaudeAdapter) DropSpawnEnvVars() []string {
	return claudeEnclosingSessionEnvVars
}

// DiscoverSessionID returns the pre-generated session ID.
// The Claude adapter uses a deterministic approach: the runner generates a UUID
// upfront and passes it via --session-id; the adapter returns this same UUID.
func (a *ClaudeAdapter) DiscoverSessionID(opts *DiscoverOptions) string {
	return opts.PresetID
}

// FilterOutput extracts the final result text from Claude stream-json output.
func (a *ClaudeAdapter) FilterOutput(stdout string) string {
	var result string
	reader := bufio.NewReader(strings.NewReader(stdout))
	for {
		line, err := reader.ReadBytes('\n')
		var event struct {
			Type   string `json:"type"`
			Result string `json:"result"`
		}
		if json.Unmarshal(line, &event) == nil && event.Type == "result" {
			result = event.Result
		}
		if err != nil {
			break
		}
	}
	return result
}

// WrapStdout forwards assistant text from Claude stream-json without replaying
// the final result event, which contains the same response.
func (a *ClaudeAdapter) WrapStdout(downstream io.Writer) io.Writer {
	filter := &claudeStreamFilter{}
	filter.downstream = downstream
	filter.onLine = filter.processLine
	return filter
}

type claudeStreamFilter struct {
	lineBufferedWriter
}

func (f *claudeStreamFilter) processLine(line []byte) error {
	text := claudeAssistantText(line)
	if text == "" {
		return nil
	}
	return f.writeDownstream([]byte(text))
}

func claudeAssistantText(line []byte) string {
	var event struct {
		Type    string `json:"type"`
		Message *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &event) != nil || event.Type != "assistant" || event.Message == nil {
		return ""
	}
	var text strings.Builder
	for _, content := range event.Message.Content {
		if content.Type == "text" {
			text.WriteString(content.Text)
		}
	}
	return text.String()
}

// ExtractUsage returns the final Claude result event's usage and reported USD
// cost. Earlier result events are superseded by the last one.
func (a *ClaudeAdapter) ExtractUsage(rawStdout string) (UsageExtraction, error) {
	var (
		modelName string
		lastUsage json.RawMessage
		lastCost  *float64
	)
	scanner := newStreamScanner(strings.NewReader(rawStdout))
	for scanner.Scan() {
		line := scanner.Bytes()
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		var event struct {
			Type         string          `json:"type"`
			Model        string          `json:"model"`
			Message      json.RawMessage `json:"message"`
			Usage        json.RawMessage `json:"usage"`
			TotalCostUSD *float64        `json:"total_cost_usd"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return UsageExtraction{}, fmt.Errorf("claude: parse stream-json: %w", err)
		}
		if event.Model != "" {
			modelName = event.Model
		}
		if len(event.Message) > 0 {
			var message struct {
				Model string `json:"model"`
			}
			if json.Unmarshal(event.Message, &message) == nil && message.Model != "" {
				modelName = message.Model
			}
		}
		if event.Type == "result" {
			lastUsage = event.Usage
			lastCost = event.TotalCostUSD
		}
	}
	if err := scanner.Err(); err != nil {
		return UsageExtraction{}, fmt.Errorf("claude: scan stream-json: %w", err)
	}
	if len(lastUsage) == 0 || string(lastUsage) == "null" {
		return UsageExtraction{
			Usage:            unavailableUsage("claude", "claude:result-event", model.UnavailableNoUsageEvent),
			EstimatedCostUSD: lastCost,
		}, nil
	}

	tokens, complete, err := tokenCountsFromObject(lastUsage, map[string]string{
		"input_tokens":                model.TokenInput,
		"cache_read_input_tokens":     model.TokenCachedInput,
		"cache_creation_input_tokens": model.TokenCacheWrite,
		"output_tokens":               model.TokenOutput,
	})
	if err != nil {
		return UsageExtraction{}, fmt.Errorf("claude: parse result usage: %w", err)
	}
	return UsageExtraction{
		Usage: model.UsageRecord{
			Status: model.UsageCollected, CLI: "claude", Provider: "anthropic", Model: modelName,
			Tokens: tokens, Source: "claude:result-event", Completeness: completeness(complete),
		},
		EstimatedCostUSD: lastCost,
	}, nil
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

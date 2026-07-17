package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/usersettings"

	"gopkg.in/yaml.v3"
)

// CopilotAdapter constructs invocation args for the GitHub Copilot CLI.
type CopilotAdapter struct {
	prepareCompletionPlugin func(CompletionCommand) (string, error) // test seam; nil uses prepareNextCommandPlugin
}

// BuildArgs constructs Copilot CLI args.
//
// Patterns:
//   - Fresh headless:      copilot -p <prompt> -s --output-format json --allow-tool=write --autopilot [--model <m>] [--reasoning-effort <e>]
//   - Resume headless:     copilot -p <prompt> -s --output-format json --allow-tool=write --autopilot --resume=<id>
//   - Fresh interactive:   copilot -i <prompt> [--model <m>] [--reasoning-effort <e>]
//   - Resume interactive:  copilot -i <prompt> --resume=<id>
//
// --allow-tool=write grants the least permission needed for autonomous
// workspace edits. --autopilot keeps the agent running until the task is complete.
// -s suppresses formatted stats while --output-format json requests the JSONL
// events needed for plain-text response and usage extraction.
// Interactive mode omits those autonomy/headless flags because a human supervises
// permissions at the terminal.
// --model and --reasoning-effort are omitted on resume: a resumed copilot thread
// keeps the model and effort it was started with.
func (a *CopilotAdapter) BuildArgs(input *BuildArgsInput) []string {
	args, _ := a.BuildArgsWithError(input)
	return args
}

// BuildArgsWithError constructs Copilot args and fails before spawn if its
// process-local completion command cannot be materialized.
func (a *CopilotAdapter) BuildArgsWithError(input *BuildArgsInput) ([]string, error) {
	args := []string{"copilot"}
	context := input.InvocationContext()
	if context.IsHeadless() {
		args = append(args, "-p", input.Prompt, "-s", "--output-format", "json")
	} else {
		args = append(args, "-i", input.Prompt)
	}
	if context.IsAutonomous() {
		args = append(args, "--allow-tool=write", "--autopilot")
		if usersettings.EffectiveAutonomousPermissionMode(input.PermissionMode) == usersettings.PermissionModeYOLO {
			args = append(args, "--allow-all-tools")
		}
	}

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

	if context.IsAutonomous() && slices.Contains(input.DisallowedTools, "AskUserQuestion") {
		args = append(args, "--no-ask-user")
	}
	if !context.IsHeadless() && input.CompletionCommand != nil && input.CompletionCommand.Valid() {
		prepare := a.prepareCompletionPlugin
		if prepare == nil {
			prepare = prepareNextCommandPlugin
		}
		pluginDir, err := prepare(*input.CompletionCommand)
		if err != nil {
			return nil, fmt.Errorf("copilot: create completion plugin: %w", err)
		}
		args = append(args, "--plugin-dir", pluginDir)
	}

	return args, nil
}

// SupportsSystemPrompt returns false — Copilot CLI has no native system prompt flag.
func (a *CopilotAdapter) SupportsSystemPrompt() bool {
	return false
}

func (a *CopilotAdapter) ProbeModel(modelName, effort string) (ProbeStrength, error) {
	return BinaryOnly, nil
}

// FilterOutput extracts the last non-empty assistant response from Copilot's
// JSONL stream.
func (a *CopilotAdapter) FilterOutput(stdout string) string {
	var result string
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var event struct {
			Type string `json:"type"`
			Data *struct {
				Content string `json:"content"`
			} `json:"data"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) == nil && event.Type == "assistant.message" && event.Data != nil && event.Data.Content != "" {
			result = event.Data.Content
		}
	}
	return result
}

// ExtractUsage sums the incremental token metrics on assistant.message
// events. Copilot's premiumRequests/cost values are AI Credits rather than
// USD, so they are intentionally not returned as cost.
func (a *CopilotAdapter) ExtractUsage(rawStdout string) (UsageExtraction, error) {
	known := map[string]string{
		"inputTokens":       model.TokenInput,
		"cachedInputTokens": model.TokenCachedInput,
		"cacheWriteTokens":  model.TokenCacheWrite,
		"outputTokens":      model.TokenOutput,
		"reasoningTokens":   model.TokenReasoning,
	}
	tokens := make(model.TokenCounts)
	seen := make(map[string]bool, len(known))
	foundUsage := false
	var modelName string

	scanner := bufio.NewScanner(strings.NewReader(rawStdout))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var event struct {
			Type string                     `json:"type"`
			Data map[string]json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return UsageExtraction{}, fmt.Errorf("copilot: parse JSONL: %w", err)
		}
		if event.Type != "assistant.message" || event.Data == nil {
			continue
		}
		if rawModel := event.Data["model"]; len(rawModel) > 0 {
			_ = json.Unmarshal(rawModel, &modelName)
		}
		tokenFields := make(map[string]json.RawMessage)
		for key, value := range event.Data {
			if strings.HasSuffix(strings.ToLower(key), "tokens") {
				tokenFields[key] = value
				if _, ok := known[key]; ok {
					seen[key] = true
				}
			}
		}
		if len(tokenFields) == 0 {
			continue
		}
		foundUsage = true
		rawTokens, err := json.Marshal(tokenFields)
		if err != nil {
			return UsageExtraction{}, fmt.Errorf("copilot: marshal token metrics: %w", err)
		}
		eventTokens, _, err := tokenCountsFromObject(rawTokens, known)
		if err != nil {
			return UsageExtraction{}, fmt.Errorf("copilot: parse token metrics: %w", err)
		}
		for category, count := range eventTokens {
			tokens[category] += count
		}
	}
	if err := scanner.Err(); err != nil {
		return UsageExtraction{}, fmt.Errorf("copilot: scan JSONL: %w", err)
	}
	if !foundUsage {
		return UsageExtraction{Usage: unavailableUsage("copilot", "copilot:assistant.message", model.UnavailableNoUsageEvent)}, nil
	}
	complete := true
	for key := range known {
		complete = complete && seen[key]
	}
	return UsageExtraction{Usage: model.UsageRecord{
		Status: model.UsageCollected, CLI: "copilot", Provider: "github", Model: modelName,
		Tokens: tokens, Source: "copilot:assistant.message", Completeness: completeness(complete),
	}}, nil
}

// DiscoverSessionID returns the session ID after a copilot process exits by
// scanning ~/.copilot/session-state/ for the most recently modified directory
// created after spawn time, matching on CWD from workspace.yaml.
func (a *CopilotAdapter) DiscoverSessionID(opts *DiscoverOptions) string {
	return discoverCopilotSession(opts.SpawnTime, opts.Workdir)
}

// discoverCopilotSession scans ~/.copilot/session-state/ for the most recently
// modified session directory created after spawnTime, matching on CWD from workspace.yaml.
// workdir is the effective CWD of the Copilot process; when empty, os.Getwd() is used.
func discoverCopilotSession(spawnTime time.Time, workdir string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	cwd := workdir
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return ""
		}
	}

	sessionStateDir := filepath.Join(home, ".copilot", "session-state")

	entries, err := os.ReadDir(sessionStateDir)
	if err != nil {
		return ""
	}

	type candidate struct {
		id      string
		modTime time.Time
	}
	var candidates []candidate

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(spawnTime) {
			continue
		}
		candidates = append(candidates, candidate{id: entry.Name(), modTime: info.ModTime()})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	var matched []string
	for _, c := range candidates {
		if matchesCopilotSessionCwd(filepath.Join(sessionStateDir, c.id), cwd) {
			matched = append(matched, c.id)
		}
	}
	if len(matched) > 1 {
		log.Printf("copilot: %d session candidates match cwd %s; using most recent — misattribution possible if concurrent sessions share this directory", len(matched), cwd)
	}
	if len(matched) > 0 {
		return matched[0]
	}

	return ""
}

type copilotWorkspace struct {
	Cwd string `yaml:"cwd"`
}

// matchesCopilotSessionCwd checks whether a copilot session directory's workspace.yaml
// matches the given working directory. Both paths are canonicalized via
// filepath.EvalSymlinks before comparison to handle symlinked directories (e.g.
// /var → /private/var on macOS).
func matchesCopilotSessionCwd(sessionDir, cwd string) bool {
	data, err := os.ReadFile(filepath.Join(sessionDir, "workspace.yaml")) // #nosec G304
	if err != nil {
		return false
	}
	var ws copilotWorkspace
	if err := yaml.Unmarshal(data, &ws); err != nil {
		return false
	}
	return canonicalize(ws.Cwd) == canonicalize(cwd)
}

// canonicalize resolves symlinks in p, falling back to filepath.Clean on error.
func canonicalize(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

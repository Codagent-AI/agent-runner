package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

// OpenCodeAdapter constructs invocation args for the OpenCode CLI.
type OpenCodeAdapter struct {
	// runDBQuery is a test seam for durability-probe database responses. The
	// production path in queryOpenCodeDB executes `opencode db` directly.
	runDBQuery func(context.Context, string) ([]byte, error)
}

// BuildArgs constructs OpenCode CLI args.
//
// Patterns:
//   - Fresh headless:      opencode run --format json [--model <m>] [--variant <e>] <prompt>
//   - Resume headless:     opencode run --format json -s <id> <prompt>
//   - Fresh interactive:   opencode --prompt <prompt> [--model <m>]
//   - Resume interactive:  opencode -s <id> --prompt <prompt>
//
// OpenCode has no native system-prompt or disallowed-tools flags. --variant
// is run-only, so interactive mode omits it. --model and --variant are omitted on resume because a resumed session
// keeps the settings it was started with.
func (a *OpenCodeAdapter) BuildArgs(input *BuildArgsInput) []string {
	args, _ := a.BuildArgsWithError(input)
	return args
}

// BuildArgsWithError constructs OpenCode args and surfaces process-local
// integration failures before the CLI can spawn.
func (a *OpenCodeAdapter) BuildArgsWithError(input *BuildArgsInput) ([]string, error) {
	invocationContext := input.InvocationContext()
	if _, err := validatedAgentCall(input); err != nil {
		return nil, fmt.Errorf("opencode: prepare agent-call integration: %w", err)
	}
	resuming := input.Resume && input.SessionID != ""
	var args []string
	if invocationContext.IsHeadless() {
		args = []string{"opencode", "run", "--format", "json"}
		if resuming {
			args = append(args, "-s", input.SessionID)
		} else {
			if input.Model != "" {
				args = append(args, "--model", input.Model)
			}
			if input.Effort != "" {
				args = append(args, "--variant", input.Effort)
			}
		}
		args = append(args, input.Prompt)
	} else {
		// Interactive invocations are normally rejected before this point via
		// InteractiveModeError; integration construction remains mode-neutral.
		args = []string{"opencode"}
		if resuming {
			args = append(args, "-s", input.SessionID)
		}
		args = append(args, "--prompt", input.Prompt)
		if !resuming && input.Model != "" {
			args = append(args, "--model", input.Model)
		}
	}

	return args, nil
}

// SpawnEnv supplies OpenCode's process-local integrations through the native
// process environment. Keeping these entries out of argv avoids depending on
// the Unix-only `env` executable and works with the runner's Windows spawn
// path as well.
func (a *OpenCodeAdapter) SpawnEnv(input *BuildArgsInput) ([]string, error) {
	invocationContext := input.InvocationContext()
	agentCall, err := validatedAgentCall(input)
	if err != nil {
		return nil, fmt.Errorf("opencode: prepare agent-call integration: %w", err)
	}
	completionEnabled := !invocationContext.IsHeadless() && input.CompletionCommand != nil && input.CompletionCommand.Valid()
	if !completionEnabled && agentCall == nil {
		return nil, nil
	}
	config := make(map[string]any)
	env := make([]string, 0, 3)
	if completionEnabled {
		command := input.CompletionCommand.ShellCommand()
		permission, _ := json.Marshal(map[string]map[string]string{"bash": {command: "allow"}})
		env = append(env, "OPENCODE_PERMISSION="+string(permission))
		config["command"] = map[string]any{
			"agent-runner:next": map[string]string{
				"description": "Complete the current Agent Runner workflow step",
				"template":    "!`" + command + "`",
			},
		}
	}
	if agentCall != nil {
		config["mcp"] = map[string]any{
			agentCallMCPServerName: map[string]any{
				"type":    "local",
				"command": agentCall.Command(),
				"enabled": true,
			},
		}
		if invocationContext.IsAutonomous() {
			config["permission"] = map[string]string{"agent-runner_call_agent": "allow"}
		}
	}
	rendered, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("opencode: encode process-local integration: %w", err)
	}
	env = append(env,
		"OPENCODE_CONFIG_CONTENT="+string(rendered),
		"OPENCODE_DISABLE_AUTOUPDATE=1",
	)
	return env, nil
}

// DropSpawnEnvVars prevents an OpenCode process launched from an enclosing
// OpenCode session from inheriting that parent's invocation-scoped MCP and
// completion integrations. SpawnEnv then adds back only those owned by the
// child invocation itself.
func (a *OpenCodeAdapter) DropSpawnEnvVars() []string {
	return []string{"OPENCODE_CONFIG_CONTENT", "OPENCODE_PERMISSION", "OPENCODE_DISABLE_AUTOUPDATE"}
}

func (a *OpenCodeAdapter) SupportsSystemPrompt() bool {
	return false
}

// InteractiveModeError declares that interactive OpenCode sessions are
// unsupported: a resumed OpenCode TUI prefills but never submits the --prompt
// supplied by the runner (anomalyco/opencode#37536, present through at least
// 1.18.3), so any workflow that resumes an interactive session stalls
// silently. Headless OpenCode is unaffected and remains fully supported.
// Remove this once the first OpenCode release containing the upstream fix is
// the supported baseline.
func (a *OpenCodeAdapter) InteractiveModeError() error {
	return errors.New("opencode does not support interactive steps: a resumed OpenCode session never submits the step prompt (anomalyco/opencode#37536), which stalls workflows silently; use autonomous (headless) mode for opencode or a different CLI for interactive steps")
}

func (a *OpenCodeAdapter) ProbeModel(modelName, effort string) (ProbeStrength, error) {
	return BinaryOnly, nil
}

func (a *OpenCodeAdapter) DiscoverSessionID(opts *DiscoverOptions) string {
	if opts.Headless {
		return discoverOpenCodeHeadlessSession(opts.ProcessOutput)
	}
	if id := discoverOpenCodeInteractiveSession(opts.SpawnTime); id != "" {
		return id
	}
	if id := discoverOpenCodeDatabaseSession(opts.SpawnTime, opts.Workdir, func(query string) ([]byte, error) {
		return exec.Command("opencode", "db", query, "--format", "json").Output() // #nosec G204 -- fixed executable; query is one argv value, not shell-expanded
	}); id != "" {
		return id
	}
	return ""
}

func (a *OpenCodeAdapter) FilterOutput(stdout string) string {
	return extractOpenCodeText(stdout)
}

func (a *OpenCodeAdapter) WrapStdout(downstream io.Writer) io.Writer {
	return newOpenCodeStreamFilter(downstream)
}

// ExtractUsage sums the per-step increments and USD costs emitted by
// step_finish events.
func (a *OpenCodeAdapter) ExtractUsage(rawStdout string) (UsageExtraction, error) {
	tokens := make(model.TokenCounts)
	complete := true
	foundUsage := false
	var totalCost float64
	foundCost := false
	canonicalTotals := model.TokenTotals{}
	totalsComplete := true
	var provider, modelName string

	scanner := newStreamScanner(strings.NewReader(rawStdout))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Part *struct {
				Tokens     json.RawMessage `json:"tokens"`
				Cost       *float64        `json:"cost"`
				ProviderID string          `json:"providerID"`
				ModelID    string          `json:"modelID"`
			} `json:"part"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return UsageExtraction{}, fmt.Errorf("opencode: parse JSONL: %w", err)
		}
		if event.Type != "step_finish" || event.Part == nil {
			continue
		}
		if event.Part.ProviderID != "" {
			provider = event.Part.ProviderID
		}
		if event.Part.ModelID != "" {
			modelName = event.Part.ModelID
		}
		if event.Part.Cost != nil {
			totalCost += *event.Part.Cost
			foundCost = true
		}
		if len(event.Part.Tokens) == 0 || string(event.Part.Tokens) == "null" {
			continue
		}
		foundUsage = true

		eventUsage, err := parseOpenCodeTokenUsage(event.Part.Tokens)
		if err != nil {
			return UsageExtraction{}, err
		}
		for category, count := range eventUsage.stepTokens {
			tokens[category] += count
		}
		for category, count := range eventUsage.cacheTokens {
			tokens[category] += count
		}
		canonicalTotals.Input += eventUsage.stepTokens[model.TokenInput] + eventUsage.cacheTokens[model.TokenCachedInput] + eventUsage.cacheTokens[model.TokenCacheWrite]
		canonicalTotals.Output += eventUsage.stepTokens[model.TokenOutput] + eventUsage.stepTokens[model.TokenReasoning]
		canonicalTotals.Total += eventUsage.reportedTotal
		complete = complete && eventUsage.complete
		totalsComplete = totalsComplete && eventUsage.totalComplete
	}
	if err := scanner.Err(); err != nil {
		return UsageExtraction{}, fmt.Errorf("opencode: scan JSONL: %w", err)
	}
	if !foundUsage {
		return UsageExtraction{Usage: unavailableUsage("opencode", "opencode:step_finish", model.UnavailableNoUsageEvent)}, nil
	}

	result := UsageExtraction{Usage: model.UsageRecord{
		Status: model.UsageCollected, CLI: "opencode", Provider: provider, Model: modelName,
		Tokens: tokens, Source: "opencode:step_finish", Completeness: completeness(complete),
	}}
	if complete && totalsComplete {
		result.Usage.TokenTotals = &canonicalTotals
	}
	if foundCost {
		result.EstimatedCostUSD = &totalCost
	}
	return result, nil
}

type openCodeTokenUsage struct {
	stepTokens    model.TokenCounts
	cacheTokens   model.TokenCounts
	reportedTotal int64
	complete      bool
	totalComplete bool
}

func parseOpenCodeTokenUsage(raw json.RawMessage) (openCodeTokenUsage, error) {
	var tokenObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &tokenObject); err != nil {
		return openCodeTokenUsage{}, fmt.Errorf("opencode: parse step_finish tokens: %w", err)
	}

	var reportedTotal int64
	rawTotal, totalPresent := tokenObject["total"]
	totalComplete := totalPresent && json.Unmarshal(rawTotal, &reportedTotal) == nil
	// total is an aggregate of the component token fields, not a disjoint
	// vendor category. Keeping it would double-count every OpenCode step.
	delete(tokenObject, "total")

	topLevelTokens, err := json.Marshal(tokenObject)
	if err != nil {
		return openCodeTokenUsage{}, fmt.Errorf("opencode: normalize step_finish tokens: %w", err)
	}
	stepTokens, topComplete, err := tokenCountsFromObject(topLevelTokens, map[string]string{
		"input": model.TokenInput, "output": model.TokenOutput, "reasoning": model.TokenReasoning,
	})
	if err != nil {
		return openCodeTokenUsage{}, fmt.Errorf("opencode: parse step_finish tokens: %w", err)
	}
	cacheTokens, cacheComplete, err := tokenCountsFromObject(tokenObject["cache"], map[string]string{
		"read": model.TokenCachedInput, "write": model.TokenCacheWrite,
	})
	if err != nil {
		return openCodeTokenUsage{}, fmt.Errorf("opencode: parse step_finish cache tokens: %w", err)
	}
	return openCodeTokenUsage{
		stepTokens: stepTokens, cacheTokens: cacheTokens, reportedTotal: reportedTotal,
		complete: topComplete && cacheComplete, totalComplete: totalComplete,
	}, nil
}

func discoverOpenCodeHeadlessSession(output string) string {
	reader := bufio.NewReader(strings.NewReader(output))
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var event struct {
				SessionID string `json:"sessionID"`
			}
			if jsonErr := json.Unmarshal(line, &event); jsonErr == nil && event.SessionID != "" {
				return event.SessionID
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("opencode: failed to read opencode session output: %v", err)
			}
			return ""
		}
	}
}

func discoverOpenCodeDatabaseSession(spawnTime time.Time, workdir string, runQuery func(string) ([]byte, error)) string {
	if workdir == "" {
		var err error
		workdir, err = os.Getwd()
		if err != nil {
			return ""
		}
	}

	escapedWorkdir := strings.ReplaceAll(workdir, "'", "''")
	query := fmt.Sprintf(
		"SELECT id, time_created FROM session WHERE time_created >= %d AND directory = '%s' ORDER BY time_created DESC",
		spawnTime.UnixMilli(), escapedWorkdir,
	)
	output, err := runQuery(query)
	if err != nil {
		return ""
	}
	var candidates []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(output, &candidates); err != nil {
		return ""
	}
	if len(candidates) > 1 {
		log.Printf("opencode: %d database sessions match spawn time and workdir; refusing to guess", len(candidates))
		return ""
	}
	if len(candidates) == 1 {
		return candidates[0].ID
	}
	return ""
}

func discoverOpenCodeInteractiveSession(spawnTime time.Time) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	pattern := filepath.Join(home, ".local", "share", "opencode", "storage", "session_diff", "ses_*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return ""
	}

	type candidate struct {
		id      string
		modTime time.Time
	}
	var candidates []candidate
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.ModTime().Before(spawnTime) {
			continue
		}
		base := filepath.Base(path)
		candidates = append(candidates, candidate{
			id:      strings.TrimSuffix(base, ".json"),
			modTime: info.ModTime(),
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	if len(candidates) > 1 {
		log.Printf("opencode: %d session candidates match spawn time; refusing to guess", len(candidates))
		return ""
	}
	if len(candidates) == 1 {
		return candidates[0].id
	}
	return ""
}

func extractOpenCodeText(output string) string {
	reader := bufio.NewReader(strings.NewReader(output))
	var text strings.Builder
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			text.WriteString(openCodeTextFromLine(line))
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("opencode: failed to read opencode output: %v", err)
			}
			break
		}
	}
	return text.String()
}

func openCodeTextFromLine(line []byte) string {
	var event struct {
		Type string `json:"type"`
		Part *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"part"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return ""
	}
	if event.Type != "text" || event.Part == nil || event.Part.Type != "text" {
		return ""
	}
	return event.Part.Text
}

type openCodeStreamFilter struct {
	lineBufferedWriter
}

func newOpenCodeStreamFilter(d io.Writer) *openCodeStreamFilter {
	f := &openCodeStreamFilter{}
	f.downstream = d
	f.onLine = f.processLine
	return f
}

func (f *openCodeStreamFilter) processLine(line []byte) error {
	text := openCodeTextFromLine(line)
	if text == "" {
		return nil
	}
	return f.writeDownstream([]byte(text))
}

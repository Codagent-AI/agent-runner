package exec

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/session"
	"github.com/codagent/agent-runner/internal/textfmt"
)

const signalFile = ".agent-runner-signal"

// ExecuteAgentStep runs an agent (Claude) step.
func ExecuteAgentStep(
	step model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	log Logger,
) (StepOutcome, error) {
	if step.Prompt == "" {
		return OutcomeFailed, nil
	}

	prefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
	startTime := time.Now()
	mode := step.Mode
	if mode == "" {
		mode = model.ModeInteractive
	}

	prompt, enrichment, err := buildAgentPrompt(step, ctx)
	if err != nil {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step, err.Error())
		return OutcomeFailed, nil
	}

	sessionID := resolveSessionID(step, ctx)

	emitAudit(ctx, audit.Event{
		Timestamp: startTime.UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data: map[string]any{
			"prompt":              prompt,
			"mode":                string(mode),
			"session_strategy":    string(step.Session),
			"resolved_session_id": sessionID,
			"model":               step.Model,
			"enrichment":          enrichment,
			"context":             contextSnapshot(ctx),
		},
	})

	args := buildAgentArgs(step, prompt, sessionID)

	log.Printf("  mode: %s\n", mode)
	if mode != model.ModeHeadless {
		log.Println("  (/continue to advance, exit to stop)")
	}
	if mode == model.ModeHeadless && os.Getenv("AGENT_RUNNER_SHOW_PROMPT") == "1" {
		for _, line := range strings.Split(prompt, "\n") {
			log.Printf("  %s\n", line)
		}
	}

	os.Remove(signalFile)

	spawnTime := time.Now()
	result, runErr := runner.RunAgent(args)
	if runErr != nil {
		return OutcomeFailed, runErr
	}

	outcome := OutcomeSuccess
	if result.ExitCode != 0 {
		outcome = OutcomeFailed
	}

	discoveredID := discoverAndStoreSession(step, ctx, spawnTime, log)

	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data: map[string]any{
			"exit_code":              result.ExitCode,
			"discovered_session_id":  discoveredID,
			"outcome":               string(outcome),
			"duration_ms":           time.Since(startTime).Milliseconds(),
		},
	})

	return outcome, nil
}

func buildAgentPrompt(step model.Step, ctx *model.ExecutionContext) (string, string, error) {
	prompt, err := textfmt.Interpolate(step.Prompt, ctx.Params, ctx.CapturedVariables)
	if err != nil {
		return "", "", err
	}

	var enrichment string
	if eng, ok := ctx.EngineRef.(engine.Engine); ok && eng != nil {
		result := eng.EnrichPrompt(step.ID, ctx.Params, engine.EnrichOptions{
			SessionStrategy: string(step.Session),
		})
		if result != "" {
			enrichment = result
			prompt = prompt + "\n\n" + enrichment
		}
	}

	return prompt, enrichment, nil
}

func buildAgentArgs(step model.Step, prompt, sessionID string) []string {
	args := []string{"claude"}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	if step.Model != "" {
		args = append(args, "--model", step.Model)
	}
	if step.Mode == model.ModeHeadless {
		args = append(args, "-p")
	}
	args = append(args, prompt)
	return args
}

func resolveSessionID(step model.Step, ctx *model.ExecutionContext) string {
	if step.Session == model.SessionResume {
		id, err := session.ResolveResumeSession(ctx)
		if err != nil {
			return ""
		}
		return id
	}
	if step.Session == model.SessionInherit {
		id, err := session.ResolveInheritSession(ctx)
		if err != nil {
			return ""
		}
		return id
	}
	return ""
}

func emitAgentFailure(ctx *model.ExecutionContext, prefix string, startTime time.Time, mode string, step model.Step, errMsg string) {
	emitAudit(ctx, audit.Event{
		Timestamp: startTime.UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepStart,
		Data: map[string]any{
			"mode":             mode,
			"session_strategy": string(step.Session),
			"context":          contextSnapshot(ctx),
		},
	})
	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data: map[string]any{
			"outcome":     "failed",
			"error":       errMsg,
			"duration_ms": time.Since(startTime).Milliseconds(),
		},
	})
}

func discoverAndStoreSession(step model.Step, ctx *model.ExecutionContext, spawnTime time.Time, log Logger) string {
	id := findConversationID(spawnTime)
	if id != "" {
		ctx.SessionIDs[step.ID] = id
		ctx.LastSessionStepID = step.ID
		log.Printf("  session: %s\n", id)
	}
	return id
}

func findConversationID(startTime time.Time) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	encodedCwd := encodeCwd(cwd)
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	projectDir := filepath.Join(home, ".claude", "projects", encodedCwd)

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return ""
	}

	type candidate struct {
		name    string
		modTime time.Time
	}
	var candidates []candidate

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(startTime) {
			continue
		}
		candidates = append(candidates, candidate{name: entry.Name(), modTime: info.ModTime()})
	}

	if len(candidates) == 0 {
		return ""
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	return strings.TrimSuffix(candidates[0].name, ".jsonl")
}

func encodeCwd(cwd string) string {
	return strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(filepath.Clean(cwd))
}

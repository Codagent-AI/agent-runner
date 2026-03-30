package exec

import (
	"os"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/session"
	"github.com/codagent/agent-runner/internal/textfmt"
)

// ExecuteAgentStep runs an agent step using the resolved CLI adapter.
func ExecuteAgentStep(
	step *model.Step,
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

	// Resolve CLI adapter (default to "claude").
	cliName := step.CLI
	if cliName == "" {
		cliName = "claude"
	}
	adapter, err := cli.Get(cliName)
	if err != nil {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step, err.Error())
		return OutcomeFailed, nil
	}

	headless := mode == model.ModeHeadless
	args := adapter.BuildArgs(cli.BuildArgsInput{
		Prompt:    prompt,
		SessionID: sessionID,
		Model:     step.Model,
		Headless:  headless,
	})

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
			"cli":                 cliName,
			"enrichment":          enrichment,
			"context":             contextSnapshot(ctx),
		},
	})

	log.Printf("  mode: %s\n", mode)
	if mode != model.ModeHeadless {
		log.Println("  (exit to stop)")
	}
	if mode == model.ModeHeadless && os.Getenv("AGENT_RUNNER_SHOW_PROMPT") == "1" {
		for _, line := range strings.Split(prompt, "\n") {
			log.Printf("  %s\n", line)
		}
	}

	spawnTime := time.Now()

	var outcome StepOutcome
	if headless {
		result, runErr := runner.RunAgent(args)
		if runErr != nil {
			return OutcomeFailed, runErr
		}
		outcome = OutcomeSuccess
		if result.ExitCode != 0 {
			outcome = OutcomeFailed
		}
	} else {
		// Interactive steps also use RunAgent; the PTY execution path
		// will be implemented by the pseudo-terminal task.
		result, runErr := runner.RunAgent(args)
		if runErr != nil {
			return OutcomeFailed, runErr
		}
		outcome = OutcomeSuccess
		if result.ExitCode != 0 {
			outcome = OutcomeAborted
		}
	}

	discoveredID := adapter.DiscoverSessionID(cli.DiscoverOptions{
		SpawnTime: spawnTime,
		PresetID:  sessionID,
		Headless:  headless,
	})
	if discoveredID != "" {
		ctx.SessionIDs[step.ID] = discoveredID
		ctx.LastSessionStepID = step.ID
		log.Printf("  session: %s\n", discoveredID)
	}

	emitAudit(ctx, audit.Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Prefix:    prefix,
		Type:      audit.EventStepEnd,
		Data: map[string]any{
			"discovered_session_id": discoveredID,
			"outcome":               string(outcome),
			"duration_ms":           time.Since(startTime).Milliseconds(),
		},
	})

	return outcome, nil
}

func buildAgentPrompt(step *model.Step, ctx *model.ExecutionContext) (prompt, enrichment string, err error) {
	prompt, err = textfmt.Interpolate(step.Prompt, ctx.Params, ctx.CapturedVariables)
	if err != nil {
		return "", "", err
	}

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

func resolveSessionID(step *model.Step, ctx *model.ExecutionContext) string {
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

func emitAgentFailure(ctx *model.ExecutionContext, prefix string, startTime time.Time, mode string, step *model.Step, errMsg string) {
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

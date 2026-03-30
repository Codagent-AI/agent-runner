package exec

import (
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

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
	mode := resolveMode(step)

	prompt, enrichment, err := buildAgentPrompt(step, ctx)
	if err != nil {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step, err.Error())
		return OutcomeFailed, nil
	}

	adapter, cliName, sessionID, isResume, err := resolveAdapterAndSession(step, ctx)
	if err != nil {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step, err.Error())
		return OutcomeFailed, nil
	}

	headless := mode == model.ModeHeadless
	args := adapter.BuildArgs(cli.BuildArgsInput{
		Prompt:    prompt,
		SessionID: sessionID,
		Resume:    isResume,
		Model:     step.Model,
		Headless:  headless,
	})

	emitAgentStart(ctx, prefix, startTime, prompt, mode, step, sessionID, cliName, enrichment)
	logAgentStep(log, mode, prompt)

	spawnTime := time.Now()
	outcome, result, runErr := runAgentProcess(runner, args, headless)
	if runErr != nil {
		return OutcomeFailed, runErr
	}

	discoveredID := discoverAndStoreSession(adapter, step, ctx, spawnTime, sessionID, headless, result.Stdout, log)

	emitAgentEnd(ctx, prefix, startTime, discoveredID, outcome)

	return outcome, nil
}

func resolveMode(step *model.Step) model.StepMode {
	if step.Mode == "" {
		return model.ModeInteractive
	}
	return step.Mode
}

// resolveAdapterAndSession returns the CLI adapter, name, session ID, and
// whether the session is a resume (vs. fresh). For fresh Claude sessions, a
// new UUID is generated so the runner knows the session ID deterministically.
func resolveAdapterAndSession(
	step *model.Step, ctx *model.ExecutionContext,
) (adapter cli.Adapter, cliName, sessionID string, isResume bool, err error) {
	cliName = step.CLI
	if cliName == "" {
		cliName = "claude"
	}
	adapter, err = cli.Get(cliName)
	if err != nil {
		return nil, cliName, "", false, err
	}

	sessionID = resolveSessionID(step, ctx)
	isResume = sessionID != ""

	// For fresh Claude sessions, generate a UUID upfront so the adapter can
	// pass it via --session-id and DiscoverSessionID can return it.
	if !isResume && cliName == "claude" {
		sessionID = uuid.New().String()
	}

	return adapter, cliName, sessionID, isResume, nil
}

func runAgentProcess(runner ProcessRunner, args []string, headless bool) (StepOutcome, ProcessResult, error) {
	// Capture stdout for headless runs so that adapters (e.g. Codex) can
	// parse session IDs from the process output.
	result, runErr := runner.RunAgent(args, headless)
	if runErr != nil {
		return OutcomeFailed, result, runErr
	}
	if headless {
		if result.ExitCode != 0 {
			return OutcomeFailed, result, nil
		}
		return OutcomeSuccess, result, nil
	}
	// Interactive: non-zero exit is treated as abort.
	if result.ExitCode != 0 {
		return OutcomeAborted, result, nil
	}
	return OutcomeSuccess, result, nil
}

func discoverAndStoreSession(
	adapter cli.Adapter,
	step *model.Step,
	ctx *model.ExecutionContext,
	spawnTime time.Time,
	presetID string,
	headless bool,
	processOutput string,
	log Logger,
) string {
	discoveredID := adapter.DiscoverSessionID(cli.DiscoverOptions{
		SpawnTime:     spawnTime,
		PresetID:      presetID,
		Headless:      headless,
		ProcessOutput: processOutput,
	})
	if discoveredID != "" {
		ctx.SessionIDs[step.ID] = discoveredID
		ctx.LastSessionStepID = step.ID
		log.Printf("  session: %s\n", discoveredID)
	}
	return discoveredID
}

func emitAgentStart(
	ctx *model.ExecutionContext,
	prefix string,
	startTime time.Time,
	prompt string,
	mode model.StepMode,
	step *model.Step,
	sessionID, cliName, enrichment string,
) {
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
}

func emitAgentEnd(ctx *model.ExecutionContext, prefix string, startTime time.Time, discoveredID string, outcome StepOutcome) {
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
}

func logAgentStep(log Logger, mode model.StepMode, prompt string) {
	log.Printf("  mode: %s\n", mode)
	if mode != model.ModeHeadless {
		log.Println("  (exit to stop)")
	}
	if mode == model.ModeHeadless && os.Getenv("AGENT_RUNNER_SHOW_PROMPT") == "1" {
		for _, line := range strings.Split(prompt, "\n") {
			log.Printf("  %s\n", line)
		}
	}
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

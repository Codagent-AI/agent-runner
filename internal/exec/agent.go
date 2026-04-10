package exec

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/pty"
	"github.com/codagent/agent-runner/internal/session"
	"github.com/codagent/agent-runner/internal/textfmt"
)

// interactiveRunnerFn runs an interactive agent step inside a PTY.
// Defaults to pty.RunInteractive; replaced in tests.
var interactiveRunnerFn = pty.RunInteractive

// completionInstruction is appended to the prompt for interactive agent steps
// so the agent knows how to signal step completion via the stdout sentinel.
const completionInstruction = "\n\nWhen you or the user determine this step is complete, continue to the next step by running the following command without any additional commentary:\nprintf '\\x1b]999;signal-continuation\\x07' > \"$AGENT_RUNNER_TTY\""

// resolveStepProfile resolves the agent profile for the given step.
// For session:new steps, it resolves from step.Agent. For resume/inherit, it
// looks up the profile name from the session-originating step.
// Step-level overrides (Mode, Model, CLI) are applied on top of the profile.
func resolveStepProfile(step *model.Step, ctx *model.ExecutionContext) (*config.ResolvedProfile, error) {
	cfg, _ := ctx.ProfileStore.(*config.Config)
	if cfg == nil {
		// No profile store — return a minimal profile using step-level values.
		return &config.ResolvedProfile{
			DefaultMode: string(step.Mode),
			CLI:         step.CLI,
			Model:       step.Model,
		}, nil
	}

	var profileName string
	if step.Session == model.SessionNew {
		profileName = step.Agent
	} else {
		// Resume/inherit: look up from session-originating step.
		profileName = ctx.SessionProfiles[ctx.LastSessionStepID]
		if profileName == "" {
			return nil, fmt.Errorf("no profile found for session-originating step %q", ctx.LastSessionStepID)
		}
	}

	resolved, err := cfg.Resolve(profileName)
	if err != nil {
		return nil, fmt.Errorf("resolving profile %q: %w", profileName, err)
	}

	// Apply step-level overrides.
	if step.Mode != "" {
		resolved.DefaultMode = string(step.Mode)
	}
	if step.Model != "" {
		resolved.Model = step.Model
	}
	if step.CLI != "" {
		resolved.CLI = step.CLI
	}

	return resolved, nil
}

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

	profile, profileErr := resolveStepProfile(step, ctx)
	if profileErr != nil {
		emitAgentFailure(ctx, prefix, startTime, "", step, profileErr.Error())
		return OutcomeFailed, nil
	}

	mode := resolveModeFromProfile(step, profile)

	prompt, enrichment, err := buildAgentPrompt(step, ctx)
	if err != nil {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step, err.Error())
		return OutcomeFailed, nil
	}

	adapter, cliName, sessionID, isResume, err := resolveAdapterAndSession(step, ctx, profile)
	if err != nil {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step, err.Error())
		return OutcomeFailed, nil
	}

	headless := mode == model.ModeHeadless

	// Build the full prompt: [system_prompt] [step prompt] [engine enrichment]
	fullPrompt := prompt
	if profile.SystemPrompt != "" {
		fullPrompt = profile.SystemPrompt + "\n\n" + fullPrompt
	}
	if enrichment != "" {
		fullPrompt = fullPrompt + "\n\n" + enrichment
	}
	if !headless {
		fullPrompt = buildStepPrefix(step.ID, ctx, isResume) + fullPrompt + completionInstruction
	}

	resolvedModel := profile.Model
	if step.Model != "" {
		resolvedModel = step.Model
	}

	input := cli.BuildArgsInput{
		SessionID: sessionID,
		Resume:    isResume,
		Model:     resolvedModel,
		Effort:    profile.Effort,
		Headless:  headless,
	}

	switch {
	case headless:
		input.Prompt = fullPrompt
	case adapter.SupportsSystemPrompt():
		input.SystemPrompt = fullPrompt
		if isResume {
			input.Prompt = fmt.Sprintf("Let's continue to the %s step", step.ID)
		} else {
			input.Prompt = fmt.Sprintf("Let's start the %s step", step.ID)
		}
	case enrichment != "" || profile.SystemPrompt != "":
		input.Prompt = "<system>\n" + fullPrompt + "\n</system>"
	default:
		input.Prompt = fullPrompt
	}

	args := adapter.BuildArgs(&input)

	emitAgentStart(ctx, prefix, startTime, prompt, mode, step, sessionID, cliName, enrichment)
	logAgentStep(log, mode, prompt)

	spawnTime := time.Now()
	outcome, result, runErr := runAgentProcess(runner, args, headless, step.Workdir, log)
	if runErr != nil {
		emitAgentEnd(ctx, prefix, startTime, "", OutcomeFailed)
		return OutcomeFailed, runErr
	}

	if headless && step.Capture != "" {
		ctx.CapturedVariables[step.Capture] = result.Stdout
	}

	discoveredID := discoverAndStoreSession(adapter, step, ctx, spawnTime, sessionID, headless, result.Stdout, log)

	// Store profile association for new sessions.
	if step.Session == model.SessionNew && step.Agent != "" && discoveredID != "" {
		ctx.SessionProfiles[step.ID] = step.Agent
	}

	emitAgentEnd(ctx, prefix, startTime, discoveredID, outcome)

	return outcome, nil
}

// resolveModeFromProfile returns the effective mode, preferring the step-level
// override, then the profile's DefaultMode, falling back to interactive.
func resolveModeFromProfile(step *model.Step, profile *config.ResolvedProfile) model.StepMode {
	if step.Mode != "" {
		return step.Mode
	}
	if profile != nil && profile.DefaultMode != "" {
		return model.StepMode(profile.DefaultMode)
	}
	return model.ModeInteractive
}

// resolveAdapterAndSession returns the CLI adapter, name, session ID, and
// whether the session is a resume (vs. fresh). For fresh Claude sessions, a
// new UUID is generated so the runner knows the session ID deterministically.
func resolveAdapterAndSession(
	step *model.Step, ctx *model.ExecutionContext, profile *config.ResolvedProfile,
) (adapter cli.Adapter, cliName, sessionID string, isResume bool, err error) {
	cliName = step.CLI
	if cliName == "" && profile != nil && profile.CLI != "" {
		cliName = profile.CLI
	}
	if cliName == "" {
		cliName = "claude"
	}
	adapter, err = cli.Get(cliName)
	if err != nil {
		return nil, cliName, "", false, err
	}

	sessionID, resolveErr := resolveSessionID(step, ctx)
	if resolveErr != nil {
		return nil, cliName, "", false, resolveErr
	}
	isResume = sessionID != ""

	// For fresh Claude sessions, generate a UUID upfront so the adapter can
	// pass it via --session-id and DiscoverSessionID can return it.
	if !isResume && cliName == "claude" {
		sessionID = uuid.New().String()
	}

	return adapter, cliName, sessionID, isResume, nil
}

func runAgentProcess(runner ProcessRunner, args []string, headless bool, workdir string, log Logger) (StepOutcome, ProcessResult, error) {
	if headless {
		// Capture stdout for headless runs so that adapters (e.g. Codex) can
		// parse session IDs from the process output.
		result, runErr := runner.RunAgent(args, true, workdir)
		if runErr != nil {
			return OutcomeFailed, result, runErr
		}
		if result.ExitCode != 0 {
			return OutcomeFailed, result, nil
		}
		// Detect AskUserQuestion failures in headless mode — these indicate
		// the agent could not complete the task autonomously. Use case-insensitive
		// matching across both stdout and stderr to handle format variations.
		combined := strings.ToLower(result.Stdout + "\n" + result.Stderr)
		if strings.Contains(combined, "askuserquestion") && strings.Contains(combined, "error") {
			log.Errorf("  headless session attempted interactive prompt (AskUserQuestion); treating as failure\n")
			return OutcomeFailed, result, nil
		}
		return OutcomeSuccess, result, nil
	}

	// Interactive: run inside a PTY with continue-trigger detection.
	ptyResult, err := interactiveRunnerFn(args, pty.Options{Workdir: workdir})
	if err != nil {
		return OutcomeFailed, ProcessResult{}, err
	}

	result := ProcessResult{ExitCode: ptyResult.ExitCode}

	if ptyResult.ContinueTriggered {
		return OutcomeSuccess, result, nil
	}

	// CLI exited without a continue trigger.
	log.Printf("\n  CLI session exited. To resume this workflow, run:\n    agent-runner --resume\n\n")
	return OutcomeAborted, result, nil
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
		for line := range strings.SplitSeq(prompt, "\n") {
			log.Printf("  %s\n", line)
		}
	}
}

// buildStepPrefix returns a preamble for interactive prompts that orients the
// agent: which step is starting and (for fresh sessions) which workflow it
// belongs to.
// buildStepPrefix returns a preamble for interactive prompts that orients the
// agent: which step is starting and (for fresh sessions) which workflow it
// belongs to.
func buildStepPrefix(stepID string, ctx *model.ExecutionContext, isResume bool) string {
	var sb strings.Builder

	switch {
	case isResume:
		fmt.Fprintf(&sb, "Now continuing to step: %q.\n\n", stepID)
	case ctx.WorkflowName != "":
		fmt.Fprintf(&sb, "You are running in the %q workflow", ctx.WorkflowName)
		if ctx.WorkflowDescription != "" {
			fmt.Fprintf(&sb, ": %s", ctx.WorkflowDescription)
		}
		fmt.Fprintf(&sb, "\n\nThe current step is %q.\n\n", stepID)
	default:
		fmt.Fprintf(&sb, "The current step is %q.\n\n", stepID)
	}

	sb.WriteString("Before doing anything else, announce that you are starting this step.\n\n")
	return sb.String()
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
		}
	}

	return prompt, enrichment, nil
}

func resolveSessionID(step *model.Step, ctx *model.ExecutionContext) (string, error) {
	if step.Session == model.SessionResume {
		id, err := session.ResolveResumeSession(ctx)
		if err != nil {
			return "", err
		}
		return id, nil
	}
	if step.Session == model.SessionInherit {
		id, err := session.ResolveInheritSession(ctx)
		if err != nil {
			return "", err
		}
		return id, nil
	}
	return "", nil
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

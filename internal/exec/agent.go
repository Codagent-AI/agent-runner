package exec

import (
	"fmt"
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

// headlessPreamble is prepended to headless prompts to reinforce autonomous behavior.
const headlessPreamble = "You are running autonomously in headless mode with no human in the loop. " +
	"Do not stop to ask for confirmation or clarification. " +
	"Do not say things like \"let me know\", \"ready when you are\", or \"shall I proceed\". " +
	"Make decisions and complete the entire task.\n\n"

// stepProfileName returns the agent profile name associated with the step's
// session strategy: the step's Agent for session:new, the declared agent for
// a named session, or the session-originating step's profile for resume/inherit.
// Returns empty when the profile cannot be determined (e.g. no prior session).
func stepProfileName(step *model.Step, ctx *model.ExecutionContext) string {
	switch {
	case step.Session == model.SessionNew:
		return step.Agent
	case model.IsNamedSession(step.Session):
		return ctx.NamedSessionDecls[string(step.Session)]
	default: // resume / inherit
		if ctx.LastSessionStepID == "" {
			return ""
		}
		return ctx.SessionProfiles[ctx.LastSessionStepID]
	}
}

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

	profileName := stepProfileName(step, ctx)
	if profileName == "" {
		if model.IsNamedSession(step.Session) {
			return nil, fmt.Errorf("no declaration found for named session %q", step.Session)
		}
		return nil, fmt.Errorf("no profile found for session-originating step %q", ctx.LastSessionStepID)
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

// prefixSetter is implemented by liverun.tuiProcessRunner. Type-asserting
// against this interface lets exec functions set the step prefix before each
// subprocess launch without importing the liverun package.
type prefixSetter interface {
	SetPrefix(string)
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
	headless := mode == model.ModeHeadless

	if step.Capture != "" && !headless {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step,
			fmt.Sprintf("capture requires headless mode, but step %q resolved to %s (check agent profile)", step.ID, mode))
		return OutcomeFailed, nil
	}

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

	input := buildAdapterInput(step, ctx, profile, adapter, prompt, enrichment, sessionID, isResume, headless)
	args := adapter.BuildArgs(&input)

	emitAgentStart(ctx, prefix, startTime, prompt, mode, step, sessionID, cliName, enrichment)

	// Set the step prefix on the process runner if it supports it (TUI mode).
	if ps, ok := runner.(prefixSetter); ok {
		ps.SetPrefix(prefix)
	}

	// Persist session bookkeeping BEFORE spawning the CLI so that if the runner
	// is killed mid-step (ctrl-c, terminal hangup, crash) resume can reconnect
	// to the session rather than orphan it. When the session ID is knowable at
	// spawn — fresh Claude (pre-generated UUID), any resume (ID carried in) —
	// we can persist it now. Fresh Codex sessions remain the exception since
	// Codex assigns the ID internally and DiscoverSessionID only succeeds after
	// the process has run; for those cases we fall back to the post-exit write
	// below.
	recordSessionOnSpawn(step, ctx, sessionID)

	spawnTime := time.Now()
	outcome, result, runErr := runAgentProcess(runner, args, headless, step.Workdir, log, ctx.SuspendHook, ctx.ResumeHook)
	if runErr != nil {
		emitAgentEnd(ctx, prefix, startTime, "", OutcomeFailed)
		return OutcomeFailed, runErr
	}

	if step.Capture != "" {
		ctx.CapturedVariables[step.Capture] = result.Stdout
	}

	// For session-originating steps (new or named), advance LastSessionStepID
	// and record the profile before discoverAndStoreSession runs, so a subsequent
	// resume/inherit step can resolve the profile even when the CLI adapter
	// discovers the session ID post-exit (e.g. Codex).
	if step.Session == model.SessionNew || model.IsNamedSession(step.Session) {
		ctx.LastSessionStepID = step.ID
		if profile := stepProfileName(step, ctx); profile != "" {
			ctx.SessionProfiles[step.ID] = profile
		}
	}

	discoveredID := discoverAndStoreSession(adapter, step, ctx, spawnTime, sessionID, headless, result.Stdout, log)

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

// buildAdapterInput assembles the full prompt and CLI input for an agent step.
func buildAdapterInput(
	step *model.Step,
	ctx *model.ExecutionContext,
	profile *config.ResolvedProfile,
	adapter cli.Adapter,
	prompt, enrichment, sessionID string,
	isResume, headless bool,
) cli.BuildArgsInput {
	// Build the full prompt: [system_prompt] [step prompt] [engine enrichment]
	fullPrompt := prompt
	if profile.SystemPrompt != "" {
		fullPrompt = profile.SystemPrompt + "\n\n" + fullPrompt
	}
	if enrichment != "" {
		fullPrompt = fullPrompt + "\n\n" + enrichment
	}
	if headless {
		fullPrompt = headlessPreamble + fullPrompt
	} else {
		fullPrompt = buildStepPrefix(step.ID, ctx, ctx.WorkflowResumed, isResume) + fullPrompt + completionInstruction
	}

	input := cli.BuildArgsInput{
		SessionID: sessionID,
		Resume:    isResume,
		Model:     profile.Model,
		Effort:    profile.Effort,
		Headless:  headless,
	}

	// Block AskUserQuestion in headless mode so the agent cannot stall
	// waiting for input. Applies to fresh and resumed headless sessions alike.
	if headless {
		input.DisallowedTools = []string{"AskUserQuestion"}
	}

	switch {
	case headless:
		input.Prompt = fullPrompt
	case adapter.SupportsSystemPrompt():
		input.SystemPrompt = fullPrompt
		switch {
		case ctx.WorkflowResumed:
			input.Prompt = fmt.Sprintf("Resume the %s step.", step.ID)
		case isResume:
			input.Prompt = fmt.Sprintf("Let's continue to the %s step", step.ID)
		default:
			input.Prompt = fmt.Sprintf("Let's start the %s step", step.ID)
		}
	case enrichment != "" || profile.SystemPrompt != "":
		input.Prompt = "<system>\n" + fullPrompt + "\n</system>"
	default:
		input.Prompt = fullPrompt
	}

	// Clear the one-shot flag after the first agent step consumes it.
	// Walk up the parent chain so child contexts (loop iterations,
	// sub-workflows) that copied the flag at creation don't re-present it
	// on subsequent iterations via a fresh child context.
	for c := ctx; c != nil; c = c.ParentContext {
		c.WorkflowResumed = false
	}

	return input
}

func runAgentProcess(runner ProcessRunner, args []string, headless bool, workdir string, log Logger, suspendHook, resumeHook func()) (StepOutcome, ProcessResult, error) {
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
		// the agent could not complete the task autonomously. Check per-line so
		// that natural-language summaries mentioning AskUserQuestion on one line
		// and an unrelated Error class on another don't trigger a false positive.
		for _, line := range strings.Split(strings.ToLower(result.Stdout+"\n"+result.Stderr), "\n") {
			if strings.Contains(line, "askuserquestion") && strings.Contains(line, "error") {
				log.Errorf("  headless session attempted interactive prompt (AskUserQuestion); treating as failure\n")
				return OutcomeFailed, result, nil
			}
		}
		return OutcomeSuccess, result, nil
	}

	// Interactive: release the terminal if a hook is set, then run inside a PTY.
	if suspendHook != nil {
		suspendHook()
	}
	ptyResult, err := interactiveRunnerFn(args, pty.Options{Workdir: workdir})
	if resumeHook != nil {
		resumeHook()
	}
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

// recordSessionOnSpawn writes session bookkeeping to ctx and flushes state
// before the agent process runs, so a kill mid-step does not orphan the
// session. It is a no-op when sessionID is empty (Codex fresh sessions discover
// the ID post-hoc).
func recordSessionOnSpawn(step *model.Step, ctx *model.ExecutionContext, sessionID string) {
	if sessionID == "" {
		return
	}
	ctx.SessionIDs[step.ID] = sessionID
	if profile := stepProfileName(step, ctx); profile != "" {
		ctx.SessionProfiles[step.ID] = profile
	}
	if model.IsNamedSession(step.Session) {
		ctx.NamedSessions[string(step.Session)] = sessionID
	}
	ctx.LastSessionStepID = step.ID
	if ctx.FlushState != nil {
		ctx.FlushState()
	}
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
		// stepProfileName reads from the pre-advance LastSessionStepID, so call
		// it before advancing below. Propagates the profile so resume after
		// workflow restart can resolve it for this step.
		if profile := stepProfileName(step, ctx); profile != "" {
			ctx.SessionProfiles[step.ID] = profile
		}
		if model.IsNamedSession(step.Session) {
			ctx.NamedSessions[string(step.Session)] = discoveredID
		}
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

// buildStepPrefix returns a preamble for interactive prompts that orients the
// agent: which step is starting and (for fresh sessions) which workflow it
// belongs to. workflowResumed is true only on the first step after a --resume
// invocation. isSessionReuse is true when the step reuses a CLI session
// (session: resume) — in that case the workflow description is omitted since
// the agent already received it.
func buildStepPrefix(stepID string, ctx *model.ExecutionContext, workflowResumed, isSessionReuse bool) string {
	var sb strings.Builder

	switch {
	case workflowResumed:
		fmt.Fprintf(&sb, "Resuming step: %q. If you already started on this step, resume from where you left off.\n\n", stepID)
	case isSessionReuse:
		fmt.Fprintf(&sb, "Continuing to step %q.\n\n", stepID)
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
	prompt, err = textfmt.Interpolate(step.Prompt, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVars())
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
	if model.IsNamedSession(step.Session) {
		// Returns empty string when the session hasn't been created yet (first use).
		return session.ResolveNamedSession(string(step.Session), ctx), nil
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

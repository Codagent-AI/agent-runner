package exec

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattn/go-isatty"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/pty"
	"github.com/codagent/agent-runner/internal/session"
	"github.com/codagent/agent-runner/internal/textfmt"
	"github.com/codagent/agent-runner/internal/usersettings"
)

// interactiveRunnerFn runs an interactive agent step inside a PTY.
// Defaults to pty.RunInteractive; replaced in tests.
var interactiveRunnerFn = pty.RunInteractive

var isStdinTerminal = func() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

// continuationMarkerPrefix begins the per-step text marker used by interactive
// agents to signal completion through the PTY output scanner.
const continuationMarkerPrefix = "AGENT_RUNNER_CONTINUE_"

// autonomyPreamble is prepended to autonomous prompts to reinforce autonomous behavior.
const autonomyPreamble = "You are running autonomously in headless mode with no human in the loop. " +
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
func resolveStepProfile(step *model.Step, ctx *model.ExecutionContext) (*config.ResolvedAgent, error) {
	cfg, _ := ctx.ProfileStore.(*config.Config)
	if cfg == nil {
		// No profile store — return a minimal profile using step-level values.
		return &config.ResolvedAgent{
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

// stdoutWrapperSetter is implemented by liverun.tuiProcessRunner. When set,
// the process runner wraps the TUI stdout writer so adapters that produce
// structured output (e.g. JSONL) can filter it before display.
type stdoutWrapperSetter interface {
	SetStdoutWrapper(func(w io.Writer) io.Writer)
}

// stderrWrapperSetter is implemented by liverun.tuiProcessRunner. When set,
// the process runner wraps the TUI stderr writer so adapters can filter known
// non-actionable diagnostics before display.
type stderrWrapperSetter interface {
	SetStderrWrapper(func(w io.Writer) io.Writer)
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
	invocationContext := cli.ContextInteractive

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

	invocationContext = resolveInvocationContext(mode, ctx, cliName, log)
	headless := invocationContext.IsHeadless()
	if errMsg := captureInvocationError(step, invocationContext); errMsg != "" {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step, errMsg)
		return OutcomeFailed, nil
	}

	if !headless {
		if r, ok := adapter.(cli.InteractiveRejector); ok {
			if modeErr := r.InteractiveModeError(); modeErr != nil {
				emitAgentFailure(ctx, prefix, startTime, string(mode), step, modeErr.Error())
				return OutcomeFailed, nil
			}
		}
	}

	continueMarker := continueMarkerForContext(invocationContext)
	input := buildAdapterInput(step, ctx, profile, adapter, prompt, enrichment, sessionID, isResume, invocationContext, continueMarker)
	args := adapter.BuildArgs(&input)
	resolvedModel := input.Model
	if resolvedModel == "" && profile != nil {
		resolvedModel = profile.Model
	}

	emitAgentStart(ctx, prefix, startTime, prompt, mode, step, sessionID, cliName, resolvedModel, enrichment)

	// Set the step prefix on the process runner if it supports it (TUI mode).
	if ps, ok := runner.(prefixSetter); ok {
		ps.SetPrefix(prefix)
	}

	cleanupOutputWrappers := configureAgentOutputWrappers(adapter, runner)
	defer cleanupOutputWrappers()

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
	debugLabel := fmt.Sprintf("workflow=%s step=%s cli=%s model=%s session=%s resume=%t",
		ctx.WorkflowName, step.ID, cliName, resolvedModel, sessionID, isResume)
	outcome, result, runErr := runAgentProcess(runner, adapter, args, headless, step.Workdir, debugLabel, continueMarker, log, ctx.SuspendHook, ctx.ResumeHook)
	if runErr != nil {
		emitAgentEnd(ctx, prefix, startTime, "", OutcomeFailed, "", result.Stderr)
		return OutcomeFailed, runErr
	}

	// When the adapter produces structured output (e.g. JSONL), extract the
	// plain-text response for capture variables and TUI display.
	filteredStdout := result.Stdout
	if f, ok := adapter.(cli.OutputFilter); ok {
		filteredStdout = f.FilterOutput(result.Stdout)
	}

	if step.Capture != "" {
		captured := strings.TrimSuffix(filteredStdout, "\r\n")
		captured = strings.TrimSuffix(captured, "\n")
		ctx.CapturedVariables[step.Capture] = model.NewCapturedString(captured)
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

	emitAgentEnd(ctx, prefix, startTime, discoveredID, outcome, filteredStdout, result.Stderr)

	return outcome, nil
}

func resolveInvocationContext(mode model.StepMode, ctx *model.ExecutionContext, cliName string, log Logger) cli.InvocationContext {
	if mode != model.ModeHeadless {
		return cli.ContextInteractive
	}

	wantsInteractive := false
	switch usersettings.AutonomousBackend(ctx.AutonomousBackend) {
	case usersettings.BackendInteractive:
		wantsInteractive = true
	case usersettings.BackendInteractiveClaude:
		wantsInteractive = cliName == "claude"
	case usersettings.BackendHeadless, "":
		wantsInteractive = false
	default:
		wantsInteractive = false
	}
	if !wantsInteractive {
		return cli.ContextAutonomousHeadless
	}
	if isStdinTerminal() {
		return cli.ContextAutonomousInteractive
	}
	if log != nil {
		log.Errorf("  autonomous backend requested interactive mode for %s, but stdin is not a TTY; falling back to headless\n", cliName)
	}
	return cli.ContextAutonomousHeadless
}

func captureInvocationError(step *model.Step, invocationContext cli.InvocationContext) string {
	if step.Capture == "" || invocationContext.IsHeadless() {
		return ""
	}
	return fmt.Sprintf("capture requires headless execution, but step %q resolved to %s", step.ID, invocationContext)
}

func configureAgentOutputWrappers(adapter cli.Adapter, runner ProcessRunner) func() {
	var cleanups []func()
	if sw, ok := adapter.(cli.StdoutWrapper); ok {
		if ws, ok2 := runner.(stdoutWrapperSetter); ok2 {
			ws.SetStdoutWrapper(sw.WrapStdout)
			cleanups = append(cleanups, func() { ws.SetStdoutWrapper(nil) })
		}
	}
	if sw, ok := adapter.(cli.StderrWrapper); ok {
		if ws, ok2 := runner.(stderrWrapperSetter); ok2 {
			ws.SetStderrWrapper(sw.WrapStderr)
			cleanups = append(cleanups, func() { ws.SetStderrWrapper(nil) })
		}
	}
	return func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
}

// ResolveAgentStepMode returns the effective mode for an agent step, accounting
// for the step-level override and the profile's DefaultMode.
func ResolveAgentStepMode(step *model.Step, ctx *model.ExecutionContext) model.StepMode {
	profile, err := resolveStepProfile(step, ctx)
	if err != nil {
		if step.Mode != "" {
			return step.Mode
		}
		return model.ModeInteractive
	}
	return resolveModeFromProfile(step, profile)
}

func resolveModeFromProfile(step *model.Step, profile *config.ResolvedAgent) model.StepMode {
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
	step *model.Step, ctx *model.ExecutionContext, profile *config.ResolvedAgent,
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

	// Re-entering a session:new step on workflow resume: the session ID was
	// persisted by recordSessionOnSpawn before the prior run aborted. Reuse
	// the persisted ID (instead of generating a fresh UUID and orphaning the
	// original) — but only for adapters that can confirm whether the session
	// actually got established on disk. When it did, attach via --resume;
	// when it didn't (pre-spawn crash), re-pass the same deterministic ID as
	// a preset so the CLI creates the session with that ID on this attempt.
	// Adapters without cli.SessionStore retain the original fresh-session
	// behavior to avoid resuming sessions whose existence can't be verified.
	if !isResume && step.Session == model.SessionNew {
		if persisted := ctx.SessionIDs[step.ID]; persisted != "" {
			if store, ok := adapter.(cli.SessionStore); ok {
				sessionID = persisted
				isResume = store.SessionExists(persisted, step.Workdir)
			}
		}
	}

	// Named sessions can hit the same pre-spawn abort case as session:new:
	// recordSessionOnSpawn flushes NamedSessions before the CLI has necessarily
	// created its transcript. If we are re-entering that same step and the
	// adapter can prove the transcript is absent, pass the persisted ID as a
	// fresh session ID instead of resuming a conversation that does not exist.
	if isResume && model.IsNamedSession(step.Session) {
		if persisted := ctx.SessionIDs[step.ID]; persisted != "" && persisted == sessionID {
			if store, ok := adapter.(cli.SessionStore); ok && !store.SessionExists(persisted, step.Workdir) {
				isResume = false
			}
		}
	}

	// For fresh Claude sessions, generate a UUID upfront so the adapter can
	// pass it via --session-id and DiscoverSessionID can return it. Skip
	// generation when sessionID is already populated — e.g. a persisted
	// session:new ID we're re-establishing after a pre-spawn crash.
	if !isResume && sessionID == "" && cliName == "claude" {
		sessionID = uuid.New().String()
	}

	return adapter, cliName, sessionID, isResume, nil
}

// buildAdapterInput assembles the full prompt and CLI input for an agent step.
func buildAdapterInput(
	step *model.Step,
	ctx *model.ExecutionContext,
	profile *config.ResolvedAgent,
	adapter cli.Adapter,
	prompt, enrichment, sessionID string,
	isResume bool,
	invocationContext cli.InvocationContext,
	continueMarker string,
) cli.BuildArgsInput {
	// Build the full prompt: [system_prompt] [step prompt] [engine enrichment]
	fullPrompt := prompt
	if profile.SystemPrompt != "" {
		fullPrompt = profile.SystemPrompt + "\n\n" + fullPrompt
	}
	if enrichment != "" {
		fullPrompt = fullPrompt + "\n\n" + enrichment
	}
	if invocationContext.IsAutonomous() {
		if !isResume {
			fullPrompt = autonomyPreamble + fullPrompt
		}
		if invocationContext == cli.ContextAutonomousInteractive {
			fullPrompt += completionInstruction(continueMarker)
		}
	} else {
		fullPrompt = buildStepPrefix(step.ID, ctx, ctx.WorkflowResumed, isResume) + fullPrompt + completionInstruction(continueMarker)
	}

	input := cli.BuildArgsInput{
		SessionID: sessionID,
		Resume:    isResume,
		Model:     profile.Model,
		Effort:    profile.Effort,
		Headless:  invocationContext.IsHeadless(),
		Context:   invocationContext,
	}

	// Block AskUserQuestion in headless mode so the agent cannot stall
	// waiting for input. Applies to fresh and resumed headless sessions alike.
	if invocationContext.IsAutonomous() {
		input.DisallowedTools = []string{"AskUserQuestion"}
	}

	switch {
	case invocationContext.IsHeadless():
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

func newContinueMarker() string {
	return continuationMarkerPrefix + strings.ReplaceAll(uuid.New().String(), "-", "")
}

func continueMarkerForContext(context cli.InvocationContext) string {
	if context.IsHeadless() {
		return ""
	}
	return newContinueMarker()
}

func completionInstruction(marker string) string {
	suffix := strings.TrimPrefix(marker, continuationMarkerPrefix)
	return "\n\nWhen you or the user determine this step is complete, continue to the next step by replying with one line containing only the current continuation marker. Construct that line by writing these pieces in this exact order with no spaces or separators: `AGENT`, `_RUNNER`, `_CONTINUE_`, and `" + suffix + "`. The line must start with `AGENT` and end with `" + suffix + "`. Do not run a shell command, use a tool, wrap it in a code block, or add any other commentary."
}

func runAgentProcess(runner ProcessRunner, adapter cli.Adapter, args []string, headless bool, workdir, debugLabel, continueMarker string, log Logger, suspendHook, resumeHook func()) (StepOutcome, ProcessResult, error) {
	if headless {
		// Capture stdout for headless runs so that adapters (e.g. Codex) can
		// parse session IDs from the process output.
		result, runErr := runner.RunAgent(args, true, workdir)
		if runErr != nil {
			return OutcomeFailed, result, runErr
		}
		if f, ok := adapter.(cli.HeadlessResultFilter); ok {
			result.ExitCode, result.Stderr = f.FilterHeadlessResult(result.ExitCode, result.Stdout, result.Stderr)
		}
		if result.ExitCode != 0 {
			return OutcomeFailed, result, nil
		}
		// Detect AskUserQuestion failures in headless mode — these indicate
		// the agent could not complete the task autonomously. Only scan
		// stderr: CLI tool-blocked errors go there, while stdout contains
		// agent natural language that may mention AskUserQuestion without
		// indicating an actual failure.
		for _, line := range strings.Split(strings.ToLower(result.Stderr), "\n") {
			if strings.Contains(line, "askuserquestion") && isToolDisallowedLine(line) {
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
	ptyResult, err := interactiveRunnerFn(args, pty.Options{Workdir: workdir, DebugLabel: debugLabel, ContinueMarker: continueMarker})
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

// isToolDisallowedLine returns true if a lowercased output line matches a
// pattern indicating the CLI blocked AskUserQuestion (e.g. "not allowed",
// "not available", "disallowed", "not permitted").
func isToolDisallowedLine(line string) bool {
	return strings.Contains(line, "not allowed") ||
		strings.Contains(line, "not available") ||
		strings.Contains(line, "not supported") ||
		strings.Contains(line, "disallowed") ||
		strings.Contains(line, "not permitted")
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
	discoveredID := adapter.DiscoverSessionID(&cli.DiscoverOptions{
		SpawnTime:     spawnTime,
		PresetID:      presetID,
		Headless:      headless,
		ProcessOutput: processOutput,
		Workdir:       step.Workdir,
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
	sessionID, cliName, resolvedModel, enrichment string,
) {
	emitStepStart(ctx, prefix, startTime, map[string]any{
		"prompt":              prompt,
		"mode":                string(mode),
		"session_strategy":    string(step.Session),
		"resolved_session_id": sessionID,
		"model":               resolvedModel,
		"cli":                 cliName,
		"enrichment":          enrichment,
	})
}

func emitAgentEnd(ctx *model.ExecutionContext, prefix string, startTime time.Time, discoveredID string, outcome StepOutcome, stdout, stderr string) {
	data := map[string]any{
		"discovered_session_id": discoveredID,
	}
	if stdout != "" {
		data["stdout"] = stdout
	}
	if stderr != "" {
		data["stderr"] = stderr
	}
	emitStepEnd(ctx, prefix, startTime, string(outcome), data)
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
	prompt, err = textfmt.InterpolateTyped(step.Prompt, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(step.ID))
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
	emitStepStart(ctx, prefix, startTime, map[string]any{
		"mode":             mode,
		"session_strategy": string(step.Session),
	})
	emitStepEnd(ctx, prefix, startTime, "failed", map[string]any{"error": errMsg})
}

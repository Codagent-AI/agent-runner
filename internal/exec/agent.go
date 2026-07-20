package exec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattn/go-isatty"

	"github.com/codagent/agent-runner/internal/agentcall"
	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/engine"
	"github.com/codagent/agent-runner/internal/interactive"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/session"
	"github.com/codagent/agent-runner/internal/textfmt"
	"github.com/codagent/agent-runner/internal/usersettings"
)

var interactiveRunnerFn = runDirectInteractive
var osExecutableFn = os.Executable

type directRunOptions struct {
	context    context.Context
	workdir    string
	invocation *directInvocation
}

type directInvocation struct {
	ctx               *model.ExecutionContext
	stepID            string
	cliName           string
	sessionID         string
	probe             cli.TurnDurabilityProbe
	spawnEnv          []string
	dropEnv           []string
	resolveSessionID  func() string
	agentCallEligible bool
	agentCallHandler  control.AgentCallHandler
}

var isStdinTerminal = func() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

// autonomyPreamble is prepended to autonomous prompts to reinforce autonomous behavior.
const autonomyPreamble = "You are running autonomously with no human in the loop. " +
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
	agentCallEligible := strings.Contains(step.Prompt, agentcall.ToolName)

	prefix := audit.BuildPrefix(nestingToAudit(ctx), step.ID)
	startTime := time.Now()
	profile, profileErr := resolveStepProfile(step, ctx)
	if profileErr != nil {
		emitAgentFailure(ctx, prefix, startTime, "", step, profileErr.Error(), log)
		return OutcomeFailed, nil
	}

	mode := resolveModeFromProfile(step, profile)

	prompt, enrichment, err := buildAgentPrompt(step, ctx)
	if err != nil {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step, err.Error(), log)
		return OutcomeFailed, nil
	}

	adapter, cliName, sessionID, isResume, err := resolveAdapterAndSession(step, ctx, profile)
	if err != nil {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step, err.Error(), log)
		return OutcomeFailed, nil
	}

	invocationContext := resolveInvocationContext(mode, ctx, cliName, step.Capture != "", log)

	if modeErr := interactiveModeError(adapter, invocationContext); modeErr != nil {
		emitAgentFailure(ctx, prefix, startTime, string(mode), step, modeErr.Error(), log)
		return OutcomeFailed, nil
	}

	args, spawnEnv, resolvedModel, argsErr := buildStepInvocation(step, ctx, profile, adapter, prompt, enrichment, sessionID, isResume, invocationContext)
	if argsErr != nil {
		emitAgentPreStartFailure(ctx, prefix, startTime, string(mode), step, argsErr.Error(), log)
		return OutcomeFailed, nil
	}

	emitAgentStart(ctx, prefix, startTime, prompt, mode, step, sessionID, cliName, resolvedModel, enrichment)

	// Bind the run-scoped endpoint before releasing the terminal lease.
	if controlErr := ensureRunnerControl(ctx, invocationContext, agentCallEligible); controlErr != nil {
		extraction := cli.UsageExtraction{Usage: defaultAgentUsage(cliName, invocationContext.IsHeadless())}
		emitAgentEnd(ctx, prefix, startTime, step, cliName, sessionID, invocationContext, isResume, false, "", OutcomeFailed, "", controlErr.Error(), &extraction, nil)
		return OutcomeFailed, controlErr
	}

	callHandler, spawnEnv, deactivate, controlErr := prepareAgentCallRuntime(
		agentCallEligible, invocationContext, step, ctx, runner, log,
		cliName, sessionID, prefix, spawnEnv,
	)
	if controlErr != nil {
		extraction := cli.UsageExtraction{Usage: defaultAgentUsage(cliName, invocationContext.IsHeadless())}
		emitAgentEnd(ctx, prefix, startTime, step, cliName, sessionID, invocationContext, isResume, false, "", OutcomeFailed, "", controlErr.Error(), &extraction, nil)
		return OutcomeFailed, controlErr
	}
	if deactivate != nil {
		defer deactivate()
	}

	// Persist knowable session IDs before spawn so interruption cannot orphan
	// the native session. CLI-assigned IDs are stored after discovery instead.
	recordSessionOnSpawn(step, ctx, sessionID)

	direct := buildWorkflowDirectInvocation(step, ctx, adapter, cliName, sessionID, spawnEnv, agentCallEligible, callHandler)
	invocationInput := buildWorkflowAgentInvocation(step, ctx, adapter, args, spawnEnv, prefix, invocationContext, cliName, resolvedModel, sessionID, isResume, log, direct)
	invocation, runErr := InvokeAgent(invocationInput, runner, log)
	if runErr != nil {
		extraction := cli.UsageExtraction{Usage: invocation.Usage, EstimatedCostUSD: invocation.EstimatedCostUSD}
		emitAgentEnd(ctx, prefix, startTime, step, cliName, sessionID, invocationContext, isResume, invocation.CLILaunched, "", invocation.Outcome, "", invocation.Stderr, &extraction, invocation.UsageError)
		return invocation.Outcome, runErr
	}

	if step.Capture != "" {
		captured := strings.TrimSuffix(invocation.Response, "\r\n")
		captured = strings.TrimSuffix(captured, "\n")
		ctx.CapturedVariables[step.Capture] = model.NewCapturedString(captured)
	}

	// Record the originating profile before post-exit session discovery.
	if step.Session == model.SessionNew || model.IsNamedSession(step.Session) {
		ctx.LastSessionStepID = step.ID
		if profile := stepProfileName(step, ctx); profile != "" {
			ctx.SessionProfiles[step.ID] = profile
		}
	}

	discoveredID := storeDiscoveredSession(step, ctx, invocation.DiscoveredSessionID, log)

	extraction := cli.UsageExtraction{Usage: invocation.Usage, EstimatedCostUSD: invocation.EstimatedCostUSD}
	emitAgentEnd(ctx, prefix, startTime, step, cliName, sessionID, invocationContext, isResume, invocation.CLILaunched, discoveredID, invocation.Outcome, invocation.Response, invocation.Stderr, &extraction, invocation.UsageError)

	return invocation.Outcome, nil
}

func buildWorkflowAgentInvocation(
	step *model.Step,
	ctx *model.ExecutionContext,
	adapter cli.Adapter,
	args, spawnEnv []string,
	prefix string,
	invocationContext cli.InvocationContext,
	cliName, resolvedModel, sessionID string,
	isResume bool,
	log Logger,
	direct *directInvocation,
) *AgentInvocation {
	return &AgentInvocation{
		Context: context.Background(), Adapter: adapter, Args: args,
		Env: spawnEnv, DropEnv: cli.DropSpawnEnvVars(adapter),
		Workdir: step.Workdir, Prefix: prefix,
		InvocationContext: invocationContext, CLI: cliName, Model: resolvedModel,
		SessionID: sessionID, SessionResumed: isResume,
		Log: log, SuspendHook: ctx.SuspendHook, ResumeHook: ctx.ResumeHook,
		direct: direct,
	}
}

func prepareAgentCallRuntime(
	eligible bool,
	invocationContext cli.InvocationContext,
	step *model.Step,
	ctx *model.ExecutionContext,
	runner ProcessRunner,
	log Logger,
	cliName, sessionID, prefix string,
	spawnEnv []string,
) (handler *AgentCallHandler, env []string, deactivate func(), err error) {
	if !eligible {
		return nil, spawnEnv, nil, nil
	}
	handler = NewAgentCallHandler(&AgentCallHandlerOptions{
		Context: ctx, Runner: runner, Log: log, Eligible: true,
		Parent: AgentCallParent{
			CLI: cliName, SessionID: sessionID,
			Workdir: step.Workdir, Prefix: prefix,
		},
	})
	if !invocationContext.IsHeadless() {
		return handler, spawnEnv, nil, nil
	}
	server, err := controlServerForContext(ctx)
	if err != nil {
		return nil, spawnEnv, nil, err
	}
	attempt := server.ActivateAttempt(context.Background(), step.ID, control.AttemptOptions{
		AgentCallEligible: true, AgentCallHandler: handler,
	})
	return handler, append(spawnEnv, attempt.Environment()...), server.Deactivate, nil
}

func buildWorkflowDirectInvocation(
	step *model.Step,
	ctx *model.ExecutionContext,
	adapter cli.Adapter,
	cliName, sessionID string,
	spawnEnv []string,
	agentCallEligible bool,
	agentCallHandler control.AgentCallHandler,
) *directInvocation {
	spawnTime := time.Now()
	probe, _ := adapter.(cli.TurnDurabilityProbe)
	return &directInvocation{
		ctx: ctx, stepID: step.ID, cliName: cliName, sessionID: sessionID, probe: probe,
		spawnEnv: spawnEnv, dropEnv: cli.DropSpawnEnvVars(adapter),
		resolveSessionID: func() string {
			return adapter.DiscoverSessionID(&cli.DiscoverOptions{SpawnTime: spawnTime, Workdir: step.Workdir})
		},
		agentCallEligible: agentCallEligible,
		agentCallHandler:  agentCallHandler,
	}
}

func interactiveModeError(adapter cli.Adapter, invocationContext cli.InvocationContext) error {
	if invocationContext.IsHeadless() {
		return nil
	}
	rejector, ok := adapter.(cli.InteractiveRejector)
	if !ok {
		return nil
	}
	return rejector.InteractiveModeError()
}

func resolveInvocationContext(mode model.StepMode, ctx *model.ExecutionContext, cliName string, hasCapture bool, log Logger) cli.InvocationContext {
	if mode != model.ModeAutonomous {
		return cli.ContextInteractive
	}

	// Captured agent output must come from a clean stdout pipe, so capture
	// currently forces headless execution even when the user selected an
	// interactive autonomous backend such as "interactive-claude".
	// TODO: Support capture for autonomous-interactive steps without relying on
	// stdout parsing, so built-in review workflows can honor interactive Claude.
	if hasCapture {
		return cli.ContextAutonomousHeadless
	}

	var wantsInteractive bool
	switch usersettings.AutonomousBackend(ctx.AutonomousBackend) {
	case usersettings.BackendInteractive:
		wantsInteractive = true
	case usersettings.BackendInteractiveClaude:
		wantsInteractive = cliName == "claude"
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

// ResolveAgentInvocationContext returns the effective adapter invocation context
// for an agent step after profile and backend settings are applied.
func ResolveAgentInvocationContext(step *model.Step, ctx *model.ExecutionContext) cli.InvocationContext {
	profile, err := resolveStepProfile(step, ctx)
	if err != nil {
		if step.Mode == model.ModeAutonomous {
			return cli.ContextAutonomousHeadless
		}
		return cli.ContextInteractive
	}
	mode := resolveModeFromProfile(step, profile)
	cliName := resolveCLIName(step, profile)
	return resolveInvocationContext(mode, ctx, cliName, step.Capture != "", nil)
}

func resolveCLIName(step *model.Step, profile *config.ResolvedAgent) string {
	if step.CLI != "" {
		return step.CLI
	}
	if profile != nil && profile.CLI != "" {
		return profile.CLI
	}
	return "claude"
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
	cliName = resolveCLIName(step, profile)
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

// buildStepInvocation constructs the CLI invocation args, the adapter's
// process-local spawn environment, and the resolved model for an agent step.
// Construction failures (e.g. a required completion integration that cannot
// be materialized) surface before the CLI is spawned.
func buildStepInvocation(
	step *model.Step,
	ctx *model.ExecutionContext,
	profile *config.ResolvedAgent,
	adapter cli.Adapter,
	prompt, enrichment, sessionID string,
	isResume bool,
	invocationContext cli.InvocationContext,
) (args, spawnEnv []string, resolvedModel string, err error) {
	completionExecutable, err := completionExecutableForContext(invocationContext)
	if err != nil {
		return nil, nil, "", fmt.Errorf("resolve completion executable: %w", err)
	}
	input := buildAdapterInput(step, ctx, profile, adapter, prompt, enrichment, sessionID, isResume, invocationContext, completionExecutable)
	if strings.Contains(step.Prompt, agentcall.ToolName) {
		executable := completionExecutable
		if executable == "" {
			executable, err = agentRunnerExecutable()
			if err != nil {
				return nil, nil, "", fmt.Errorf("resolve agent-call executable: %w", err)
			}
		}
		input.RunnerIntegration = &cli.RunnerIntegration{AgentCall: &cli.MCPServerCommand{
			Executable: executable, Args: []string{"internal", "call-agent-mcp"},
		}}
		if !input.RunnerIntegration.Valid() {
			return nil, nil, "", errors.New("agent-call invocation has an invalid Runner integration descriptor")
		}
	}
	if err := validateCompletionIntegration(&input); err != nil {
		return nil, nil, "", err
	}
	args, err = cli.BuildInvocationArgs(adapter, &input)
	if err != nil {
		return nil, nil, "", err
	}
	spawnEnv, err = cli.SpawnEnvForInvocation(adapter, &input)
	if err != nil {
		return nil, nil, "", err
	}
	resolvedModel = input.Model
	if resolvedModel == "" && profile != nil {
		resolvedModel = profile.Model
	}
	return args, spawnEnv, resolvedModel, nil
}

func buildAdapterInput(
	step *model.Step,
	ctx *model.ExecutionContext,
	profile *config.ResolvedAgent,
	adapter cli.Adapter,
	prompt, enrichment, sessionID string,
	isResume bool,
	invocationContext cli.InvocationContext,
	completionExecutable string,
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
			fullPrompt += completionInstruction(completionExecutable)
		}
	} else {
		fullPrompt = buildStepPrefix(step.ID, ctx, ctx.WorkflowResumed, isResume) + fullPrompt + completionInstruction(completionExecutable)
	}

	input := cli.BuildArgsInput{
		SessionID:      sessionID,
		Resume:         isResume,
		Model:          profile.Model,
		Effort:         profile.Effort,
		Context:        invocationContext,
		PermissionMode: usersettings.AutonomousPermissionMode(ctx.AutonomousPermissionMode),
		Workdir:        step.Workdir,
	}
	if ctx.SessionDir != "" {
		input.RunID = filepath.Base(filepath.Clean(ctx.SessionDir))
	}

	// Block AskUserQuestion in autonomous mode so the agent cannot stall
	// waiting for input. Applies to fresh and resumed autonomous sessions alike.
	if invocationContext.IsAutonomous() {
		input.DisallowedTools = []string{"AskUserQuestion"}
	}
	if completionExecutable != "" {
		input.CompletionCommand = &cli.CompletionCommand{Executable: completionExecutable, Args: []string{"step", "complete"}}
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
		if continueMarkerPromptNeedsRefresh(ctx.WorkflowResumed, isResume, invocationContext) {
			input.Prompt += completionInstruction(completionExecutable)
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

func continueMarkerPromptNeedsRefresh(workflowResumed, isResume bool, invocationContext cli.InvocationContext) bool {
	return !invocationContext.IsHeadless() && (workflowResumed || isResume)
}

func completionExecutableForContext(invocationContext cli.InvocationContext) (string, error) {
	if invocationContext.IsHeadless() {
		return "", nil
	}
	executable, err := agentRunnerExecutable()
	if err != nil {
		return "", err
	}
	if executable == "" {
		return "", errors.New("resolved executable path is empty")
	}
	return executable, nil
}

func agentRunnerExecutable() (string, error) {
	if executable := os.Getenv("AGENT_RUNNER_EXECUTABLE"); filepath.IsAbs(executable) && isExecutableFile(executable) {
		return executable, nil
	}
	executable, err := osExecutableFn()
	if err != nil {
		return "", err
	}
	if executable == "" {
		return "", errors.New("current executable path is empty")
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return "", fmt.Errorf("resolve absolute executable path: %w", err)
	}
	if !isExecutableFile(absolute) {
		return "", fmt.Errorf("current executable %q is not an executable file", absolute)
	}
	return absolute, nil
}

// isExecutableFile reports whether path is an existing, executable regular
// file. A stale or bogus inherited override would otherwise silently break
// both the completion client instruction and the watchdog executable.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path) // #nosec G703 -- the override is an operator-provided absolute path; this stat IS the validation gate before it is trusted.
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

func completionInstruction(executable string) string {
	command := cli.CompletionCommand{Executable: executable, Args: []string{"step", "complete"}}.ShellCommand()
	if command == "" {
		return ""
	}
	return "\n\nWhen you or the user determine this step is complete, signal it through the Agent Runner control channel. You MUST run the absolute path command `" + command + "` with your shell tool. The executable path and `step complete` are separate shell words; do not quote the entire command as one word. Run that exact command with no extra arguments as the final action before finishing the current response. Do not merely say that the step is complete."
}

func validateCompletionIntegration(input *cli.BuildArgsInput) error {
	if input.Context.IsHeadless() {
		if input.CompletionCommand != nil {
			return errors.New("headless invocation unexpectedly includes a completion command")
		}
		return nil
	}
	if input.CompletionCommand == nil || input.CompletionCommand.Executable == "" {
		return errors.New("interactive invocation is missing its completion command")
	}
	command := input.CompletionCommand.ShellCommand()
	if !strings.Contains(input.Prompt, command) && !strings.Contains(input.SystemPrompt, command) {
		return errors.New("interactive prompt does not include its completion command")
	}
	return nil
}

func runDirectInteractive(args []string, options directRunOptions) (interactive.DirectResult, error) {
	invocation := options.invocation
	if invocation == nil || invocation.ctx == nil {
		return interactive.DirectResult{}, errors.New("direct interactive runner: execution context is unavailable")
	}
	server, err := controlServerForContext(invocation.ctx)
	if err != nil {
		return interactive.DirectResult{}, err
	}
	executable, err := agentRunnerExecutable()
	if err != nil {
		return interactive.DirectResult{}, fmt.Errorf("resolve watchdog executable: %w", err)
	}
	direct := interactive.NewDirectRunner(&interactive.DirectOptions{
		Args: args, Workdir: options.workdir, StepID: invocation.stepID,
		SessionID: invocation.sessionID, CLI: invocation.cliName,
		Env: invocation.spawnEnv, DropEnv: invocation.dropEnv,
		Control: server, Probe: invocation.probe, ResolveSessionID: invocation.resolveSessionID, Foreground: true,
		AgentCallEligible: invocation.agentCallEligible, AgentCallHandler: invocation.agentCallHandler,
		WatchdogExecutable: executable, Logger: invocation.ctx.AuditLogger,
		Prefix: audit.BuildPrefix(nestingToAudit(invocation.ctx), invocation.stepID),
		Persist: func(metadata *interactive.ProcessMetadata) {
			setInteractiveAttempt(invocation.ctx, metadata)
			if invocation.ctx.FlushState != nil {
				invocation.ctx.FlushState()
			}
		},
	})
	ctx := options.context
	if ctx == nil {
		ctx = context.Background()
	}
	return direct.Run(ctx)
}

func controlServerForContext(ctx *model.ExecutionContext) (*control.ControlServer, error) {
	root := ctx
	for root.ParentContext != nil {
		root = root.ParentContext
	}
	if server, ok := root.Control.(*control.ControlServer); ok && server != nil {
		ctx.Control = server
		return server, nil
	}
	if root.SessionDir == "" {
		return nil, errors.New("create interactive control endpoint: session directory is unavailable")
	}
	proof, err := runlock.ProveHeld(root.SessionDir)
	if err != nil {
		return nil, err
	}
	server, err := control.NewControlServer(&control.ControlConfig{
		RunID: filepath.Base(root.SessionDir), RunDir: root.SessionDir,
		LockProof: proof, Logger: root.AuditLogger,
	})
	if err != nil {
		return nil, err
	}
	root.Control = server
	ctx.Control = server
	return server, nil
}

func ensureRunnerControl(ctx *model.ExecutionContext, invocationContext cli.InvocationContext, agentCallEligible bool) error {
	if invocationContext.IsHeadless() && !agentCallEligible {
		return nil
	}
	if ctx.SessionDir == "" {
		if agentCallEligible {
			return errors.New("create agent-call control endpoint: session directory is unavailable")
		}
		return nil
	}
	_, err := controlServerForContext(ctx)
	return err
}

func setInteractiveAttempt(ctx *model.ExecutionContext, metadata *interactive.ProcessMetadata) {
	root := ctx
	for root.ParentContext != nil {
		root = root.ParentContext
	}
	var stored *model.InteractiveAttemptMetadata
	if metadata != nil {
		stored = &model.InteractiveAttemptMetadata{
			ChildPID: metadata.ChildPID, PGID: metadata.PGID,
			StartTime: metadata.StartTime, Socket: metadata.Socket,
		}
	}
	ctx.InteractiveAttempt = stored
	root.InteractiveAttempt = stored
}

func runAgentProcess(runner ProcessRunner, adapter cli.Adapter, options *AgentProcessOptions, invocationContext cli.InvocationContext, log Logger, suspendHook, resumeHook func() error, direct *directInvocation) (StepOutcome, ProcessResult, bool, error) {
	if invocationContext.IsHeadless() {
		// Capture stdout for headless runs so that adapters (e.g. Codex) can
		// parse session IDs from the process output.
		result, runErr := runner.RunAgent(options)
		if runErr != nil {
			return OutcomeFailed, result, result.Started, runErr
		}
		if f, ok := adapter.(cli.HeadlessResultFilter); ok {
			result.ExitCode, result.Stderr = f.FilterHeadlessResult(result.ExitCode, result.Stdout, result.Stderr)
		}
		if result.ExitCode != 0 {
			return OutcomeFailed, result, true, nil
		}
		// Detect AskUserQuestion failures in autonomous mode — these indicate
		// the agent could not complete the task autonomously. Only scan
		// stderr: CLI tool-blocked errors go there, while stdout contains
		// agent natural language that may mention AskUserQuestion without
		// indicating an actual failure.
		for _, line := range strings.Split(strings.ToLower(result.Stderr), "\n") {
			if strings.Contains(line, "askuserquestion") && isToolDisallowedLine(line) {
				log.Errorf("  autonomous session attempted interactive prompt (AskUserQuestion); treating as failure\n")
				return OutcomeFailed, result, true, nil
			}
		}
		return OutcomeSuccess, result, true, nil
	}

	// Interactive: release the terminal if a hook is set, then hand it directly to the CLI.
	if suspendHook != nil {
		if err := suspendHook(); err != nil {
			return OutcomeFailed, ProcessResult{}, false, err
		}
	}
	directResult, err := interactiveRunnerFn(options.Args, directRunOptions{context: options.Context, workdir: options.Workdir, invocation: direct})
	if resumeHook != nil {
		if resumeErr := resumeHook(); err == nil && resumeErr != nil {
			err = resumeErr
		}
	}
	result := ProcessResult{ExitCode: directResult.ExitCode}
	if directResult.DurabilityFailed {
		if err == nil {
			err = directResult.DurabilityError
		}
		return OutcomeFailed, result, directResult.Started, err
	}

	if directResult.Completed {
		return OutcomeSuccess, result, true, err
	}
	if err != nil {
		return OutcomeFailed, result, directResult.Started, err
	}

	// CLI exited without a continue trigger.
	log.Printf("\n  CLI session exited. To resume this workflow, run:\n    agent-runner --resume\n\n")
	return OutcomeAborted, result, true, nil
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

func storeDiscoveredSession(
	step *model.Step,
	ctx *model.ExecutionContext,
	discoveredID string,
	log Logger,
) string {
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

func emitAgentEnd(
	ctx *model.ExecutionContext,
	prefix string,
	startTime time.Time,
	step *model.Step,
	cliName, sessionID string,
	invocationContext cli.InvocationContext,
	sessionResumed bool,
	agentInvoked bool,
	discoveredID string,
	outcome StepOutcome,
	stdout, stderr string,
	extraction *cli.UsageExtraction,
	usageErr error,
) {
	resolvedSessionID := discoveredID
	if resolvedSessionID == "" {
		resolvedSessionID = sessionID
	}
	identity := executionIdentity(ctx, step, "step", 0, agentInvoked, cliName, resolvedSessionID)
	identity.SessionResumed = sessionResumed
	data := map[string]any{
		"discovered_session_id":  discoveredID,
		"identity":               identity,
		"usage":                  extraction.Usage,
		"estimated_api_cost_usd": extraction.EstimatedCostUSD,
	}
	if stdout != "" {
		data["stdout"] = stdout
	}
	if stderr != "" {
		data["stderr"] = stderr
	}
	if usageErr != nil {
		data["usage_error"] = usageErr.Error()
	}
	emitStepEnd(ctx, prefix, startTime, string(outcome), data, step)
}

func extractAgentUsage(adapter cli.Adapter, cliName string, invocationContext cli.InvocationContext, rawStdout string) (cli.UsageExtraction, error) {
	if !invocationContext.IsHeadless() {
		return cli.UsageExtraction{Usage: defaultAgentUsage(cliName, false)}, nil
	}
	extractor, ok := adapter.(cli.UsageExtractor)
	if !ok {
		return cli.UsageExtraction{Usage: defaultAgentUsage(cliName, true)}, nil
	}
	extraction, err := extractor.ExtractUsage(rawStdout)
	if err != nil {
		return cli.UsageExtraction{Usage: model.UsageRecord{
			Status: model.UsageUnavailable, Reason: model.UnavailableParseFailure,
			CLI: cliName, Source: "agent-runner",
		}}, fmt.Errorf("%s usage extraction: %w", cliName, err)
	}
	return extraction, nil
}

func defaultAgentUsage(cliName string, headless bool) model.UsageRecord {
	reason := model.UnavailableUnsupportedAdapter
	if !headless {
		reason = model.UnavailablePTYContext
	}
	return model.UsageRecord{
		Status: model.UsageUnavailable, Reason: reason, CLI: cliName, Source: "agent-runner",
	}
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

// emitAgentFailure records a pre-spawn agent step failure in the audit log and
// surfaces the reason on the step logger — without the latter, the console only
// shows "step failed" while the actual cause is buried in audit.log.
func emitAgentFailure(ctx *model.ExecutionContext, prefix string, startTime time.Time, mode string, step *model.Step, errMsg string, log Logger) {
	if log != nil {
		log.Errorf("agent-runner: step %q: %s\n", step.ID, errMsg)
	}
	emitStepStart(ctx, prefix, startTime, map[string]any{
		"mode":             mode,
		"session_strategy": string(step.Session),
	})
	emitAgentFailureEnd(ctx, prefix, startTime, step, mode, errMsg)
}

// emitAgentPreStartFailure records an invocation-construction failure without
// claiming that the step started. Resolution and adapter construction happen
// before emitAgentStart, so the terminal record is sufficient for diagnostics
// and correctly reports that no agent process was invoked.
func emitAgentPreStartFailure(ctx *model.ExecutionContext, prefix string, startTime time.Time, mode string, step *model.Step, errMsg string, log Logger) {
	if log != nil {
		log.Errorf("agent-runner: step %q: %s\n", step.ID, errMsg)
	}
	emitAgentFailureEnd(ctx, prefix, startTime, step, mode, errMsg)
}

func emitAgentFailureEnd(ctx *model.ExecutionContext, prefix string, startTime time.Time, step *model.Step, mode, errMsg string) {
	cliName := step.CLI
	if cliName == "" {
		cliName = "claude"
	}
	headless := mode != string(model.ModeInteractive)
	emitStepEnd(ctx, prefix, startTime, "failed", map[string]any{
		"error":                  errMsg,
		"identity":               executionIdentity(ctx, step, "step", 0, false, cliName, ""),
		"usage":                  defaultAgentUsage(cliName, headless),
		"estimated_api_cost_usd": (*float64)(nil),
	}, step)
}

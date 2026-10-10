package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/model"
)

// AgentInvocation contains the resolved inputs for one agent CLI invocation.
// Workflow-step and agent-call wrappers resolve their own policy and lifecycle
// concerns before using this shared execution core.
type AgentInvocation struct {
	Context context.Context
	Adapter cli.Adapter
	Args    []string
	Env     []string
	DropEnv []string
	Workdir string
	Prefix  string
	// OutputCopySuffix keeps a separate raw-output copy for this invocation.
	OutputCopySuffix string

	StdoutWrapper func(io.Writer) io.Writer
	StderrWrapper func(io.Writer) io.Writer
	Supervision   AgentProcessSupervision

	InvocationContext cli.InvocationContext
	CLI               string
	Model             string
	Effort            string
	SessionID         string
	SessionResumed    bool

	Log         Logger
	SuspendHook func() error
	ResumeHook  func() error
	OnStarted   func()
	direct      *directInvocation
	Now         func() time.Time
}

// AgentInvocationResult is reusable execution evidence. It deliberately does
// not contain workflow-step audit, capture, or state-transition semantics.
type AgentInvocationResult struct {
	Outcome    StepOutcome
	Response   string
	Stdout     string
	Stderr     string
	ExitCode   int
	Crashed    bool
	CrashError string

	CLI                 string
	Model               string
	SessionID           string
	DiscoveredSessionID string
	SessionResumed      bool

	Usage                 model.UsageRecord
	EstimatedCostUSD      *float64
	CostUnavailableReason model.UnavailableReason
	CostReportError       string
	UsageError            error

	StartedAt   time.Time
	FinishedAt  time.Time
	Duration    time.Duration
	CLILaunched bool
}

// InvokeAgent executes one resolved agent invocation and returns typed output,
// identity, session-discovery, usage, cost, timing, and launch evidence.
//
//nolint:funlen // Invocation assembles one process result and its usage evidence.
func InvokeAgent(input *AgentInvocation, runner ProcessRunner, fallbackLog Logger) (AgentInvocationResult, error) {
	now := input.Now
	if now == nil {
		now = time.Now
	}
	startedAt := now()
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	log := input.Log
	if log == nil {
		log = fallbackLog
	}
	stdoutWrapper := input.StdoutWrapper
	if stdoutWrapper == nil {
		if wrapper, ok := input.Adapter.(cli.StdoutWrapper); ok {
			stdoutWrapper = wrapper.WrapStdout
		}
	}
	if watcher, ok := input.Adapter.(cli.HeadlessStreamWatch); ok && input.InvocationContext.IsHeadless() {
		watchCtx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)
		ctx = watchCtx
		inner := stdoutWrapper
		stdoutWrapper = func(w io.Writer) io.Writer {
			next := w
			if inner != nil {
				next = inner(w)
			}
			return watcher.WatchHeadlessStream(next, func() {
				cancel(cli.ErrCursorResultStall)
			})
		}
	}
	stderrWrapper := input.StderrWrapper
	if stderrWrapper == nil {
		if wrapper, ok := input.Adapter.(cli.StderrWrapper); ok {
			stderrWrapper = wrapper.WrapStderr
		}
	}
	supervision := input.Supervision
	if !supervision.ProcessGroup {
		supervision.ProcessGroup = true
	}
	dropEnv := append([]string(nil), input.DropEnv...)
	dropEnv = append(dropEnv, control.EnvironmentVariables()...)
	processOptions := AgentProcessOptions{
		Context: ctx, Args: input.Args, CaptureStdout: true,
		Env: input.Env, DropEnv: dropEnv, Workdir: input.Workdir,
		Prefix: input.Prefix, OutputCopySuffix: input.OutputCopySuffix, StdoutWrapper: stdoutWrapper,
		StderrWrapper: stderrWrapper, Supervision: supervision, OnStarted: input.OnStarted, startedOnce: &sync.Once{},
	}
	direct := input.direct
	if direct != nil {
		invocationCopy := *direct
		invocationCopy.spawnEnv = append([]string(nil), input.Env...)
		invocationCopy.dropEnv = append([]string(nil), dropEnv...)
		invocationCopy.onStarted = processOptions.NotifyStarted
		direct = &invocationCopy
	}
	usageContext := cli.UsageContext{Workdir: input.Workdir, Env: BuildAgentEnvironment(os.Environ(), dropEnv, input.Env)}
	prepareInvocationUsage(input, &processOptions, direct, &usageContext)
	outcome, processResult, launched, crashed, runErr := runAgentProcess(
		runner, input.Adapter, &processOptions, input.InvocationContext, log,
		input.SuspendHook, input.ResumeHook, direct,
	)
	if launched {
		processOptions.NotifyStarted()
	}
	if cause := context.Cause(ctx); errors.Is(cause, cli.ErrCursorResultStall) {
		runErr = cause
		outcome = OutcomeFailed
		crashed = true
	}
	extraction, usageErr := extractAgentUsage(input.Adapter, input.CLI, input.InvocationContext, processResult.Stdout, usageContext)
	attachInvocationIdentity(&extraction.Usage, input.CLI, input.Model, input.Effort)
	result := AgentInvocationResult{
		Outcome: outcome, Stdout: processResult.Stdout, Stderr: processResult.Stderr,
		Crashed:  crashed,
		ExitCode: processResult.ExitCode, CLI: input.CLI, Model: input.Model,
		SessionID: input.SessionID, SessionResumed: input.SessionResumed,
		Usage: extraction.Usage, EstimatedCostUSD: extraction.EstimatedCostUSD,
		CostUnavailableReason: extraction.CostUnavailableReason,
		CostReportError:       extraction.CostReportError,
		UsageError:            usageErr, StartedAt: startedAt, CLILaunched: launched,
	}
	if runErr != nil {
		result.CrashError = runErr.Error()
	}
	if runErr == nil {
		result.Response = processResult.Stdout
		if filter, ok := input.Adapter.(cli.OutputFilter); ok {
			result.Response = filter.FilterOutput(processResult.Stdout)
		}
	}
	if launched {
		result.DiscoveredSessionID = discoverInvocationSessionID(input, startedAt, processResult.Stdout)
	}
	result.FinishedAt = now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt)
	return result, runErr
}

func discoverInvocationSessionID(input *AgentInvocation, startedAt time.Time, stdout string) string {
	if input == nil || input.Adapter == nil {
		return ""
	}
	opts := &cli.DiscoverOptions{
		SpawnTime: startedAt, PresetID: input.SessionID,
		Headless:      input.InvocationContext.IsHeadless(),
		ProcessOutput: stdout, Workdir: input.Workdir,
	}
	if input.direct != nil {
		opts.ExcludeSessionIDs = agentCallChildSessionIDs(input.direct.agentCallHandler)
	}
	return input.Adapter.DiscoverSessionID(opts)
}

func attachInvocationIdentity(usage *model.UsageRecord, requestedCLI, requestedModel, requestedEffort string) {
	usage.Identity.RequestedCLI = requestedCLI
	usage.Identity.RequestedModel = requestedModel
	usage.Identity.RequestedEffort = requestedEffort
	usage.Identity.EffectiveCLI = usage.CLI
	if usage.Identity.EffectiveCLI == "" {
		usage.Identity.EffectiveCLI = requestedCLI
		usage.CLI = requestedCLI
	}
	if usage.Provider != "" {
		usage.Identity.EffectiveProvider = usage.Provider
		usage.Identity.ProviderSource = model.IdentitySourceAdapter
	} else if provider := invocationProvider(requestedCLI, requestedModel); provider != "" {
		usage.Provider = provider
		usage.Identity.EffectiveProvider = provider
		usage.Identity.ProviderSource = model.IdentitySourceInvocation
	}
	if usage.Model != "" {
		usage.Identity.EffectiveModel = usage.Model
		usage.Identity.ModelSource = model.IdentitySourceTelemetry
	} else if requestedModel != "" {
		usage.Model = requestedModel
		usage.Identity.EffectiveModel = requestedModel
		usage.Identity.ModelSource = model.IdentitySourceInvocation
	}
	if requestedEffort != "" {
		usage.Identity.EffectiveEffort = requestedEffort
		usage.Identity.EffortSource = model.IdentitySourceInvocation
	}
}

func invocationProvider(cliName, modelName string) string {
	switch cliName {
	case "claude":
		return "anthropic"
	case "codex":
		return "openai"
	case "copilot":
		return "github"
	case "cursor":
		return "cursor"
	case "opencode":
		provider, _, found := strings.Cut(modelName, "/")
		if found {
			return provider
		}
	}
	return ""
}

func prepareInvocationUsage(input *AgentInvocation, processOptions *AgentProcessOptions, direct *directInvocation, usageContext *cli.UsageContext) {
	if input.InvocationContext != cli.ContextAutonomousInteractive {
		return
	}
	collector, ok := input.Adapter.(cli.InteractiveUsageCollector)
	if !ok {
		return
	}
	if direct != nil && direct.ctx != nil {
		usageContext.StateDir = direct.ctx.SessionDir
		if input.Prefix != "" {
			identity := executionIdentity(direct.ctx, &model.Step{ID: direct.stepID}, "step", 0, true, input.CLI, input.SessionID)
			prefix := strings.NewReplacer("/", "-", `\`, "-").Replace(input.Prefix)
			reportName := fmt.Sprintf("%s-%d.statusline.jsonl", prefix, max(1, attemptForIdentity(direct.ctx, &identity)))
			usageContext.ReportName = &reportName
		}
	}
	plan, err := collector.PrepareInteractiveUsage(input.SessionID, input.SessionResumed, *usageContext)
	if err != nil {
		plan.PrepareErr = model.UnavailableTranscriptSpanUnavailable
		plan.ReportEnabled = false
		plan.ReportError = err.Error()
	}
	usageContext.InteractivePlan = &plan
	if executable, err := agentRunnerExecutable(); err == nil {
		processOptions.Args = cli.MergeInteractiveUsageSettings(processOptions.Args, &plan, executable)
	} else {
		plan.ReportEnabled = false
		plan.ReportReason = model.UnavailableCostReportUnavailable
		plan.ReportError = err.Error()
	}
	if direct != nil {
		direct.usageCollector = collector
		direct.usagePlan = plan
	}
}

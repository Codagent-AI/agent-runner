package exec

import (
	"context"
	"io"
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

	StdoutWrapper func(io.Writer) io.Writer
	StderrWrapper func(io.Writer) io.Writer
	Supervision   AgentProcessSupervision

	InvocationContext cli.InvocationContext
	CLI               string
	Model             string
	SessionID         string
	SessionResumed    bool

	Log         Logger
	SuspendHook func() error
	ResumeHook  func() error
	direct      *directInvocation
	Now         func() time.Time
}

// AgentInvocationResult is reusable execution evidence. It deliberately does
// not contain workflow-step audit, capture, or state-transition semantics.
type AgentInvocationResult struct {
	Outcome  StepOutcome
	Response string
	Stdout   string
	Stderr   string
	ExitCode int

	CLI                 string
	Model               string
	SessionID           string
	DiscoveredSessionID string
	SessionResumed      bool

	Usage            model.UsageRecord
	EstimatedCostUSD *float64
	UsageError       error

	StartedAt   time.Time
	FinishedAt  time.Time
	Duration    time.Duration
	CLILaunched bool
}

// InvokeAgent executes one resolved agent invocation and returns typed output,
// identity, session-discovery, usage, cost, timing, and launch evidence.
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
		Prefix: input.Prefix, StdoutWrapper: stdoutWrapper,
		StderrWrapper: stderrWrapper, Supervision: supervision,
	}
	direct := input.direct
	if direct != nil {
		invocationCopy := *direct
		invocationCopy.spawnEnv = append([]string(nil), input.Env...)
		invocationCopy.dropEnv = append([]string(nil), dropEnv...)
		direct = &invocationCopy
	}
	outcome, processResult, launched, runErr := runAgentProcess(
		runner, input.Adapter, &processOptions, input.InvocationContext, log,
		input.SuspendHook, input.ResumeHook, direct,
	)
	extraction, usageErr := extractAgentUsage(input.Adapter, input.CLI, input.InvocationContext, processResult.Stdout)
	result := AgentInvocationResult{
		Outcome: outcome, Stdout: processResult.Stdout, Stderr: processResult.Stderr,
		ExitCode: processResult.ExitCode, CLI: input.CLI, Model: input.Model,
		SessionID: input.SessionID, SessionResumed: input.SessionResumed,
		Usage: extraction.Usage, EstimatedCostUSD: extraction.EstimatedCostUSD,
		UsageError: usageErr, StartedAt: startedAt, CLILaunched: launched,
	}
	if runErr == nil {
		result.Response = processResult.Stdout
		if filter, ok := input.Adapter.(cli.OutputFilter); ok {
			result.Response = filter.FilterOutput(processResult.Stdout)
		}
		result.DiscoveredSessionID = input.Adapter.DiscoverSessionID(&cli.DiscoverOptions{
			SpawnTime: startedAt, PresetID: input.SessionID,
			Headless:      input.InvocationContext.IsHeadless(),
			ProcessOutput: processResult.Stdout, Workdir: input.Workdir,
		})
	}
	result.FinishedAt = now()
	result.Duration = result.FinishedAt.Sub(result.StartedAt)
	return result, runErr
}

package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/externaluser"
	"github.com/codagent/agent-runner/internal/interactive"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/usersettings"
)

func executeExternalUser(step *model.Step, ctx *model.ExecutionContext, runner ProcessRunner, log Logger, adapter cli.Adapter, profile *config.ResolvedAgent, args, spawnEnv []string, cliName, sessionID string, isResume bool, prefix string, startTime time.Time, handler *AgentCallHandler, onStarted func()) (StepOutcome, error) {
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result := AgentInvocationResult{Outcome: OutcomeFailed, Usage: defaultAgentUsage(cliName, true)}
	finish := func(err error) (StepOutcome, error) {
		return finishExternalUserStep(&result, err, step, ctx, log, cliName, sessionID, isResume, prefix, startTime, handler)
	}
	server, err := controlServerForContext(ctx)
	if err != nil {
		return finish(err)
	}
	probe, ok := adapter.(cli.TurnDurabilityProbe)
	if !ok {
		return finish(errors.New("external-user lead requires a turn durability probe"))
	}
	resolver := &externalSessionResolver{adapter: adapter, handler: handler, sessionID: sessionID, spawnedAt: time.Now(), workdir: step.Workdir}
	resolveSession := resolver.Resolve
	attempt := server.ActivateAttempt(runCtx, step.ID, control.AttemptOptions{
		CompletionEligible: true, AgentCallEligible: step.HasTool(model.RunnerToolCallAgent), AgentCallHandler: handler,
		Checkpoint: func() (cli.Checkpoint, error) { return externalCheckpoint(runCtx, probe, resolveSession()) },
	})
	defer server.Deactivate()
	if handler != nil {
		handler.options.AttemptContext = attempt.Context
	}
	spawnEnv = append(spawnEnv, attempt.Environment()...)
	exchange, err := externalUserExchangeOptions(ctx, step, cliName, sessionID)
	if err != nil {
		return finish(err)
	}
	replyPrompt, pending, err := externalUserReplay(runCtx, ctx, prefix, exchange.key, exchange.timeout, exchange.command, exchange.executable)
	if err != nil {
		return finish(err)
	}
	recordSessionOnSpawn(step, ctx, sessionID)
	var turnUsage externalTurnUsage
	for turn := 1; ; turn++ {
		if replyPrompt != nil {
			args, err = externalReplyArgs(step, ctx, profile, adapter, *replyPrompt, sessionID, exchange.executable)
			if err != nil {
				return finish(err)
			}
		}
		input := buildWorkflowAgentInvocation(step, ctx, adapter, args, spawnEnv, prefix, cli.ContextExternalUser, cliName, profile.Model, profile.Effort, sessionID, isResume || turn > 1, log, nil, onStarted)
		input.Context = runCtx
		input.OutputCopySuffix = fmt.Sprintf(".attempt-%d.turn-%d", exchange.attempt, turn)
		current, runErr := invokeExternalTurn(input, runner, log, server, &attempt, probe, resolveSession, ctx)
		result = current
		sessionID = recordExternalSession(step, ctx, current.DiscoveredSessionID, sessionID, log, resolver)
		turnUsage.Add(&result)
		if runErr != nil {
			return finish(runErr)
		}
		done, err := externalTurnFinished(ctx, &result, replyPrompt, pending, exchange.command, sessionID)
		if done || err != nil {
			return finish(err)
		}
		id, err := publishExternalRequest(ctx, step, adapter, current.Stdout, prefix, exchange.stepPath, exchange.key, exchange.attempt, turn, cliName, sessionID, exchange.command)
		if err != nil {
			return finish(err)
		}
		reply, err := receiveExternalReply(runCtx, ctx, prefix, id, exchange.timeout, exchange.command, false)
		if err != nil {
			return finish(err)
		}
		replyPrompt = &reply.Text
		pending = id
	}
}

func receiveExternalReply(runCtx context.Context, ctx *model.ExecutionContext, prefix string, id externaluser.Identity, timeout time.Duration, command string, replayed bool) (externaluser.Reply, error) {
	reply, err := externaluser.WaitReply(runCtx, filepath.Join(ctx.ExternalUser.Dir, id.ReplyName()), timeout)
	if err == nil {
		err = externaluser.Append(ctx.SessionDir, &externaluser.Event{Type: "reply_acted", Identity: id, Reply: &reply, CompletionCommand: command})
	}
	if err != nil {
		emitExternalEvent(ctx, prefix, "external_user_failure", id, map[string]any{"error": err.Error()})
		return reply, err
	}
	kind := "text"
	if reply.Action == "abort" {
		kind = "abort"
	}
	emitExternalEvent(ctx, prefix, "external_user_reply", id, map[string]any{"type": kind, "replayed": replayed})
	if reply.Action == "abort" {
		err = fmt.Errorf("external user aborted: %s", reply.Reason)
		emitExternalEvent(ctx, prefix, "external_user_failure", id, map[string]any{"error": err.Error()})
	}
	return reply, err
}
func emitExternalEvent(ctx *model.ExecutionContext, prefix, event string, id externaluser.Identity, data map[string]any) {
	if ctx.AuditLogger == nil {
		return
	}
	data["step_key"] = id.StepKey
	data["attempt"] = id.Attempt
	data["turn"] = id.Turn
	ctx.AuditLogger.Emit(audit.Event{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Prefix: prefix, Type: audit.EventType(event), Data: data})
}
func externalReplyArgs(step *model.Step, ctx *model.ExecutionContext, profile *config.ResolvedAgent, adapter cli.Adapter, text, sessionID, executable string) ([]string, error) {
	if err := evaluateWorkspaceDirs(ctx, step.ID); err != nil {
		return nil, err
	}
	input := cli.BuildArgsInput{AdditionalDirs: ctx.WorkspaceDirs, Context: cli.ContextExternalUser, SessionID: sessionID, Resume: true, Prompt: text, Model: profile.Model, Effort: profile.Effort, Workdir: step.Workdir, RunID: filepath.Base(ctx.SessionDir), PermissionMode: usersettings.AutonomousPermissionMode(ctx.AutonomousPermissionMode), DisallowedTools: []string{"AskUserQuestion"}, CompletionCommand: &cli.CompletionCommand{Executable: executable, Args: []string{"step", "complete"}}, RunnerCommands: []cli.RunnerCommand{{Kind: cli.RunnerCommandCompleteStep, Executable: executable}}}
	if step.HasTool(model.RunnerToolCallAgent) {
		input.RunnerIntegration = &cli.RunnerIntegration{AgentCall: &cli.MCPServerCommand{Executable: executable, Args: []string{"internal", "call-agent-mcp"}}}
	}
	return cli.BuildInvocationArgs(adapter, &input)
}

func invokeExternalTurn(input *AgentInvocation, runner ProcessRunner, log Logger, server *control.ControlServer, attempt *control.Attempt, probe cli.TurnDurabilityProbe, resolveSession func() string, ctx *model.ExecutionContext) (AgentInvocationResult, error) {
	processCtx, cancel := context.WithCancel(input.Context)
	defer cancel()
	input.Context = processCtx
	type response struct {
		result AgentInvocationResult
		err    error
	}
	done := make(chan response, 1)
	go func() { result, err := InvokeAgent(input, runner, log); done <- response{result, err} }()
	var finished *response
	var completion control.CompletionRequest
	select {
	case completion = <-server.Completions():
	case out := <-done:
		finished = &out
		select {
		case completion = <-server.Completions():
		default:
			out.result.Outcome = OutcomeFailed
			return out.result, out.err
		}
	case <-processCtx.Done():
		cancel()
		out := <-done
		return out.result, processCtx.Err()
	}
	committed, unsubscribe := server.SubscribeCommittedTurn(attempt.ID)
	defer unsubscribe()
	durability := interactive.AwaitTurnDurability(processCtx, &interactive.DurabilityOptions{CLI: input.CLI, SessionID: resolveSession(), Probe: probe, Checkpoint: completion.Checkpoint, CheckpointErr: completion.CheckpointErr, Receipt: completion.RequestID, CommittedTurn: committed, Logger: ctx.AuditLogger, Prefix: input.Prefix})
	// A headless CLI normally exits just after writing its final usage event.
	// Give it time to flush that event after durability confirmation, then
	// terminate a process that remains alive as the interactive runner does.
	if finished == nil && durability.Err == nil {
		timer := time.NewTimer(5 * time.Second)
		select {
		case out := <-done:
			finished = &out
		case <-timer.C:
		case <-processCtx.Done():
		}
		timer.Stop()
	}
	cancel()
	if finished == nil {
		out := <-done
		finished = &out
	}
	if durability.Err != nil {
		finished.result.Outcome = OutcomeFailed
		return finished.result, durability.Err
	}
	finished.result.Outcome = OutcomeSuccess
	return finished.result, nil
}

// Cumulative sources retain their final snapshot for the metrics collector,
// which already attributes against earlier steps and resumed attempts.
func mergeExternalUsage(total, next *model.UsageRecord) {
	if total.CLI == "" {
		*total = *next
		return
	}
	partial := total.Completeness == model.CompletenessPartial || total.Status != model.UsageCollected || next.Status != model.UsageCollected || next.Completeness == model.CompletenessPartial
	reason := total.Reason
	if next.Reason != "" {
		reason = next.Reason
	}
	switch {
	case len(next.RawCumulative) > 0:
		if externalCounterReset(total.RawCumulative, next.RawCumulative) {
			partial = true
			reason = model.UnavailableCounterReset
		}
		*total = *next
	case next.Status == model.UsageCollected:
		if total.Tokens == nil {
			total.Tokens = make(model.TokenCounts)
		}
		for key, value := range next.Tokens {
			total.Tokens[key] += value
		}
		if total.TokenTotals != nil && next.TokenTotals != nil {
			total.TokenTotals.Input += next.TokenTotals.Input
			total.TokenTotals.Output += next.TokenTotals.Output
			total.TokenTotals.Total += next.TokenTotals.Total
		} else {
			total.TokenTotals = nil
		}
		total.Allocations = append(total.Allocations, next.Allocations...)
		total.Status = model.UsageCollected
	}
	if partial {
		total.Completeness = model.CompletenessPartial
		total.Reason = reason
	}
}

func externalUserStepPath(ctx *model.ExecutionContext, stepID string) string {
	var parts []string
	for _, segment := range nestingToAudit(ctx) {
		part := segment.StepID
		if segment.Iteration != nil {
			part += fmt.Sprintf(":%d", *segment.Iteration)
		}
		if segment.RepairAttempt != nil {
			part += fmt.Sprintf(":repair:%d", *segment.RepairAttempt)
		}
		parts = append(parts, part)
	}
	return strings.Join(append(parts, stepID), ".")
}

func externalUserCLIError(invocationContext cli.InvocationContext, cliName, stepID string) error {
	if invocationContext == cli.ContextExternalUser && cliName != "claude" && cliName != "codex" {
		return fmt.Errorf("external-user mode supports only Claude and Codex leads; step %s resolved to %s", stepID, cliName)
	}
	return nil
}

func externalUserReplay(runCtx context.Context, ctx *model.ExecutionContext, prefix, key string, timeout time.Duration, command, executable string) (*string, externaluser.Identity, error) {
	last, err := externaluser.Last(ctx.SessionDir, key)
	if err != nil {
		return nil, externaluser.Identity{}, err
	}
	var replyPrompt *string
	var replayText string
	if last.Type == "request_written" && last.Request != nil {
		if err := restoreExternalRequest(ctx, prefix, &last); err != nil {
			return nil, externaluser.Identity{}, err
		}
		reply, err := receiveExternalReply(runCtx, ctx, prefix, last.Identity, timeout, last.CompletionCommand, false)
		if err != nil {
			return nil, externaluser.Identity{}, err
		}
		replyPrompt = &reply.Text
	} else if last.Type == "reply_acted" && last.Reply != nil && last.Reply.Action != "abort" {
		replyPrompt = &last.Reply.Text
		emitExternalEvent(ctx, prefix, "external_user_reply", last.Identity, map[string]any{"type": "text", "replayed": true})
	}
	if replyPrompt != nil {
		replayText = *replyPrompt
	}
	if replyPrompt != nil && last.CompletionCommand != command {
		updated := *replyPrompt + completionInstruction(executable)
		replyPrompt = &updated
		emitExternalEvent(ctx, prefix, "external_user_completion_refreshed", last.Identity, map[string]any{"completion_command": command})
	}
	if replyPrompt != nil {
		// Record the command delivered by this replay too, so another resume can
		// decide whether the instruction needs refreshing. Keep the reply verbatim.
		reply := externaluser.Reply{SchemaVersion: 1, Text: replayText}
		if err := externaluser.Append(ctx.SessionDir, &externaluser.Event{Type: "reply_acted", Identity: last.Identity, Reply: &reply, CompletionCommand: command}); err != nil {
			return nil, externaluser.Identity{}, err
		}
	}
	return replyPrompt, last.Identity, nil
}

func publishExternalRequest(ctx *model.ExecutionContext, step *model.Step, adapter cli.Adapter, stdout, prefix, stepPath, key string, attemptNumber, turn int, cliName, sessionID, command string) (externaluser.Identity, error) {
	message := cli.ExternalUserTurnText(adapter, stdout)
	id := externaluser.Identity{StepKey: key, Attempt: attemptNumber, Turn: turn}
	request := externaluser.Request{SchemaVersion: 1, RunID: filepath.Base(ctx.SessionDir), Step: stepPath, StepID: step.ID, Attempt: attemptNumber, Turn: turn, CLI: cliName, SessionID: sessionID, AgentMessage: message, EmptyTurn: strings.TrimSpace(message) == ""}
	replyPath := filepath.Join(ctx.ExternalUser.Dir, id.ReplyName())
	if _, err := os.Stat(replyPath); err == nil {
		return id, fmt.Errorf("unexpected reply file %q", replyPath)
	} else if !os.IsNotExist(err) {
		return id, err
	}
	event := externaluser.Event{Type: "request_written", Identity: id, Request: &request, CompletionCommand: command}
	if err := externaluser.Append(ctx.SessionDir, &event); err != nil {
		return id, err
	}
	body, _ := json.Marshal(request)
	if err := externaluser.AtomicWrite(filepath.Join(ctx.ExternalUser.Dir, id.RequestName()), body, 0o644); err != nil {
		return id, err
	}
	emitExternalEvent(ctx, prefix, "external_user_request", id, map[string]any{"file": id.RequestName()})
	return id, nil
}

func finishExternalUserStep(result *AgentInvocationResult, err error, step *model.Step, ctx *model.ExecutionContext, log Logger, cliName, sessionID string, isResume bool, prefix string, startTime time.Time, handler *AgentCallHandler) (StepOutcome, error) {
	if err != nil {
		result.Outcome = OutcomeFailed
		result.Stderr = err.Error()
	}
	if result.Outcome == OutcomeSuccess {
		if pending := handler.UncollectedCalls(); len(pending) > 0 {
			failAgentStepForUncollectedCalls(result, pending)
		}
		if step.Capture != "" {
			captureAgentResponse(step, ctx, result.Response)
		}
		if step.Session == model.SessionNew || model.IsNamedSession(step.Session) {
			ctx.LastSessionStepID = step.ID
			ctx.SessionProfiles[step.ID] = stepProfileName(step, ctx)
		}
	}
	return finishAgentStep(ctx, prefix, startTime, step, cliName, sessionID, cli.ContextExternalUser, isResume, result, err, log, handler)
}

type externalSessionResolver struct {
	mu                 sync.Mutex
	adapter            cli.Adapter
	handler            *AgentCallHandler
	sessionID, workdir string
	spawnedAt          time.Time
}

func (r *externalSessionResolver) Resolve() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessionID == "" {
		r.sessionID = discoverParentSessionID(r.adapter, r.spawnedAt, r.workdir, r.handler)
	}
	return r.sessionID
}
func (r *externalSessionResolver) Store(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = sessionID
}
func externalCheckpoint(ctx context.Context, probe cli.TurnDurabilityProbe, sessionID string) (cli.Checkpoint, error) {
	checkpoint, err := probe.Checkpoint(ctx, sessionID)
	if errors.Is(err, os.ErrNotExist) {
		return cli.Checkpoint{}, nil
	}
	return checkpoint, err
}

type externalTurnUsage struct {
	usage model.UsageRecord
	turns []model.UsageRecord
	cost  *float64
}

func (u *externalTurnUsage) Add(result *AgentInvocationResult) {
	u.turns = append(u.turns, result.Usage)
	mergeExternalUsage(&u.usage, &result.Usage)
	if result.EstimatedCostUSD != nil {
		if u.cost == nil {
			u.cost = new(float64)
		}
		*u.cost += *result.EstimatedCostUSD
	}
	lastCost := result.Usage.RawCumulativeCostUSD
	result.Usage = u.usage
	result.Usage.Turns = u.turns
	result.Usage.RawCumulativeCostUSD = lastCost
	result.EstimatedCostUSD = u.cost
}
func validateAgentInvocationContext(adapter cli.Adapter, invocationContext cli.InvocationContext, cliName, stepID string) error {
	if err := externalUserCLIError(invocationContext, cliName, stepID); err != nil {
		return err
	}
	return interactiveModeError(adapter, invocationContext)
}

type externalExchangeOptions struct {
	stepPath, key, executable, command string
	attempt                            int
	timeout                            time.Duration
}

func externalUserExchangeOptions(ctx *model.ExecutionContext, step *model.Step, cliName, sessionID string) (externalExchangeOptions, error) {
	identity := executionIdentity(ctx, step, "step", 0, false, cliName, sessionID)
	opts := externalExchangeOptions{attempt: max(1, attemptForIdentity(ctx, &identity)), stepPath: externalUserStepPath(ctx, step.ID)}
	opts.key = externalUserStepKey(ctx, step.ID)
	var err error
	if ctx.ExternalUser.Timeout != "" {
		opts.timeout, err = time.ParseDuration(ctx.ExternalUser.Timeout)
		if err != nil {
			return opts, err
		}
	}
	opts.executable, err = completionExecutableForContext(cli.ContextExternalUser)
	if err != nil {
		return opts, err
	}
	opts.command = cli.CompletionCommand{Executable: opts.executable, Args: []string{"step", "complete"}}.ShellCommand()
	return opts, nil
}

func recordExternalSession(step *model.Step, ctx *model.ExecutionContext, discovered, previous string, log Logger, resolver *externalSessionResolver) string {
	if discovered == "" {
		return previous
	}
	sessionID := storeDiscoveredSession(step, ctx, discovered, log)
	recordSessionOnSpawn(step, ctx, sessionID)
	resolver.Store(sessionID)
	return sessionID
}
func externalTurnFinished(ctx *model.ExecutionContext, result *AgentInvocationResult, replyPrompt *string, pending externaluser.Identity, command, sessionID string) (bool, error) {
	if result.Outcome != OutcomeSuccess {
		if result.ExitCode != 0 {
			return true, fmt.Errorf("external-user turn failed (exit %d): %s", result.ExitCode, result.Stderr)
		}
		if sessionID == "" {
			return true, errors.New("external-user turn did not establish a CLI session")
		}
	}
	if replyPrompt != nil {
		if err := externaluser.Append(ctx.SessionDir, &externaluser.Event{Type: "turn_finished", Identity: pending, CompletionCommand: command}); err != nil {
			return true, err
		}
	}
	return result.Outcome == OutcomeSuccess, nil
}
func externalCounterReset(previous, current model.TokenCounts) bool {
	for key, value := range current {
		if prior, ok := previous[key]; ok && value < prior {
			return true
		}
	}
	return false
}
func restoreExternalRequest(ctx *model.ExecutionContext, prefix string, last *externaluser.Event) error {
	path := filepath.Join(ctx.ExternalUser.Dir, last.Identity.RequestName())
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		body, _ := json.Marshal(last.Request)
		if err := externaluser.AtomicWrite(path, body, 0o644); err != nil {
			return err
		}
		emitExternalEvent(ctx, prefix, "external_user_request", last.Identity, map[string]any{"file": last.Identity.RequestName(), "restored": true})
	} else if statErr != nil {
		return statErr
	}
	return nil
}

// Encode components separately so authored dots and iteration-like IDs cannot
// collide with nesting separators or Runner-generated iteration suffixes.
func externalUserStepKey(ctx *model.ExecutionContext, stepID string) string {
	component := func(id string) string { return strings.ReplaceAll(externaluser.StepKey(id), ".", "_2e_") }
	var parts []string
	for _, segment := range ctx.NestingPath {
		part := component(segment.StepID)
		if segment.Iteration != nil {
			part += fmt.Sprintf("_i_%d", *segment.Iteration)
		}
		if segment.RepairAttempt != nil {
			part += fmt.Sprintf("_r_%d", *segment.RepairAttempt)
		}
		parts = append(parts, part)
	}
	return strings.Join(append(parts, component(stepID)), ".")
}

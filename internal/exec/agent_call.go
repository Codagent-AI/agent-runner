package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/codagent/agent-runner/internal/agentcall"
	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/usersettings"
)

type AgentCallParent struct {
	CLI              string
	SessionID        string
	NamedSession     string
	Worktree         string
	Workdir          string
	Prefix           string
	ResolveSessionID func() string
}

type AgentCallAccepted struct {
	CallID          string
	RequestID       string
	ParentAttemptID string
	Target          agentcall.Target
	StartedAt       time.Time
}

type AgentCallHandlerOptions struct {
	Context  *model.ExecutionContext
	Runner   ProcessRunner
	Log      Logger
	Eligible bool
	Parent   AgentCallParent

	Adapter    func(string) (cli.Adapter, error)
	NewID      func() string
	Now        func() time.Time
	OnAccepted func(AgentCallAccepted)
}

type acceptedAgentCall struct {
	callID   string
	target   agentcall.Target
	started  time.Time
	done     chan struct{}
	response json.RawMessage
}

// AgentCallHandler is attempt-scoped. It owns validation, acceptance,
// deduplication, serialization, and execution while control owns only
// authenticated admission and the connection lease.
type AgentCallHandler struct {
	options AgentCallHandlerOptions

	mu       sync.Mutex
	accepted map[string]*acceptedAgentCall
	active   *acceptedAgentCall

	parentMu        sync.Mutex
	parentSessionID string
}

func NewAgentCallHandler(input *AgentCallHandlerOptions) *AgentCallHandler {
	options := AgentCallHandlerOptions{}
	if input != nil {
		options = *input
	}
	if options.Adapter == nil {
		options.Adapter = cli.Get
	}
	if options.NewID == nil {
		options.NewID = uuid.NewString
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Log == nil {
		options.Log = discardLogger{}
	}
	return &AgentCallHandler{
		options: options, accepted: make(map[string]*acceptedAgentCall),
		parentSessionID: options.Parent.SessionID,
	}
}

func (h *AgentCallHandler) HandleAgentCall(ctx context.Context, envelope control.AgentCallRequest) json.RawMessage {
	// An accepted request ID owns its eventual result even if a retry carries a
	// different payload. Check the registry before repeating validation.
	h.mu.Lock()
	if existing := h.accepted[envelope.RequestID]; existing != nil {
		h.mu.Unlock()
		<-existing.done
		return append(json.RawMessage(nil), existing.response...)
	}
	h.mu.Unlock()

	resolved, failure := h.resolve(envelope.Payload)
	if failure != nil {
		return h.reject(envelope, failure)
	}

	h.mu.Lock()
	// Recheck after resolution because another delivery of this request may
	// have reserved it while profile/model/workdir validation ran.
	if existing := h.accepted[envelope.RequestID]; existing != nil {
		h.mu.Unlock()
		<-existing.done
		return append(json.RawMessage(nil), existing.response...)
	}
	if h.active != nil {
		active := h.active
		elapsed := h.options.Now().Sub(active.started).Round(time.Second)
		if elapsed < 0 {
			elapsed = 0
		}
		h.mu.Unlock()
		message := fmt.Sprintf(
			"agent calls are serial; active call %s:%s has been running for %s and must finish or be canceled first",
			active.target.Kind, active.target.Name, elapsed,
		)
		return h.reject(envelope, &agentcall.Error{
			Code: agentcall.CodeCallInProgress, Message: message, Target: &active.target,
		})
	}
	record := &acceptedAgentCall{
		callID: h.options.NewID(), target: resolved.target,
		started: h.options.Now(), done: make(chan struct{}),
	}
	h.accepted[envelope.RequestID] = record
	h.active = record
	h.mu.Unlock()

	if h.options.OnAccepted != nil {
		h.options.OnAccepted(AgentCallAccepted{
			CallID: record.callID, RequestID: envelope.RequestID,
			ParentAttemptID: envelope.AttemptID,
			Target:          resolved.target, StartedAt: record.started,
		})
	}

	response := h.execute(ctx, record, resolved)
	raw := marshalAgentCallResponse(response)
	h.mu.Lock()
	record.response = raw
	if h.active == record {
		h.active = nil
	}
	close(record.done)
	h.mu.Unlock()
	return append(json.RawMessage(nil), raw...)
}

func (h *AgentCallHandler) reject(envelope control.AgentCallRequest, failure *agentcall.Error) json.RawMessage {
	if h.options.Context != nil && h.options.Context.AuditLogger != nil {
		h.options.Context.AuditLogger.Emit(audit.Event{
			Timestamp: h.options.Now().UTC().Format(time.RFC3339Nano),
			Prefix:    h.options.Parent.Prefix,
			Type:      audit.EventControlRejected,
			Data: map[string]any{
				"reason": failure.Message, "error_code": failure.Code,
				"message_type": control.MessageAgentCall,
				"request_id":   envelope.RequestID, "attempt_id": envelope.AttemptID,
			},
		})
	}
	return marshalAgentCallResponse(agentcall.Response{Error: failure})
}

type resolvedAgentCall struct {
	request   agentcall.Request
	target    agentcall.Target
	profile   *config.ResolvedAgent
	adapter   cli.Adapter
	cliName   string
	model     string
	workdir   string
	sessionID string
	resume    bool
}

func (h *AgentCallHandler) resolve(raw json.RawMessage) (*resolvedAgentCall, *agentcall.Error) {
	if !h.options.Eligible {
		return nil, &agentcall.Error{Code: agentcall.CodeIneligible, Message: "call_agent is not enabled for this parent prompt"}
	}
	request, failure := agentcall.DecodeRequest(raw)
	if failure != nil {
		return nil, failure
	}
	target := request.Target()
	ctx := h.options.Context
	if ctx == nil {
		return nil, callFailure(agentcall.CodeControlFailure, "agent-call execution context is unavailable", target)
	}
	if h.options.Runner == nil {
		return nil, callFailure(agentcall.CodeControlFailure, "agent-call process runner is unavailable", target)
	}
	cfg, _ := ctx.ProfileStore.(*config.Config)
	if cfg == nil {
		return nil, callFailure(agentcall.CodeUnknownAgent, "agent profile configuration is unavailable", target)
	}
	profileName := target.Name
	if target.Kind == agentcall.TargetSession {
		profileName = ctx.NamedSessionDecls[target.Name]
		if profileName == "" {
			return nil, callFailure(agentcall.CodeUnknownSession, fmt.Sprintf("named session %q is not declared", target.Name), target)
		}
	}
	profile, err := cfg.Resolve(profileName)
	if err != nil {
		return nil, callFailure(agentcall.CodeUnknownAgent, fmt.Sprintf("resolving profile %q: %v", profileName, err), target)
	}
	resolvedProfile := *profile
	if request.CLI != nil {
		resolvedProfile.CLI = strings.TrimSpace(*request.CLI)
	}
	if request.Model != nil {
		resolvedProfile.Model = strings.TrimSpace(*request.Model)
	}
	adapter, err := h.options.Adapter(resolvedProfile.CLI)
	if err != nil {
		return nil, callFailure(agentcall.CodeInvalidCLI, fmt.Sprintf("invalid CLI %q: %v", resolvedProfile.CLI, err), target)
	}
	if _, err := adapter.ProbeModel(resolvedProfile.Model, resolvedProfile.Effort); err != nil {
		return nil, callFailure(agentcall.CodeInvalidModel, fmt.Sprintf("invalid model %q for %s: %v", resolvedProfile.Model, resolvedProfile.CLI, err), target)
	}
	workdir := h.options.Parent.Workdir
	if request.Workdir != nil {
		workdir, err = resolveAgentCallWorkdir(
			h.options.Parent.Worktree, h.options.Parent.Workdir, strings.TrimSpace(*request.Workdir),
		)
		if err != nil {
			return nil, callFailure(agentcall.CodeInvalidWorkdir, fmt.Sprintf("invalid workdir %q: %v", *request.Workdir, err), target)
		}
	}
	sessionID := ""
	if target.Kind == agentcall.TargetSession {
		sessionID = ctx.NamedSessions[target.Name]
		sameCLI := h.options.Parent.CLI == resolvedProfile.CLI
		sameNamedSession := h.options.Parent.NamedSession != "" && h.options.Parent.NamedSession == target.Name
		sameResolvedSession := sessionID != "" && h.activeParentSessionID() == sessionID
		if sameCLI && (sameNamedSession || sameResolvedSession) {
			return nil, callFailure(agentcall.CodeSelfSession, fmt.Sprintf("named session %q is the parent's active CLI session", target.Name), target)
		}
	}
	return &resolvedAgentCall{
		request: request, target: target, profile: &resolvedProfile,
		adapter: adapter, cliName: resolvedProfile.CLI, model: resolvedProfile.Model,
		workdir: workdir, sessionID: sessionID, resume: sessionID != "",
	}, nil
}

func (h *AgentCallHandler) activeParentSessionID() string {
	h.parentMu.Lock()
	defer h.parentMu.Unlock()
	if h.parentSessionID == "" && h.options.Parent.ResolveSessionID != nil {
		h.parentSessionID = strings.TrimSpace(h.options.Parent.ResolveSessionID())
	}
	return h.parentSessionID
}

func resolveAgentCallWorkdir(worktree, base, requested string) (string, error) {
	root, err := filepath.Abs(worktree)
	if err != nil {
		return "", fmt.Errorf("resolve parent worktree: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve parent worktree: %w", err)
	}
	if base == "" {
		base = root
	} else if !filepath.IsAbs(base) {
		base = filepath.Join(root, base)
	}
	base, err = canonicalContainedDirectory(root, base)
	if err != nil {
		return "", fmt.Errorf("resolve parent workdir: %w", err)
	}
	if requested == "" {
		return base, nil
	}
	candidate := requested
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, candidate)
	}
	return canonicalContainedDirectory(root, candidate)
}

func canonicalContainedDirectory(root, candidate string) (string, error) {
	var err error
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	candidate, err = filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(candidate) // #nosec G703 -- candidate has been canonicalized and is containment-checked below.
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("not a directory")
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("directory is outside the parent worktree")
	}
	return candidate, nil
}

func (h *AgentCallHandler) execute(ctx context.Context, record *acceptedAgentCall, call *resolvedAgentCall) agentcall.Response {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionID := call.sessionID
	if !call.resume && sessionID == "" && call.cliName == "claude" {
		sessionID = uuid.NewString()
	}
	prompt := call.request.Prompt
	if call.profile.SystemPrompt != "" {
		prompt = call.profile.SystemPrompt + "\n\n" + prompt
	}
	if !call.resume {
		prompt = autonomyPreamble + prompt
	}
	input := cli.BuildArgsInput{
		Prompt: prompt, SessionID: sessionID, Resume: call.resume,
		Model: call.model, Effort: call.profile.Effort,
		Context:         cli.ContextAutonomousHeadless,
		PermissionMode:  usersettings.AutonomousPermissionMode(h.options.Context.AutonomousPermissionMode),
		DisallowedTools: []string{"AskUserQuestion"}, Workdir: call.workdir,
	}
	if h.options.Context.SessionDir != "" {
		input.RunID = filepath.Base(filepath.Clean(h.options.Context.SessionDir))
	}
	args, err := cli.BuildInvocationArgs(call.adapter, &input)
	if err != nil {
		return acceptedFailure(record, agentcall.CodeExecutionFailed, "prepare called agent: "+err.Error(), call.target)
	}
	spawnEnv, err := cli.SpawnEnvForInvocation(call.adapter, &input)
	if err != nil {
		return acceptedFailure(record, agentcall.CodeExecutionFailed, "prepare called agent environment: "+err.Error(), call.target)
	}
	invocation, runErr := InvokeAgent(&AgentInvocation{
		Context: ctx, Adapter: call.adapter, Args: args,
		Env: spawnEnv, DropEnv: cli.DropSpawnEnvVars(call.adapter),
		Workdir: call.workdir, Prefix: strings.TrimSuffix(h.options.Parent.Prefix, "/") + "/call:" + record.callID,
		InvocationContext: cli.ContextAutonomousHeadless,
		CLI:               call.cliName, Model: call.model, SessionID: sessionID, SessionResumed: call.resume,
		Log: h.options.Log,
	}, h.options.Runner, h.options.Log)
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || ctx.Err() != nil {
			return acceptedFailure(record, agentcall.CodeCallCanceled, "called agent was canceled", call.target)
		}
		return acceptedFailure(record, agentcall.CodeExecutionFailed, "called agent failed: "+runErr.Error(), call.target)
	}
	if call.target.Kind == agentcall.TargetSession {
		discovered := invocation.DiscoveredSessionID
		if discovered == "" {
			discovered = sessionID
		}
		if discovered != "" {
			h.options.Context.NamedSessions[call.target.Name] = discovered
			if h.options.Context.FlushState != nil {
				h.options.Context.FlushState()
			}
		}
	}
	if invocation.Outcome != OutcomeSuccess {
		return acceptedFailure(record, agentcall.CodeExecutionFailed, fmt.Sprintf("called agent failed with exit code %d", invocation.ExitCode), call.target)
	}
	return agentcall.Response{
		CallID: record.callID,
		Result: &agentcall.Result{Target: call.target, Response: invocation.Response},
	}
}

func acceptedFailure(record *acceptedAgentCall, code, message string, target agentcall.Target) agentcall.Response {
	return agentcall.Response{CallID: record.callID, Error: callFailure(code, message, target)}
}

func callFailure(code, message string, target agentcall.Target) *agentcall.Error {
	return &agentcall.Error{Code: code, Message: message, Target: &target}
}

func marshalAgentCallResponse(response agentcall.Response) json.RawMessage {
	raw, err := json.Marshal(response)
	if err != nil {
		return json.RawMessage(`{"error":{"code":"control_failure","message":"encode call_agent response"}}`)
	}
	return raw
}

type discardLogger struct{}

func (discardLogger) Println(...any)        {}
func (discardLogger) Printf(string, ...any) {}
func (discardLogger) Errorf(string, ...any) {}

var _ control.AgentCallHandler = (*AgentCallHandler)(nil)

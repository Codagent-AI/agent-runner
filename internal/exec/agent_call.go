package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	Prefix          string
	ParentPrefix    string
}

// AgentCallLifecycleNotifier lets the live process wrapper move the run view
// into a dynamic child as soon as acceptance evidence is durable, then back to
// the still-active parent after the child finishes.
type AgentCallLifecycleNotifier interface {
	NotifyAgentCallAccepted(*AgentCallAccepted)
	NotifyAgentCallFinished(*AgentCallAccepted)
}

type AgentCallHandlerOptions struct {
	Context  *model.ExecutionContext
	Runner   ProcessRunner
	Log      Logger
	Eligible bool
	Parent   AgentCallParent

	// WaitBudget caps how long call_agent and get_agent_call hold one open
	// request while the child runs. Zero waits for the child, which keeps a
	// parent from ending its turn on a call it never collected. Only a parent
	// whose MCP client aborts a long tools/call needs a budget; the default is
	// derived from the parent CLI.
	WaitBudget time.Duration

	Adapter        func(string) (cli.Adapter, error)
	NewID          func() string
	Now            func() time.Time
	AttemptContext context.Context
	OnAccepted     func(AgentCallAccepted)
	OnFinished     func(AgentCallAccepted)
}

type acceptedAgentCall struct {
	callID          string
	requestID       string
	parentAttemptID string
	target          agentcall.Target
	started         time.Time
	status          string
	cancel          context.CancelFunc
	done            chan struct{}
	response        json.RawMessage
	childSessionID  string
}

type agentCallExecution struct {
	response     agentcall.Response
	invocation   AgentInvocationResult
	errorMessage string
}

// cursorAgentCallWaitBudget keeps one open request inside the roughly 60s
// tools/call limit Cursor's MCP client enforces. Measured against a headless
// cursor-agent parent: an open call fails with "MCP error -32001: Request timed
// out" at 60.3s, and Runner's progress notifications do not extend it.
const cursorAgentCallWaitBudget = 45 * time.Second

// agentCallWaitBudget reports how long a parent CLI can hold one MCP request
// open while a child runs. Zero, the default, waits for the child so the parent
// cannot proceed on a result it never collected. Cursor is the one supported
// parent that aborts a long tools/call, so it trades that safety for polling.
func agentCallWaitBudget(parentCLI string) time.Duration {
	if strings.EqualFold(strings.TrimSpace(parentCLI), "cursor") {
		return cursorAgentCallWaitBudget
	}
	return 0
}

// AgentCallHandler is attempt-scoped. It owns validation, acceptance,
// deduplication, serialization, and execution while control owns only
// authenticated admission. After acceptance the child is leased to the
// parent attempt, not a live MCP or control connection.
type AgentCallHandler struct {
	options AgentCallHandlerOptions

	mu       sync.Mutex
	accepted map[string]*acceptedAgentCall
	byCallID map[string]*acceptedAgentCall
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
	if options.WaitBudget <= 0 {
		options.WaitBudget = agentCallWaitBudget(options.Parent.CLI)
	}
	return &AgentCallHandler{
		options: options, accepted: make(map[string]*acceptedAgentCall), byCallID: make(map[string]*acceptedAgentCall),
		parentSessionID: options.Parent.SessionID,
	}
}

func (h *AgentCallHandler) HandleAgentCall(ctx context.Context, envelope control.AgentCallRequest) json.RawMessage {
	// An accepted request ID owns its eventual result even if a retry carries a
	// different payload. Check the registry before repeating validation.
	h.mu.Lock()
	if existing := h.accepted[envelope.RequestID]; existing != nil {
		h.mu.Unlock()
		return h.awaitAgentCallResult(ctx, existing)
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
		return h.awaitAgentCallResult(ctx, existing)
	}
	if h.active != nil {
		active := h.active
		elapsed := h.elapsedLocked(active)
		h.mu.Unlock()
		message := fmt.Sprintf(
			"agent calls are serial; active call %s (%s:%s) has been running for %s; poll get_agent_call or cancel cancel_agent_call before starting another",
			active.callID, active.target.Kind, active.target.Name, elapsed,
		)
		return h.reject(envelope, &agentcall.Error{
			Code: agentcall.CodeCallInProgress, Message: message, Target: &active.target,
			CallID: active.callID, Elapsed: elapsed,
		})
	}
	parent := h.childParentContext(ctx)
	childCtx, cancel := context.WithCancel(parent)
	record := &acceptedAgentCall{
		callID: h.options.NewID(), requestID: envelope.RequestID, parentAttemptID: envelope.AttemptID, target: resolved.target,
		started: h.options.Now(), status: agentcall.StatusAccepted, cancel: cancel, done: make(chan struct{}),
	}
	h.accepted[envelope.RequestID] = record
	h.byCallID[record.callID] = record
	h.active = record
	h.mu.Unlock()
	h.emitAgentCallStart(record, resolved)

	if h.options.OnAccepted != nil {
		h.options.OnAccepted(h.lifecycleEvent(record, resolved.target))
	}

	go h.finishAccepted(childCtx, record, resolved)
	return h.awaitAgentCallResult(ctx, record)
}

// awaitAgentCallResult blocks until the call is terminal, the wait budget
// expires, or the requesting context ends, then returns the current snapshot.
// Ending the request never cancels the child, which is leased to the parent
// attempt rather than to one MCP request.
func (h *AgentCallHandler) awaitAgentCallResult(ctx context.Context, record *acceptedAgentCall) json.RawMessage {
	if ctx == nil {
		ctx = context.Background()
	}
	var expired <-chan time.Time
	if budget := h.options.WaitBudget; budget > 0 {
		timer := time.NewTimer(budget)
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case <-record.done:
	case <-ctx.Done():
	case <-expired:
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.marshalSnapshotLocked(record)
}

func (h *AgentCallHandler) HandleGetAgentCall(ctx context.Context, envelope control.AgentCallRequest) json.RawMessage {
	request, failure := agentcall.DecodeCallIDRequest(envelope.Payload)
	if failure != nil {
		return marshalAgentCallResponse(agentcall.Response{Error: failure})
	}
	h.mu.Lock()
	record := h.byCallID[request.CallID]
	if record == nil {
		h.mu.Unlock()
		return marshalAgentCallResponse(agentcall.Response{Error: &agentcall.Error{
			Code: agentcall.CodeUnknownCall, Message: "call_id does not belong to the active parent attempt",
		}})
	}
	h.mu.Unlock()
	return h.awaitAgentCallResult(ctx, record)
}

func (h *AgentCallHandler) HandleCancelAgentCall(ctx context.Context, envelope control.AgentCallRequest) json.RawMessage {
	request, failure := agentcall.DecodeCallIDRequest(envelope.Payload)
	if failure != nil {
		return marshalAgentCallResponse(agentcall.Response{Error: failure})
	}
	h.mu.Lock()
	record := h.byCallID[request.CallID]
	if record == nil {
		h.mu.Unlock()
		return marshalAgentCallResponse(agentcall.Response{Error: &agentcall.Error{
			Code: agentcall.CodeUnknownCall, Message: "call_id does not belong to the active parent attempt",
		}})
	}
	if len(record.response) > 0 {
		raw := h.marshalSnapshotLocked(record)
		h.mu.Unlock()
		return raw
	}
	cancel := record.cancel
	done := record.done
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
	h.mu.Lock()
	raw := h.marshalSnapshotLocked(record)
	h.mu.Unlock()
	return raw
}

func (h *AgentCallHandler) finishAccepted(ctx context.Context, record *acceptedAgentCall, resolved *resolvedAgentCall) {
	h.mu.Lock()
	record.status = agentcall.StatusRunning
	h.mu.Unlock()

	execution := h.execute(ctx, record, resolved)
	h.finalizeExecution(record, resolved, &execution)
}

func (h *AgentCallHandler) finalizeExecution(record *acceptedAgentCall, resolved *resolvedAgentCall, execution *agentCallExecution) {
	h.emitAgentCallEnd(record, resolved, execution)
	if h.options.OnFinished != nil {
		h.options.OnFinished(h.lifecycleEvent(record, resolved.target))
	}
	execution.response.CallID = record.callID
	target := resolved.target
	execution.response.Target = &target
	if execution.response.Error != nil {
		if execution.response.Error.Code == agentcall.CodeCallCanceled {
			execution.response.Status = agentcall.StatusCanceled
		} else {
			execution.response.Status = agentcall.StatusFailed
		}
	} else {
		execution.response.Status = agentcall.StatusSucceeded
	}
	var raw json.RawMessage
	if successfulAgentCallResponseDefinitelyTooLarge(execution.response) {
		execution.response = oversizedAgentCallFailure(record, resolved.target)
		raw = marshalAgentCallResponse(execution.response)
	} else {
		raw = marshalAgentCallResponse(execution.response)
		if execution.response.Result != nil && !control.FitsAgentCallPayload(raw) {
			execution.response = oversizedAgentCallFailure(record, resolved.target)
			raw = marshalAgentCallResponse(execution.response)
		}
	}
	h.mu.Lock()
	record.response = raw
	record.status = execution.response.Status
	record.childSessionID = strings.TrimSpace(execution.invocation.DiscoveredSessionID)
	if h.active == record {
		h.active = nil
	}
	close(record.done)
	h.mu.Unlock()
}

// UncollectedCalls returns the ids of accepted calls that never reached a
// terminal status, oldest first. A parent that ends its turn with one
// outstanding leaves the child to be killed at attempt teardown, so its work
// never reaches the workflow.
func (h *AgentCallHandler) UncollectedCalls() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var pending []*acceptedAgentCall
	for _, record := range h.accepted {
		if len(record.response) == 0 {
			pending = append(pending, record)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].started.Before(pending[j].started) })
	ids := make([]string, 0, len(pending))
	for _, record := range pending {
		ids = append(ids, record.callID)
	}
	return ids
}

func (h *AgentCallHandler) ChildSessionIDs() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var ids []string
	seen := make(map[string]struct{}, len(h.accepted))
	for _, record := range h.accepted {
		id := strings.TrimSpace(record.childSessionID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func agentCallChildSessionIDs(handler control.AgentCallHandler) []string {
	provider, ok := handler.(interface{ ChildSessionIDs() []string })
	if !ok {
		return nil
	}
	return provider.ChildSessionIDs()
}

func (h *AgentCallHandler) probeChildSessionID(ctx context.Context, record *acceptedAgentCall, call *resolvedAgentCall, output *synchronizedBuffer) {
	if output == nil {
		return
	}
	defer output.Close()
	if h.publishDiscoveredChildSession(record, call, output.String()) {
		return
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			h.publishDiscoveredChildSession(record, call, output.String())
			return
		case <-ticker.C:
			if h.publishDiscoveredChildSession(record, call, output.String()) {
				return
			}
		}
	}
}

func (h *AgentCallHandler) publishDiscoveredChildSession(record *acceptedAgentCall, call *resolvedAgentCall, output string) bool {
	id := h.discoverRunningChildSessionID(record, call, output)
	if id == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if record.childSessionID == "" {
		record.childSessionID = id
	}
	return record.childSessionID != ""
}

func (h *AgentCallHandler) discoverRunningChildSessionID(record *acceptedAgentCall, call *resolvedAgentCall, output string) string {
	if call == nil || call.adapter == nil {
		return ""
	}
	if id := strings.TrimSpace(call.adapter.DiscoverSessionID(&cli.DiscoverOptions{
		SpawnTime: record.started, PresetID: call.sessionID,
		Headless: true, ProcessOutput: output, Workdir: call.workdir,
	})); id != "" {
		return id
	}
	exclude := append([]string(nil), h.ChildSessionIDs()...)
	h.parentMu.Lock()
	parentID := h.parentSessionID
	h.parentMu.Unlock()
	if parentID != "" {
		exclude = append(exclude, parentID)
	}
	return strings.TrimSpace(call.adapter.DiscoverSessionID(&cli.DiscoverOptions{
		SpawnTime: record.started, PresetID: call.sessionID,
		Headless: false, ProcessOutput: output, Workdir: call.workdir,
		ExcludeSessionIDs: exclude,
	}))
}

func childStdoutCapture(adapter cli.Adapter, capture io.Writer) func(io.Writer) io.Writer {
	return func(w io.Writer) io.Writer {
		next := w
		if wrapper, ok := adapter.(cli.StdoutWrapper); ok {
			next = wrapper.WrapStdout(next)
		}
		if capture == nil {
			return next
		}
		return io.MultiWriter(next, capture)
	}
}

const maxChildSessionProbeBytes = 64 * 1024

type synchronizedBuffer struct {
	mu     sync.Mutex
	buf    strings.Builder
	limit  int
	closed bool
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	if b == nil {
		return len(p), nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return len(p), nil
	}
	limit := b.limit
	if limit <= 0 {
		limit = maxChildSessionProbeBytes
	}
	remain := limit - b.buf.Len()
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) > remain {
		_, err := b.buf.Write(p[:remain])
		return len(p), err
	}
	return b.buf.Write(p)
}

func (b *synchronizedBuffer) String() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ""
	}
	return b.buf.String()
}

func (b *synchronizedBuffer) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.buf.Reset()
}

func (h *AgentCallHandler) childParentContext(requestCtx context.Context) context.Context {
	if h.options.AttemptContext != nil {
		return h.options.AttemptContext
	}
	if requestCtx != nil {
		return requestCtx
	}
	return context.Background()
}

func (h *AgentCallHandler) elapsedLocked(record *acceptedAgentCall) string {
	elapsed := h.options.Now().Sub(record.started).Round(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}
	return elapsed.String()
}

func (h *AgentCallHandler) marshalSnapshotLocked(record *acceptedAgentCall) json.RawMessage {
	elapsed := h.elapsedLocked(record)
	if len(record.response) > 0 {
		var stored agentcall.Response
		if err := json.Unmarshal(record.response, &stored); err == nil {
			stored.Elapsed = elapsed
			return marshalAgentCallResponse(stored)
		}
		return append(json.RawMessage(nil), record.response...)
	}
	target := record.target
	return marshalAgentCallResponse(agentcall.Response{
		CallID: record.callID, Status: record.status, Target: &target, Elapsed: elapsed,
	})
}

func waitForAgentCallResponse(ctx context.Context, call *acceptedAgentCall) json.RawMessage {
	select {
	case <-call.done:
		return append(json.RawMessage(nil), call.response...)
	default:
	}
	select {
	case <-call.done:
		return append(json.RawMessage(nil), call.response...)
	case <-ctx.Done():
		select {
		case <-call.done:
			return append(json.RawMessage(nil), call.response...)
		default:
		}
		return marshalAgentCallResponse(acceptedFailure(
			call,
			agentcall.CodeCallCanceled,
			"call_agent request was canceled while waiting for the accepted call result",
			call.target,
		))
	}
}

func (h *AgentCallHandler) lifecycleEvent(record *acceptedAgentCall, target agentcall.Target) AgentCallAccepted {
	return AgentCallAccepted{
		CallID: record.callID, RequestID: record.requestID, ParentAttemptID: record.parentAttemptID,
		Target: target, StartedAt: record.started,
		Prefix: agentCallPrefix(h.options.Parent.Prefix, record.callID), ParentPrefix: h.options.Parent.Prefix,
	}
}

func (h *AgentCallHandler) reject(envelope control.AgentCallRequest, failure *agentcall.Error) json.RawMessage {
	if h.options.Context != nil && h.options.Context.AuditLogger != nil {
		h.options.Context.AuditLogger.Emit(audit.Event{
			Timestamp: formatAuditTimestamp(h.options.Now()),
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
	request     agentcall.Request
	target      agentcall.Target
	profile     *config.ResolvedAgent
	adapter     cli.Adapter
	cliName     string
	model       string
	workdir     string
	sessionID   string
	resume      bool
	profileName string
}

func (h *AgentCallHandler) resolve(raw json.RawMessage) (*resolvedAgentCall, *agentcall.Error) {
	if !h.options.Eligible {
		return nil, &agentcall.Error{Code: agentcall.CodeIneligible, Message: "call_agent is not enabled in the active step declaration"}
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
	EmitAgentDeprecations(ctx, h.options.Log, profile.Deprecations)
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
		workdir: workdir, sessionID: sessionID, resume: sessionID != "", profileName: profileName,
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

func (h *AgentCallHandler) execute(ctx context.Context, record *acceptedAgentCall, call *resolvedAgentCall) agentCallExecution {
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
		return h.preLaunchFailure(record, call, "prepare called agent: "+err.Error())
	}
	spawnEnv, err := cli.SpawnEnvForInvocation(call.adapter, &input)
	if err != nil {
		return h.preLaunchFailure(record, call, "prepare called agent environment: "+err.Error())
	}
	probeCtx, stopProbe := context.WithCancel(ctx)
	defer stopProbe()
	output := &synchronizedBuffer{limit: maxChildSessionProbeBytes}
	invocation, runErr := InvokeAgent(&AgentInvocation{
		Context: ctx, Adapter: call.adapter, Args: args,
		Env: spawnEnv, DropEnv: cli.DropSpawnEnvVars(call.adapter),
		Workdir: call.workdir, Prefix: agentCallPrefix(h.options.Parent.Prefix, record.callID),
		InvocationContext: cli.ContextAutonomousHeadless,
		CLI:               call.cliName, Model: call.model, Effort: call.profile.Effort, SessionID: sessionID, SessionResumed: call.resume,
		Log: h.options.Log, Now: h.options.Now,
		StdoutWrapper: childStdoutCapture(call.adapter, output),
		OnStarted: func() {
			go h.probeChildSessionID(probeCtx, record, call, output)
		},
	}, h.options.Runner, h.options.Log)
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || ctx.Err() != nil {
			invocation.Outcome = OutcomeAborted
			message := "called agent was canceled"
			return agentCallExecution{response: acceptedFailure(record, agentcall.CodeCallCanceled, message, call.target), invocation: invocation, errorMessage: message}
		}
		message := "called agent failed: " + runErr.Error()
		return agentCallExecution{response: acceptedFailure(record, agentcall.CodeExecutionFailed, message, call.target), invocation: invocation, errorMessage: message}
	}
	if invocation.Outcome != OutcomeSuccess {
		message := fmt.Sprintf("called agent failed with exit code %d", invocation.ExitCode)
		return agentCallExecution{response: acceptedFailure(record, agentcall.CodeExecutionFailed, message, call.target), invocation: invocation, errorMessage: message}
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
	return agentCallExecution{response: agentcall.Response{
		CallID: record.callID, Result: &agentcall.Result{Target: call.target, Response: invocation.Response},
	}, invocation: invocation}
}

func (h *AgentCallHandler) preLaunchFailure(record *acceptedAgentCall, call *resolvedAgentCall, message string) agentCallExecution {
	finished := h.options.Now()
	return agentCallExecution{
		response: acceptedFailure(record, agentcall.CodeExecutionFailed, message, call.target),
		invocation: AgentInvocationResult{
			Outcome: OutcomeFailed, CLI: call.cliName, Model: call.model, SessionID: call.sessionID, SessionResumed: call.resume,
			Usage: defaultAgentUsage(call.cliName, true), StartedAt: record.started, FinishedAt: finished, Duration: finished.Sub(record.started),
		},
		errorMessage: message,
	}
}

func (h *AgentCallHandler) emitAgentCallStart(record *acceptedAgentCall, call *resolvedAgentCall) {
	data := agentCallAuditData(record, call)
	data["prompt"] = call.request.Prompt
	data["workdir"] = call.workdir
	data["profile"] = call.profileName
	data["cli"] = call.cliName
	data["model"] = call.model
	data["session_strategy"] = agentCallSessionStrategy(call)
	data["resolved_session_id"] = call.sessionID
	data["session_resumed"] = call.resume
	data["context"] = contextSnapshot(h.options.Context)
	emitAudit(h.options.Context, audit.Event{
		Timestamp: formatAuditTimestamp(record.started), Prefix: agentCallPrefix(h.options.Parent.Prefix, record.callID),
		Type: audit.EventAgentCallStart, Data: data,
	})
}

func (h *AgentCallHandler) emitAgentCallEnd(record *acceptedAgentCall, call *resolvedAgentCall, execution *agentCallExecution) {
	invocation := execution.invocation
	finished := invocation.FinishedAt
	if finished.IsZero() {
		finished = h.options.Now()
	}
	duration := finished.Sub(record.started)
	if duration < 0 {
		duration = 0
	}
	resolvedSessionID := invocation.DiscoveredSessionID
	if resolvedSessionID == "" {
		resolvedSessionID = invocation.SessionID
	}
	identity := model.ExecutionIdentity{
		ExecutionSessionID: h.options.Context.ExecutionSessionID,
		StepID:             record.callID, Prefix: agentCallIdentityPrefix(h.options.Parent.Prefix), StepType: "agent", Kind: "agent-call",
		CLI: call.cliName, SessionID: resolvedSessionID, SessionStrategy: agentCallSessionStrategy(call),
		SessionResumed: call.resume, AgentInvoked: invocation.CLILaunched,
		Role: call.profileName, Tool: "agent-runner",
	}
	data := agentCallAuditData(record, call)
	data["outcome"] = string(invocation.Outcome)
	data["duration_ms"] = duration.Milliseconds()
	data["cli_launched"] = invocation.CLILaunched
	data["discovered_session_id"] = invocation.DiscoveredSessionID
	data["resolved_session_id"] = resolvedSessionID
	data["identity"] = identity
	data["usage"] = invocation.Usage
	data["estimated_api_cost_usd"] = invocation.EstimatedCostUSD
	if invocation.CLILaunched {
		data["exit_code"] = invocation.ExitCode
	}
	if execution.errorMessage != "" {
		data["error"] = execution.errorMessage
	}
	if invocation.Stderr != "" {
		data["stderr"] = invocation.Stderr
	}
	if invocation.UsageError != nil {
		data["usage_error"] = invocation.UsageError.Error()
	}
	emitAudit(h.options.Context, audit.Event{
		Timestamp: formatAuditTimestamp(finished), Prefix: agentCallPrefix(h.options.Parent.Prefix, record.callID),
		Type: audit.EventAgentCallEnd, Data: data,
	})
}

func agentCallAuditData(record *acceptedAgentCall, call *resolvedAgentCall) map[string]any {
	return map[string]any{
		"call_id":           record.callID,
		"request_id":        record.requestID,
		"parent_attempt_id": record.parentAttemptID,
		"target_kind":       string(call.target.Kind),
		"target_name":       call.target.Name,
	}
}

func agentCallSessionStrategy(call *resolvedAgentCall) string {
	if call.target.Kind == agentcall.TargetSession {
		return call.target.Name
	}
	return string(model.SessionNew)
}

func agentCallPrefix(parent, callID string) string {
	parent = strings.TrimSpace(parent)
	if strings.HasPrefix(parent, "[") && strings.HasSuffix(parent, "]") {
		return strings.TrimSuffix(parent, "]") + ", call:" + callID + "]"
	}
	if parent == "" {
		return "call:" + callID
	}
	return strings.TrimSuffix(parent, "/") + "/call:" + callID
}

func agentCallIdentityPrefix(parent string) string {
	parent = strings.TrimSpace(parent)
	if strings.HasPrefix(parent, "[") && strings.HasSuffix(parent, "]") {
		parent = strings.TrimSuffix(strings.TrimPrefix(parent, "["), "]")
		parent = strings.ReplaceAll(parent, ", ", "/")
	}
	return parent
}

func acceptedFailure(record *acceptedAgentCall, code, message string, target agentcall.Target) agentcall.Response {
	status := agentcall.StatusFailed
	if code == agentcall.CodeCallCanceled {
		status = agentcall.StatusCanceled
	}
	return agentcall.Response{CallID: record.callID, Status: status, Target: &target, Error: callFailure(code, message, target)}
}

func callFailure(code, message string, target agentcall.Target) *agentcall.Error {
	return &agentcall.Error{Code: code, Message: message, Target: &target}
}

func successfulAgentCallResponseDefinitelyTooLarge(response agentcall.Response) bool {
	if response.Result == nil {
		return false
	}
	const minimumResultEnvelopeBytes = len(`{"result":{"target":{"kind":"","name":""},"response":""}}`)
	return len(response.Result.Response) > control.MaxControlMessageBytes-minimumResultEnvelopeBytes
}

func oversizedAgentCallFailure(record *acceptedAgentCall, target agentcall.Target) agentcall.Response {
	return acceptedFailure(
		record,
		agentcall.CodeResultTooLarge,
		fmt.Sprintf("called agent result exceeds the %d-byte control message limit; inspect the persisted call output", control.MaxControlMessageBytes),
		target,
	)
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

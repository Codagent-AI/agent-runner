// Package control contains the authenticated, run-scoped control plane used
// by interactive completion and runner-owned agent integrations.
package control

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/runlock"
)

const (
	EnvControlSocket = "AGENT_RUNNER_CONTROL_SOCKET"
	EnvRunID         = "AGENT_RUNNER_RUN_ID"
	EnvStepID        = "AGENT_RUNNER_STEP_ID"
	EnvAttemptID     = "AGENT_RUNNER_ATTEMPT_ID"
	EnvControlToken  = "AGENT_RUNNER_CONTROL_TOKEN"

	MessageCompleteStep  = "complete_step"
	MessageTurnCommitted = "turn_committed"
	MessageAgentCall     = "agent_call"

	ControlSocketPointerFile  = "control-socket"
	MaxControlMessageBytes    = 16 * 1024 * 1024
	ControlConnectionDeadline = 5 * time.Second
)

// EnvironmentVariables returns every attempt-scoped credential variable that
// must be removed from inherited agent environments before selectively adding
// a fresh active attempt.
func EnvironmentVariables() []string {
	return []string{EnvControlSocket, EnvRunID, EnvStepID, EnvAttemptID, EnvControlToken}
}

// ControlConfig identifies a run-scoped control endpoint. Callers create the
// server only after acquiring the run lock; that lock is what makes removal of
// a stale socket safe on resume.
type ControlConfig struct {
	RunID     string
	RunDir    string
	TempDir   string
	LockProof runlock.HeldProof
	Logger    audit.EventLogger
	Now       func() time.Time
}

// Attempt is the child-facing identity and credential for one step attempt.
type Attempt struct {
	ID         string
	RunID      string
	StepID     string
	Token      string
	SocketPath string
	Context    context.Context
}

// EnvironmentMap returns the exact control environment injected into a child.
func (a *Attempt) EnvironmentMap() map[string]string {
	return map[string]string{
		EnvControlSocket: a.SocketPath,
		EnvRunID:         a.RunID,
		EnvStepID:        a.StepID,
		EnvAttemptID:     a.ID,
		EnvControlToken:  a.Token,
	}
}

// Environment returns the control environment as key=value entries.
func (a *Attempt) Environment() []string {
	values := a.EnvironmentMap()
	return []string{
		EnvControlSocket + "=" + values[EnvControlSocket],
		EnvRunID + "=" + values[EnvRunID],
		EnvStepID + "=" + values[EnvStepID],
		EnvAttemptID + "=" + values[EnvAttemptID],
		EnvControlToken + "=" + values[EnvControlToken],
	}
}

// CompletionRequest is emitted only once for an accepted attempt.
type CompletionRequest struct {
	AttemptID     string
	RunID         string
	StepID        string
	RequestID     string
	Checkpoint    cli.Checkpoint
	CheckpointErr error
}

// CommittedTurn is authenticated semantic evidence from a CLI post-turn hook.
type CommittedTurn struct {
	AttemptID string
	RunID     string
	StepID    string
	RequestID string
}

type controlRequest struct {
	Type      string          `json:"type"`
	RunID     string          `json:"run_id"`
	StepID    string          `json:"step_id"`
	AttemptID string          `json:"attempt_id,omitempty"`
	Token     string          `json:"token"`
	RequestID string          `json:"request_id"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type controlResponse struct {
	OK bool `json:"ok"`
	// Receipt echoes the accepted completion request's ID so the client can
	// print a receipt line into the CLI's tool output. Idempotent retries and
	// duplicate completions repeat the originally accepted receipt. Older
	// clients ignore the field.
	Receipt string          `json:"receipt,omitempty"`
	Error   string          `json:"error,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// AgentCallRequest is an authenticated, admitted call envelope. Admission is
// deliberately distinct from acceptance: the registered supervising handler
// remains responsible for validation and request-ID reservation.
type AgentCallRequest struct {
	AttemptID string
	RequestID string
	Payload   json.RawMessage
}

type AgentCallHandler interface {
	HandleAgentCall(context.Context, AgentCallRequest) json.RawMessage
}

type AttemptOptions struct {
	Checkpoint        func() (cli.Checkpoint, error)
	AgentCallEligible bool
	AgentCallHandler  AgentCallHandler
}

type attemptState struct {
	Attempt
	completionAccepted bool
	acceptedRequestID  string
	checkpoint         func() (cli.Checkpoint, error)
	agentCallEligible  bool
	agentCallHandler   AgentCallHandler
	cancel             context.CancelFunc
}

type acceptedCompletion struct {
	// ready is closed once the accept-time checkpoint has been captured and
	// request holds its final value. Retries block on it so every client is
	// acknowledged only after the checkpoint exists.
	ready     chan struct{}
	request   CompletionRequest
	delivered bool
}

// ControlServer owns one Unix socket for a workflow run and rotates its active
// credential for each runner-integrated agent attempt.
type ControlServer struct {
	runID       string
	runDir      string
	socketPath  string
	pointerPath string
	listener    net.Listener
	logger      audit.EventLogger
	now         func() time.Time

	mu          sync.Mutex
	active      *attemptState
	accepted    map[string]*acceptedCompletion
	committed   map[string]struct{}
	turnWaiters map[string]map[uint64]chan struct{}
	nextWaiter  uint64
	completions chan CompletionRequest
	turns       chan CommittedTurn
	done        chan struct{}

	closeOnce sync.Once
	closeErr  error
	wg        sync.WaitGroup
}

// NewControlServer binds the private run endpoint and writes its pointer file.
func NewControlServer(config *ControlConfig) (*ControlServer, error) {
	if err := controlPlatformError(); err != nil {
		return nil, fmt.Errorf("create control endpoint: %w", err)
	}
	if strings.TrimSpace(config.RunID) == "" {
		return nil, errors.New("create control endpoint: run ID is required")
	}
	if strings.TrimSpace(config.RunDir) == "" {
		return nil, errors.New("create control endpoint: run directory is required")
	}
	if err := config.LockProof.Validate(config.RunDir); err != nil {
		return nil, fmt.Errorf("create control endpoint: invalid run lock proof: %w", err)
	}
	tempDir := config.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	privateDir := filepath.Join(tempDir, "agent-runner-"+platformUserID())
	if err := os.MkdirAll(privateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create private control directory: %w", err)
	}
	if err := os.Chmod(privateDir, 0o700); err != nil { // #nosec G302 -- a private directory needs owner execute permission
		return nil, fmt.Errorf("secure private control directory: %w", err)
	}
	// Run IDs are only unique within one project's run directory tree, so the
	// socket name must also cover the canonical run directory: two projects
	// with the same run ID must never share a socket in the per-user temp dir.
	canonicalRunDir, err := filepath.Abs(config.RunDir)
	if err != nil {
		return nil, fmt.Errorf("resolve run directory for control socket: %w", err)
	}
	digest := sha256.Sum256([]byte(canonicalRunDir + "\x00" + config.RunID))
	socketPath := filepath.Join(privateDir, hex.EncodeToString(digest[:12])+".sock")
	if len(socketPath) >= 104 {
		return nil, fmt.Errorf("create control endpoint: socket path is %d bytes, exceeds macOS sun_path limit: %s", len(socketPath), socketPath)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale control socket: %w", err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on control socket: %w", err)
	}
	cleanup := func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("secure control socket: %w", err)
	}
	pointerPath := filepath.Join(config.RunDir, ControlSocketPointerFile)
	if err := os.WriteFile(pointerPath, []byte(socketPath+"\n"), 0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("write control socket pointer: %w", err)
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	server := &ControlServer{
		runID:       config.RunID,
		runDir:      config.RunDir,
		socketPath:  socketPath,
		pointerPath: pointerPath,
		listener:    listener,
		logger:      config.Logger,
		now:         now,
		accepted:    make(map[string]*acceptedCompletion),
		committed:   make(map[string]struct{}),
		turnWaiters: make(map[string]map[uint64]chan struct{}),
		completions: make(chan CompletionRequest, 1),
		turns:       make(chan CommittedTurn, 1),
		done:        make(chan struct{}),
	}
	server.wg.Add(1)
	go server.acceptLoop()
	return server, nil
}

func (s *ControlServer) SocketPath() string { return s.socketPath }

// ActivateWithCheckpoint rotates the credential and binds the durability
// checkpoint captured synchronously when completion is accepted.
func (s *ControlServer) ActivateWithCheckpoint(stepID string, checkpoint func() (cli.Checkpoint, error)) Attempt {
	return s.Activate(context.Background(), stepID, checkpoint)
}

// Activate rotates the credential and binds an attempt lifetime plus the
// durability checkpoint captured synchronously when completion is accepted.
func (s *ControlServer) Activate(ctx context.Context, stepID string, checkpoint func() (cli.Checkpoint, error)) Attempt {
	return s.ActivateAttempt(ctx, stepID, AttemptOptions{Checkpoint: checkpoint})
}

// ActivateAttempt rotates the credential and binds all Runner-owned controls
// enabled for one parent invocation.
func (s *ControlServer) ActivateAttempt(ctx context.Context, stepID string, options AttemptOptions) Attempt {
	if ctx == nil {
		ctx = context.Background()
	}
	attemptContext, cancel := context.WithCancel(ctx)
	attempt := Attempt{
		ID:         uuid.NewString(),
		RunID:      s.runID,
		StepID:     stepID,
		Token:      strings.ReplaceAll(uuid.NewString(), "-", ""),
		SocketPath: s.socketPath,
		Context:    attemptContext,
	}
	s.mu.Lock()
	previous := s.active
	s.active = &attemptState{
		Attempt: attempt, checkpoint: options.Checkpoint, cancel: cancel,
		agentCallEligible: options.AgentCallEligible, agentCallHandler: options.AgentCallHandler,
	}
	s.mu.Unlock()
	if previous != nil {
		previous.cancel()
	}
	return attempt
}

// Deactivate invalidates the current credential while leaving the run socket alive.
func (s *ControlServer) Deactivate() {
	s.mu.Lock()
	active := s.active
	s.active = nil
	s.mu.Unlock()
	if active != nil {
		active.cancel()
	}
}

func (s *ControlServer) Completions() <-chan CompletionRequest { return s.completions }
func (s *ControlServer) CommittedTurns() <-chan CommittedTurn  { return s.turns }

// SubscribeCommittedTurn returns evidence scoped to one attempt. The
// unsubscribe function removes a waiter whose durability was confirmed by a
// different source, so it cannot consume or retain a later attempt's event.
func (s *ControlServer) SubscribeCommittedTurn(attemptID string) (committed <-chan struct{}, unsubscribe func()) {
	s.mu.Lock()
	waiter := make(chan struct{})
	if _, ok := s.committed[attemptID]; ok {
		close(waiter)
		s.mu.Unlock()
		return waiter, func() {}
	}
	s.nextWaiter++
	waiterID := s.nextWaiter
	if s.turnWaiters[attemptID] == nil {
		s.turnWaiters[attemptID] = make(map[uint64]chan struct{})
	}
	s.turnWaiters[attemptID][waiterID] = waiter
	s.mu.Unlock()

	return waiter, func() {
		s.mu.Lock()
		waiters := s.turnWaiters[attemptID]
		delete(waiters, waiterID)
		if len(waiters) == 0 {
			delete(s.turnWaiters, attemptID)
		}
		s.mu.Unlock()
	}
}

// Close stops the server and removes both the socket and its run-directory pointer.
func (s *ControlServer) Close() error {
	s.closeOnce.Do(func() {
		s.Deactivate()
		close(s.done)
		if err := s.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			s.closeErr = err
		}
		s.wg.Wait()
		for _, path := range []string{s.socketPath, s.pointerPath} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) && s.closeErr == nil {
				s.closeErr = err
			}
		}
	})
	return s.closeErr
}

func (s *ControlServer) acceptLoop() {
	defer s.wg.Done()
	for {
		connection, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConnection(connection)
		}()
	}
}

func (s *ControlServer) handleConnection(connection net.Conn) {
	defer func() { _ = connection.Close() }()
	_ = connection.SetDeadline(time.Now().Add(ControlConnectionDeadline))
	request, err := readControlRequest(connection)
	if err != nil {
		s.reject(connection, err.Error(), &request)
		return
	}

	s.mu.Lock()
	cacheKey := requestCacheKey(&request)
	if cached, ok := s.accepted[cacheKey]; ok {
		s.mu.Unlock()
		s.acknowledgeCompletion(connection, cached)
		return
	}
	active := s.active
	if err := validateControlRequest(&request, active, s.runID); err != nil {
		s.mu.Unlock()
		s.reject(connection, err.Error(), &request)
		return
	}

	switch request.Type {
	case MessageCompleteStep:
		s.handleCompletion(connection, &request, active, cacheKey)
	case MessageTurnCommitted:
		s.handleCommittedTurn(connection, &request, active)
	case MessageAgentCall:
		s.handleAgentCall(connection, &request, active)
	}
}

func (s *ControlServer) handleAgentCall(connection net.Conn, request *controlRequest, active *attemptState) {
	if !active.agentCallEligible || active.agentCallHandler == nil {
		s.mu.Unlock()
		s.reject(connection, "call_agent is not enabled for the active step attempt", request)
		return
	}
	handler := active.agentCallHandler
	parentContext := active.Context
	s.mu.Unlock()

	// Calls have no Runner deadline. Once authentication framing is complete,
	// the connection itself leases the work: EOF, bridge exit, or request
	// cancellation closes the socket and cancels the handler context.
	_ = connection.SetDeadline(time.Time{})
	leaseContext, cancel := context.WithCancel(parentContext)
	defer cancel()
	go func() {
		var one [1]byte
		_, _ = connection.Read(one[:])
		cancel()
	}()
	payload := handler.HandleAgentCall(leaseContext, AgentCallRequest{
		AttemptID: request.AttemptID,
		RequestID: request.RequestID,
		Payload:   append(json.RawMessage(nil), request.Payload...),
	})
	_ = writeControlResponse(connection, controlResponse{OK: true, Payload: payload})
}

func (s *ControlServer) handleCompletion(connection net.Conn, request *controlRequest, active *attemptState, cacheKey string) {
	if active.completionAccepted {
		receipt := active.acceptedRequestID
		s.mu.Unlock()
		_ = writeControlResponse(connection, controlResponse{OK: true, Receipt: receipt})
		return
	}
	active.completionAccepted = true
	active.acceptedRequestID = request.RequestID
	accepted := &acceptedCompletion{
		ready:   make(chan struct{}),
		request: CompletionRequest{AttemptID: active.ID, RunID: request.RunID, StepID: request.StepID, RequestID: request.RequestID},
	}
	s.accepted[cacheKey] = accepted
	checkpoint := active.checkpoint
	s.mu.Unlock()

	// The checkpoint is the accept-time baseline, so it must be captured
	// before any client is acknowledged, but it can stall on a slow native
	// store and therefore must not run while holding the server mutex.
	s.emit(audit.EventCompletionRequested, request.StepID, map[string]any{"request_id": request.RequestID, "attempt_id": accepted.request.AttemptID})
	var captured cli.Checkpoint
	var capturedErr error
	if checkpoint != nil {
		captured, capturedErr = checkpoint()
	}
	s.mu.Lock()
	accepted.request.Checkpoint, accepted.request.CheckpointErr = captured, capturedErr
	s.mu.Unlock()
	close(accepted.ready)
	s.acknowledgeCompletion(connection, accepted)
}

// acknowledgeCompletion waits for the accept-time checkpoint, acknowledges the
// client, and delivers the completion to the consumer exactly once across
// idempotent retries.
func (s *ControlServer) acknowledgeCompletion(connection net.Conn, accepted *acceptedCompletion) {
	<-accepted.ready
	if writeControlResponse(connection, controlResponse{OK: true, Receipt: accepted.request.RequestID}) != nil {
		return
	}
	s.mu.Lock()
	alreadyDelivered := accepted.delivered
	accepted.delivered = true
	completion := accepted.request
	s.mu.Unlock()
	if alreadyDelivered {
		return
	}
	s.emit(audit.EventCompletionAcknowledged, completion.StepID, map[string]any{"request_id": completion.RequestID, "attempt_id": completion.AttemptID})
	s.deliverCompletion(&completion)
}

func (s *ControlServer) handleCommittedTurn(connection net.Conn, request *controlRequest, active *attemptState) {
	if !active.completionAccepted {
		s.mu.Unlock()
		// Post-turn hooks run after every agent turn, including conversational
		// turns before the step is complete. Acknowledge those notifications so
		// the CLI does not surface a hook error, but do not retain them as
		// durability evidence for a later completion request.
		_ = writeControlResponse(connection, controlResponse{OK: true})
		return
	}
	turnKey := active.ID
	if _, duplicate := s.committed[turnKey]; duplicate {
		s.mu.Unlock()
		_ = writeControlResponse(connection, controlResponse{OK: true})
		return
	}
	turn := CommittedTurn{AttemptID: active.ID, RunID: request.RunID, StepID: request.StepID, RequestID: request.RequestID}
	if writeControlResponse(connection, controlResponse{OK: true}) != nil {
		s.mu.Unlock()
		return
	}
	s.committed[turnKey] = struct{}{}
	s.mu.Unlock()
	s.emit(audit.EventTurnCommitted, request.StepID, map[string]any{"request_id": request.RequestID, "attempt_id": active.ID})
	s.deliverCommittedTurn(turn)
}

func (s *ControlServer) deliverCompletion(request *CompletionRequest) {
	deliverBeforeShutdown(s.completions, *request, s.done)
}

func (s *ControlServer) deliverCommittedTurn(turn CommittedTurn) {
	s.mu.Lock()
	waiters := s.turnWaiters[turn.AttemptID]
	delete(s.turnWaiters, turn.AttemptID)
	for _, waiter := range waiters {
		close(waiter)
	}
	s.mu.Unlock()
	deliverBeforeShutdown(s.turns, turn, s.done)
}

// deliverBeforeShutdown prefers an already-acknowledged event over shutdown.
// The shutdown channel is consulted only when delivery would otherwise block.
func deliverBeforeShutdown[T any](destination chan<- T, value T, done <-chan struct{}) {
	select {
	case destination <- value:
		return
	default:
	}
	select {
	case destination <- value:
	case <-done:
	}
}

func readControlRequest(reader io.Reader) (controlRequest, error) {
	limited := io.LimitReader(reader, MaxControlMessageBytes+1)
	line, err := bufio.NewReader(limited).ReadBytes('\n')
	if len(line) > MaxControlMessageBytes {
		return controlRequest{}, fmt.Errorf("control message exceeds %d-byte limit", MaxControlMessageBytes)
	}
	if err != nil {
		return controlRequest{}, fmt.Errorf("read single-line control message: %w", err)
	}
	line = line[:len(line)-1]
	decoder := json.NewDecoder(strings.NewReader(string(line)))
	decoder.DisallowUnknownFields()
	var request controlRequest
	if err := decoder.Decode(&request); err != nil {
		return controlRequest{}, fmt.Errorf("decode control message: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return controlRequest{}, err
	}
	return request, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode control message: multiple JSON values")
		}
		return fmt.Errorf("decode control message: %w", err)
	}
	return nil
}

func validateControlRequest(request *controlRequest, active *attemptState, runID string) error {
	if active == nil {
		return errors.New("no interactive step is active")
	}
	if request.Type != MessageCompleteStep && request.Type != MessageTurnCommitted && request.Type != MessageAgentCall {
		return fmt.Errorf("unknown control message type %q", request.Type)
	}
	if request.RunID == "" || request.StepID == "" || request.Token == "" || request.RequestID == "" {
		return errors.New("control message is missing required fields")
	}
	if request.RunID != runID || request.RunID != active.RunID || request.StepID != active.StepID || request.Token != active.Token {
		return errors.New("control credential does not match the active step attempt")
	}
	if request.Type == MessageAgentCall && request.AttemptID != active.ID {
		return errors.New("control credential does not match the active step attempt")
	}
	return nil
}

func requestCacheKey(request *controlRequest) string {
	return strings.Join([]string{request.Type, request.RunID, request.StepID, request.Token, request.RequestID}, "\x00")
}

func writeControlResponse(writer io.Writer, response controlResponse) error {
	return json.NewEncoder(writer).Encode(response)
}

func (s *ControlServer) reject(connection net.Conn, reason string, request *controlRequest) {
	s.emit(audit.EventControlRejected, request.StepID, map[string]any{"reason": reason, "message_type": request.Type, "request_id": request.RequestID})
	_ = writeControlResponse(connection, controlResponse{OK: false, Error: reason})
}

func (s *ControlServer) emit(eventType audit.EventType, stepID string, data map[string]any) {
	if s.logger == nil {
		return
	}
	prefix := ""
	if stepID != "" {
		prefix = audit.BuildPrefix(nil, stepID)
	}
	s.logger.Emit(audit.Event{
		Timestamp: s.now().UTC().Format(time.RFC3339Nano),
		Prefix:    prefix,
		Type:      eventType,
		Data:      data,
	})
}

// SendControlEventFromEnvironment sends one authenticated event using only
// the child environment and returns the server's acknowledgement receipt —
// the accepted completion request's ID — so the caller can print it into the
// CLI-visible tool output. A lost acknowledgement is retried once with the
// same request ID, so the server can answer idempotently and the retry yields
// the same receipt.
func SendControlEventFromEnvironment(ctx context.Context, messageType string, getenv func(string) string) (string, error) {
	if err := controlPlatformError(); err != nil {
		return "", err
	}
	if messageType != MessageCompleteStep && messageType != MessageTurnCommitted {
		return "", fmt.Errorf("unsupported control message type %q", messageType)
	}
	values, missing := requiredEnvironmentValues(getenv, EnvControlSocket, EnvRunID, EnvStepID, EnvControlToken)
	if missing != "" {
		return "", fmt.Errorf("%s must run inside an interactive agent step session (missing %s)", controlCommandName(messageType), missing)
	}
	request := controlRequest{
		Type:      messageType,
		RunID:     values[EnvRunID],
		StepID:    values[EnvStepID],
		Token:     values[EnvControlToken],
		RequestID: uuid.NewString(),
	}
	for attempt := 0; attempt < 2; attempt++ {
		receipt, retryable, err := sendControlRequest(ctx, values[EnvControlSocket], &request)
		if err == nil {
			return receipt, nil
		}
		if !retryable || attempt == 1 {
			return "", err
		}
	}
	return "", errors.New("control request failed")
}

// SendAgentCallFromEnvironment forwards one typed agent-call payload and
// keeps the authenticated connection open as the call's lease. It applies no
// Runner timeout; only the caller's context deadline or cancellation bounds
// the request.
func SendAgentCallFromEnvironment(
	ctx context.Context,
	requestID string,
	payload json.RawMessage,
	getenv func(string) string,
) (json.RawMessage, error) {
	if err := controlPlatformError(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(requestID) == "" || len(payload) == 0 {
		return nil, errors.New("agent-call control request is missing request ID or payload")
	}
	values, missing := requiredEnvironmentValues(getenv, EnvironmentVariables()...)
	if missing != "" {
		return nil, fmt.Errorf("agent-runner internal call-agent-mcp must run inside an enabled agent step (missing %s)", missing)
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", values[EnvControlSocket])
	if err != nil {
		return nil, fmt.Errorf("connect to agent-call control socket: %w", err)
	}
	defer func() { _ = connection.Close() }()

	writeDeadline := time.Now().Add(ControlConnectionDeadline)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(writeDeadline) {
		writeDeadline = deadline
	}
	_ = connection.SetWriteDeadline(writeDeadline)
	request := controlRequest{
		Type: MessageAgentCall, RunID: values[EnvRunID], StepID: values[EnvStepID],
		AttemptID: values[EnvAttemptID], Token: values[EnvControlToken],
		RequestID: requestID, Payload: payload,
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return nil, fmt.Errorf("send agent-call control request: %w", err)
	}
	_ = connection.SetWriteDeadline(time.Time{})
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetReadDeadline(deadline)
	} else {
		_ = connection.SetReadDeadline(time.Time{})
	}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-finished:
		}
	}()
	var response controlResponse
	if err := json.NewDecoder(io.LimitReader(connection, MaxControlMessageBytes+1)).Decode(&response); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("read agent-call control response: %w", err)
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "agent-call control request rejected"
		}
		return nil, errors.New(response.Error)
	}
	if len(response.Payload) == 0 {
		return nil, errors.New("agent-call control response is missing payload")
	}
	return append(json.RawMessage(nil), response.Payload...), nil
}

func requiredEnvironmentValues(getenv func(string) string, keys ...string) (values map[string]string, missing string) {
	values = make(map[string]string, len(keys))
	for _, key := range keys {
		value := getenv(key)
		if value == "" {
			return nil, key
		}
		values[key] = value
	}
	return values, ""
}

func sendControlRequest(ctx context.Context, socketPath string, request *controlRequest) (receipt string, retryable bool, err error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return "", false, fmt.Errorf("connect to interactive step control socket: %w", err)
	}
	defer func() { _ = connection.Close() }()
	deadline := time.Now().Add(ControlConnectionDeadline)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = connection.SetDeadline(deadline)
	payload, err := json.Marshal(request)
	if err != nil {
		return "", false, err
	}
	payload = append(payload, '\n')
	written, err := connection.Write(payload)
	if err != nil {
		return "", written > 0, fmt.Errorf("send interactive step control message: %w", err)
	}
	var response controlResponse
	if err := json.NewDecoder(io.LimitReader(connection, MaxControlMessageBytes+1)).Decode(&response); err != nil {
		return "", true, fmt.Errorf("read interactive step control acknowledgement: %w", err)
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "control request rejected"
		}
		return "", false, errors.New(response.Error)
	}
	return response.Receipt, false, nil
}

func controlCommandName(messageType string) string {
	if messageType == MessageTurnCommitted {
		return "agent-runner internal turn-committed"
	}
	return "agent-runner step complete"
}

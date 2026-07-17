// Package interactive contains the out-of-band control plane and direct
// interactive execution primitives. The control plane is intentionally
// usable before the production PTY execution path is cut over.
package interactive

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
)

const (
	EnvControlSocket = "AGENT_RUNNER_CONTROL_SOCKET"
	EnvRunID         = "AGENT_RUNNER_RUN_ID"
	EnvStepID        = "AGENT_RUNNER_STEP_ID"
	EnvControlToken  = "AGENT_RUNNER_CONTROL_TOKEN"

	MessageCompleteStep  = "complete_step"
	MessageTurnCommitted = "turn_committed"

	ControlSocketPointerFile  = "control-socket"
	MaxControlMessageBytes    = 4 * 1024
	ControlConnectionDeadline = 5 * time.Second
)

// ControlConfig identifies a run-scoped control endpoint. Callers create the
// server only after acquiring the run lock; that lock is what makes removal of
// a stale socket safe on resume.
type ControlConfig struct {
	RunID   string
	RunDir  string
	TempDir string
	Logger  audit.EventLogger
	Now     func() time.Time
}

// Attempt is the child-facing identity and credential for one step attempt.
type Attempt struct {
	ID         string
	RunID      string
	StepID     string
	Token      string
	SocketPath string
}

// EnvironmentMap returns the exact control environment injected into a child.
func (a *Attempt) EnvironmentMap() map[string]string {
	return map[string]string{
		EnvControlSocket: a.SocketPath,
		EnvRunID:         a.RunID,
		EnvStepID:        a.StepID,
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
		EnvControlToken + "=" + values[EnvControlToken],
	}
}

// CompletionRequest is emitted only once for an accepted attempt.
type CompletionRequest struct {
	AttemptID string
	RunID     string
	StepID    string
	RequestID string
}

// CommittedTurn is authenticated semantic evidence from a CLI post-turn hook.
type CommittedTurn struct {
	AttemptID string
	RunID     string
	StepID    string
	RequestID string
}

type controlRequest struct {
	Type      string `json:"type"`
	RunID     string `json:"run_id"`
	StepID    string `json:"step_id"`
	Token     string `json:"token"`
	RequestID string `json:"request_id"`
}

type controlResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type attemptState struct {
	Attempt
	completionAccepted bool
}

type acceptedCompletion struct {
	request   CompletionRequest
	delivered bool
}

// ControlServer owns one Unix socket for a workflow run and rotates its active
// credential for each interactive step attempt.
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
	completions chan CompletionRequest
	turns       chan CommittedTurn

	closeOnce sync.Once
	closeErr  error
	wg        sync.WaitGroup
}

// NewControlServer binds the private run endpoint and writes its pointer file.
func NewControlServer(config ControlConfig) (*ControlServer, error) {
	if strings.TrimSpace(config.RunID) == "" {
		return nil, errors.New("create control endpoint: run ID is required")
	}
	if strings.TrimSpace(config.RunDir) == "" {
		return nil, errors.New("create control endpoint: run directory is required")
	}
	tempDir := config.TempDir
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	privateDir := filepath.Join(tempDir, fmt.Sprintf("agent-runner-%d", os.Getuid()))
	if err := os.MkdirAll(privateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create private control directory: %w", err)
	}
	if err := os.Chmod(privateDir, 0o700); err != nil {
		return nil, fmt.Errorf("secure private control directory: %w", err)
	}
	digest := sha256.Sum256([]byte(config.RunID))
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
		completions: make(chan CompletionRequest, 1),
		turns:       make(chan CommittedTurn, 1),
	}
	server.wg.Add(1)
	go server.acceptLoop()
	return server, nil
}

func (s *ControlServer) SocketPath() string { return s.socketPath }

// Activate rotates the credential and makes one interactive attempt current.
func (s *ControlServer) Activate(stepID string) Attempt {
	attempt := Attempt{
		ID:         uuid.NewString(),
		RunID:      s.runID,
		StepID:     stepID,
		Token:      strings.ReplaceAll(uuid.NewString(), "-", ""),
		SocketPath: s.socketPath,
	}
	s.mu.Lock()
	s.active = &attemptState{Attempt: attempt}
	s.mu.Unlock()
	return attempt
}

// Deactivate invalidates the current credential while leaving the run socket alive.
func (s *ControlServer) Deactivate() {
	s.mu.Lock()
	s.active = nil
	s.mu.Unlock()
}

func (s *ControlServer) Completions() <-chan CompletionRequest { return s.completions }
func (s *ControlServer) CommittedTurns() <-chan CommittedTurn  { return s.turns }

// Close stops the server and removes both the socket and its run-directory pointer.
func (s *ControlServer) Close() error {
	s.closeOnce.Do(func() {
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
		writeErr := writeControlResponse(connection, controlResponse{OK: true})
		if writeErr == nil && !cached.delivered {
			cached.delivered = true
			accepted := cached.request
			s.mu.Unlock()
			s.emit(audit.EventCompletionAcknowledged, accepted.StepID, map[string]any{"request_id": accepted.RequestID, "attempt_id": accepted.AttemptID})
			s.completions <- accepted
			return
		}
		s.mu.Unlock()
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
		if active.completionAccepted {
			s.mu.Unlock()
			_ = writeControlResponse(connection, controlResponse{OK: true})
			return
		}
		active.completionAccepted = true
		accepted := CompletionRequest{AttemptID: active.ID, RunID: request.RunID, StepID: request.StepID, RequestID: request.RequestID}
		s.accepted[cacheKey] = &acceptedCompletion{request: accepted}
		s.mu.Unlock()
		s.emit(audit.EventCompletionRequested, request.StepID, map[string]any{"request_id": request.RequestID, "attempt_id": active.ID})
		if writeControlResponse(connection, controlResponse{OK: true}) == nil {
			s.mu.Lock()
			s.accepted[cacheKey].delivered = true
			s.mu.Unlock()
			s.emit(audit.EventCompletionAcknowledged, request.StepID, map[string]any{"request_id": request.RequestID, "attempt_id": active.ID})
			s.completions <- accepted
		}
	case MessageTurnCommitted:
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
		s.turns <- turn
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
	if request.Type != MessageCompleteStep && request.Type != MessageTurnCommitted {
		return fmt.Errorf("unknown control message type %q", request.Type)
	}
	if request.RunID == "" || request.StepID == "" || request.Token == "" || request.RequestID == "" {
		return errors.New("control message is missing required fields")
	}
	if request.RunID != runID || request.RunID != active.RunID || request.StepID != active.StepID || request.Token != active.Token {
		return errors.New("control credential does not match the active step attempt")
	}
	return nil
}

func requestCacheKey(request *controlRequest) string {
	return strings.Join([]string{request.RunID, request.StepID, request.Token, request.RequestID}, "\x00")
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
// the child environment. A lost acknowledgement is retried once with the same
// request ID so the server can answer idempotently.
func SendControlEventFromEnvironment(ctx context.Context, messageType string, getenv func(string) string) error {
	if messageType != MessageCompleteStep && messageType != MessageTurnCommitted {
		return fmt.Errorf("unsupported control message type %q", messageType)
	}
	values := map[string]string{
		EnvControlSocket: getenv(EnvControlSocket),
		EnvRunID:         getenv(EnvRunID),
		EnvStepID:        getenv(EnvStepID),
		EnvControlToken:  getenv(EnvControlToken),
	}
	for _, key := range []string{EnvControlSocket, EnvRunID, EnvStepID, EnvControlToken} {
		if values[key] == "" {
			return fmt.Errorf("%s must run inside an interactive agent step session (missing %s)", controlCommandName(messageType), key)
		}
	}
	request := controlRequest{
		Type:      messageType,
		RunID:     values[EnvRunID],
		StepID:    values[EnvStepID],
		Token:     values[EnvControlToken],
		RequestID: uuid.NewString(),
	}
	for attempt := 0; attempt < 2; attempt++ {
		retryable, err := sendControlRequest(ctx, values[EnvControlSocket], &request)
		if err == nil {
			return nil
		}
		if !retryable || attempt == 1 {
			return err
		}
	}
	return errors.New("control request failed")
}

func sendControlRequest(ctx context.Context, socketPath string, request *controlRequest) (bool, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return false, fmt.Errorf("connect to interactive step control socket: %w", err)
	}
	defer func() { _ = connection.Close() }()
	deadline := time.Now().Add(ControlConnectionDeadline)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = connection.SetDeadline(deadline)
	payload, err := json.Marshal(request)
	if err != nil {
		return false, err
	}
	payload = append(payload, '\n')
	written, err := connection.Write(payload)
	if err != nil {
		return written > 0, fmt.Errorf("send interactive step control message: %w", err)
	}
	var response controlResponse
	if err := json.NewDecoder(io.LimitReader(connection, MaxControlMessageBytes+1)).Decode(&response); err != nil {
		return true, fmt.Errorf("read interactive step control acknowledgement: %w", err)
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "control request rejected"
		}
		return false, errors.New(response.Error)
	}
	return false, nil
}

func controlCommandName(messageType string) string {
	if messageType == MessageTurnCommitted {
		return "agent-runner internal turn-committed"
	}
	return "agent-runner step complete"
}

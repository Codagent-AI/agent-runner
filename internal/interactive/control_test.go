package interactive

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/runlock"
)

func TestControlServerCreatesPrivateEndpointAndAttemptEnvironment(t *testing.T) {
	runDir := t.TempDir()
	tempDir := shortTempDir(t)
	logger := &recordingEventLogger{}
	server, err := NewControlServer(&ControlConfig{
		RunID:     "run-with-a-deliberately-long-identifier",
		RunDir:    runDir,
		TempDir:   tempDir,
		LockProof: heldRunLockProof(t, runDir),
		Logger:    logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })

	if len(server.SocketPath()) >= 104 {
		t.Fatalf("socket path length = %d, want below macOS sun_path limit: %s", len(server.SocketPath()), server.SocketPath())
	}
	if got := filepath.Dir(server.SocketPath()); got != filepath.Join(tempDir, fmt.Sprintf("agent-runner-%d", os.Getuid())) {
		t.Fatalf("socket directory = %q", got)
	}
	assertMode(t, filepath.Dir(server.SocketPath()), 0o700)
	assertMode(t, server.SocketPath(), 0o600)
	pointer, err := os.ReadFile(filepath.Join(runDir, ControlSocketPointerFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(pointer)); got != server.SocketPath() {
		t.Fatalf("pointer = %q, want %q", got, server.SocketPath())
	}

	attempt := server.ActivateWithCheckpoint("review", nil)
	want := map[string]string{
		EnvControlSocket: server.SocketPath(),
		EnvRunID:         "run-with-a-deliberately-long-identifier",
		EnvStepID:        "review",
		EnvControlToken:  attempt.Token,
	}
	if diff := cmp.Diff(want, attempt.EnvironmentMap()); diff != "" {
		t.Fatalf("attempt environment mismatch (-want +got):\n%s", diff)
	}
	if attempt.Token == "" {
		t.Fatal("attempt token is empty")
	}

	previous := attempt
	attempt = server.ActivateWithCheckpoint("review", nil)
	if attempt.Token == previous.Token || attempt.ID == previous.ID {
		t.Fatalf("attempt credential was not rotated: before=%#v after=%#v", previous, attempt)
	}
}

func TestControlServerSocketPathIsUniquePerRunDirectory(t *testing.T) {
	tempDir := shortTempDir(t)
	logger := &recordingEventLogger{}
	paths := make(map[string]string)
	for _, name := range []string{"project-a", "project-b"} {
		runDir := t.TempDir()
		server, err := NewControlServer(&ControlConfig{
			RunID:     "run",
			RunDir:    runDir,
			TempDir:   tempDir,
			LockProof: heldRunLockProof(t, runDir),
			Logger:    logger,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = server.Close() })
		if len(server.SocketPath()) >= 104 {
			t.Fatalf("socket path length = %d, want below macOS sun_path limit: %s", len(server.SocketPath()), server.SocketPath())
		}
		paths[name] = server.SocketPath()
	}
	if paths["project-a"] == paths["project-b"] {
		t.Fatalf("socket path %q is shared by two projects with the same run ID", paths["project-a"])
	}
}

func TestControlServerCreationFailureIsDescriptive(t *testing.T) {
	root := t.TempDir()
	notDir := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	runDir := t.TempDir()
	_, err := NewControlServer(&ControlConfig{RunID: "run", RunDir: runDir, TempDir: notDir, LockProof: heldRunLockProof(t, runDir)})
	if err == nil || !strings.Contains(err.Error(), "create private control directory") {
		t.Fatalf("NewControlServer error = %v", err)
	}
}

func TestControlServerRequiresHeldRunLockProof(t *testing.T) {
	_, err := NewControlServer(&ControlConfig{
		RunID:     "run",
		RunDir:    t.TempDir(),
		TempDir:   shortTempDir(t),
		LockProof: runlock.HeldProof{},
	})
	if err == nil || !strings.Contains(err.Error(), "run lock proof") {
		t.Fatalf("NewControlServer error = %v", err)
	}
}

func TestControlServerCloseRemovesSocketAndPointer(t *testing.T) {
	runDir := t.TempDir()
	server := newTestControlServer(t, runDir, &recordingEventLogger{})
	socketPath := server.SocketPath()
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{socketPath, filepath.Join(runDir, ControlSocketPointerFile)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after close (err=%v)", path, err)
		}
	}
}

func TestControlServerCloseUnblocksBackpressuredDeliveries(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})

	first := server.ActivateWithCheckpoint("first", nil)
	if response := exchange(t, server.SocketPath(), &controlRequest{
		Type: MessageCompleteStep, RunID: first.RunID, StepID: first.StepID, Token: first.Token, RequestID: "complete-first",
	}); !response.OK {
		t.Fatalf("first completion response = %#v", response)
	}
	if response := exchange(t, server.SocketPath(), &controlRequest{
		Type: MessageTurnCommitted, RunID: first.RunID, StepID: first.StepID, Token: first.Token, RequestID: "turn-first",
	}); !response.OK {
		t.Fatalf("first turn response = %#v", response)
	}

	second := server.ActivateWithCheckpoint("second", nil)
	if response := exchange(t, server.SocketPath(), &controlRequest{
		Type: MessageCompleteStep, RunID: second.RunID, StepID: second.StepID, Token: second.Token, RequestID: "complete-second",
	}); !response.OK {
		t.Fatalf("second completion response = %#v", response)
	}
	if response := exchange(t, server.SocketPath(), &controlRequest{
		Type: MessageTurnCommitted, RunID: second.RunID, StepID: second.StepID, Token: second.Token, RequestID: "turn-second",
	}); !response.OK {
		t.Fatalf("second turn response = %#v", response)
	}

	closed := make(chan error, 1)
	go func() { closed <- server.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Close blocked behind unread completion and turn deliveries")
	}
}

func TestControlServerPrefersDeliveryOverShutdownForAcknowledgedEvents(t *testing.T) {
	// The old implementation selected randomly between the buffered send and
	// the closed done channel, dropping acknowledged events roughly half the
	// time. Loop enough iterations to make the old behavior fail reliably.
	for iteration := 0; iteration < 200; iteration++ {
		server := &ControlServer{
			accepted:    make(map[string]*acceptedCompletion),
			committed:   make(map[string]struct{}),
			turnWaiters: make(map[string]map[uint64]chan struct{}),
			completions: make(chan CompletionRequest, 1),
			turns:       make(chan CommittedTurn, 1),
			done:        make(chan struct{}),
		}
		close(server.done)

		server.deliverCompletion(&CompletionRequest{RequestID: "acknowledged"})
		select {
		case completion := <-server.completions:
			if completion.RequestID != "acknowledged" {
				t.Fatalf("iteration %d: completion = %#v", iteration, completion)
			}
		default:
			t.Fatalf("iteration %d: acknowledged completion was dropped on shutdown", iteration)
		}

		server.deliverCommittedTurn(CommittedTurn{RequestID: "acknowledged"})
		select {
		case turn := <-server.turns:
			if turn.RequestID != "acknowledged" {
				t.Fatalf("iteration %d: turn = %#v", iteration, turn)
			}
		default:
			t.Fatalf("iteration %d: acknowledged committed turn was dropped on shutdown", iteration)
		}
	}
}

func TestControlServerAcceptsCurrentCredentialAndAcknowledgesIdempotently(t *testing.T) {
	logger := &recordingEventLogger{}
	server := newTestControlServer(t, t.TempDir(), logger)
	defer server.Close()
	attempt := server.ActivateWithCheckpoint("implement", nil)
	request := controlRequest{
		Type:      MessageCompleteStep,
		RunID:     attempt.RunID,
		StepID:    attempt.StepID,
		Token:     attempt.Token,
		RequestID: "request-1",
	}

	if response := exchange(t, server.SocketPath(), &request); !response.OK {
		t.Fatalf("completion response = %#v", response)
	}
	select {
	case got := <-server.Completions():
		if got.AttemptID != attempt.ID || got.RequestID != request.RequestID {
			t.Fatalf("completion = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("completion was not delivered")
	}

	if response := exchange(t, server.SocketPath(), &request); !response.OK {
		t.Fatalf("retry response = %#v", response)
	}
	request.RequestID = "request-2"
	if response := exchange(t, server.SocketPath(), &request); !response.OK {
		t.Fatalf("duplicate response = %#v", response)
	}
	select {
	case duplicate := <-server.Completions():
		t.Fatalf("duplicate completion advanced state: %#v", duplicate)
	case <-time.After(40 * time.Millisecond):
	}

	if diff := cmp.Diff([]audit.EventType{audit.EventCompletionRequested, audit.EventCompletionAcknowledged}, logger.types()); diff != "" {
		t.Fatalf("audit event types mismatch (-want +got):\n%s", diff)
	}
}

func TestControlServerCapturesDurabilityCheckpointBeforeAcknowledgement(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	wantCheckpoint := cli.Checkpoint{Artifact: "/native/session.jsonl", Offset: 42}
	checkpointCaptured := false
	attempt := server.ActivateWithCheckpoint("implement", func() (cli.Checkpoint, error) {
		checkpointCaptured = true
		return wantCheckpoint, nil
	})

	response := exchange(t, server.SocketPath(), &controlRequest{
		Type: MessageCompleteStep, RunID: attempt.RunID, StepID: attempt.StepID, Token: attempt.Token, RequestID: "complete",
	})
	if !response.OK {
		t.Fatalf("completion response = %#v", response)
	}
	if !checkpointCaptured {
		t.Fatal("completion was acknowledged before its durability checkpoint was captured")
	}
	select {
	case completion := <-server.Completions():
		if diff := cmp.Diff(wantCheckpoint, completion.Checkpoint); diff != "" {
			t.Fatalf("checkpoint mismatch (-want +got):\n%s", diff)
		}
	case <-time.After(time.Second):
		t.Fatal("completion was not delivered")
	}
}

func TestControlServerRunsCheckpointOutsideServerMutex(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	attempt := server.ActivateWithCheckpoint("implement", func() (cli.Checkpoint, error) {
		close(started)
		<-release
		return cli.Checkpoint{Artifact: "fixture"}, nil
	})

	acknowledged := make(chan controlResponse, 1)
	go func() {
		response, err := tryExchange(server.SocketPath(), &controlRequest{
			Type: MessageCompleteStep, RunID: attempt.RunID, StepID: attempt.StepID, Token: attempt.Token, RequestID: "complete",
		})
		if err != nil {
			t.Errorf("completion exchange failed: %v", err)
			return
		}
		acknowledged <- response
	}()
	<-started

	otherOperations := make(chan struct{})
	go func() {
		defer close(otherOperations)
		_, unsubscribe := server.SubscribeCommittedTurn("unrelated-attempt")
		unsubscribe()
		rejected, err := tryExchange(server.SocketPath(), &controlRequest{
			Type: MessageCompleteStep, RunID: attempt.RunID, StepID: attempt.StepID, Token: "wrong-token", RequestID: "other",
		})
		if err != nil {
			t.Errorf("rejection exchange failed: %v", err)
			return
		}
		if rejected.OK {
			t.Error("mismatched credential was accepted while a checkpoint was in flight")
		}
	}()
	select {
	case <-otherOperations:
	case <-time.After(time.Second):
		t.Fatal("control operations blocked behind an in-flight completion checkpoint")
	}

	select {
	case <-acknowledged:
		t.Fatal("completion was acknowledged before its checkpoint finished")
	default:
	}
	unblock()
	select {
	case response := <-acknowledged:
		if !response.OK {
			t.Fatalf("completion response = %#v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("completion was not acknowledged after the checkpoint finished")
	}
	select {
	case completion := <-server.Completions():
		if completion.Checkpoint.Artifact != "fixture" {
			t.Fatalf("completion checkpoint = %#v", completion.Checkpoint)
		}
	case <-time.After(time.Second):
		t.Fatal("completion was not delivered")
	}
}

func TestControlServerRetryDuringInFlightCheckpointIsIdempotent(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	var checkpointCalls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	attempt := server.ActivateWithCheckpoint("implement", func() (cli.Checkpoint, error) {
		if checkpointCalls.Add(1) == 1 {
			close(started)
		}
		<-release
		return cli.Checkpoint{Artifact: "fixture", Offset: 7}, nil
	})
	request := controlRequest{
		Type: MessageCompleteStep, RunID: attempt.RunID, StepID: attempt.StepID, Token: attempt.Token, RequestID: "complete-once",
	}

	responses := make(chan controlResponse, 2)
	sendCompletion := func() {
		response, err := tryExchange(server.SocketPath(), &request)
		if err != nil {
			t.Errorf("completion exchange failed: %v", err)
			return
		}
		responses <- response
	}
	go sendCompletion()
	<-started
	go sendCompletion()

	select {
	case response := <-responses:
		t.Fatalf("completion was acknowledged before its checkpoint finished: %#v", response)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	for i := 0; i < 2; i++ {
		select {
		case response := <-responses:
			if !response.OK {
				t.Fatalf("completion response = %#v", response)
			}
		case <-time.After(time.Second):
			t.Fatal("completion retry was not acknowledged")
		}
	}
	if got := checkpointCalls.Load(); got != 1 {
		t.Fatalf("checkpoint ran %d times, want 1", got)
	}
	select {
	case completion := <-server.Completions():
		if completion.RequestID != request.RequestID || completion.Checkpoint.Offset != 7 {
			t.Fatalf("completion = %#v", completion)
		}
	case <-time.After(time.Second):
		t.Fatal("completion was not delivered")
	}
	select {
	case duplicate := <-server.Completions():
		t.Fatalf("retry double-delivered the completion: %#v", duplicate)
	case <-time.After(40 * time.Millisecond):
	}
}

func TestControlServerAcceptsTurnCommittedAfterCompletion(t *testing.T) {
	logger := &recordingEventLogger{}
	server := newTestControlServer(t, t.TempDir(), logger)
	defer server.Close()
	attempt := server.ActivateWithCheckpoint("implement", nil)
	complete := controlRequest{Type: MessageCompleteStep, RunID: attempt.RunID, StepID: attempt.StepID, Token: attempt.Token, RequestID: "complete"}
	if response := exchange(t, server.SocketPath(), &complete); !response.OK {
		t.Fatalf("completion response = %#v", response)
	}
	<-server.Completions()

	committed := controlRequest{Type: MessageTurnCommitted, RunID: attempt.RunID, StepID: attempt.StepID, Token: attempt.Token, RequestID: "turn"}
	if response := exchange(t, server.SocketPath(), &committed); !response.OK {
		t.Fatalf("turn response = %#v", response)
	}
	select {
	case got := <-server.CommittedTurns():
		if got.AttemptID != attempt.ID {
			t.Fatalf("turn committed = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("turn-committed event was not delivered")
	}
	if got := logger.types(); got[len(got)-1] != audit.EventTurnCommitted {
		t.Fatalf("last audit type = %q, want turn_committed", got[len(got)-1])
	}
}

func TestControlServerRoutesCommittedTurnsToAttemptSubscriber(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()

	stale := server.ActivateWithCheckpoint("implement", nil)
	staleCommitted, unsubscribeStale := server.SubscribeCommittedTurn(stale.ID)
	unsubscribeStale()

	current := server.ActivateWithCheckpoint("implement", nil)
	currentCommitted, unsubscribeCurrent := server.SubscribeCommittedTurn(current.ID)
	defer unsubscribeCurrent()
	complete := controlRequest{Type: MessageCompleteStep, RunID: current.RunID, StepID: current.StepID, Token: current.Token, RequestID: "complete"}
	if response := exchange(t, server.SocketPath(), &complete); !response.OK {
		t.Fatalf("completion response = %#v", response)
	}
	<-server.Completions()
	committed := controlRequest{Type: MessageTurnCommitted, RunID: current.RunID, StepID: current.StepID, Token: current.Token, RequestID: "turn"}
	if response := exchange(t, server.SocketPath(), &committed); !response.OK {
		t.Fatalf("turn response = %#v", response)
	}

	select {
	case <-currentCommitted:
	case <-time.After(time.Second):
		t.Fatal("current attempt did not receive committed-turn evidence")
	}
	select {
	case <-staleCommitted:
		t.Fatal("cancelled stale attempt received a later attempt's committed-turn evidence")
	default:
	}
}

func TestControlServerLateCommittedTurnSubscriberReceivesRecordedEvidence(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	attempt := server.ActivateWithCheckpoint("implement", nil)
	complete := controlRequest{Type: MessageCompleteStep, RunID: attempt.RunID, StepID: attempt.StepID, Token: attempt.Token, RequestID: "complete"}
	if response := exchange(t, server.SocketPath(), &complete); !response.OK {
		t.Fatalf("completion response = %#v", response)
	}
	<-server.Completions()
	committed := controlRequest{Type: MessageTurnCommitted, RunID: attempt.RunID, StepID: attempt.StepID, Token: attempt.Token, RequestID: "turn"}
	if response := exchange(t, server.SocketPath(), &committed); !response.OK {
		t.Fatalf("turn response = %#v", response)
	}

	recorded, unsubscribe := server.SubscribeCommittedTurn(attempt.ID)
	defer unsubscribe()
	select {
	case <-recorded:
	case <-time.After(time.Second):
		t.Fatal("late subscriber did not receive recorded committed-turn evidence")
	}
}

func TestControlServerRejectsTurnCommittedBeforeCompletion(t *testing.T) {
	logger := &recordingEventLogger{}
	server := newTestControlServer(t, t.TempDir(), logger)
	defer server.Close()
	attempt := server.ActivateWithCheckpoint("implement", nil)
	committed := controlRequest{Type: MessageTurnCommitted, RunID: attempt.RunID, StepID: attempt.StepID, Token: attempt.Token, RequestID: "early-turn"}

	response := exchange(t, server.SocketPath(), &committed)
	if response.OK || !strings.Contains(response.Error, "before completion") {
		t.Fatalf("early turn response = %#v", response)
	}
	select {
	case turn := <-server.CommittedTurns():
		t.Fatalf("pre-completion turn was delivered: %#v", turn)
	case <-time.After(40 * time.Millisecond):
	}
	if diff := cmp.Diff([]audit.EventType{audit.EventControlRejected}, logger.types()); diff != "" {
		t.Fatalf("audit events mismatch (-want +got):\n%s", diff)
	}
}

func TestControlServerRetriesTurnCommittedAfterLostAcknowledgement(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	attempt := server.ActivateWithCheckpoint("implement", nil)
	complete := controlRequest{Type: MessageCompleteStep, RunID: attempt.RunID, StepID: attempt.StepID, Token: attempt.Token, RequestID: "complete"}
	if response := exchange(t, server.SocketPath(), &complete); !response.OK {
		t.Fatalf("completion response = %#v", response)
	}
	<-server.Completions()
	request := controlRequest{Type: MessageTurnCommitted, RunID: attempt.RunID, StepID: attempt.StepID, Token: attempt.Token, RequestID: "turn"}

	client, peer := net.Pipe()
	handled := make(chan struct{})
	go func() {
		server.handleConnection(peer)
		close(handled)
	}()
	if _, err := client.Write(marshalLine(t, request)); err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	<-handled

	if response := exchange(t, server.SocketPath(), &request); !response.OK {
		t.Fatalf("turn retry response = %#v", response)
	}
	select {
	case got := <-server.CommittedTurns():
		if got.AttemptID != attempt.ID {
			t.Fatalf("turn committed = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("turn retry was acknowledged but not delivered")
	}
}

func TestControlServerRejectsStaleMalformedAndInactiveEvents(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*ControlServer) []byte
	}{
		{
			name: "stale credential",
			prepare: func(server *ControlServer) []byte {
				stale := server.ActivateWithCheckpoint("step", nil)
				server.ActivateWithCheckpoint("step", nil)
				return marshalLine(t, controlRequest{Type: MessageCompleteStep, RunID: stale.RunID, StepID: stale.StepID, Token: stale.Token, RequestID: "stale"})
			},
		},
		{
			name: "malformed payload",
			prepare: func(server *ControlServer) []byte {
				server.ActivateWithCheckpoint("step", nil)
				return []byte("{not-json}\n")
			},
		},
		{
			name: "no active step",
			prepare: func(server *ControlServer) []byte {
				return marshalLine(t, controlRequest{Type: MessageCompleteStep, RunID: "run", StepID: "step", Token: "unknown", RequestID: "inactive"})
			},
		},
		{
			name: "message exceeds bound",
			prepare: func(server *ControlServer) []byte {
				server.ActivateWithCheckpoint("step", nil)
				return append([]byte(`{"type":"complete_step","padding":"`), append([]byte(strings.Repeat("x", MaxControlMessageBytes)), []byte(`"}`+"\n")...)...)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &recordingEventLogger{}
			server := newTestControlServer(t, t.TempDir(), logger)
			defer server.Close()
			response := exchangeRaw(t, server.SocketPath(), tt.prepare(server))
			if response.OK || response.Error == "" {
				t.Fatalf("rejection response = %#v", response)
			}
			if diff := cmp.Diff([]audit.EventType{audit.EventControlRejected}, logger.types()); diff != "" {
				t.Fatalf("audit events mismatch (-want +got):\n%s", diff)
			}
			select {
			case completion := <-server.Completions():
				t.Fatalf("rejected event delivered completion: %#v", completion)
			case <-time.After(20 * time.Millisecond):
			}
		})
	}
}

func TestSendControlEventFromEnvironment(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	attempt := server.ActivateWithCheckpoint("step", nil)
	environment := attempt.EnvironmentMap()
	getenv := func(key string) string { return environment[key] }

	if err := SendControlEventFromEnvironment(context.Background(), MessageCompleteStep, getenv); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.Completions():
	case <-time.After(time.Second):
		t.Fatal("client did not send completion")
	}
}

func TestSendControlEventRequiresInteractiveStepEnvironment(t *testing.T) {
	err := SendControlEventFromEnvironment(context.Background(), MessageCompleteStep, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "inside an interactive agent step session") {
		t.Fatalf("error = %v", err)
	}
}

func TestSendControlEventRetriesLostAcknowledgementWithSameRequestID(t *testing.T) {
	socket := filepath.Join(shortTempDir(t), "control.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	requests := make(chan controlRequest, 2)
	go func() {
		for attempt := 0; attempt < 2; attempt++ {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			line, _ := bufio.NewReader(connection).ReadBytes('\n')
			var request controlRequest
			_ = json.Unmarshal(line, &request)
			requests <- request
			if attempt == 1 {
				_, _ = connection.Write([]byte("{\"ok\":true}\n"))
			}
			_ = connection.Close()
		}
	}()
	environment := map[string]string{
		EnvControlSocket: socket,
		EnvRunID:         "run",
		EnvStepID:        "step",
		EnvControlToken:  "token",
	}
	if err := SendControlEventFromEnvironment(context.Background(), MessageCompleteStep, func(key string) string { return environment[key] }); err != nil {
		t.Fatal(err)
	}
	first, second := <-requests, <-requests
	if first.RequestID == "" || first.RequestID != second.RequestID {
		t.Fatalf("request IDs = %q and %q, want same non-empty ID", first.RequestID, second.RequestID)
	}
}

func newTestControlServer(t *testing.T, runDir string, logger audit.EventLogger) *ControlServer {
	t.Helper()
	server, err := NewControlServer(&ControlConfig{
		RunID: "run", RunDir: runDir, TempDir: shortTempDir(t), LockProof: heldRunLockProof(t, runDir), Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func heldRunLockProof(t *testing.T, runDir string) runlock.HeldProof {
	t.Helper()
	activePID, err := runlock.Acquire(runDir)
	if err != nil || activePID != 0 {
		t.Fatalf("acquire run lock: active PID %d: %v", activePID, err)
	}
	t.Cleanup(func() { runlock.Delete(runDir) })
	proof, err := runlock.ProveHeld(runDir)
	if err != nil {
		t.Fatal(err)
	}
	return proof
}

func exchange(t *testing.T, socket string, request *controlRequest) controlResponse {
	t.Helper()
	return exchangeRaw(t, socket, marshalLine(t, request))
}

func marshalLine(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

// tryExchange is safe to call from non-test goroutines: it reports transport
// failures instead of failing the test.
func tryExchange(socket string, request *controlRequest) (controlResponse, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return controlResponse{}, err
	}
	connection, err := net.Dial("unix", socket)
	if err != nil {
		return controlResponse{}, err
	}
	defer connection.Close()
	if _, err := connection.Write(append(payload, '\n')); err != nil {
		return controlResponse{}, err
	}
	line, err := bufio.NewReader(connection).ReadBytes('\n')
	if err != nil {
		return controlResponse{}, err
	}
	var response controlResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return controlResponse{}, err
	}
	return response, nil
}

func exchangeRaw(t *testing.T, socket string, request []byte) controlResponse {
	t.Helper()
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write(request); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(connection).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response controlResponse
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "ar-control-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

type recordingEventLogger struct {
	mu     sync.Mutex
	events []audit.Event
}

func (l *recordingEventLogger) Emit(event audit.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *recordingEventLogger) snapshot() []audit.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]audit.Event(nil), l.events...)
}

func (l *recordingEventLogger) types() []audit.EventType {
	events := l.snapshot()
	types := make([]audit.EventType, len(events))
	for index := range events {
		types[index] = events[index].Type
	}
	return types
}

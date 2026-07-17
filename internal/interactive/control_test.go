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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/audit"
)

func TestControlServerCreatesPrivateEndpointAndAttemptEnvironment(t *testing.T) {
	runDir := t.TempDir()
	tempDir := shortTempDir(t)
	logger := &recordingEventLogger{}
	server, err := NewControlServer(ControlConfig{
		RunID:   "run-with-a-deliberately-long-identifier",
		RunDir:  runDir,
		TempDir: tempDir,
		Logger:  logger,
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

	attempt := server.Activate("review")
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
	attempt = server.Activate("review")
	if attempt.Token == previous.Token || attempt.ID == previous.ID {
		t.Fatalf("attempt credential was not rotated: before=%#v after=%#v", previous, attempt)
	}
}

func TestControlServerCreationFailureIsDescriptive(t *testing.T) {
	root := t.TempDir()
	notDir := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewControlServer(ControlConfig{RunID: "run", RunDir: t.TempDir(), TempDir: notDir})
	if err == nil || !strings.Contains(err.Error(), "create private control directory") {
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

func TestControlServerAcceptsCurrentCredentialAndAcknowledgesIdempotently(t *testing.T) {
	logger := &recordingEventLogger{}
	server := newTestControlServer(t, t.TempDir(), logger)
	defer server.Close()
	attempt := server.Activate("implement")
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

func TestControlServerAcceptsTurnCommittedAfterCompletion(t *testing.T) {
	logger := &recordingEventLogger{}
	server := newTestControlServer(t, t.TempDir(), logger)
	defer server.Close()
	attempt := server.Activate("implement")
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

func TestControlServerRetriesTurnCommittedAfterLostAcknowledgement(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	attempt := server.Activate("implement")
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
				stale := server.Activate("step")
				server.Activate("step")
				return marshalLine(t, controlRequest{Type: MessageCompleteStep, RunID: stale.RunID, StepID: stale.StepID, Token: stale.Token, RequestID: "stale"})
			},
		},
		{
			name: "malformed payload",
			prepare: func(server *ControlServer) []byte {
				server.Activate("step")
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
				server.Activate("step")
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
	attempt := server.Activate("step")
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
	server, err := NewControlServer(ControlConfig{RunID: "run", RunDir: runDir, TempDir: shortTempDir(t), Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	return server
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

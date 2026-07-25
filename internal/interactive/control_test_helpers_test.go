package interactive

import (
	"os"
	"sync"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/runlock"
)

func newTestControlServer(t *testing.T, runDir string, logger audit.EventLogger) *control.ControlServer {
	t.Helper()
	tempDir, err := os.MkdirTemp("/tmp", "ar-interactive-control-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	activePID, err := runlock.Acquire(runDir)
	if err != nil || activePID != 0 {
		t.Fatalf("acquire run lock: active PID %d: %v", activePID, err)
	}
	t.Cleanup(func() { runlock.Delete(runDir) })
	proof, err := runlock.ProveHeld(runDir)
	if err != nil {
		t.Fatal(err)
	}
	server, err := control.NewControlServer(&control.ControlConfig{
		RunID: "run", RunDir: runDir, TempDir: tempDir, LockProof: proof, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
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

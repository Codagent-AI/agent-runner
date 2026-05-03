package liverun

import (
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// mockProgram records ReleaseTerminal/RestoreTerminal calls and messages.
type mockProgram struct {
	mu       sync.Mutex
	released int
	restored int
	msgs     []tea.Msg
}

func (m *mockProgram) ReleaseTerminal() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.released++
	return nil
}

func (m *mockProgram) RestoreTerminal() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restored++
	return nil
}

func (m *mockProgram) Send(msg tea.Msg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = append(m.msgs, msg)
}

func (m *mockProgram) counts() (released, restored int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.released, m.restored
}

func (m *mockProgram) hasMsg(t *testing.T, msgType string) bool {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.msgs {
		switch msg.(type) {
		case SuspendedMsg:
			if msgType == "suspended" {
				return true
			}
		case ResumedMsg:
			if msgType == "resumed" {
				return true
			}
		case ShowTUIMsg:
			if msgType == "show" {
				return true
			}
		case ExecDoneMsg:
			if msgType == "done" {
				return true
			}
		}
	}
	return false
}

func (m *mockProgram) clearMsgs() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.msgs = nil
}

func TestHandleUIStep_NilRequestReturnsError(t *testing.T) {
	c := NewCoordinator(&mockProgram{}, "")

	_, err := c.HandleUIStep(nil)
	if err == nil || !strings.Contains(err.Error(), "ui step request is nil") {
		t.Fatalf("expected nil request error, got %v", err)
	}
}

func TestBeforeInteractive_ReleasesTerminal(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	c.BeforeInteractive()

	rel, _ := mp.counts()
	if rel != 1 {
		t.Errorf("expected 1 release, got %d", rel)
	}
	if !mp.hasMsg(t, "suspended") {
		t.Error("expected SuspendedMsg")
	}
}

func TestBeforeInteractive_IdempotentWhenSuspended(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	c.BeforeInteractive()
	c.BeforeInteractive() // second call should be no-op

	rel, _ := mp.counts()
	if rel != 1 {
		t.Errorf("expected exactly 1 release, got %d", rel)
	}
}

func TestAfterInteractive_DoesNotRestore(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	c.BeforeInteractive()
	c.AfterInteractive()

	_, res := mp.counts()
	if res != 0 {
		t.Errorf("expected 0 restores, got %d", res)
	}
	if mp.hasMsg(t, "resumed") {
		t.Error("should not send ResumedMsg on lazy AfterInteractive")
	}
}

func TestPrepareForStep_RestoresForNonInteractive(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	c.BeforeInteractive()
	c.AfterInteractive()
	mp.clearMsgs()

	c.PrepareForStep(false) // non-interactive step coming

	_, res := mp.counts()
	if res != 1 {
		t.Errorf("expected 1 restore, got %d", res)
	}
	if !mp.hasMsg(t, "resumed") {
		t.Error("expected ResumedMsg")
	}
	if !mp.hasMsg(t, "show") {
		t.Error("expected ShowTUIMsg for first alt-screen entry")
	}
}

func TestPrepareForStep_StaysSuspendedForInteractive(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	c.BeforeInteractive()
	c.AfterInteractive()
	mp.clearMsgs()

	c.PrepareForStep(true) // another interactive step coming

	_, res := mp.counts()
	if res != 0 {
		t.Errorf("expected 0 restores, got %d", res)
	}
}

func TestConsecutiveInteractiveSteps_NoFlicker(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	// Step 1: interactive
	c.PrepareForStep(true)
	c.BeforeInteractive()
	// ... step runs ...
	c.AfterInteractive()

	// Step 2: interactive — should NOT restore+release
	c.PrepareForStep(true)
	c.BeforeInteractive()
	// ... step runs ...
	c.AfterInteractive()

	// Step 3: interactive — same
	c.PrepareForStep(true)
	c.BeforeInteractive()
	// ... step runs ...
	c.AfterInteractive()

	rel, res := mp.counts()
	if rel != 1 {
		t.Errorf("expected exactly 1 release across 3 interactive steps, got %d", rel)
	}
	if res != 0 {
		t.Errorf("expected 0 restores between consecutive interactive steps, got %d", res)
	}
}

func TestInteractiveThenNonInteractive_Restores(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	// Interactive step
	c.PrepareForStep(true)
	c.BeforeInteractive()
	c.AfterInteractive()

	// Non-interactive step
	c.PrepareForStep(false)

	rel, res := mp.counts()
	if rel != 1 {
		t.Errorf("expected 1 release, got %d", rel)
	}
	if res != 1 {
		t.Errorf("expected 1 restore, got %d", res)
	}
}

func TestPrepareForStep_ShowTUIOnlyOnce(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	// First interactive → non-interactive transition
	c.BeforeInteractive()
	c.AfterInteractive()
	c.PrepareForStep(false)

	// Second interactive → non-interactive transition
	c.BeforeInteractive()
	c.AfterInteractive()
	mp.clearMsgs()
	c.PrepareForStep(false)

	// ShowTUIMsg should NOT be sent again (alt-screen was already requested)
	if mp.hasMsg(t, "show") {
		t.Error("ShowTUIMsg should only be sent once")
	}
}

func TestNotifyDone_RestoresWhenSuspended(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	c.BeforeInteractive()
	c.AfterInteractive()
	mp.clearMsgs()

	c.NotifyDone("success", nil)

	_, res := mp.counts()
	if res != 1 {
		t.Errorf("expected 1 restore, got %d", res)
	}
	if !mp.hasMsg(t, "resumed") {
		t.Error("expected ResumedMsg")
	}
	if !mp.hasMsg(t, "show") {
		t.Error("expected ShowTUIMsg")
	}
	if !mp.hasMsg(t, "done") {
		t.Error("expected ExecDoneMsg")
	}
}

func TestNotifyDone_NoRestoreWhenNotSuspended(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	c.NotifyDone("success", nil)

	_, res := mp.counts()
	if res != 0 {
		t.Errorf("expected 0 restores, got %d", res)
	}
	if !mp.hasMsg(t, "done") {
		t.Error("expected ExecDoneMsg")
	}
}

func TestNotifyDone_ShowsTUIEvenIfNeverShown(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	// Workflow with only interactive steps — TUI was never shown
	c.BeforeInteractive()
	c.AfterInteractive()
	c.BeforeInteractive()
	c.AfterInteractive()

	c.NotifyDone("success", nil)

	if !mp.hasMsg(t, "show") {
		t.Error("expected ShowTUIMsg so results are visible")
	}
}

func TestPrepareForStep_NonInteractiveWithoutPending_NoOp(t *testing.T) {
	mp := &mockProgram{}
	c := NewCoordinator(mp, "")

	c.PrepareForStep(false) // no pending resume, not suspended

	rel, res := mp.counts()
	if rel != 0 || res != 0 {
		t.Errorf("expected no terminal changes, got release=%d restore=%d", rel, res)
	}
}

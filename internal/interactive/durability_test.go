package interactive

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
)

func TestAwaitTurnDurabilityConfirmsAfterCheckpoint(t *testing.T) {
	probe := &fakeDurabilityProbe{checkpoint: cli.Checkpoint{Artifact: "/native/session.jsonl", Offset: 12}}
	probe.wait = func(ctx context.Context, sessionID string, after cli.Checkpoint) error {
		if sessionID != "session-1" || after != probe.checkpoint {
			t.Fatalf("WaitForCommittedTurn(%q, %#v)", sessionID, after)
		}
		return nil
	}
	logger := &recordingEventLogger{}
	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		CLI:        "codex",
		SessionID:  "session-1",
		Probe:      probe,
		Checkpoint: probe.checkpoint,
		Logger:     logger,
		Timeout:    time.Second,
	})
	if diff := cmp.Diff(DurabilityResult{Outcome: CompletionSuccess, TerminateChild: true}, result, cmp.Comparer(compareErrors)); diff != "" {
		t.Fatalf("result mismatch (-want +got):\n%s", diff)
	}
	if probe.checkpointCalls != 0 || probe.waitCalls != 1 {
		t.Fatalf("probe calls checkpoint=%d wait=%d", probe.checkpointCalls, probe.waitCalls)
	}
	if len(logger.snapshot()) != 0 {
		t.Fatalf("successful durability emitted failure audit: %#v", logger.snapshot())
	}
}

func TestAwaitTurnDurabilityUsesAcceptanceCheckpointWithoutRecapturing(t *testing.T) {
	checkpoint := cli.Checkpoint{Artifact: "/native/session.jsonl", Offset: 27}
	probe := &fakeDurabilityProbe{}
	probe.wait = func(_ context.Context, sessionID string, after cli.Checkpoint) error {
		if sessionID != "session-accept" || after != checkpoint {
			t.Fatalf("WaitForCommittedTurn(%q, %#v)", sessionID, after)
		}
		return nil
	}

	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		CLI:        "codex",
		SessionID:  "session-accept",
		Probe:      probe,
		Checkpoint: checkpoint,
		Timeout:    time.Second,
	})
	if result.Outcome != CompletionSuccess || result.Err != nil {
		t.Fatalf("result = %#v", result)
	}
	if probe.checkpointCalls != 0 {
		t.Fatalf("durability checkpoint recaptured %d times after acknowledgement", probe.checkpointCalls)
	}
}

func TestAwaitTurnDurabilityAcceptsHookEvidenceAfterCompletion(t *testing.T) {
	committed := make(chan struct{}, 1)
	committed <- struct{}{}
	probe := &fakeDurabilityProbe{
		checkpoint: cli.Checkpoint{Artifact: "/native/session.jsonl"},
		wait: func(ctx context.Context, _ string, _ cli.Checkpoint) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		CLI:           "claude",
		SessionID:     "session-1",
		Probe:         probe,
		CommittedTurn: committed,
		Timeout:       time.Second,
	})
	if result.Outcome != CompletionSuccess || !result.TerminateChild || result.Err != nil {
		t.Fatalf("result = %#v", result)
	}
}

func TestAwaitTurnDurabilityTimesOutAndAuditsFailure(t *testing.T) {
	probe := &fakeDurabilityProbe{
		checkpoint: cli.Checkpoint{Artifact: "/native/session.jsonl", Offset: 99},
		wait: func(ctx context.Context, _ string, _ cli.Checkpoint) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	logger := &recordingEventLogger{}
	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		CLI:        "copilot",
		SessionID:  "session-9",
		Probe:      probe,
		Checkpoint: probe.checkpoint,
		Logger:     logger,
		Timeout:    25 * time.Millisecond,
	})
	if result.Outcome != CompletionFailed || !result.TerminateChild || !errors.Is(result.Err, ErrDurabilityTimeout) {
		t.Fatalf("result = %#v", result)
	}
	events := logger.snapshot()
	if len(events) != 1 || events[0].Type != audit.EventDurabilityFailure {
		t.Fatalf("audit events = %#v", events)
	}
	wantData := map[string]any{
		"cli":                "copilot",
		"session_id":         "session-9",
		"timeout":            "25ms",
		"inspected_artifact": "/native/session.jsonl",
		"error":              ErrDurabilityTimeout.Error(),
	}
	if diff := cmp.Diff(wantData, events[0].Data); diff != "" {
		t.Fatalf("durability audit data mismatch (-want +got):\n%s", diff)
	}
}

func TestAwaitTurnDurabilityContinuesAfterChildExit(t *testing.T) {
	childExited := make(chan struct{})
	close(childExited)
	probe := &fakeDurabilityProbe{
		checkpoint: cli.Checkpoint{Artifact: "/native/session.jsonl"},
		wait: func(context.Context, string, cli.Checkpoint) error {
			time.Sleep(20 * time.Millisecond)
			return nil
		},
	}
	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		CLI:         "cursor",
		SessionID:   "session-2",
		Probe:       probe,
		Checkpoint:  probe.checkpoint,
		ChildExited: childExited,
		Timeout:     time.Second,
	})
	if result.Outcome != CompletionSuccess || result.Err != nil {
		t.Fatalf("result after child exit = %#v", result)
	}
}

func TestAwaitTurnDurabilityCheckpointFailureAuditsArtifact(t *testing.T) {
	probe := &fakeDurabilityProbe{}
	logger := &recordingEventLogger{}
	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		CLI:           "opencode",
		SessionID:     "session-3",
		Probe:         probe,
		CheckpointErr: errors.New("store missing"),
		Logger:        logger,
		Timeout:       time.Second,
	})
	if result.Outcome != CompletionFailed || !strings.Contains(result.Err.Error(), "checkpoint") {
		t.Fatalf("result = %#v", result)
	}
	events := logger.snapshot()
	if len(events) != 1 || events[0].Data["inspected_artifact"] != "unavailable" {
		t.Fatalf("events = %#v", events)
	}
}

func TestAwaitTurnDurabilityAcceptsNativeHookWhenStoreCheckpointUnavailable(t *testing.T) {
	committed := make(chan struct{}, 1)
	committed <- struct{}{}
	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		Probe:         &fakeDurabilityProbe{},
		CheckpointErr: errors.New("session id not discovered yet"),
		CommittedTurn: committed,
		Timeout:       time.Second,
	})

	if result.Outcome != CompletionSuccess || result.Err != nil {
		t.Fatalf("result = %#v, want hook-confirmed success", result)
	}
}

func TestAwaitTurnDurabilityTimeoutReportsUnavailableCheckpoint(t *testing.T) {
	checkpointErr := errors.New("session ID is not discoverable")
	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		Probe:         &fakeDurabilityProbe{},
		CheckpointErr: checkpointErr,
		CommittedTurn: make(chan struct{}),
		Timeout:       20 * time.Millisecond,
	})

	if !errors.Is(result.Err, ErrDurabilityTimeout) || !strings.Contains(result.Err.Error(), checkpointErr.Error()) {
		t.Fatalf("result error = %v, want timeout with checkpoint cause", result.Err)
	}
}

func TestAwaitTurnDurabilityWaitsForStoreToAppearFromEmptyBaseline(t *testing.T) {
	notExist := fmt.Errorf("locate session %q: %w", "session-late", fs.ErrNotExist)
	baselines := make(chan cli.Checkpoint, 2)
	probe := &fakeDurabilityProbe{}
	probe.checkpointFn = func(calls int) (cli.Checkpoint, error) {
		if calls < 3 {
			return cli.Checkpoint{}, notExist
		}
		return cli.Checkpoint{Artifact: "/native/session.jsonl", Offset: 512}, nil
	}
	probe.wait = func(_ context.Context, sessionID string, after cli.Checkpoint) error {
		if after.Artifact == "" {
			return errors.New("durability checkpoint has no inspected artifact")
		}
		if sessionID != "session-late" {
			t.Errorf("WaitForCommittedTurn session = %q", sessionID)
		}
		baselines <- after
		return nil
	}

	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		CLI:       "claude",
		SessionID: "session-late",
		Probe:     probe,
		// Store absent at accept time: empty baseline, no checkpoint error.
		Checkpoint: cli.Checkpoint{},
		Timeout:    5 * time.Second,
	})
	if result.Outcome != CompletionSuccess || result.Err != nil {
		t.Fatalf("result = %#v, want success once the store appears within the bound", result)
	}
	want := cli.Checkpoint{Artifact: "/native/session.jsonl"}
	if got := <-baselines; got != want {
		t.Fatalf("wait baseline = %#v, want start-of-store baseline %#v", got, want)
	}
}

func TestAwaitTurnDurabilityKeepsFailingOnRealWaitErrors(t *testing.T) {
	realErr := errors.New("decode session store: corrupt record")
	probe := &fakeDurabilityProbe{
		checkpoint: cli.Checkpoint{Artifact: "/native/session.jsonl"},
		wait: func(context.Context, string, cli.Checkpoint) error {
			return realErr
		},
	}
	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		CLI:        "claude",
		SessionID:  "session-1",
		Probe:      probe,
		Checkpoint: probe.checkpoint,
		Timeout:    time.Second,
	})
	if result.Outcome != CompletionFailed || !errors.Is(result.Err, realErr) {
		t.Fatalf("result = %#v, want immediate failure on a real probe error", result)
	}
}

func TestAwaitTurnDurabilityPrefersReceiptEvidenceWhenAvailable(t *testing.T) {
	checkpoint := cli.Checkpoint{Artifact: "/native/store.db", Marker: "baseline"}
	probe := &fakeReceiptDurabilityProbe{}
	probe.wait = func(context.Context, string, cli.Checkpoint) error {
		t.Error("receipt-capable probe used the receipt-free wait despite an available receipt")
		return errors.New("wrong wait method")
	}
	receipts := make(chan string, 1)
	probe.receiptWait = func(_ context.Context, sessionID string, after cli.Checkpoint, receipt string) error {
		if sessionID != "session-1" || after != checkpoint {
			t.Errorf("WaitForCommittedTurnWithReceipt(%q, %#v)", sessionID, after)
		}
		receipts <- receipt
		return nil
	}

	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		CLI:        "cursor",
		SessionID:  "session-1",
		Probe:      probe,
		Checkpoint: checkpoint,
		Receipt:    "receipt-request-1",
		Timeout:    time.Second,
	})
	if result.Outcome != CompletionSuccess || result.Err != nil {
		t.Fatalf("result = %#v", result)
	}
	if got := <-receipts; got != "receipt-request-1" {
		t.Fatalf("probe receipt = %q, want receipt-request-1", got)
	}
}

func TestAwaitTurnDurabilityFallsBackToPlainWaitWithoutReceipt(t *testing.T) {
	checkpoint := cli.Checkpoint{Artifact: "/native/store.db"}
	probe := &fakeReceiptDurabilityProbe{}
	waited := make(chan struct{}, 1)
	probe.wait = func(context.Context, string, cli.Checkpoint) error {
		waited <- struct{}{}
		return nil
	}
	probe.receiptWait = func(context.Context, string, cli.Checkpoint, string) error {
		t.Error("receipt wait used without an available receipt")
		return errors.New("no receipt")
	}

	result := AwaitTurnDurability(context.Background(), &DurabilityOptions{
		CLI:        "cursor",
		SessionID:  "session-1",
		Probe:      probe,
		Checkpoint: checkpoint,
		Timeout:    time.Second,
	})
	if result.Outcome != CompletionSuccess || result.Err != nil {
		t.Fatalf("result = %#v", result)
	}
	<-waited
}

func TestActiveRuntimeTimerPausesDeadline(t *testing.T) {
	timer := NewActiveRuntimeTimer(50 * time.Millisecond)
	defer timer.Stop()
	time.Sleep(10 * time.Millisecond)
	timer.Pause()
	select {
	case <-timer.Done():
		t.Fatal("timer expired while paused")
	case <-time.After(75 * time.Millisecond):
	}
	timer.Resume()
	select {
	case <-timer.Done():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timer did not expire after active runtime resumed")
	}
}

func compareErrors(left, right error) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Error() == right.Error()
}

type fakeDurabilityProbe struct {
	mu              sync.Mutex
	checkpoint      cli.Checkpoint
	checkpointErr   error
	checkpointFn    func(calls int) (cli.Checkpoint, error)
	wait            func(context.Context, string, cli.Checkpoint) error
	checkpointCalls int
	waitCalls       int
}

func (p *fakeDurabilityProbe) Checkpoint(string) (cli.Checkpoint, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.checkpointCalls++
	if p.checkpointFn != nil {
		return p.checkpointFn(p.checkpointCalls)
	}
	return p.checkpoint, p.checkpointErr
}

func (p *fakeDurabilityProbe) WaitForCommittedTurn(ctx context.Context, sessionID string, after cli.Checkpoint) error {
	p.mu.Lock()
	p.waitCalls++
	p.mu.Unlock()
	if p.wait != nil {
		return p.wait(ctx, sessionID, after)
	}
	return nil
}

type fakeReceiptDurabilityProbe struct {
	fakeDurabilityProbe
	receiptWait func(context.Context, string, cli.Checkpoint, string) error
}

func (p *fakeReceiptDurabilityProbe) WaitForCommittedTurnWithReceipt(ctx context.Context, sessionID string, after cli.Checkpoint, receipt string) error {
	if p.receiptWait != nil {
		return p.receiptWait(ctx, sessionID, after, receipt)
	}
	return nil
}

package interactive

import (
	"context"
	"errors"
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
	wait            func(context.Context, string, cli.Checkpoint) error
	checkpointCalls int
	waitCalls       int
}

func (p *fakeDurabilityProbe) Checkpoint(string) (cli.Checkpoint, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.checkpointCalls++
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

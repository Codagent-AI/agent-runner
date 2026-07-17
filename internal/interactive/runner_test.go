package interactive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/cli"
)

func TestCheckedTerminalFDRejectsOverflow(t *testing.T) {
	overflow := uintptr(^uint(0)>>1) + 1
	if _, err := checkedTerminalFD(overflow); err == nil {
		t.Fatalf("checkedTerminalFD(%d) succeeded, want overflow error", overflow)
	} else if got, want := err.Error(), fmt.Sprintf("terminal file descriptor %d overflows int", overflow); got != want {
		t.Fatalf("checkedTerminalFD error = %q, want %q", got, want)
	}
}

func TestDirectRunnerReleaseFailurePreventsSpawn(t *testing.T) {
	spawned := filepath.Join(t.TempDir(), "spawned")
	runner := NewDirectRunner(&DirectOptions{
		Args:   []string{"sh", "-c", "touch " + spawned},
		Before: func() error { return errors.New("release terminal: boom") },
	})

	_, err := runner.Run(context.Background())
	if err == nil || err.Error() != "release terminal: boom" {
		t.Fatalf("Run error = %v, want release failure", err)
	}
	if _, statErr := os.Stat(spawned); !os.IsNotExist(statErr) {
		t.Fatalf("child was spawned despite release failure: %v", statErr)
	}
}

func TestDirectRunnerCompletesThroughControlChannelAndRestores(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	t.Setenv("AGENT_RUNNER_DIRECT_HELPER", "1")

	var before, after int
	runner := NewDirectRunner(&DirectOptions{
		Args:              []string{os.Args[0], "-test.run=^TestDirectRunnerHelperProcess$"},
		StepID:            "implement",
		SessionID:         "session-1",
		CLI:               "fake",
		Control:           server,
		Probe:             immediateDurabilityProbe{},
		Foreground:        false,
		TerminationGrace:  250 * time.Millisecond,
		DurabilityTimeout: time.Second,
		Before: func() error {
			before++
			return nil
		},
		After: func() error {
			after++
			return nil
		},
	})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Completed || result.DurabilityFailed {
		t.Fatalf("Run result = %#v, want completed durable result", result)
	}
	if before != 1 || after != 1 {
		t.Fatalf("lease calls = before %d after %d, want 1 each", before, after)
	}
}

func TestDirectRunnerResolvesFreshSessionBeforeDurabilityCheckpoint(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	t.Setenv("AGENT_RUNNER_DIRECT_HELPER", "2")
	probe := &sessionRecordingDurabilityProbe{
		checkpointSession:  make(chan string, 1),
		waitSession:        make(chan string, 1),
		checkpointFailures: 2,
	}
	resolveAttempts := 0
	runner := NewDirectRunner(&DirectOptions{
		Args:    []string{os.Args[0], "-test.run=^TestDirectRunnerHelperProcess$"},
		StepID:  "implement",
		CLI:     "fake",
		Control: server,
		Probe:   probe,
		ResolveSessionID: func() string {
			resolveAttempts++
			if resolveAttempts < 3 {
				return ""
			}
			return "discovered-session"
		},
		Foreground:        false,
		TerminationGrace:  250 * time.Millisecond,
		DurabilityTimeout: time.Second,
	})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Completed || result.DurabilityFailed {
		t.Fatalf("Run result = %#v, want completed durable result", result)
	}
	if got := <-probe.checkpointSession; got != "discovered-session" {
		t.Fatalf("checkpoint session = %q, want discovered-session", got)
	}
	if got := <-probe.waitSession; got != "discovered-session" {
		t.Fatalf("wait session = %q, want discovered-session", got)
	}
	if resolveAttempts != 3 {
		t.Fatalf("session resolution attempts = %d, want 3", resolveAttempts)
	}
	if probe.checkpointAttempts != 3 {
		t.Fatalf("checkpoint attempts = %d, want 3", probe.checkpointAttempts)
	}
}

func TestCaptureDurabilityCheckpointTreatsAbsentStoreAsEmptyBaseline(t *testing.T) {
	t.Parallel()
	probe := &fakeDurabilityProbe{checkpointErr: fmt.Errorf("locate session %q: %w", "fresh", os.ErrNotExist)}

	checkpoint, err := captureDurabilityCheckpoint(context.Background(), probe, "fresh")
	if err != nil {
		t.Fatalf("captureDurabilityCheckpoint error = %v, want empty baseline for an absent store", err)
	}
	if checkpoint != (cli.Checkpoint{}) {
		t.Fatalf("checkpoint = %#v, want zero start-of-store baseline", checkpoint)
	}
}

func TestCaptureDurabilityCheckpointKeepsRealErrors(t *testing.T) {
	t.Parallel()
	realErr := errors.New("open session store: permission denied")
	probe := &fakeDurabilityProbe{checkpointErr: realErr}

	_, err := captureDurabilityCheckpoint(context.Background(), probe, "session-1")
	if !errors.Is(err, realErr) {
		t.Fatalf("captureDurabilityCheckpoint error = %v, want %v", err, realErr)
	}
}

func TestDirectRunnerSucceedsWhenSessionStoreAppearsAfterAcceptance(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	t.Setenv("AGENT_RUNNER_DIRECT_HELPER", "2")
	// The store appears only after the accept-time capture window has expired,
	// so the completion must fall back to an empty baseline and confirm within
	// the durability bound instead of failing at accept time.
	probe := &lateAppearingStoreProbe{appearAt: time.Now().Add(freshSessionResolveTimeout + time.Second)}
	runner := NewDirectRunner(&DirectOptions{
		Args:              []string{os.Args[0], "-test.run=^TestDirectRunnerHelperProcess$"},
		StepID:            "implement",
		SessionID:         "session-late",
		CLI:               "fake",
		Control:           server,
		Probe:             probe,
		Foreground:        false,
		TerminationGrace:  250 * time.Millisecond,
		DurabilityTimeout: 8 * time.Second,
	})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Completed || result.DurabilityFailed {
		t.Fatalf("Run result = %#v (durability error %v), want durable completion for a store that appeared after acceptance", result, result.DurabilityError)
	}
}

func TestDirectRunnerHelperProcess(t *testing.T) {
	helperMode := os.Getenv("AGENT_RUNNER_DIRECT_HELPER")
	if helperMode != "1" && helperMode != "2" {
		return
	}
	if err := SendControlEventFromEnvironment(context.Background(), MessageCompleteStep, os.Getenv); err != nil {
		os.Exit(10)
	}
	if helperMode == "1" {
		if err := SendControlEventFromEnvironment(context.Background(), MessageTurnCommitted, os.Getenv); err != nil {
			os.Exit(11)
		}
	}
	select {}
}

type immediateDurabilityProbe struct{}

func (immediateDurabilityProbe) Checkpoint(string) (cli.Checkpoint, error) {
	return cli.Checkpoint{Artifact: "fixture"}, nil
}

func (immediateDurabilityProbe) WaitForCommittedTurn(ctx context.Context, _ string, _ cli.Checkpoint) error {
	<-ctx.Done()
	return ctx.Err()
}

type lateAppearingStoreProbe struct {
	appearAt time.Time
}

func (p *lateAppearingStoreProbe) Checkpoint(string) (cli.Checkpoint, error) {
	if time.Now().Before(p.appearAt) {
		return cli.Checkpoint{}, fmt.Errorf("locate session: native session store not found: %w", os.ErrNotExist)
	}
	return cli.Checkpoint{Artifact: "fixture", Offset: 64}, nil
}

func (p *lateAppearingStoreProbe) WaitForCommittedTurn(_ context.Context, _ string, after cli.Checkpoint) error {
	if after.Artifact == "" {
		return errors.New("durability checkpoint has no inspected artifact")
	}
	return nil
}

type sessionRecordingDurabilityProbe struct {
	checkpointSession  chan string
	waitSession        chan string
	checkpointFailures int
	checkpointAttempts int
}

func (p *sessionRecordingDurabilityProbe) Checkpoint(sessionID string) (cli.Checkpoint, error) {
	p.checkpointAttempts++
	if p.checkpointAttempts <= p.checkpointFailures {
		return cli.Checkpoint{}, errors.New("native store temporarily unavailable")
	}
	p.checkpointSession <- sessionID
	return cli.Checkpoint{Artifact: "fixture"}, nil
}

func (p *sessionRecordingDurabilityProbe) WaitForCommittedTurn(_ context.Context, sessionID string, _ cli.Checkpoint) error {
	p.waitSession <- sessionID
	return nil
}

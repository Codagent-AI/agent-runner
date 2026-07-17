package interactive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/cli"
)

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

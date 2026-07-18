package interactive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/cli"
)

func TestStartDirectChildDropsConfiguredEnvVars(t *testing.T) {
	t.Setenv("AGENT_RUNNER_TEST_LEAK", "leaked")
	out := filepath.Join(t.TempDir(), "env.out")
	options := &DirectOptions{
		Args:    []string{"sh", "-c", `printf '%s' "${AGENT_RUNNER_TEST_LEAK-absent}" > ` + out},
		DropEnv: []string{"AGENT_RUNNER_TEST_LEAK"},
	}

	cmd, _, _, err := startDirectChild(options, &Attempt{})
	if err != nil {
		t.Fatalf("startDirectChild: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child failed: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read child env capture: %v", err)
	}
	if got := string(data); got != "absent" {
		t.Fatalf("child saw AGENT_RUNNER_TEST_LEAK = %q, want it dropped", got)
	}
}

func TestPruneEnvironmentKeepsUnrelatedEntries(t *testing.T) {
	env := []string{"KEEP=1", "DROP=2", "ALSO_KEEP=DROP=nested"}
	got := pruneEnvironment(env, []string{"DROP"})
	want := []string{"KEEP=1", "ALSO_KEEP=DROP=nested"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("pruneEnvironment mismatch (-want +got):\n%s", diff)
	}
}

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

func TestDirectRunnerJoinsRunAndRestoreFailures(t *testing.T) {
	restoreErr := errors.New("restore terminal: boom")
	runner := NewDirectRunner(&DirectOptions{
		Args:  []string{"unused"},
		After: func() error { return restoreErr },
	})

	_, err := runner.Run(context.Background())
	if err == nil || !errors.Is(err, restoreErr) {
		t.Fatalf("Run() error = %v, want joined restore failure", err)
	}
	for _, want := range []string{"control server is required", "restore terminal after direct child"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Run() error = %v, want %q", err, want)
		}
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
	if !result.Started || !result.Completed || result.DurabilityFailed {
		t.Fatalf("Run result = %#v, want completed durable result", result)
	}
	if before != 1 || after != 1 {
		t.Fatalf("lease calls = before %d after %d, want 1 each", before, after)
	}
}

func TestDirectRunnerPassesAcceptedReceiptToReceiptProbe(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	t.Setenv("AGENT_RUNNER_DIRECT_HELPER", "2")
	probe := &receiptCapturingProbe{receipts: make(chan string, 1)}
	runner := NewDirectRunner(&DirectOptions{
		Args:              []string{os.Args[0], "-test.run=^TestDirectRunnerHelperProcess$"},
		StepID:            "implement",
		SessionID:         "session-1",
		CLI:               "cursor",
		Control:           server,
		Probe:             probe,
		Foreground:        false,
		TerminationGrace:  250 * time.Millisecond,
		DurabilityTimeout: time.Second,
	})

	result, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Completed || result.DurabilityFailed {
		t.Fatalf("Run result = %#v, want receipt-confirmed completion", result)
	}
	select {
	case receipt := <-probe.receipts:
		if receipt == "" {
			t.Fatal("durability wait ran without the accepted completion receipt")
		}
	default:
		t.Fatal("receipt-capable probe was not used for the durability wait")
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

func TestCaptureDurabilityCheckpointBoundsProbeContext(t *testing.T) {
	t.Parallel()
	probe := &contextBlockingCheckpointProbe{}

	start := time.Now()
	_, err := captureDurabilityCheckpoint(context.Background(), probe, "session-1")
	if err == nil {
		t.Fatal("captureDurabilityCheckpoint succeeded despite a stalled probe subprocess")
	}
	if elapsed := time.Since(start); elapsed > freshSessionResolveTimeout+time.Second {
		t.Fatalf("captureDurabilityCheckpoint took %v; the capture window was not plumbed into the probe context", elapsed)
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

func TestDirectRunnerWatchdogStartFailureTearsDownChild(t *testing.T) {
	server := newTestControlServer(t, t.TempDir(), &recordingEventLogger{})
	defer server.Close()
	t.Setenv("AGENT_RUNNER_DIRECT_HELPER", "1")

	var childPID int
	runner := NewDirectRunner(&DirectOptions{
		Args:               []string{os.Args[0], "-test.run=^TestDirectRunnerHelperProcess$"},
		StepID:             "implement",
		SessionID:          "session-1",
		CLI:                "fake",
		Control:            server,
		Probe:              immediateDurabilityProbe{},
		Foreground:         false,
		TerminationGrace:   250 * time.Millisecond,
		DurabilityTimeout:  time.Second,
		WatchdogExecutable: filepath.Join(t.TempDir(), "missing-watchdog"),
		Persist: func(metadata *ProcessMetadata) {
			if metadata != nil {
				childPID = metadata.ChildPID
			}
		},
	})

	result, err := runner.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "start child watchdog") {
		t.Fatalf("Run error = %v, want watchdog start failure", err)
	}
	if !result.Started {
		t.Fatalf("Run result = %#v, want started child despite post-spawn watchdog failure", result)
	}
	if childPID == 0 {
		t.Fatal("child pid was never persisted")
	}
	if killErr := syscall.Kill(childPID, 0); !errors.Is(killErr, syscall.ESRCH) {
		t.Fatalf("kill(child, 0) = %v, want ESRCH for a killed and reaped child", killErr)
	}
}

func TestWatchdogCommandRunsInOwnProcessGroup(t *testing.T) {
	t.Parallel()
	metadata := ProcessMetadata{ChildPID: 123, PGID: 123, StartTime: "42"}

	command := newWatchdogCommand("/bin/agent-runner", metadata, time.Second)

	if command.SysProcAttr == nil || !command.SysProcAttr.Setpgid {
		t.Fatalf("watchdog command SysProcAttr = %+v, want Setpgid enabled so group-directed signals to the runner do not kill the watchdog", command.SysProcAttr)
	}
	wantArgs := []string{"/bin/agent-runner", "internal", "watchdog", "--pid", "123", "--pgid", "123", "--start-time", "42", "--grace", "1s"}
	if diff := cmp.Diff(wantArgs, command.Args); diff != "" {
		t.Fatalf("watchdog command args mismatch (-want +got):\n%s", diff)
	}
}

func TestDirectRunnerHelperProcess(t *testing.T) {
	helperMode := os.Getenv("AGENT_RUNNER_DIRECT_HELPER")
	if helperMode != "1" && helperMode != "2" {
		return
	}
	if _, err := SendControlEventFromEnvironment(context.Background(), MessageCompleteStep, os.Getenv); err != nil {
		os.Exit(10)
	}
	if helperMode == "1" {
		if _, err := SendControlEventFromEnvironment(context.Background(), MessageTurnCommitted, os.Getenv); err != nil {
			os.Exit(11)
		}
	}
	select {}
}

// contextBlockingCheckpointProbe models an inspection subprocess that hangs
// until its context is cancelled — only a plumbed-through capture window can
// unblock it.
type contextBlockingCheckpointProbe struct{}

func (p *contextBlockingCheckpointProbe) Checkpoint(ctx context.Context, _ string) (cli.Checkpoint, error) {
	<-ctx.Done()
	return cli.Checkpoint{}, ctx.Err()
}

func (p *contextBlockingCheckpointProbe) WaitForCommittedTurn(ctx context.Context, _ string, _ cli.Checkpoint) error {
	<-ctx.Done()
	return ctx.Err()
}

type receiptCapturingProbe struct {
	receipts chan string
}

func (p *receiptCapturingProbe) Checkpoint(context.Context, string) (cli.Checkpoint, error) {
	return cli.Checkpoint{Artifact: "fixture"}, nil
}

func (p *receiptCapturingProbe) WaitForCommittedTurn(context.Context, string, cli.Checkpoint) error {
	return errors.New("receipt-free wait used despite an available receipt")
}

func (p *receiptCapturingProbe) WaitForCommittedTurnWithReceipt(_ context.Context, _ string, _ cli.Checkpoint, receipt string) error {
	p.receipts <- receipt
	return nil
}

type immediateDurabilityProbe struct{}

func (immediateDurabilityProbe) Checkpoint(context.Context, string) (cli.Checkpoint, error) {
	return cli.Checkpoint{Artifact: "fixture"}, nil
}

func (immediateDurabilityProbe) WaitForCommittedTurn(ctx context.Context, _ string, _ cli.Checkpoint) error {
	<-ctx.Done()
	return ctx.Err()
}

type lateAppearingStoreProbe struct {
	appearAt time.Time
}

func (p *lateAppearingStoreProbe) Checkpoint(context.Context, string) (cli.Checkpoint, error) {
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

func (p *sessionRecordingDurabilityProbe) Checkpoint(_ context.Context, sessionID string) (cli.Checkpoint, error) {
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

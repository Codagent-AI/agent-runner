package interactive

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
)

const DefaultDurabilityTimeout = 30 * time.Second

var ErrDurabilityTimeout = errors.New("committed-turn durability confirmation timed out")

type CompletionOutcome string

const (
	CompletionSuccess CompletionOutcome = "success"
	CompletionFailed  CompletionOutcome = "failed"
)

// DurabilityOptions provides the semantic probe and optional native hook
// signal used after the completion client has received its acknowledgement.
type DurabilityOptions struct {
	CLI           string
	SessionID     string
	Probe         cli.TurnDurabilityProbe
	Checkpoint    cli.Checkpoint
	CheckpointErr error
	CommittedTurn <-chan struct{}
	ChildExited   <-chan struct{}
	Timeout       time.Duration
	Timer         *ActiveRuntimeTimer
	Logger        audit.EventLogger
	Prefix        string
	Now           func() time.Time
}

// DurabilityResult tells the direct runner whether to terminate the child and
// which workflow outcome to record. Both success and durability failure end
// the interactive process; only semantic confirmation yields success.
type DurabilityResult struct {
	Outcome        CompletionOutcome
	TerminateChild bool
	Err            error
}

// AwaitTurnDurability uses the checkpoint captured at acceptance time, then
// waits for either a native post-turn signal or an adapter-confirmed committed
// assistant turn. Child exit is deliberately not a cancellation source: store
// inspection continues for the remaining active-runtime bound.
func AwaitTurnDurability(ctx context.Context, options *DurabilityOptions) DurabilityResult {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultDurabilityTimeout
	}
	if options.Probe == nil {
		err := errors.New("turn durability probe is required")
		emitDurabilityFailure(options, timeout, "unavailable", err)
		return DurabilityResult{Outcome: CompletionFailed, TerminateChild: true, Err: err}
	}
	checkpoint := options.Checkpoint
	if options.CheckpointErr != nil && options.CommittedTurn == nil {
		err := fmt.Errorf("checkpoint committed-turn evidence: %w", options.CheckpointErr)
		emitDurabilityFailure(options, timeout, "unavailable", err)
		return DurabilityResult{Outcome: CompletionFailed, TerminateChild: true, Err: err}
	}

	timer := options.Timer
	ownTimer := timer == nil
	if timer == nil {
		timer = NewActiveRuntimeTimer(timeout)
	}
	if ownTimer {
		defer timer.Stop()
	}
	waitCtx, cancelWait := context.WithCancel(ctx)
	defer cancelWait()
	var probeResult chan error
	if options.CheckpointErr == nil {
		probeResult = make(chan error, 1)
		go func() {
			probeResult <- waitForDurableTurn(waitCtx, options.Probe, options.SessionID, checkpoint)
		}()
	}

	childExited := options.ChildExited
	for {
		select {
		case <-options.CommittedTurn:
			return DurabilityResult{Outcome: CompletionSuccess, TerminateChild: true}
		case waitErr := <-probeResult:
			if waitErr == nil {
				return DurabilityResult{Outcome: CompletionSuccess, TerminateChild: true}
			}
			if ctx.Err() != nil {
				return DurabilityResult{Outcome: CompletionFailed, TerminateChild: true, Err: ctx.Err()}
			}
			emitDurabilityFailure(options, timeout, checkpoint.Artifact, waitErr)
			return DurabilityResult{Outcome: CompletionFailed, TerminateChild: true, Err: waitErr}
		case <-childExited:
			// Hook evidence can no longer arrive, but the adapter continues
			// inspecting the native store for the remainder of the bound.
			childExited = nil
		case <-timer.Done():
			cancelWait()
			timeoutErr := fmt.Errorf("%w", ErrDurabilityTimeout)
			if options.CheckpointErr != nil {
				timeoutErr = fmt.Errorf("%w: checkpoint unavailable: %v", ErrDurabilityTimeout, options.CheckpointErr)
			}
			emitDurabilityFailure(options, timeout, checkpoint.Artifact, timeoutErr)
			return DurabilityResult{Outcome: CompletionFailed, TerminateChild: true, Err: timeoutErr}
		case <-ctx.Done():
			return DurabilityResult{Outcome: CompletionFailed, TerminateChild: true, Err: ctx.Err()}
		}
	}
}

// storeAppearancePollInterval paces retries while the native session store
// does not exist yet. The wait itself stays bounded by the durability timer.
const storeAppearancePollInterval = 50 * time.Millisecond

// waitForDurableTurn confirms a committed turn against the accept-time
// baseline. A zero baseline means the native store did not exist when the
// completion was accepted; the store is then awaited and inspected from its
// start. A store that is still (or again) absent is not a real failure, so
// the probe keeps polling until the context ends the bound.
func waitForDurableTurn(ctx context.Context, probe cli.TurnDurabilityProbe, sessionID string, baseline cli.Checkpoint) error {
	for {
		current := baseline
		if current.Artifact == "" {
			captured, err := probe.Checkpoint(sessionID)
			if err != nil {
				if !errors.Is(err, fs.ErrNotExist) {
					return err
				}
				if sleepErr := sleepContext(ctx, storeAppearancePollInterval); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			current = cli.Checkpoint{Artifact: captured.Artifact}
		}
		err := probe.WaitForCommittedTurn(ctx, sessionID, current)
		if err == nil || !errors.Is(err, fs.ErrNotExist) || ctx.Err() != nil {
			return err
		}
		if sleepErr := sleepContext(ctx, storeAppearancePollInterval); sleepErr != nil {
			return sleepErr
		}
	}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func emitDurabilityFailure(options *DurabilityOptions, timeout time.Duration, artifact string, err error) {
	if options.Logger == nil {
		return
	}
	if artifact == "" {
		artifact = "unavailable"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	options.Logger.Emit(audit.Event{
		Timestamp: now().UTC().Format(time.RFC3339Nano),
		Prefix:    options.Prefix,
		Type:      audit.EventDurabilityFailure,
		Data: map[string]any{
			"cli":                options.CLI,
			"session_id":         options.SessionID,
			"timeout":            timeout.String(),
			"inspected_artifact": artifact,
			"error":              err.Error(),
		},
	})
}

// ActiveRuntimeTimer is a deadline whose remaining duration does not decrease
// while paused. The direct supervisor will pause it on child stop events.
type ActiveRuntimeTimer struct {
	mu        sync.Mutex
	done      chan struct{}
	timer     *time.Timer
	remaining time.Duration
	started   time.Time
	paused    bool
	finished  bool
}

func NewActiveRuntimeTimer(duration time.Duration) *ActiveRuntimeTimer {
	t := &ActiveRuntimeTimer{done: make(chan struct{}), remaining: duration, started: time.Now()}
	if duration <= 0 {
		t.finished = true
		close(t.done)
		return t
	}
	t.timer = time.AfterFunc(duration, t.expire)
	return t
}

func (t *ActiveRuntimeTimer) Done() <-chan struct{} { return t.done }

func (t *ActiveRuntimeTimer) Pause() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished || t.paused {
		return
	}
	if !t.timer.Stop() {
		return
	}
	t.remaining -= time.Since(t.started)
	if t.remaining < 0 {
		t.remaining = 0
	}
	t.paused = true
}

func (t *ActiveRuntimeTimer) Resume() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished || !t.paused {
		return
	}
	t.paused = false
	t.started = time.Now()
	if t.remaining <= 0 {
		t.finished = true
		close(t.done)
		return
	}
	t.timer = time.AfterFunc(t.remaining, t.expire)
}

func (t *ActiveRuntimeTimer) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished {
		return
	}
	if t.timer != nil {
		t.timer.Stop()
	}
	t.finished = true
}

func (t *ActiveRuntimeTimer) expire() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finished || t.paused {
		return
	}
	t.finished = true
	close(t.done)
}

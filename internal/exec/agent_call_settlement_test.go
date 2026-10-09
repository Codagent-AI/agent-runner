package exec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/agentcall"
	"github.com/codagent/agent-runner/internal/control"
)

func TestCallTimeoutAndFollowUp(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		runner := &callTestRunner{release: make(chan struct{})}
		h := NewAgentCallHandler(testAgentCallOptions(t.TempDir(), runner, &callTestAdapter{}))
		result := decodeCallResponse(t, h.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "r", Payload: []byte(`{"agent":"implementor","prompt":"x","timeout":"1s"}`)}))
		if result.Error == nil || result.Error.Code != agentcall.CodeTimedOut {
			t.Fatalf("%+v", result)
		}
		if result.Details == nil || result.Details.Exit != "terminated" {
			t.Fatalf("details: %+v", result.Details)
		}
	})
	t.Run("follow-up", func(t *testing.T) {
		a := &callTestAdapter{discovered: "native"}
		h := NewAgentCallHandler(testAgentCallOptions(t.TempDir(), &callTestRunner{result: ProcessResult{Started: true}}, a))
		first := decodeCallResponse(t, h.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "r1", Payload: []byte(`{"agent":"implementor","prompt":"x"}`)}))
		next := decodeCallResponse(t, h.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "r2", Payload: []byte(`{"follow_up":"` + first.CallID + `","prompt":"fix"}`)}))
		if next.Error != nil {
			t.Fatal(next.Error)
		}
		if len(a.inputs) != 2 || !a.inputs[1].Resume || a.inputs[1].SessionID != "native" {
			t.Fatalf("inputs: %+v", a.inputs)
		}
		if next.Details == nil || next.Details.Session.Model != "base-model" {
			t.Fatalf("details %+v", next.Details)
		}
	})
}

func TestNaturalSettlementWinsOverLateDeadline(t *testing.T) {
	var fire func()
	paused, release := make(chan struct{}), make(chan struct{})
	o := testAgentCallOptions(t.TempDir(), &callTestRunner{result: ProcessResult{Started: true}}, &callTestAdapter{})
	o.AfterFunc = func(_ time.Duration, f func()) *time.Timer { fire = f; return time.NewTimer(time.Hour) }
	o.afterSettle = func() { close(paused); <-release }
	h := NewAgentCallHandler(o)
	done := make(chan agentcall.Response, 1)
	go func() {
		done <- decodeCallResponse(t, h.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "r", Payload: []byte(`{"agent":"implementor","prompt":"x","timeout":"1s"}`)}))
	}()
	<-paused
	fire()
	close(release)
	r := <-done
	if r.Error != nil || r.Details.Exit != "exited" {
		t.Fatalf("%+v", r)
	}
}

func TestFollowUpSourceRejections(t *testing.T) {
	for _, tc := range []struct{ name, status, kind, native, code string }{
		{"unknown", "", "", "", agentcall.CodeUnknownCall},
		{"running", agentcall.StatusRunning, "agent", "native", agentcall.CodeCallInProgress},
		{"named", agentcall.StatusSucceeded, "session", "native", agentcall.CodeInvalidTarget},
		{"no-session", agentcall.StatusFailed, "agent", "", agentcall.CodeNotResumable},
		{"self", agentcall.StatusSucceeded, "agent", "parent-session", agentcall.CodeSelfSession},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := testAgentCallOptions(t.TempDir(), &callTestRunner{}, &callTestAdapter{})
			h := NewAgentCallHandler(o)
			c, failure := h.resolve([]byte(`{"agent":"implementor","prompt":"x"}`))
			if failure != nil {
				t.Fatal(failure)
			}
			if tc.name != "unknown" {
				h.byCallID["source"] = &acceptedAgentCall{callID: "source", target: agentcall.Target{Kind: agentcall.TargetKind(tc.kind)}, status: tc.status, nativeSessionID: tc.native, resolved: c, started: time.Now()}
			}
			_, err := h.resolve([]byte(`{"follow_up":"source","prompt":"fix"}`))
			if err == nil || err.Code != tc.code {
				t.Fatalf("got %+v want %s", err, tc.code)
			}
		})
	}
}

func TestFollowUpTrimsExplicitWorkdir(t *testing.T) {
	worktree := t.TempDir()
	if err := os.MkdirAll(filepath.Join(worktree, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	h := NewAgentCallHandler(testAgentCallOptions(worktree, &callTestRunner{}, &callTestAdapter{}))
	source, failure := h.resolve([]byte(`{"agent":"implementor","prompt":"x"}`))
	if failure != nil {
		t.Fatal(failure)
	}
	h.byCallID["source"] = &acceptedAgentCall{callID: "source", target: agentcall.Target{Kind: agentcall.TargetAgent}, status: agentcall.StatusSucceeded, nativeSessionID: "native", resolved: source, started: time.Now()}
	trimmed, failure := h.resolve([]byte(`{"follow_up":"source","prompt":"fix","workdir":"child"}`))
	if failure != nil {
		t.Fatal(failure)
	}
	padded, failure := h.resolve([]byte(`{"follow_up":"source","prompt":"fix","workdir":"  child\t"}`))
	if failure != nil {
		t.Fatalf("padded workdir: %+v", failure)
	}
	if padded.workdir != trimmed.workdir {
		t.Fatalf("padded workdir = %q, want %q", padded.workdir, trimmed.workdir)
	}
}

func TestSettlementDeadlineAndCancelRaces(t *testing.T) {
	for _, winner := range []string{"cancel", "timeout", "exit-then-cancel"} {
		t.Run(winner, func(t *testing.T) {
			var fire func()
			paused, release := make(chan struct{}), make(chan struct{})
			processRelease := make(chan struct{})
			runner := &callTestRunner{release: processRelease, started: make(chan AgentProcessOptions, 1), result: ProcessResult{Started: true}}
			o := pollingAgentCallOptions(t.TempDir(), runner, &callTestAdapter{})
			o.AfterFunc = func(_ time.Duration, f func()) *time.Timer { fire = f; return time.NewTimer(time.Hour) }
			o.afterSettle = func() { close(paused); <-release }
			h := NewAgentCallHandler(o)
			start := startAgentCall(t, h, control.AgentCallRequest{RequestID: "r", Payload: []byte(`{"agent":"implementor","prompt":"x","timeout":"1s"}`)})
			<-runner.started
			canceled := make(chan agentcall.Response, 1)
			switch winner {
			case "timeout":
				go fire()
			case "cancel":
				go func() { canceled <- cancelAgentCall(t, h, start.CallID) }()
			case "exit-then-cancel":
				close(processRelease)
			}
			<-paused
			h.mu.Lock()
			at := h.byCallID[start.CallID].settlement.at
			h.mu.Unlock()
			if winner == "cancel" {
				fire()
			}
			if winner == "exit-then-cancel" {
				fire()
				go func() { canceled <- cancelAgentCall(t, h, start.CallID) }()
			}
			close(release)
			response := awaitAgentCall(t, h, start.CallID)
			switch winner {
			case "timeout":
				if response.Error == nil || response.Error.Code != agentcall.CodeTimedOut || response.Details.Exit != "terminated" {
					t.Fatalf("%+v", response)
				}
			case "cancel":
				if response.Error == nil || response.Error.Code != agentcall.CodeCallCanceled {
					t.Fatalf("%+v", response)
				}
			case "exit-then-cancel":
				if response.Error != nil || response.Details.Exit != "exited" {
					t.Fatalf("%+v", response)
				}
			}
			h.mu.Lock()
			if !h.byCallID[start.CallID].settlement.at.Equal(at) {
				t.Error("settlement moved")
			}
			h.mu.Unlock()
			if winner != "timeout" {
				r := <-canceled
				if r.Status != response.Status {
					t.Fatal("cancel disagreed with settlement")
				}
			}
		})
	}
}

func TestFinalizeExecutionEnsuresSettlement(t *testing.T) {
	for _, tc := range []struct {
		name             string
		launched         bool
		exit             int
		outcome          StepOutcome
		status, exitKind string
	}{
		{"no invocation", false, 0, "", agentcall.StatusFailed, "not_launched"},
		{"successful invocation", true, 0, OutcomeSuccess, agentcall.StatusSucceeded, "exited"},
		{"failed invocation", true, 9, OutcomeFailed, agentcall.StatusFailed, "exited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := testAgentCallOptions(t.TempDir(), &callTestRunner{}, &callTestAdapter{})
			o.Git = func(context.Context, string, ...string) (string, error) { return "", errors.New("unavailable") }
			h := NewAgentCallHandler(o)
			resolved, failure := h.resolve([]byte(`{"agent":"implementor","prompt":"x"}`))
			if failure != nil {
				t.Fatal(failure)
			}
			record := &acceptedAgentCall{callID: "call", started: time.Now(), done: make(chan struct{})}
			h.active = record
			execution := agentCallExecution{invocation: AgentInvocationResult{CLILaunched: tc.launched, ExitCode: tc.exit, Outcome: tc.outcome, Response: "done"}}
			h.finalizeExecution(record, resolved, &execution)
			response := decodeCallResponse(t, record.response)
			if response.Status != tc.status || response.Details == nil || response.Details.Exit != tc.exitKind {
				t.Fatalf("%+v", response)
			}
			if tc.launched {
				if response.Details.ExitCode == nil || *response.Details.ExitCode != tc.exit {
					t.Fatalf("exit code: %+v", response.Details)
				}
			} else if response.Error == nil || response.Error.Code != agentcall.CodeExecutionFailed {
				t.Fatalf("missing launch failure: %+v", response)
			}
			select {
			case <-record.done:
			default:
				t.Fatal("terminal result not published")
			}
			if h.active != nil {
				t.Fatal("active slot not released")
			}
		})
	}
}

type canceledBeforeLaunchRunner struct{ callTestRunner }

func (r *canceledBeforeLaunchRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	r.started <- *options
	<-options.Context.Done()
	return ProcessResult{}, options.Context.Err()
}

func TestCancellationSettlementSurvivesUnlaunchedRunnerResult(t *testing.T) {
	for _, kind := range []string{"cancel", "timeout", "teardown"} {
		t.Run(kind, func(t *testing.T) {
			runner := &canceledBeforeLaunchRunner{callTestRunner: callTestRunner{started: make(chan AgentProcessOptions, 1)}}
			o := pollingAgentCallOptions(t.TempDir(), runner, &callTestAdapter{})
			o.Git = func(context.Context, string, ...string) (string, error) { return "", errors.New("unavailable") }
			parent, cancelParent := context.WithCancel(context.Background())
			defer cancelParent()
			o.AttemptContext = parent
			var fire func()
			o.AfterFunc = func(_ time.Duration, f func()) *time.Timer { fire = f; return time.NewTimer(time.Hour) }
			h := NewAgentCallHandler(o)
			start := startAgentCall(t, h, control.AgentCallRequest{RequestID: "r", Payload: []byte(`{"agent":"implementor","prompt":"x","timeout":"1s"}`)})
			<-runner.started
			wantCode, wantStatus := agentcall.CodeCallCanceled, agentcall.StatusCanceled
			switch kind {
			case "cancel":
				cancelAgentCall(t, h, start.CallID)
			case "timeout":
				fire()
				wantCode = agentcall.CodeTimedOut
				wantStatus = agentcall.StatusFailed
			case "teardown":
				cancelParent()
			}
			response := awaitAgentCall(t, h, start.CallID)
			if response.Error == nil || response.Error.Code != wantCode || response.Status != wantStatus || response.Details == nil || response.Details.Exit != "terminated" {
				t.Fatalf("unlaunched runner result changed settlement: %+v", response)
			}
		})
	}
}

package exec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/interactive"
	"github.com/codagent/agent-runner/internal/model"
)

func TestInvokeAgentCrashClassification(t *testing.T) {
	for _, tt := range []struct {
		name    string
		result  ProcessResult
		err     error
		crashed bool
	}{
		{"exit one", ProcessResult{Started: true, ExitCode: 1}, nil, true},
		{"signal", ProcessResult{Started: true, ExitCode: -1}, nil, true},
		{"launch", ProcessResult{}, errors.New("launch failed"), true},
		{"cancel", ProcessResult{Started: true}, context.Canceled, false},
		{"normal", ProcessResult{Started: true}, nil, false},
		{"disallowed question", ProcessResult{Started: true, Stderr: "AskUserQuestion not allowed"}, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runner := &invocationRecordingRunner{options: make(chan AgentProcessOptions, 1), result: tt.result, err: tt.err}
			got, _ := InvokeAgent(&AgentInvocation{Context: context.Background(), Adapter: &invocationTestAdapter{}, Args: []string{"agent"}, CLI: "test", InvocationContext: cli.ContextAutonomousHeadless}, runner, &mockLogger{})
			if got.Crashed != tt.crashed {
				t.Fatalf("Crashed = %t, want %t", got.Crashed, tt.crashed)
			}
		})
	}
}

func TestBoundStderr(t *testing.T) {
	for _, n := range []int{4096, 4097, 10000} {
		original := strings.Repeat("é", n/2) + strings.Repeat("x", n%2)
		got := boundStderr(original)
		if len(got) > 4096 || !utf8.ValidString(got) {
			t.Fatalf("size=%d valid=%t", len(got), utf8.ValidString(got))
		}
		if len(original) <= 4096 && got != original {
			t.Fatal("unneeded truncation")
		}
		if len(original) > 4096 && (!strings.Contains(got, stderrMarker) || !strings.HasPrefix(original, strings.Split(got, stderrMarker)[0]) || !strings.HasSuffix(original, strings.Split(got, stderrMarker)[1])) {
			t.Fatal("lost head or tail")
		}
	}
}

func TestBoundStderrReplacesInvalidUTF8(t *testing.T) {
	if got := boundStderr("ok\xffdone"); got != "ok\uFFFDdone" {
		t.Fatalf("got %q", got)
	}
}

func TestBoundStderrExactHeadTail(t *testing.T) {
	original := strings.Repeat("a", 5000) + strings.Repeat("z", 5000)
	remaining := 4096 - len(stderrMarker)
	head := remaining / 2
	tail := remaining - head
	want := original[:head] + stderrMarker + original[len(original)-tail:]
	if got := boundStderr(original); got != want || len(got) != 4096 {
		t.Fatalf("excerpt length=%d", len(got))
	}
}

func TestClassifyCrash(t *testing.T) {
	code := 1
	record := &model.CrashRecord{StepID: "repair", Path: []model.NestingSegment{{StepID: "check"}, {StepID: "repair"}}, Stderr: "\ncapacity\nmore", ExitCode: &code}
	if got := ClassifyCrash(record, 1); got != "check repair failed (infrastructure): capacity after 1 repair attempts" {
		t.Fatal(got)
	}
}

func TestClassifyCrashEvidenceFallbacks(t *testing.T) {
	code := -1
	for _, tt := range []struct {
		name   string
		record model.CrashRecord
		want   string
	}{
		{"error", model.CrashRecord{StepID: "agent", Error: "\nprocess failed\nmore", Stderr: "provider message"}, "agent failed (infrastructure): process failed"},
		{"stderr", model.CrashRecord{StepID: "agent", Stderr: "\nprovider message\n"}, "agent failed (infrastructure): provider message"},
		{"exit", model.CrashRecord{StepID: "agent", ExitCode: &code}, "agent failed (infrastructure): exit code -1"},
		{"none", model.CrashRecord{StepID: "agent"}, "agent failed (infrastructure): agent session did not finish"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyCrash(&tt.record, 0); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestInteractiveAgentCrashClassification(t *testing.T) {
	original := interactiveRunnerFn
	defer func() { interactiveRunnerFn = original }()
	for _, tt := range []struct {
		name        string
		direct      interactive.DirectResult
		runErr      error
		suspendErr  error
		wantCrash   bool
		wantOutcome StepOutcome
	}{
		{"suspend", interactive.DirectResult{}, nil, errors.New("suspend failed"), true, OutcomeFailed},
		{"direct error", interactive.DirectResult{Started: true}, errors.New("direct failed"), nil, true, OutcomeFailed},
		{"durability", interactive.DirectResult{Started: true, DurabilityFailed: true, DurabilityError: errors.New("durability failed")}, nil, nil, true, OutcomeFailed},
		{"complete", interactive.DirectResult{Started: true, Completed: true}, nil, nil, false, OutcomeSuccess},
		{"aborted", interactive.DirectResult{Started: true}, nil, nil, false, OutcomeAborted},
	} {
		t.Run(tt.name, func(t *testing.T) {
			interactiveRunnerFn = func([]string, directRunOptions) (interactive.DirectResult, error) { return tt.direct, tt.runErr }
			var suspend func() error
			if tt.suspendErr != nil {
				suspend = func() error { return tt.suspendErr }
			}
			outcome, _, _, crashed, _ := runAgentProcess(&invocationRecordingRunner{options: make(chan AgentProcessOptions, 1)}, &invocationTestAdapter{}, &AgentProcessOptions{Context: context.Background()}, cli.ContextInteractive, &mockLogger{}, suspend, nil, nil)
			if outcome != tt.wantOutcome || crashed != tt.wantCrash {
				t.Fatalf("outcome=%q crashed=%t", outcome, crashed)
			}
		})
	}
}

func TestClassifyCrashBoundsReason(t *testing.T) {
	long := strings.Repeat("é", maxCrashReasonRunes+50)
	got := ClassifyCrash(&model.CrashRecord{StepID: "agent", Stderr: long}, 2)
	message := strings.TrimSuffix(strings.TrimPrefix(got, "agent failed (infrastructure): "), " after 2 repair attempts")
	if utf8.RuneCountInString(message) != maxCrashReasonRunes || !strings.HasSuffix(message, "…") || !utf8.ValidString(got) {
		t.Fatalf("message runes=%d: %q", utf8.RuneCountInString(message), got)
	}
	short := ClassifyCrash(&model.CrashRecord{StepID: "agent", Stderr: strings.Repeat("x", maxCrashReasonRunes)}, 0)
	if strings.HasSuffix(short, "…") {
		t.Fatalf("unneeded truncation: %q", short)
	}
}

func TestRecordAgentCrashAgentSessionID(t *testing.T) {
	for _, tt := range []struct {
		name       string
		invocation *AgentInvocationResult
		want       string
	}{
		{"discovered", &AgentInvocationResult{SessionID: "resumed", DiscoveredSessionID: "discovered"}, "discovered"},
		{"resumed", &AgentInvocationResult{SessionID: "resumed"}, "resumed"},
		{"none", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &model.ExecutionContext{ExecutionSessionID: "runner-exec", Crashes: model.NewCrashLedger()}
			recordAgentCrash(ctx, &model.Step{ID: "agent"}, "[agent]", 1, tt.invocation, nil)
			origin := ctx.StepFailure.Origin
			if origin.AgentSessionID != tt.want || origin.ExecutionSessionID != "runner-exec" {
				t.Fatalf("agent=%q execution=%q", origin.AgentSessionID, origin.ExecutionSessionID)
			}
		})
	}
}

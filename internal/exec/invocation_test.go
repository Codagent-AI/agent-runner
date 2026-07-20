package exec

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/model"
)

type invocationTestAdapter struct{}

func (*invocationTestAdapter) BuildArgs(*cli.BuildArgsInput) []string { return nil }
func (*invocationTestAdapter) DiscoverSessionID(*cli.DiscoverOptions) string {
	return "discovered-session"
}
func (*invocationTestAdapter) SupportsSystemPrompt() bool { return false }
func (*invocationTestAdapter) ProbeModel(string, string) (cli.ProbeStrength, error) {
	return cli.BinaryOnly, nil
}
func (*invocationTestAdapter) FilterOutput(raw string) string { return "filtered:" + raw }
func (*invocationTestAdapter) ExtractUsage(string) (cli.UsageExtraction, error) {
	cost := 0.25
	return cli.UsageExtraction{
		Usage: model.UsageRecord{
			Status: model.UsageCollected, CLI: "test", Source: "test",
			Tokens: model.TokenCounts{model.TokenInput: 12},
		},
		EstimatedCostUSD: &cost,
	}, nil
}

type invocationRecordingRunner struct {
	options chan AgentProcessOptions
	result  ProcessResult
	err     error
}

type isolatingInvocationRunner struct {
	started chan AgentProcessOptions
	release map[string]chan struct{}
}

func (r *isolatingInvocationRunner) RunShell(string, bool, string) (ProcessResult, error) {
	return ProcessResult{}, nil
}
func (r *isolatingInvocationRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	r.started <- *options
	select {
	case <-options.Context.Done():
		return ProcessResult{Started: true, ExitCode: -1, Stderr: options.Prefix + " canceled"}, options.Context.Err()
	case <-r.release[options.Prefix]:
		return ProcessResult{Started: true, Stdout: options.Prefix + " output"}, nil
	}
}
func (r *isolatingInvocationRunner) RunScript(string, []byte, bool, string) (ProcessResult, error) {
	return ProcessResult{}, nil
}

func (r *invocationRecordingRunner) RunShell(string, bool, string) (ProcessResult, error) {
	return ProcessResult{}, nil
}
func (r *invocationRecordingRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	r.options <- *options
	return r.result, r.err
}

func TestInvokeAgentRetainsLaunchEvidenceWhenRunningProcessIsCanceled(t *testing.T) {
	runner := &invocationRecordingRunner{
		options: make(chan AgentProcessOptions, 1),
		result:  ProcessResult{Started: true, ExitCode: -1, Stderr: "canceled"},
		err:     context.Canceled,
	}

	got, err := InvokeAgent(&AgentInvocation{
		Context: context.Background(), Adapter: &invocationTestAdapter{},
		Args: []string{"test-agent"}, InvocationContext: cli.ContextAutonomousHeadless,
		CLI: "test",
	}, runner, &mockLogger{})
	if err != context.Canceled {
		t.Fatalf("InvokeAgent() error = %v, want context canceled", err)
	}
	if !got.CLILaunched || got.Outcome != OutcomeFailed || got.Stderr != "canceled" {
		t.Fatalf("InvokeAgent() result = %#v", got)
	}
}
func (r *invocationRecordingRunner) RunScript(string, []byte, bool, string) (ProcessResult, error) {
	return ProcessResult{}, nil
}

func TestInvokeAgentReturnsTypedInvocationEvidence(t *testing.T) {
	started := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	finished := started.Add(3 * time.Second)
	times := []time.Time{started, finished}
	now := func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}
	runner := &invocationRecordingRunner{
		options: make(chan AgentProcessOptions, 1),
		result:  ProcessResult{ExitCode: 0, Stdout: "raw", Stderr: "warning"},
	}

	got, err := InvokeAgent(&AgentInvocation{
		Context: context.Background(), Adapter: &invocationTestAdapter{},
		Args: []string{"test-agent", "run"}, Env: []string{"TEST_VALUE=one"},
		DropEnv: []string{"OLD_VALUE"}, Workdir: "/tmp/work", Prefix: "parent/call:1",
		StdoutWrapper:     func(w io.Writer) io.Writer { return w },
		StderrWrapper:     func(w io.Writer) io.Writer { return w },
		InvocationContext: cli.ContextAutonomousHeadless,
		CLI:               "test", Model: "test-model", SessionID: "preset-session",
		SessionResumed: true, Now: now,
	}, runner, &mockLogger{})
	if err != nil {
		t.Fatalf("InvokeAgent() error = %v", err)
	}
	want := AgentInvocationResult{
		Outcome: OutcomeSuccess, Response: "filtered:raw",
		Stdout: "raw", Stderr: "warning", ExitCode: 0,
		CLI: "test", Model: "test-model", SessionID: "preset-session",
		DiscoveredSessionID: "discovered-session", SessionResumed: true,
		Usage: model.UsageRecord{
			Status: model.UsageCollected, CLI: "test", Source: "test",
			Tokens: model.TokenCounts{model.TokenInput: 12},
		},
		EstimatedCostUSD: float64Pointer(0.25),
		StartedAt:        started, FinishedAt: finished, Duration: 3 * time.Second,
		CLILaunched: true,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("InvokeAgent() mismatch (-want +got):\n%s", diff)
	}

	processOptions := <-runner.options
	if diff := cmp.Diff([]string{"test-agent", "run"}, processOptions.Args); diff != "" {
		t.Fatalf("process args mismatch (-want +got):\n%s", diff)
	}
	if processOptions.Context == nil || processOptions.Workdir != "/tmp/work" || processOptions.Prefix != "parent/call:1" {
		t.Fatalf("process options = %#v", processOptions)
	}
	wantDropEnv := append([]string{"OLD_VALUE"}, control.EnvironmentVariables()...)
	if diff := cmp.Diff(wantDropEnv, processOptions.DropEnv); diff != "" {
		t.Fatalf("process environment removals mismatch (-want +got):\n%s", diff)
	}
}

func TestInvokeAgentOverlappingCallsKeepProcessOptionsIsolated(t *testing.T) {
	firstContext, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	secondContext, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	runner := &isolatingInvocationRunner{
		started: make(chan AgentProcessOptions, 2),
		release: map[string]chan struct{}{"first": make(chan struct{}), "second": make(chan struct{})},
	}
	type completedInvocation struct {
		prefix string
		result AgentInvocationResult
		err    error
	}
	done := make(chan completedInvocation, 2)
	wrapper := func(tag string) func(io.Writer) io.Writer {
		return func(writer io.Writer) io.Writer {
			return &taggingWriter{tag: tag, writer: writer}
		}
	}

	invoke := func(input AgentInvocation) {
		result, err := InvokeAgent(&input, runner, &mockLogger{})
		done <- completedInvocation{prefix: input.Prefix, result: result, err: err}
	}
	go invoke(AgentInvocation{
		Context: firstContext, Adapter: &invocationTestAdapter{}, Args: []string{"first"},
		Env: []string{"VALUE=first"}, Workdir: "/first", Prefix: "first",
		StdoutWrapper: wrapper("first:"), StderrWrapper: wrapper("first-err:"),
		InvocationContext: cli.ContextAutonomousHeadless, CLI: "test",
	})
	go invoke(AgentInvocation{
		Context: secondContext, Adapter: &invocationTestAdapter{}, Args: []string{"second"},
		Env: []string{"VALUE=second"}, Workdir: "/second", Prefix: "second",
		StdoutWrapper: wrapper("second:"), StderrWrapper: wrapper("second-err:"),
		InvocationContext: cli.ContextAutonomousHeadless, CLI: "test",
	})

	byPrefix := make(map[string]AgentProcessOptions)
	for range 2 {
		options := <-runner.started
		byPrefix[options.Prefix] = options
	}
	if diff := cmp.Diff([]string{"VALUE=first"}, byPrefix["first"].Env); diff != "" {
		t.Fatalf("first environment mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"VALUE=second"}, byPrefix["second"].Env); diff != "" {
		t.Fatalf("second environment mismatch (-want +got):\n%s", diff)
	}
	if byPrefix["first"].Context != firstContext || byPrefix["second"].Context != secondContext {
		t.Fatal("invocation contexts crossed between overlapping calls")
	}
	for prefix, want := range map[string]string{"first": "first:x", "second": "second:x"} {
		var output bytes.Buffer
		_, _ = byPrefix[prefix].StdoutWrapper(&output).Write([]byte("x"))
		if output.String() != want {
			t.Fatalf("%s output wrapper produced %q, want %q", prefix, output.String(), want)
		}
	}

	cancelFirst()
	close(runner.release["second"])
	completed := map[string]completedInvocation{}
	for range 2 {
		result := <-done
		completed[result.prefix] = result
	}
	if completed["first"].err != context.Canceled || completed["first"].result.CLILaunched != true {
		t.Fatalf("first completion = %#v", completed["first"])
	}
	if completed["second"].err != nil || completed["second"].result.Response != "filtered:second output" {
		t.Fatalf("second completion = %#v", completed["second"])
	}
}

type taggingWriter struct {
	tag    string
	writer io.Writer
}

func (w *taggingWriter) Write(data []byte) (int, error) {
	if _, err := io.WriteString(w.writer, w.tag); err != nil {
		return 0, err
	}
	return w.writer.Write(data)
}

func float64Pointer(value float64) *float64 { return &value }

func TestBuildAgentEnvironmentRemovesAndOverridesByName(t *testing.T) {
	base := []string{"KEEP=one", "DROP=old", "OVERRIDE=old"}
	got := BuildAgentEnvironment(base, []string{"DROP"}, []string{"OVERRIDE=new", "ADD=value"})
	want := []string{"KEEP=one", "OVERRIDE=new", "ADD=value"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("BuildAgentEnvironment() mismatch (-want +got):\n%s", diff)
	}
}

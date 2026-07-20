package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/agentcall"
	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/control"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
)

type callTestAdapter struct {
	mu         sync.Mutex
	inputs     []cli.BuildArgsInput
	discovered string
	probeErr   error
}

func (a *callTestAdapter) BuildArgs(input *cli.BuildArgsInput) []string {
	a.mu.Lock()
	a.inputs = append(a.inputs, *input)
	a.mu.Unlock()
	return []string{"test-agent"}
}
func (a *callTestAdapter) DiscoverSessionID(*cli.DiscoverOptions) string { return a.discovered }
func (a *callTestAdapter) SupportsSystemPrompt() bool                    { return true }
func (a *callTestAdapter) ProbeModel(string, string) (cli.ProbeStrength, error) {
	return cli.Verified, a.probeErr
}
func (a *callTestAdapter) FilterOutput(raw string) string { return "filtered:" + raw }

type callTestRunner struct {
	mu      sync.Mutex
	calls   int
	started chan AgentProcessOptions
	release chan struct{}
	result  ProcessResult
	err     error
}

func (r *callTestRunner) RunShell(string, bool, string) (ProcessResult, error) {
	return ProcessResult{}, nil
}
func (r *callTestRunner) RunScript(string, []byte, bool, string) (ProcessResult, error) {
	return ProcessResult{}, nil
}
func (r *callTestRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	if r.started != nil {
		r.started <- *options
	}
	if r.release != nil {
		select {
		case <-options.Context.Done():
			return ProcessResult{Started: true, ExitCode: -1}, options.Context.Err()
		case <-r.release:
		}
	}
	return r.result, r.err
}

func TestAgentCallHandlerRejectsBeforeAcceptance(t *testing.T) {
	temp := t.TempDir()
	tests := []struct {
		name    string
		payload string
		mutate  func(*AgentCallHandlerOptions)
		code    string
	}{
		{name: "missing prompt", payload: `{"agent":"implementor"}`, code: agentcall.CodeInvalidRequest},
		{name: "multiple targets", payload: `{"prompt":"x","agent":"implementor","session":"named"}`, code: agentcall.CodeInvalidTarget},
		{name: "unknown field mode", payload: `{"prompt":"x","agent":"implementor","mode":"interactive"}`, code: agentcall.CodeInvalidRequest},
		{name: "unknown profile", payload: `{"prompt":"x","agent":"missing"}`, code: agentcall.CodeUnknownAgent},
		{name: "undeclared session", payload: `{"prompt":"x","session":"missing"}`, code: agentcall.CodeUnknownSession},
		{name: "reserved session", payload: `{"prompt":"x","session":"resume"}`, code: agentcall.CodeInvalidSession},
		{name: "invalid cli", payload: `{"prompt":"x","agent":"implementor","cli":"missing"}`, code: agentcall.CodeInvalidCLI},
		{name: "invalid model", payload: `{"prompt":"x","agent":"implementor","model":"bad"}`, mutate: func(o *AgentCallHandlerOptions) {
			o.Adapter = func(string) (cli.Adapter, error) { return &callTestAdapter{probeErr: errors.New("bad model")}, nil }
		}, code: agentcall.CodeInvalidModel},
		{name: "invalid workdir", payload: `{"prompt":"x","agent":"implementor","workdir":"missing"}`, code: agentcall.CodeInvalidWorkdir},
		{name: "workdir outside parent worktree", payload: `{"prompt":"x","agent":"implementor","workdir":".."}`, code: agentcall.CodeInvalidWorkdir},
		{name: "named cli override", payload: `{"prompt":"x","session":"named","cli":"codex"}`, code: agentcall.CodeInvalidRequest},
		{name: "self session", payload: `{"prompt":"x","session":"named"}`, mutate: func(o *AgentCallHandlerOptions) { o.Context.NamedSessions["named"] = "parent-session" }, code: agentcall.CodeSelfSession},
		{name: "self session before CLI ID discovery", payload: `{"prompt":"x","session":"named"}`, mutate: func(o *AgentCallHandlerOptions) {
			o.Parent.SessionID = ""
			o.Parent.NamedSession = "named"
		}, code: agentcall.CodeSelfSession},
		{name: "ineligible", payload: `{"prompt":"x","agent":"implementor"}`, mutate: func(o *AgentCallHandlerOptions) { o.Eligible = false }, code: agentcall.CodeIneligible},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &callTestRunner{}
			accepted := 0
			options := testAgentCallOptions(temp, runner, &callTestAdapter{})
			options.OnAccepted = func(AgentCallAccepted) { accepted++ }
			if tt.mutate != nil {
				tt.mutate(options)
			}
			handler := NewAgentCallHandler(options)
			response := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "request", Payload: json.RawMessage(tt.payload)}))
			if response.Error == nil || response.Error.Code != tt.code {
				t.Fatalf("response = %#v, want error code %q", response, tt.code)
			}
			if runner.calls != 0 || accepted != 0 {
				t.Fatalf("pre-acceptance rejection launched=%d accepted=%d", runner.calls, accepted)
			}
		})
	}
}

func TestAgentCallHandlerAuditsPreAcceptanceRejection(t *testing.T) {
	options := testAgentCallOptions(t.TempDir(), &callTestRunner{}, &callTestAdapter{})
	logger := &recordingAuditLogger{}
	options.Context.AuditLogger = logger
	handler := NewAgentCallHandler(options)
	handler.HandleAgentCall(context.Background(), control.AgentCallRequest{
		AttemptID: "attempt", RequestID: "bad", Payload: json.RawMessage(`{"prompt":"x","agent":"missing"}`),
	})
	event := findAuditEvent(logger.events, audit.EventControlRejected)
	if event == nil || event.Data["request_id"] != "bad" || event.Data["error_code"] != agentcall.CodeUnknownAgent {
		t.Fatalf("control rejection event = %#v", event)
	}
}

func TestAgentCallHandlerRejectsLateDiscoveredParentSessionIdentity(t *testing.T) {
	options := testAgentCallOptions(t.TempDir(), &callTestRunner{}, &callTestAdapter{})
	options.Context.NamedSessions["named"] = "late-parent-session"
	options.Parent.SessionID = ""
	options.Parent.ResolveSessionID = func() string { return "late-parent-session" }
	handler := NewAgentCallHandler(options)

	response := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{
		RequestID: "self", Payload: json.RawMessage(`{"prompt":"x","session":"named"}`),
	}))
	if response.Error == nil || response.Error.Code != agentcall.CodeSelfSession {
		t.Fatalf("response = %#v, want %q", response, agentcall.CodeSelfSession)
	}
}

func TestAgentCallHandlerRejectsWorkdirSymlinkOutsideParentWorktree(t *testing.T) {
	worktree := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(worktree, "outside-link")); err != nil {
		t.Fatal(err)
	}
	options := testAgentCallOptions(worktree, &callTestRunner{}, &callTestAdapter{})
	handler := NewAgentCallHandler(options)

	response := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{
		RequestID: "outside", Payload: json.RawMessage(`{"prompt":"x","agent":"implementor","workdir":"outside-link"}`),
	}))
	if response.Error == nil || response.Error.Code != agentcall.CodeInvalidWorkdir {
		t.Fatalf("response = %#v, want %q", response, agentcall.CodeInvalidWorkdir)
	}
}

func TestAgentCallHandlerRunsFreshProfileAutonomousHeadless(t *testing.T) {
	workdir := t.TempDir()
	childWorkdir := filepath.Join(workdir, "child")
	if err := os.Mkdir(childWorkdir, 0o700); err != nil {
		t.Fatal(err)
	}
	childWorkdir, err := filepath.EvalSymlinks(childWorkdir)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &callTestAdapter{discovered: "fresh-session"}
	runner := &callTestRunner{result: ProcessResult{Started: true, Stdout: "raw"}}
	options := testAgentCallOptions(workdir, runner, adapter)
	options.Context.EngineRef = "engine enrichment must be ignored"
	options.Context.LastSessionStepID = "prior"
	acceptedBeforeLaunch := false
	options.OnAccepted = func(AgentCallAccepted) { acceptedBeforeLaunch = runner.calls == 0 }
	handler := NewAgentCallHandler(options)

	payload, _ := json.Marshal(agentcall.Request{
		Prompt: "child task", Agent: stringPointer("implementor"), CLI: stringPointer("test"),
		Model: stringPointer("override-model"), Workdir: stringPointer("child"),
	})
	response := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{
		RequestID: "request", Payload: payload,
	}))
	if response.Error != nil || response.Result == nil || response.Result.Response != "filtered:raw" {
		t.Fatalf("response = %#v", response)
	}
	if response.Result.Target != (agentcall.Target{Kind: agentcall.TargetAgent, Name: "implementor"}) {
		t.Fatalf("target = %#v", response.Result.Target)
	}
	if !acceptedBeforeLaunch || response.CallID == "" {
		t.Fatalf("acceptance boundary not observed before launch: %#v", response)
	}
	input := adapter.inputs[0]
	if input.Context != cli.ContextAutonomousHeadless || input.Resume || input.Workdir != childWorkdir || input.Model != "override-model" {
		t.Fatalf("adapter input = %#v", input)
	}
	if !strings.Contains(input.Prompt, "system rules") || !strings.Contains(input.Prompt, "child task") || strings.Contains(input.Prompt, "engine enrichment") {
		t.Fatalf("child prompt = %q", input.Prompt)
	}
	if input.CompletionCommand != nil {
		t.Fatalf("called child received Runner completion integration: %#v", input.CompletionCommand)
	}
	if input.RunnerIntegration != nil {
		t.Fatalf("called child received agent-call integration: %#v", input.RunnerIntegration)
	}
	if len(options.Context.NamedSessions) != 0 || options.Context.LastSessionStepID != "prior" {
		t.Fatalf("fresh call changed workflow session bookkeeping: named=%v last=%q", options.Context.NamedSessions, options.Context.LastSessionStepID)
	}
}

func TestAgentCallHandlerResolvesRelativeWorkdirFromParentEffectiveDirectory(t *testing.T) {
	worktree := t.TempDir()
	parentWorkdir := filepath.Join(worktree, "apps")
	childWorkdir := filepath.Join(parentWorkdir, "frontend")
	if err := os.MkdirAll(childWorkdir, 0o700); err != nil {
		t.Fatal(err)
	}
	childWorkdir, err := filepath.EvalSymlinks(childWorkdir)
	if err != nil {
		t.Fatal(err)
	}
	runner := &callTestRunner{result: ProcessResult{Started: true, Stdout: "done"}}
	adapter := &callTestAdapter{}
	options := testAgentCallOptions(worktree, runner, adapter)
	options.Parent.Workdir = parentWorkdir
	handler := NewAgentCallHandler(options)

	response := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{
		RequestID: "relative", Payload: json.RawMessage(`{"prompt":"x","agent":"implementor","workdir":"frontend"}`),
	}))
	if response.Error != nil {
		t.Fatalf("response = %#v", response)
	}
	if got := adapter.inputs[0].Workdir; got != childWorkdir {
		t.Fatalf("child workdir = %q, want %q", got, childWorkdir)
	}
}

func TestPrepareAgentCallRuntimeUsesEstablishedProjectRoot(t *testing.T) {
	workspace := t.TempDir()
	projectRoot := filepath.Join(workspace, "repo")
	workingDir := filepath.Join(projectRoot, "packages", "api")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	projectRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	workingDir, err = filepath.EvalSymlinks(workingDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := model.NewRootContext(&model.RootContextOptions{
		WorkflowFile: "workflow.yaml", ProjectRoot: projectRoot, WorkingDir: workingDir,
	})
	step := &model.Step{ID: "parent", Session: model.SessionNew}

	handler, _, _, err := prepareAgentCallRuntime(
		true, cli.ContextInteractive, step, ctx, &callTestAdapter{}, nil, nil,
		"test", "parent-session", "parent", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if handler.options.Parent.Worktree != projectRoot || handler.options.Parent.Workdir != workingDir {
		t.Fatalf("parent paths = worktree %q workdir %q", handler.options.Parent.Worktree, handler.options.Parent.Workdir)
	}
}

func TestBuildStepInvocationCarriesPreInterpolationAgentCallEligibility(t *testing.T) {
	ctx := model.NewRootContext(&model.RootContextOptions{SessionDir: t.TempDir()})
	profile := &config.ResolvedAgent{CLI: "test"}
	for _, tt := range []struct {
		name            string
		authoredPrompt  string
		interpolated    string
		wantIntegration bool
	}{
		{name: "authored token enables", authoredPrompt: "Use call_agent for {{task}}", interpolated: "Use delegated tool for work", wantIntegration: true},
		{name: "interpolated token does not enable", authoredPrompt: "Do {{task}}", interpolated: "Do call_agent now", wantIntegration: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &callTestAdapter{}
			step := &model.Step{ID: "parent", Prompt: tt.authoredPrompt, Agent: "implementor", Mode: model.ModeAutonomous}
			if _, _, _, err := buildStepInvocation(step, ctx, profile, adapter, tt.interpolated, "engine call_agent", "", false, cli.ContextAutonomousHeadless); err != nil {
				t.Fatal(err)
			}
			got := adapter.inputs[0].RunnerIntegration != nil
			if got != tt.wantIntegration {
				t.Fatalf("RunnerIntegration present = %v, want %v", got, tt.wantIntegration)
			}
		})
	}
}

func TestAgentCallHandlerSharesAndFlushesNamedSessions(t *testing.T) {
	workdir := t.TempDir()
	adapter := &callTestAdapter{discovered: "discovered-session"}
	runner := &callTestRunner{result: ProcessResult{Started: true, Stdout: "one"}}
	options := testAgentCallOptions(workdir, runner, adapter)
	flushes := 0
	options.Context.FlushState = func() { flushes++ }
	handler := NewAgentCallHandler(options)

	first := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{
		RequestID: "first", Payload: json.RawMessage(`{"prompt":"first","session":"named","model":"override"}`),
	}))
	if first.Error != nil || options.Context.NamedSessions["named"] != "discovered-session" || flushes == 0 {
		t.Fatalf("first response=%#v sessions=%v flushes=%d", first, options.Context.NamedSessions, flushes)
	}
	if options.Context.NamedSessionDecls["named"] != "implementor" {
		t.Fatalf("named declaration changed: %v", options.Context.NamedSessionDecls)
	}

	adapter.discovered = "discovered-session"
	runner.result.Stdout = "two"
	second := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{
		RequestID: "second", Payload: json.RawMessage(`{"prompt":"second","session":"named"}`),
	}))
	if second.Error != nil {
		t.Fatalf("second response = %#v", second)
	}
	if len(adapter.inputs) != 2 || adapter.inputs[0].Resume || !adapter.inputs[1].Resume || adapter.inputs[1].SessionID != "discovered-session" {
		t.Fatalf("adapter inputs = %#v", adapter.inputs)
	}
	if adapter.inputs[0].Model != "override" || adapter.inputs[1].Model != "base-model" {
		t.Fatalf("model overrides leaked or were not applied: %#v", adapter.inputs)
	}
}

func TestAgentCallHandlerCachesAcceptedLaunchFailure(t *testing.T) {
	adapter := &callTestAdapter{}
	runner := &callTestRunner{err: errors.New("launch failed")}
	handler := NewAgentCallHandler(testAgentCallOptions(t.TempDir(), runner, adapter))
	request := control.AgentCallRequest{RequestID: "same", Payload: json.RawMessage(`{"prompt":"x","agent":"implementor"}`)}
	first := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), request))
	second := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), request))
	if first.Error == nil || first.Error.Code != agentcall.CodeExecutionFailed || second.Error == nil || first.CallID != second.CallID {
		t.Fatalf("responses = %#v %#v", first, second)
	}
	if runner.calls != 1 {
		t.Fatalf("launch attempts = %d, want 1", runner.calls)
	}
}

func TestAgentCallHandlerRejectsDistinctConcurrentCallAndReusesSlot(t *testing.T) {
	runner := &callTestRunner{started: make(chan AgentProcessOptions, 2), release: make(chan struct{}), result: ProcessResult{Started: true, Stdout: "done"}}
	handler := NewAgentCallHandler(testAgentCallOptions(t.TempDir(), runner, &callTestAdapter{}))
	firstDone := make(chan agentcall.Response, 1)
	go func() {
		firstDone <- decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "first", Payload: json.RawMessage(`{"prompt":"x","agent":"implementor"}`)}))
	}()
	<-runner.started
	second := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "second", Payload: json.RawMessage(`{"prompt":"y","agent":"implementor"}`)}))
	if second.Error == nil || second.Error.Code != agentcall.CodeCallInProgress || !strings.Contains(second.Error.Message, "agent:implementor") || !strings.Contains(second.Error.Message, "serial") {
		t.Fatalf("concurrent response = %#v", second)
	}
	close(runner.release)
	first := <-firstDone
	if first.Error != nil {
		t.Fatalf("first response = %#v", first)
	}
	third := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "third", Payload: json.RawMessage(`{"prompt":"z","agent":"implementor"}`)}))
	if third.Error != nil || third.CallID == first.CallID {
		t.Fatalf("later response = %#v", third)
	}
}

func TestAgentCallHandlerDuplicateWaitsForSameEventualResult(t *testing.T) {
	runner := &callTestRunner{started: make(chan AgentProcessOptions, 1), release: make(chan struct{}), result: ProcessResult{Started: true, Stdout: "done"}}
	handler := NewAgentCallHandler(testAgentCallOptions(t.TempDir(), runner, &callTestAdapter{}))
	request := control.AgentCallRequest{RequestID: "same", Payload: json.RawMessage(`{"prompt":"x","agent":"implementor"}`)}
	results := make(chan agentcall.Response, 2)
	go func() { results <- decodeCallResponse(t, handler.HandleAgentCall(context.Background(), request)) }()
	<-runner.started
	go func() { results <- decodeCallResponse(t, handler.HandleAgentCall(context.Background(), request)) }()
	time.Sleep(10 * time.Millisecond)
	if runner.calls != 1 {
		t.Fatalf("duplicate launched %d children", runner.calls)
	}
	close(runner.release)
	first, second := <-results, <-results
	if first.CallID == "" || first.CallID != second.CallID || first.Result == nil || second.Result == nil {
		t.Fatalf("eventual results = %#v %#v", first, second)
	}
}

func TestAgentCallHandlerCancellationIsCachedAndReleasesSlot(t *testing.T) {
	runner := &callTestRunner{started: make(chan AgentProcessOptions, 2), release: make(chan struct{})}
	handler := NewAgentCallHandler(testAgentCallOptions(t.TempDir(), runner, &callTestAdapter{}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan agentcall.Response, 1)
	request := control.AgentCallRequest{RequestID: "cancel-me", Payload: json.RawMessage(`{"prompt":"x","agent":"implementor"}`)}
	go func() { done <- decodeCallResponse(t, handler.HandleAgentCall(ctx, request)) }()
	options := <-runner.started
	cancel()
	first := <-done
	if first.Error == nil || first.Error.Code != agentcall.CodeCallCanceled || options.Context.Err() == nil {
		t.Fatalf("canceled response=%#v child context err=%v", first, options.Context.Err())
	}
	retry := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), request))
	if retry.Error == nil || retry.Error.Code != agentcall.CodeCallCanceled || retry.CallID != first.CallID || runner.calls != 1 {
		t.Fatalf("cached retry=%#v calls=%d", retry, runner.calls)
	}
	close(runner.release)
	later := decodeCallResponse(t, handler.HandleAgentCall(context.Background(), control.AgentCallRequest{RequestID: "later", Payload: json.RawMessage(`{"prompt":"y","agent":"implementor"}`)}))
	if later.Error != nil {
		t.Fatalf("later response = %#v", later)
	}
}

func TestExecuteAgentStepEnablesAuthenticatedCallsOnlyFromAuthoredPrompt(t *testing.T) {
	runDir := t.TempDir()
	if activePID, err := runlock.Acquire(runDir); err != nil || activePID != 0 {
		t.Fatalf("acquire run lock: active=%d err=%v", activePID, err)
	}
	ctx := model.NewRootContext(&model.RootContextOptions{
		WorkflowFile: "workflow.yaml", SessionDir: runDir,
		ProjectRoot: runDir, WorkingDir: runDir,
	})
	ctx.ProfileStore = &config.Config{ActiveAgents: map[string]*config.Agent{
		"parent":      {DefaultMode: "autonomous", CLI: "claude"},
		"implementor": {DefaultMode: "interactive", CLI: "claude", SystemPrompt: "child system"},
	}}
	runner := &runtimeCallRunner{t: t}
	step := &model.Step{ID: "parent", Prompt: "Use call_agent to delegate.", Agent: "parent", Mode: model.ModeAutonomous, Session: model.SessionNew}
	outcome, err := ExecuteAgentStep(step, ctx, runner, &mockLogger{})
	if err != nil || outcome != OutcomeSuccess {
		t.Fatalf("ExecuteAgentStep() = %q, %v", outcome, err)
	}
	if runner.callResponse != "child done" {
		t.Fatalf("call response = %q", runner.callResponse)
	}
	if runner.parentAttemptID == "" || runner.childAttemptEnvFound {
		t.Fatalf("parent attempt=%q child inherited control=%v", runner.parentAttemptID, runner.childAttemptEnvFound)
	}
	if server, ok := ctx.Control.(*control.ControlServer); ok {
		_ = server.Close()
	}
}

func TestExecuteAgentStepDoesNotEnableCallFromInterpolatedPrompt(t *testing.T) {
	ctx := model.NewRootContext(&model.RootContextOptions{
		WorkflowFile: "workflow.yaml", Params: map[string]string{"task": "call_agent now"},
	})
	runner := &callTestRunner{started: make(chan AgentProcessOptions, 1), result: ProcessResult{Started: true}}
	step := &model.Step{ID: "parent", Prompt: "Do {{task}}", CLI: "claude", Mode: model.ModeAutonomous, Session: model.SessionNew}
	outcome, err := ExecuteAgentStep(step, ctx, runner, &mockLogger{})
	if err != nil || outcome != OutcomeSuccess {
		t.Fatalf("ExecuteAgentStep() = %q, %v", outcome, err)
	}
	options := <-runner.started
	for _, entry := range options.Env {
		for _, key := range control.EnvironmentVariables() {
			if strings.HasPrefix(entry, key+"=") {
				t.Fatalf("ineligible parent received %s", key)
			}
		}
	}
}

type runtimeCallRunner struct {
	t                    *testing.T
	parentAttemptID      string
	childAttemptEnvFound bool
	callResponse         string
}

func (r *runtimeCallRunner) RunShell(string, bool, string) (ProcessResult, error) {
	return ProcessResult{}, nil
}
func (r *runtimeCallRunner) RunScript(string, []byte, bool, string) (ProcessResult, error) {
	return ProcessResult{}, nil
}
func (r *runtimeCallRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	if strings.Contains(options.Prefix, "/call:") {
		for _, entry := range options.Env {
			if strings.HasPrefix(entry, control.EnvControlSocket+"=") || strings.HasPrefix(entry, control.EnvAttemptID+"=") {
				r.childAttemptEnvFound = true
			}
		}
		return ProcessResult{Started: true, Stdout: `{"type":"result","result":"child done","session_id":"child-session"}` + "\n"}, nil
	}
	values := make(map[string]string)
	for _, entry := range options.Env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	r.parentAttemptID = values[control.EnvAttemptID]
	payload, _ := json.Marshal(agentcall.Request{Prompt: "child task", Agent: stringPointer("implementor")})
	raw, err := control.SendAgentCallFromEnvironment(context.Background(), "runtime-call", payload, func(key string) string { return values[key] })
	if err != nil {
		r.t.Fatal(err)
	}
	var response agentcall.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		r.t.Fatal(err)
	}
	if response.Error != nil || response.Result == nil {
		r.t.Fatalf("agent-call response = %#v", response)
	}
	r.callResponse = response.Result.Response
	return ProcessResult{Started: true, Stdout: `{"type":"result","result":"parent done","session_id":"parent-session"}` + "\n"}, nil
}

func stringPointer(value string) *string { return &value }

func testAgentCallOptions(workdir string, runner ProcessRunner, adapter cli.Adapter) *AgentCallHandlerOptions {
	ctx := model.NewRootContext(&model.RootContextOptions{WorkflowFile: "workflow.yaml", SessionDir: workdir})
	ctx.NamedSessionDecls["named"] = "implementor"
	ctx.ProfileStore = &config.Config{ActiveAgents: map[string]*config.Agent{
		"implementor": {DefaultMode: "interactive", CLI: "test", Model: "base-model", Effort: "high", SystemPrompt: "system rules"},
	}}
	ids := 0
	return &AgentCallHandlerOptions{
		Context: ctx, Runner: runner, Eligible: true,
		Parent: AgentCallParent{CLI: "test", SessionID: "parent-session", Worktree: workdir, Workdir: workdir, Prefix: "parent"},
		Adapter: func(name string) (cli.Adapter, error) {
			if name != "test" {
				return nil, errors.New("unknown adapter")
			}
			return adapter, nil
		},
		NewID: func() string { ids++; return fmt.Sprintf("call-%d", ids) },
		Now:   time.Now,
	}
}

func decodeCallResponse(t *testing.T, raw json.RawMessage) agentcall.Response {
	t.Helper()
	var response agentcall.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

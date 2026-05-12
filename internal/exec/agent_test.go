package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/pty"
)

type recordingAuditLogger struct {
	events []audit.Event
}

func (l *recordingAuditLogger) Emit(event audit.Event) {
	l.events = append(l.events, event)
}

func findAuditEvent(events []audit.Event, typ audit.EventType) *audit.Event {
	for i := range events {
		if events[i].Type == typ {
			return &events[i]
		}
	}
	return nil
}

func TestExecuteAgentStep(t *testing.T) {
	t.Run("returns success for exit code 0", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do something", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
	})

	t.Run("returns failed for non-zero exit code", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do something", Session: model.SessionNew}
		outcome, _ := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
	})

	t.Run("returns failed for empty prompt", func(t *testing.T) {
		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "", Session: model.SessionNew}
		outcome, _ := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
	})

	t.Run("builds correct claude args for headless mode", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "implement feature", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(runner.calls) == 0 {
			t.Fatal("expected command to be called")
		}
		args := runner.calls[0]
		if args[0] != "claude" {
			t.Fatal("expected 'claude' as first arg")
		}
		// Should have -p flag for headless
		if !containsArg(args, "-p") {
			t.Fatal("expected -p flag for headless mode")
		}
		// Last arg should contain the prompt (with headless preamble prepended)
		lastArg := args[len(args)-1]
		if !strings.Contains(lastArg, "implement feature") {
			t.Fatalf("expected prompt in last arg, got %q", lastArg)
		}
	})

	t.Run("fresh claude step uses --session-id with generated UUID", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew}
		ctx := makeCtx()
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		args := runner.calls[0]
		if !containsArg(args, "--session-id") {
			t.Fatalf("expected --session-id for fresh claude step, got %v", args)
		}
		// Should store session ID in context
		if ctx.SessionIDs["s"] == "" {
			t.Fatal("expected session ID to be stored for fresh claude step")
		}
	})

	t.Run("persists session ID before CLI invocation so kill mid-step is resumable", func(t *testing.T) {
		// Regression for the bug where a workflow runner killed mid-agent-step
		// lost the session ID because it was only written after the CLI exited.
		// The session ID must be flushed to state BEFORE the CLI process runs.
		var (
			sessionIDAtSpawn string
			flushed          bool
		)
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew}
		ctx := makeCtx()
		ctx.FlushState = func() {
			flushed = true
			sessionIDAtSpawn = ctx.SessionIDs[step.ID]
		}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if !flushed {
			t.Fatal("expected FlushState to be called before the CLI runs")
		}
		if sessionIDAtSpawn == "" {
			t.Fatal("expected session ID to be populated at flush time")
		}
		if sessionIDAtSpawn != ctx.SessionIDs["s"] {
			t.Fatalf("expected pre-spawn session ID %q to equal final %q", sessionIDAtSpawn, ctx.SessionIDs["s"])
		}
	})

	t.Run("session:new step reuses persisted session ID on workflow resume", func(t *testing.T) {
		// Regression: when a session:new step aborts mid-flight, its session ID
		// is persisted in state. On --resume, the step is re-entered, but
		// because step.Session == SessionNew the runner used to generate a
		// fresh UUID — orphaning the original session and overwriting the
		// persisted ID. Resume must reuse the existing ID instead.
		const persistedID = "persisted-session-xyz"
		withFakeClaudeSession(t, persistedID)

		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := makeCtx()
		ctx.WorkflowResumed = true
		ctx.SessionIDs["plan"] = persistedID
		ctx.LastSessionStepID = "plan"
		ctx.SessionProfiles["plan"] = "planner"
		step := model.Step{ID: "plan", Mode: model.ModeHeadless, Prompt: "keep planning", Session: model.SessionNew}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})

		if ctx.SessionIDs["plan"] != persistedID {
			t.Fatalf("expected persisted session ID to be preserved, got %q (was %q)", ctx.SessionIDs["plan"], persistedID)
		}

		args := runner.calls[0]
		foundResume := false
		for i, a := range args {
			if a == "--resume" && i+1 < len(args) && args[i+1] == persistedID {
				foundResume = true
			}
		}
		if !foundResume {
			t.Fatalf("expected --resume %s to reuse persisted session, got %v", persistedID, args)
		}
		for _, a := range args {
			if a == "--session-id" {
				t.Fatalf("did not expect --session-id (would create a fresh session), got %v", args)
			}
		}
	})

	t.Run("session:new step re-establishes persisted ID when transcript missing", func(t *testing.T) {
		// Edge case: if the prior run crashed after persisting the session ID
		// but before Claude wrote its transcript, --resume would fail because
		// no session exists on disk. The runner must pass the persisted ID
		// via --session-id so the CLI recreates the session with the same
		// deterministic UUID rather than generating a new one.
		withFakeClaudeHome(t) // home exists, but no transcript file
		const persistedID = "persisted-never-established"

		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := makeCtx()
		ctx.WorkflowResumed = true
		ctx.SessionIDs["plan"] = persistedID
		ctx.LastSessionStepID = "plan"
		step := model.Step{ID: "plan", Mode: model.ModeHeadless, Prompt: "start", Session: model.SessionNew}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})

		if ctx.SessionIDs["plan"] != persistedID {
			t.Fatalf("expected persisted session ID to be preserved, got %q (was %q)", ctx.SessionIDs["plan"], persistedID)
		}

		args := runner.calls[0]
		foundSessionID := false
		for i, a := range args {
			if a == "--session-id" && i+1 < len(args) && args[i+1] == persistedID {
				foundSessionID = true
			}
			if a == "--resume" {
				t.Fatalf("did not expect --resume for a never-established session, got %v", args)
			}
		}
		if !foundSessionID {
			t.Fatalf("expected --session-id %s to re-establish with persisted ID, got %v", persistedID, args)
		}
	})

	t.Run("named session re-establishes persisted ID when transcript missing on same step", func(t *testing.T) {
		withFakeClaudeHome(t) // home exists, but no transcript file
		const persistedID = "persisted-named-never-established"
		const sessionName = "validator-setup-session"

		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := makeCtx()
		ctx.WorkflowResumed = true
		ctx.NamedSessions[sessionName] = persistedID
		ctx.NamedSessionDecls[sessionName] = "planner"
		ctx.SessionIDs["setup"] = persistedID
		ctx.LastSessionStepID = "setup"
		step := model.Step{ID: "setup", Mode: model.ModeHeadless, Prompt: "start", Session: model.SessionStrategy(sessionName)}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})

		if ctx.NamedSessions[sessionName] != persistedID {
			t.Fatalf("expected named session ID to be preserved, got %q (was %q)", ctx.NamedSessions[sessionName], persistedID)
		}

		args := runner.calls[0]
		foundSessionID := false
		for i, a := range args {
			if a == "--session-id" && i+1 < len(args) && args[i+1] == persistedID {
				foundSessionID = true
			}
			if a == "--resume" {
				t.Fatalf("did not expect --resume for a never-established named session, got %v", args)
			}
		}
		if !foundSessionID {
			t.Fatalf("expected --session-id %s to re-establish named session with persisted ID, got %v", persistedID, args)
		}
	})

	t.Run("persists resumed session ID before CLI invocation", func(t *testing.T) {
		// When resuming, the session ID is known at spawn (carried in from
		// prior state); it must be re-flushed so mid-step kills preserve it.
		var flushedID string
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s2", Mode: model.ModeHeadless, Prompt: "continue", Session: model.SessionResume}
		ctx := makeCtx()
		ctx.SessionIDs["prev"] = "session-abc"
		ctx.LastSessionStepID = "prev"
		ctx.FlushState = func() {
			flushedID = ctx.SessionIDs[step.ID]
		}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if flushedID != "session-abc" {
			t.Fatalf("expected pre-spawn flush to record resumed session ID, got %q", flushedID)
		}
	})

	t.Run("headless resume uses --resume flag", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := makeCtx()
		ctx.SessionIDs["prev"] = "session-abc"
		ctx.LastSessionStepID = "prev"
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "continue", Session: model.SessionResume}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		args := runner.calls[0]
		// Headless resume uses --resume because --session-id is rejected by
		// Claude CLI when the UUID already exists on disk.
		foundResume := false
		for i, a := range args {
			if a == "--resume" && i+1 < len(args) && args[i+1] == "session-abc" {
				foundResume = true
			}
		}
		if !foundResume {
			t.Fatalf("expected --resume session-abc, got %v", args)
		}
		for _, a := range args {
			if a == "--session-id" {
				t.Fatalf("expected no --session-id for headless resume, got %v", args)
			}
		}
	})

	t.Run("resume step propagates session profile from originating step", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := makeCtx()
		ctx.SessionIDs["proposal"] = "session-abc"
		ctx.SessionProfiles["proposal"] = "planner"
		ctx.LastSessionStepID = "proposal"
		step := model.Step{ID: "specs", Mode: model.ModeHeadless, Prompt: "write specs", Session: model.SessionResume}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		// After the resume step runs and discovers a session, it should
		// propagate the profile so that a subsequent workflow resume can
		// resolve the profile for "specs".
		if ctx.SessionProfiles["specs"] != "planner" {
			t.Fatalf("expected profile 'planner' propagated to 'specs', got %q", ctx.SessionProfiles["specs"])
		}
		if ctx.LastSessionStepID != "specs" {
			t.Fatalf("expected LastSessionStepID to be 'specs', got %q", ctx.LastSessionStepID)
		}
	})

	t.Run("step_start audit model uses resolved profile default", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		auditLog := &recordingAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = auditLog
		ctx.ProfileStore = &config.Config{
			ActiveAgents: map[string]*config.Agent{
				"implementor": {
					DefaultMode: "headless",
					CLI:         "claude",
					Model:       "sonnet",
				},
			},
		}
		step := model.Step{ID: "implement", Agent: "implementor", Prompt: "do it", Session: model.SessionNew}

		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})

		event := findAuditEvent(auditLog.events, audit.EventStepStart)
		if event == nil {
			t.Fatal("expected step_start audit event")
		}
		if got := event.Data["model"]; got != "sonnet" {
			t.Fatalf("step_start model = %#v, want %q", got, "sonnet")
		}
	})

	t.Run("step_start audit model for resumed session uses originating profile", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		auditLog := &recordingAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = auditLog
		ctx.ProfileStore = &config.Config{
			ActiveAgents: map[string]*config.Agent{
				"planner": {
					DefaultMode: "headless",
					CLI:         "claude",
					Model:       "opus",
				},
			},
		}
		ctx.SessionIDs["proposal"] = "session-abc"
		ctx.SessionProfiles["proposal"] = "planner"
		ctx.LastSessionStepID = "proposal"
		step := model.Step{ID: "specs", Prompt: "continue", Session: model.SessionResume}

		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})

		event := findAuditEvent(auditLog.events, audit.EventStepStart)
		if event == nil {
			t.Fatal("expected step_start audit event")
		}
		if got := event.Data["model"]; got != "opus" {
			t.Fatalf("step_start model = %#v, want %q", got, "opus")
		}
	})

	t.Run("step_start audit model uses step override when present", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		auditLog := &recordingAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = auditLog
		ctx.ProfileStore = &config.Config{
			ActiveAgents: map[string]*config.Agent{
				"implementor": {
					DefaultMode: "headless",
					CLI:         "claude",
					Model:       "sonnet",
				},
			},
		}
		step := model.Step{
			ID:      "implement",
			Agent:   "implementor",
			Model:   "opus",
			Prompt:  "do it",
			Session: model.SessionNew,
		}

		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})

		event := findAuditEvent(auditLog.events, audit.EventStepStart)
		if event == nil {
			t.Fatal("expected step_start audit event")
		}
		if got := event.Data["model"]; got != "opus" {
			t.Fatalf("step_start model = %#v, want %q", got, "opus")
		}
	})

	t.Run("adds --model flag for model override", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew, Model: "opus"}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := runner.calls[0]
		foundModel := false
		for i, a := range args {
			if a == "--model" && i+1 < len(args) && args[i+1] == "opus" {
				foundModel = true
			}
		}
		if !foundModel {
			t.Fatalf("expected --model opus, got %v", args)
		}
	})

	t.Run("interpolates prompt with params", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := model.NewRootContext(&model.RootContextOptions{
			Params:       map[string]string{"task": "build"},
			WorkflowFile: "test.yaml",
		})
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "Do {{task}}", Session: model.SessionNew}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		args := runner.calls[0]
		lastArg := args[len(args)-1]
		if !strings.Contains(lastArg, "Do build") {
			t.Fatalf("expected interpolated prompt, got %q", lastArg)
		}
	})

	t.Run("handles undefined variable gracefully", func(t *testing.T) {
		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "{{missing}}", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
	})

	t.Run("defaults to claude adapter", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(runner.calls) == 0 {
			t.Fatal("expected command to be called")
		}
		if runner.calls[0][0] != "claude" {
			t.Fatalf("expected 'claude' as agent command, got %q", runner.calls[0][0])
		}
	})

	t.Run("uses codex adapter when cli is codex", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew, CLI: "codex"}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(runner.calls) == 0 {
			t.Fatal("expected command to be called")
		}
		if runner.calls[0][0] != "codex" {
			t.Fatalf("expected 'codex' as agent command, got %q", runner.calls[0][0])
		}
	})

	t.Run("no -p flag for interactive mode", func(t *testing.T) {
		var ptyCalls [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			ptyCalls = append(ptyCalls, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(ptyCalls) == 0 {
			t.Fatal("expected PTY to be called")
		}
		if containsArg(ptyCalls[0], "-p") {
			t.Fatal("did not expect -p flag for interactive mode")
		}
	})

	t.Run("codex headless uses exec subcommand", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew, CLI: "codex"}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := runner.calls[0]
		if !containsArg(args, "exec") {
			t.Fatalf("expected 'exec' subcommand for codex headless, got %v", args)
		}
	})

	t.Run("codex interactive uses --no-alt-screen", func(t *testing.T) {
		var ptyCalls [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			ptyCalls = append(ptyCalls, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review", Session: model.SessionNew, CLI: "codex"}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(ptyCalls) == 0 {
			t.Fatal("expected PTY to be called")
		}
		if !containsArg(ptyCalls[0], "--no-alt-screen") {
			t.Fatalf("expected --no-alt-screen for codex interactive, got %v", ptyCalls[0])
		}
	})

	t.Run("codex model uses -m flag", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew, CLI: "codex", Model: "o3"}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := runner.calls[0]
		foundModel := false
		for i, a := range args {
			if a == "-m" && i+1 < len(args) && args[i+1] == "o3" {
				foundModel = true
			}
		}
		if !foundModel {
			t.Fatalf("expected -m o3 in codex args, got %v", args)
		}
	})

	t.Run("interactive continue trigger returns success", func(t *testing.T) {
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(_ []string, _ pty.Options) (pty.Result, error) {
			return pty.Result{ContinueTriggered: true, ExitCode: 0}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
	})

	t.Run("interactive exit without trigger returns aborted", func(t *testing.T) {
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(_ []string, _ pty.Options) (pty.Result, error) {
			return pty.Result{ContinueTriggered: false, ExitCode: 0}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		log := &mockLogger{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeAborted {
			t.Fatalf("expected aborted, got %q", outcome)
		}
		foundResume := false
		for _, line := range log.lines {
			if strings.Contains(line, "agent-runner --resume") {
				foundResume = true
			}
		}
		if !foundResume {
			t.Fatal("expected resume message in log output")
		}
	})

	t.Run("captures stdout on headless step with capture", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "review-output"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "review", Session: model.SessionNew, Capture: "review_result"}
		outcome, err := ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
		if ctx.CapturedVariables["review_result"].Str != "review-output" {
			t.Fatalf("expected captured output, got %q", ctx.CapturedVariables["review_result"].Str)
		}
	})

	t.Run("strips trailing newlines from captured agent output", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "path/to/tasks/*.md\n"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "find tasks", Session: model.SessionNew, Capture: "tasks_glob"}
		outcome, err := ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
		if ctx.CapturedVariables["tasks_glob"].Str != "path/to/tasks/*.md" {
			t.Fatalf("expected trailing newline stripped, got %q", ctx.CapturedVariables["tasks_glob"].Str)
		}
	})

	t.Run("preserves multiple trailing newlines except final one", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "value\n\n"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "get output", Session: model.SessionNew, Capture: "result"}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if ctx.CapturedVariables["result"].Str != "value\n" {
			t.Fatalf("expected one trailing newline preserved, got %q", ctx.CapturedVariables["result"].Str)
		}
	})

	t.Run("preserves leading whitespace in captured agent output", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "  indented output\n"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "get output", Session: model.SessionNew, Capture: "result"}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if ctx.CapturedVariables["result"].Str != "  indented output" {
			t.Fatalf("expected leading whitespace preserved, got %q", ctx.CapturedVariables["result"].Str)
		}
	})

	t.Run("captures stdout on failed headless step with capture", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1, Stdout: "review-failures"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "review", Session: model.SessionNew, Capture: "review_result"}
		outcome, err := ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
		if ctx.CapturedVariables["review_result"].Str != "review-failures" {
			t.Fatalf("expected captured output on failure, got %q", ctx.CapturedVariables["review_result"].Str)
		}
	})

	t.Run("capture with OutputFilter adapter extracts filtered text", func(t *testing.T) {
		streamJSON := `{"type":"system","subtype":"init","session_id":"abc","model":"composer-1.5","cwd":"/tmp"}` + "\n" +
			`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]},"session_id":"abc"}` + "\n" +
			`{"type":"result","subtype":"success","result":"filtered response","session_id":"abc","is_error":false}` + "\n"
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: streamJSON}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "echo test", Session: model.SessionNew, CLI: "cursor", Capture: "result"}
		outcome, err := ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
		if ctx.CapturedVariables["result"].Str != "filtered response" {
			t.Fatalf("expected filtered response, got %q", ctx.CapturedVariables["result"].Str)
		}
	})

	t.Run("codex headless rollout recording error after completed turn succeeds", func(t *testing.T) {
		stdout := `{"type":"thread.started","thread_id":"019dc6a3-68a4-7751-8c3a-43c3c84a24ba"}` + "\n" +
			`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"codex headless smoke ok."}}` + "\n" +
			`{"type":"turn.completed","usage":{"input_tokens":2521}}` + "\n"
		stderr := "2026-04-25T21:54:58.585861Z ERROR codex_core::session: failed to record rollout items: thread 019dc6a3-68a4-7751-8c3a-43c3c84a24ba not found"
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1, Stdout: stdout, Stderr: stderr}}}
		auditLogger := &recordingAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = auditLogger
		cfg := &config.Config{ActiveAgents: map[string]*config.Agent{
			"codex-test": {CLI: "codex", DefaultMode: "headless"},
		}}
		ctx.ProfileStore = cfg
		step := model.Step{ID: "codex-headless", Agent: "codex-test", Prompt: "reply", Session: model.SessionNew, Capture: "result"}
		outcome, err := ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success, got %q", outcome)
		}
		if ctx.CapturedVariables["result"].Str != "codex headless smoke ok." {
			t.Fatalf("expected filtered codex output captured, got %q", ctx.CapturedVariables["result"].Str)
		}
		end := findAuditEvent(auditLogger.events, audit.EventStepEnd)
		if end == nil {
			t.Fatal("expected step_end audit event")
		}
		if got, _ := end.Data["stdout"].(string); got != "codex headless smoke ok." {
			t.Fatalf("expected filtered stdout in audit, got %q", got)
		}
		if _, ok := end.Data["stderr"]; ok {
			t.Fatalf("expected rollout stderr to be filtered from audit, got %v", end.Data["stderr"])
		}
	})

	t.Run("codex headless turn failure surfaces JSON error and hides diagnostics", func(t *testing.T) {
		stdout := `{"type":"thread.started","thread_id":"019dc6bc-d6c4-7a13-bd75-6aab9fd8b457"}` + "\n" +
			`{"type":"turn.started"}` + "\n" +
			`{"type":"error","message":"You've hit your usage limit."}` + "\n" +
			`{"type":"turn.failed","error":{"message":"You've hit your usage limit."}}` + "\n"
		stderr := "Reading additional input from stdin...\n2026-04-25T22:22:40.578939Z ERROR codex_core::session: failed to record rollout items: thread 019dc6bc-d6c4-7a13-bd75-6aab9fd8b457 not found"
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 1, Stdout: stdout, Stderr: stderr}}}
		auditLogger := &recordingAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = auditLogger
		cfg := &config.Config{ActiveAgents: map[string]*config.Agent{
			"codex-test": {CLI: "codex", DefaultMode: "headless"},
		}}
		ctx.ProfileStore = cfg
		step := model.Step{ID: "codex-headless", Agent: "codex-test", Prompt: "reply", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed, got %q", outcome)
		}
		end := findAuditEvent(auditLogger.events, audit.EventStepEnd)
		if end == nil {
			t.Fatal("expected step_end audit event")
		}
		if got, _ := end.Data["stdout"].(string); got != "You've hit your usage limit." {
			t.Fatalf("expected filtered codex error in audit stdout, got %q", got)
		}
		if _, ok := end.Data["stderr"]; ok {
			t.Fatalf("expected ignored stderr diagnostics to be filtered from audit, got %v", end.Data["stderr"])
		}
	})

	t.Run("does not capture on headless step without capture field", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "some-output"}}}
		ctx := makeCtx()
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if _, ok := ctx.CapturedVariables["output"]; ok {
			t.Fatal("expected no captured variable when capture field is empty")
		}
	})

	t.Run("headless fails when AskUserQuestion error detected in stderr", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stderr: "Tool error: AskUserQuestion error: not supported in headless mode"}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "finalize", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed for AskUserQuestion in headless, got %q", outcome)
		}
	})

	t.Run("headless fails on case-variant AskUserQuestion error in stderr", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stderr: "Error: askuserquestion not available"}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "finalize", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed for case-insensitive AskUserQuestion detection, got %q", outcome)
		}
	})

	t.Run("headless succeeds when output mentions AskUserQuestion without error", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "I considered using AskUserQuestion but proceeded instead"}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success when AskUserQuestion mentioned without error, got %q", outcome)
		}
	})

	t.Run("headless succeeds when stdout mentions AskUserQuestion — only stderr is checked", func(t *testing.T) {
		stdout := "uses `--no-ask-user` when `AskUserQuestion` is disallowed\n`CopilotAdapter.InteractiveModeError()`: rejects interactive mode"
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: stdout}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "implement", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success when AskUserQuestion only appears in stdout, got %q", outcome)
		}
	})

	t.Run("headless succeeds when natural language mentions AskUserQuestion and error on same line", func(t *testing.T) {
		stdout := "I attempted to call AskUserQuestion, but the request encountered an error during validation, so I proceeded autonomously."
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: stdout}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "implement", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected success when natural language mentions both AskUserQuestion and error, got %q", outcome)
		}
	})

	t.Run("headless fails when AskUserQuestion tool is disallowed", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stderr: "tool AskUserQuestion is not allowed"}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "finalize", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed when AskUserQuestion is disallowed, got %q", outcome)
		}
	})

	t.Run("interactive does not call RunAgent on ProcessRunner", func(t *testing.T) {
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(_ []string, _ pty.Options) (pty.Result, error) {
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(runner.calls) != 0 {
			t.Fatalf("expected no RunAgent calls for interactive step, got %d", len(runner.calls))
		}
	})

	t.Run("interactive claude routes prompt to system prompt", func(t *testing.T) {
		var ptyCalls [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			ptyCalls = append(ptyCalls, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review code", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(ptyCalls) == 0 {
			t.Fatal("expected PTY to be called")
		}
		args := ptyCalls[0]
		if !containsArg(args, "--append-system-prompt") {
			t.Fatal("expected --append-system-prompt for interactive claude step")
		}
		lastArg := args[len(args)-1]
		if lastArg != "Let's start the s step" {
			t.Fatalf("expected 'Let's start the s step' as positional arg, got %q", lastArg)
		}
	})

	t.Run("interactive codex without enrichment passes prompt positionally", func(t *testing.T) {
		var ptyCalls [][]string
		var ptyOpts []pty.Options
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, opts pty.Options) (pty.Result, error) {
			ptyCalls = append(ptyCalls, args)
			ptyOpts = append(ptyOpts, opts)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review code", Session: model.SessionNew, CLI: "codex"}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(ptyCalls) == 0 {
			t.Fatal("expected PTY to be called")
		}
		args := ptyCalls[0]
		lastArg := args[len(args)-1]
		if !strings.Contains(lastArg, "review code") {
			t.Fatalf("expected prompt in positional arg for codex without enrichment, got %q", lastArg)
		}
		if len(ptyOpts) == 0 {
			t.Fatal("expected PTY options to be captured")
		}
		assertContinueMarkerInstruction(t, lastArg, ptyOpts[0].ContinueMarker)
	})

	t.Run("headless mode passes prompt as positional arg without wrapping", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "implement feature", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := runner.calls[0]
		lastArg := args[len(args)-1]
		if !strings.Contains(lastArg, "implement feature") {
			t.Fatalf("expected prompt in headless positional arg, got %q", lastArg)
		}
		if containsArg(args, "--append-system-prompt") {
			t.Fatalf("did not expect --append-system-prompt for headless mode, got %v", args)
		}
	})

	t.Run("headless codex passes prompt without XML wrapping", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "implement feature", Session: model.SessionNew, CLI: "codex"}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := runner.calls[0]
		lastArg := args[len(args)-1]
		if !strings.Contains(lastArg, "implement feature") {
			t.Fatalf("expected prompt in headless codex positional arg, got %q", lastArg)
		}
		if strings.Contains(lastArg, "<system>") {
			t.Fatalf("did not expect XML wrapping for headless mode, got %q", lastArg)
		}
	})

	t.Run("interactive step prompt includes completion instruction", func(t *testing.T) {
		var capturedArgs [][]string
		var capturedOpts []pty.Options
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, opts pty.Options) (pty.Result, error) {
			capturedArgs = append(capturedArgs, args)
			capturedOpts = append(capturedOpts, opts)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "do the task", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(capturedArgs) == 0 {
			t.Fatal("expected PTY to be called")
		}
		if len(capturedOpts) == 0 {
			t.Fatal("expected PTY options to be captured")
		}
		// For Claude interactive, the completion instruction goes into --append-system-prompt.
		args := capturedArgs[0]
		for i, a := range args {
			if a != "--append-system-prompt" || i+1 >= len(args) {
				continue
			}
			sysPrompt := args[i+1]
			assertContinueMarkerInstruction(t, sysPrompt, capturedOpts[0].ContinueMarker)
			if !strings.Contains(sysPrompt, "you or the user") {
				t.Fatalf("expected 'you or the user' wording in completion instruction, got %q", sysPrompt)
			}
			return
		}
		t.Fatalf("expected --append-system-prompt with completion instruction, got %v", args)
	})

	t.Run("headless prompt includes autonomy preamble", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do the task", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		lastArg := runner.calls[0][len(runner.calls[0])-1]
		if !strings.Contains(lastArg, "autonomously in headless mode") {
			t.Fatalf("expected headless preamble in prompt, got %q", lastArg)
		}
	})

	t.Run("headless resume prompt omits autonomy preamble", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := makeCtx()
		ctx.SessionIDs["prev"] = "session-abc"
		ctx.LastSessionStepID = "prev"
		step := model.Step{
			ID:      "s",
			Mode:    model.ModeHeadless,
			CLI:     "cursor",
			Prompt:  "what value did I just send you?",
			Session: model.SessionResume,
		}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		lastArg := runner.calls[0][len(runner.calls[0])-1]
		if strings.Contains(lastArg, "autonomously in headless mode") {
			t.Fatalf("did not expect headless preamble on resumed prompt, got %q", lastArg)
		}
		if lastArg != step.Prompt {
			t.Fatalf("expected resumed prompt %q, got %q", step.Prompt, lastArg)
		}
	})

	t.Run("interactive prompt does not include autonomy preamble", func(t *testing.T) {
		var ptyCalls [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			ptyCalls = append(ptyCalls, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review code", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := ptyCalls[0]
		for _, a := range args {
			if strings.Contains(a, "autonomously in headless mode") {
				t.Fatalf("did not expect headless preamble in interactive prompt, got %q", a)
			}
		}
	})

	t.Run("headless claude includes --disallowedTools AskUserQuestion", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := runner.calls[0]
		foundDisallowed := false
		for i, a := range args {
			if a == "--disallowedTools" && i+1 < len(args) && args[i+1] == "AskUserQuestion" {
				foundDisallowed = true
			}
		}
		if !foundDisallowed {
			t.Fatalf("expected --disallowedTools AskUserQuestion for headless claude, got %v", args)
		}
	})

	t.Run("headless resume includes --disallowedTools", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := makeCtx()
		ctx.SessionIDs["prev"] = "session-abc"
		ctx.LastSessionStepID = "prev"
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "continue", Session: model.SessionResume}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		args := runner.calls[0]
		foundDisallowed := false
		for i, a := range args {
			if a == "--disallowedTools" && i+1 < len(args) && args[i+1] == "AskUserQuestion" {
				foundDisallowed = true
			}
		}
		if !foundDisallowed {
			t.Fatalf("expected --disallowedTools AskUserQuestion on headless resume, got %v", args)
		}
	})

	t.Run("interactive claude does not include --disallowedTools", func(t *testing.T) {
		var ptyCalls [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			ptyCalls = append(ptyCalls, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		for _, a := range ptyCalls[0] {
			if a == "--disallowedTools" {
				t.Fatalf("did not expect --disallowedTools for interactive mode, got %v", ptyCalls[0])
			}
		}
	})

	t.Run("headless codex includes sandbox bypass", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew, CLI: "codex"}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := runner.calls[0]
		foundBypass := false
		for _, a := range args {
			if a == "--dangerously-bypass-approvals-and-sandbox" {
				foundBypass = true
			}
		}
		if !foundBypass {
			t.Fatalf("expected sandbox bypass for headless codex, got %v", args)
		}
	})

	t.Run("interactive codex does not include permission bypass flags", func(t *testing.T) {
		var ptyCalls [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			ptyCalls = append(ptyCalls, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review", Session: model.SessionNew, CLI: "codex"}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		for i, a := range ptyCalls[0] {
			if a == "-a" && i+1 < len(ptyCalls[0]) && ptyCalls[0][i+1] == "never" {
				t.Fatalf("did not expect -a never for interactive codex, got %v", ptyCalls[0])
			}
			if a == "--dangerously-bypass-approvals-and-sandbox" {
				t.Fatalf("did not expect sandbox bypass for interactive codex, got %v", ptyCalls[0])
			}
		}
	})

	t.Run("headless step prompt does not include completion instruction", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do the task", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(runner.calls) == 0 {
			t.Fatal("expected command to be called")
		}
		lastArg := runner.calls[0][len(runner.calls[0])-1]
		if strings.Contains(lastArg, "signal-continuation") || strings.Contains(lastArg, "AGENT_RUNNER_") {
			t.Fatalf("expected no completion instruction in headless prompt, got %q", lastArg)
		}
	})

	t.Run("interactive step includes step prefix with step ID", func(t *testing.T) {
		var capturedArgs [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			capturedArgs = append(capturedArgs, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "my-step", Mode: model.ModeInteractive, Prompt: "do the task", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := capturedArgs[0]
		for i, a := range args {
			if a == "--append-system-prompt" && i+1 < len(args) {
				sysPrompt := args[i+1]
				if !strings.Contains(sysPrompt, "my-step") {
					t.Fatalf("expected step ID in prefix, got %q", sysPrompt)
				}
				if !strings.Contains(sysPrompt, "announce that you are starting") {
					t.Fatalf("expected announcement instruction in prefix, got %q", sysPrompt)
				}
				return
			}
		}
		t.Fatalf("expected --append-system-prompt, got %v", args)
	})

	t.Run("fresh interactive step includes workflow name and description", func(t *testing.T) {
		var capturedArgs [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			capturedArgs = append(capturedArgs, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "specs", Mode: model.ModeInteractive, Prompt: "write specs", Session: model.SessionNew}
		ctx := makeCtx()
		ctx.WorkflowName = "plan-change"
		ctx.WorkflowDescription = "Plan a change"
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		args := capturedArgs[0]
		for i, a := range args {
			if a == "--append-system-prompt" && i+1 < len(args) {
				sysPrompt := args[i+1]
				if !strings.Contains(sysPrompt, "plan-change") {
					t.Fatalf("expected workflow name in prefix, got %q", sysPrompt)
				}
				if !strings.Contains(sysPrompt, "Plan a change") {
					t.Fatalf("expected workflow description in prefix, got %q", sysPrompt)
				}
				return
			}
		}
		t.Fatalf("expected --append-system-prompt, got %v", args)
	})

	t.Run("session resume step does not include workflow description", func(t *testing.T) {
		var capturedArgs [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			capturedArgs = append(capturedArgs, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "specs", Mode: model.ModeInteractive, Prompt: "write specs", Session: model.SessionResume}
		ctx := makeCtx()
		ctx.WorkflowName = "plan-change"
		ctx.WorkflowDescription = "Plan a change"
		ctx.SessionIDs["specs"] = "existing-session"
		ctx.LastSessionStepID = "specs"
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		args := capturedArgs[0]
		for i, a := range args {
			if a == "--append-system-prompt" && i+1 < len(args) {
				sysPrompt := args[i+1]
				if strings.Contains(sysPrompt, "Plan a change") {
					t.Fatalf("expected no workflow description in resumed step prefix, got %q", sysPrompt)
				}
				if !strings.Contains(sysPrompt, "specs") {
					t.Fatalf("expected step ID in resumed prefix, got %q", sysPrompt)
				}
				return
			}
		}
		t.Fatalf("expected --append-system-prompt, got %v", args)
	})

	t.Run("copilot step in interactive mode spawns CLI", func(t *testing.T) {
		var ptyCalls [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			ptyCalls = append(ptyCalls, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", CLI: "copilot", Mode: model.ModeInteractive, Prompt: "do something", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected OutcomeSuccess for copilot interactive step, got %q", outcome)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("expected no CLI invocations, got %d", len(runner.calls))
		}
		if len(ptyCalls) != 1 {
			t.Fatalf("expected one PTY invocation, got %d", len(ptyCalls))
		}
		args := ptyCalls[0]
		if args[0] != "copilot" {
			t.Fatalf("expected copilot command, got %v", args)
		}
		if !containsArg(args, "-i") {
			t.Fatalf("expected -i for copilot interactive prompt, got %v", args)
		}
		for _, disallowed := range []string{"--allow-all", "--autopilot", "-s", "-p", "--no-ask-user"} {
			if containsArg(args, disallowed) {
				t.Fatalf("did not expect %s in copilot interactive args, got %v", disallowed, args)
			}
		}
	})

	t.Run("cursor step in interactive mode spawns CLI", func(t *testing.T) {
		var ptyCalls [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			ptyCalls = append(ptyCalls, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		auditLog := &recordingAuditLogger{}
		ctx := makeCtx()
		ctx.AuditLogger = auditLog
		step := model.Step{ID: "s", CLI: "cursor", Mode: model.ModeInteractive, Prompt: "do something", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeSuccess {
			t.Fatalf("expected OutcomeSuccess for cursor interactive step, got %q", outcome)
		}
		if len(runner.calls) != 0 {
			t.Fatalf("expected no CLI invocations, got %d", len(runner.calls))
		}
		if len(ptyCalls) != 1 {
			t.Fatalf("expected one PTY invocation, got %d", len(ptyCalls))
		}
		args := ptyCalls[0]
		if args[0] != "agent" {
			t.Fatalf("expected cursor agent command, got %v", args)
		}
		for _, disallowed := range []string{"-p", "--output-format", "stream-json", "--trust", "--force"} {
			if containsArg(args, disallowed) {
				t.Fatalf("did not expect %s in cursor interactive args, got %v", disallowed, args)
			}
		}
		end := findAuditEvent(auditLog.events, audit.EventStepEnd)
		if end == nil {
			t.Fatal("expected step end audit event")
		}
		if got := end.Data["outcome"]; got != string(OutcomeSuccess) {
			t.Fatalf("expected successful step end audit event, got %v", end.Data)
		}
	})
}

// withFakeClaudeHome redirects $HOME to a test-scoped temp dir and returns
// the encoded Claude projects directory for the current working directory.
// The caller can then plant transcript files under the returned path to
// simulate the presence or absence of a Claude session on disk.
func withFakeClaudeHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	replacer := strings.NewReplacer("/", "-", ".", "-", "_", "-")
	encoded := replacer.Replace(abs)
	projects := filepath.Join(tmp, ".claude", "projects", encoded)
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatalf("mkdir projects: %v", err)
	}
	return projects
}

// withFakeClaudeSession sets up a fake Claude home and plants an empty
// transcript for sessionID so that ClaudeAdapter.SessionExists reports true.
func withFakeClaudeSession(t *testing.T, sessionID string) {
	t.Helper()
	projects := withFakeClaudeHome(t)
	transcript := filepath.Join(projects, sessionID+".jsonl")
	if err := os.WriteFile(transcript, nil, 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
}

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

func assertContinueMarkerInstruction(t *testing.T, prompt, marker string) {
	t.Helper()
	if marker == "" {
		t.Fatal("expected interactive PTY continue marker")
	}
	if !strings.HasPrefix(marker, continuationMarkerPrefix) {
		t.Fatalf("expected marker prefix %q, got %q", continuationMarkerPrefix, marker)
	}
	suffix := strings.TrimPrefix(marker, continuationMarkerPrefix)
	if suffix == "" {
		t.Fatalf("expected marker suffix, got %q", marker)
	}
	for _, part := range []string{"`AGENT`", "`_RUNNER`", "`_CONTINUE_`", "`" + suffix + "`"} {
		if !strings.Contains(prompt, part) {
			t.Fatalf("expected dynamic text marker instruction part %q in prompt, marker %q prompt %q", part, marker, prompt)
		}
	}
	for _, phrase := range []string{"in this exact order", "no spaces or separators", "must start with `AGENT`", "end with `" + suffix + "`"} {
		if !strings.Contains(prompt, phrase) {
			t.Fatalf("expected marker assembly guidance %q in prompt, got %q", phrase, prompt)
		}
	}
	if strings.Contains(prompt, marker) {
		t.Fatalf("completion instruction must not include the exact marker contiguously, got %q", prompt)
	}
	for _, disallowed := range []string{"signal-continuation", "AGENT_RUNNER_TTY", "printf"} {
		if strings.Contains(prompt, disallowed) {
			t.Fatalf("completion instruction should use hosted-CLI text marker, got %q", prompt)
		}
	}
}

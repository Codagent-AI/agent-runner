package exec

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/pty"
)

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
		// Last arg should be the prompt
		if args[len(args)-1] != "implement feature" {
			t.Fatalf("expected prompt as last arg, got %q", args[len(args)-1])
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

	t.Run("adds --resume flag for resume session", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		ctx := makeCtx()
		ctx.SessionIDs["prev"] = "session-abc"
		ctx.LastSessionStepID = "prev"
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "continue", Session: model.SessionResume}
		ExecuteAgentStep(&step, ctx, runner, &mockLogger{})
		args := runner.calls[0]
		foundResume := false
		for i, a := range args {
			if a == "--resume" && i+1 < len(args) && args[i+1] == "session-abc" {
				foundResume = true
			}
		}
		if !foundResume {
			t.Fatalf("expected --resume session-abc, got %v", args)
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
		if args[len(args)-1] != "Do build" {
			t.Fatalf("expected interpolated prompt, got %q", args[len(args)-1])
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

	t.Run("logs mode", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		log := &mockLogger{}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do it", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, log)
		found := false
		for _, line := range log.lines {
			if strings.Contains(line, "headless") {
				found = true
			}
		}
		if !found {
			t.Fatal("expected mode to be logged")
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
		if len(args) < 2 || args[1] != "exec" {
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
		if ctx.CapturedVariables["review_result"] != "review-output" {
			t.Fatalf("expected captured output, got %q", ctx.CapturedVariables["review_result"])
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
		if ctx.CapturedVariables["review_result"] != "review-failures" {
			t.Fatalf("expected captured output on failure, got %q", ctx.CapturedVariables["review_result"])
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

	t.Run("headless fails when AskUserQuestion error detected in output", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "Tool error: AskUserQuestion error: not supported in headless mode"}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "finalize", Session: model.SessionNew}
		outcome, err := ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if outcome != OutcomeFailed {
			t.Fatalf("expected failed for AskUserQuestion in headless, got %q", outcome)
		}
	})

	t.Run("headless fails on case-variant AskUserQuestion error", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0, Stdout: "Error: askuserquestion not available"}}}
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
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			ptyCalls = append(ptyCalls, args)
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
		// Interactive steps include the completion instruction appended to the prompt.
		if !strings.Contains(lastArg, "red-slippers") {
			t.Fatalf("expected completion instruction in codex interactive prompt, got %q", lastArg)
		}
	})

	t.Run("headless mode passes prompt as positional arg without wrapping", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "implement feature", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := runner.calls[0]
		lastArg := args[len(args)-1]
		if lastArg != "implement feature" {
			t.Fatalf("expected plain prompt for headless, got %q", lastArg)
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
		if lastArg != "implement feature" {
			t.Fatalf("expected plain prompt for headless codex, got %q", lastArg)
		}
		if strings.Contains(lastArg, "<system>") {
			t.Fatalf("did not expect XML wrapping for headless mode, got %q", lastArg)
		}
	})

	t.Run("interactive step prompt includes completion instruction", func(t *testing.T) {
		var capturedArgs [][]string
		oldFn := interactiveRunnerFn
		interactiveRunnerFn = func(args []string, _ pty.Options) (pty.Result, error) {
			capturedArgs = append(capturedArgs, args)
			return pty.Result{ContinueTriggered: true}, nil
		}
		defer func() { interactiveRunnerFn = oldFn }()

		runner := &mockRunner{}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "do the task", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(capturedArgs) == 0 {
			t.Fatal("expected PTY to be called")
		}
		// For Claude interactive, the completion instruction goes into --append-system-prompt.
		args := capturedArgs[0]
		for i, a := range args {
			if a == "--append-system-prompt" && i+1 < len(args) {
				sysPrompt := args[i+1]
				if !strings.Contains(sysPrompt, "red-slippers") {
					t.Fatalf("expected completion instruction in system prompt, got %q", sysPrompt)
				}
				if !strings.Contains(sysPrompt, "you or the user") {
					t.Fatalf("expected 'you or the user' wording in completion instruction, got %q", sysPrompt)
				}
				return
			}
		}
		t.Fatalf("expected --append-system-prompt with completion instruction, got %v", args)
	})

	t.Run("headless step prompt does not include completion instruction", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeHeadless, Prompt: "do the task", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		if len(runner.calls) == 0 {
			t.Fatal("expected command to be called")
		}
		lastArg := runner.calls[0][len(runner.calls[0])-1]
		if strings.Contains(lastArg, "red-slippers") {
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

	t.Run("resumed interactive step does not include workflow description", func(t *testing.T) {
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
}

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

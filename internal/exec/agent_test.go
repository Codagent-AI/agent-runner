package exec

import (
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
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
		found := false
		for _, a := range args {
			if a == "-p" {
				found = true
			}
		}
		if !found {
			t.Fatal("expected -p flag for headless mode")
		}
		// Last arg should be the prompt
		if args[len(args)-1] != "implement feature" {
			t.Fatalf("expected prompt as last arg, got %q", args[len(args)-1])
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
		ctx := model.NewRootContext(model.RootContextOptions{
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

	t.Run("no -p flag for interactive mode", func(t *testing.T) {
		runner := &mockRunner{results: []ProcessResult{{ExitCode: 0}}}
		step := model.Step{ID: "s", Mode: model.ModeInteractive, Prompt: "review", Session: model.SessionNew}
		ExecuteAgentStep(&step, makeCtx(), runner, &mockLogger{})
		args := runner.calls[0]
		for _, a := range args {
			if a == "-p" {
				t.Fatal("did not expect -p flag for interactive mode")
			}
		}
	})
}

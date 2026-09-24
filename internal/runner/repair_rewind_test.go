package runner

import (
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/model"
)

func TestRunWorkflowRerunRepairReplaysTargetAndPasses(t *testing.T) {
	maxAttempts := 1
	runner := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: claudeAgentOutput("opened the PR (draft)")}, // open-draft-pr, first run
		{ExitCode: 1, Stderr: "expected one open PR"},                     // verify-draft-pr, first run: fails
		{ExitCode: 0, Stdout: claudeAgentOutput("opened the PR (ready)")}, // open-draft-pr, replay
		{ExitCode: 0, Stdout: "pr is open"},                               // verify-draft-pr, replay: passes
	}}
	w := model.Workflow{
		Name: "test",
		Steps: []model.Step{
			{ID: "open-draft-pr", Mode: model.ModeAutonomous, Prompt: "open it", Agent: "test-agent", Session: model.SessionNew},
			{
				ID: "verify-draft-pr", Command: "exit 1",
				Repair: &model.Repair{Rerun: "open-draft-pr", Max: &maxAttempts},
			},
		},
	}
	w.ApplyDefaults()
	sessionDir := t.TempDir()
	log := &mockLog{}

	result, err := RunWorkflow(&w, map[string]string{}, &Options{
		ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: log, SessionDir: sessionDir,
		ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"test-agent": {CLI: "claude"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("expected success, got %q", result)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("expected 4 process calls (agent, check, agent replay, check replay), got %d", len(runner.calls))
	}
}

func TestRunWorkflowRerunRepairExhaustsBudget(t *testing.T) {
	maxAttempts := 1
	runner := &mockRunner{results: []exec.ProcessResult{
		{ExitCode: 0, Stdout: claudeAgentOutput("opened the PR (draft)")},
		{ExitCode: 1, Stderr: "expected one open PR"},
		{ExitCode: 0, Stdout: claudeAgentOutput("opened the PR (still draft)")},
		{ExitCode: 1, Stderr: "expected one open PR"},
	}}
	w := model.Workflow{
		Name: "test",
		Steps: []model.Step{
			{ID: "open-draft-pr", Mode: model.ModeAutonomous, Prompt: "open it", Agent: "test-agent", Session: model.SessionNew},
			{
				ID: "verify-draft-pr", Command: "exit 1",
				Repair: &model.Repair{Rerun: "open-draft-pr", Max: &maxAttempts},
			},
		},
	}
	w.ApplyDefaults()
	sessionDir := t.TempDir()
	log := &mockLog{}

	result, err := RunWorkflow(&w, map[string]string{}, &Options{
		ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: log, SessionDir: sessionDir,
		ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{"test-agent": {CLI: "claude"}}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != ResultFailed {
		t.Fatalf("expected failed, got %q", result)
	}
}

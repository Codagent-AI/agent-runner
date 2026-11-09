package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/google/go-cmp/cmp"
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

func TestRunWorkflowToleratedRerunRepairBudget(t *testing.T) {
	for _, tt := range []struct {
		name         string
		warn         bool
		recover      bool
		wantCalls    int
		wantWarnings int
	}{
		{name: "continue_on_failure exhausts budget", wantCalls: 7},
		{name: "warn_on_failure exhausts budget", warn: true, wantCalls: 7, wantWarnings: 1},
		{name: "warn_on_failure recovers within budget", warn: true, recover: true, wantCalls: 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			maxAttempts := 2
			runner := &mockRunner{}
			// Supply more failures than the budget allows so resetting the frame
			// cannot accidentally look like a correctly exhausted repair cycle.
			for i := 0; i < 5; i++ {
				check := exec.ProcessResult{ExitCode: 1, Stderr: "still failing"}
				if tt.recover && i > 0 {
					check = exec.ProcessResult{ExitCode: 0}
				}
				runner.results = append(runner.results, exec.ProcessResult{Stdout: claudeAgentOutput("acted")}, check)
			}
			w := model.Workflow{Name: "test", Steps: []model.Step{
				{ID: "act", Mode: model.ModeAutonomous, Prompt: "act", Agent: "test-agent", Session: model.SessionNew},
				{ID: "check", Command: "exit 1", ContinueOnFailure: !tt.warn, WarnOnFailure: tt.warn,
					Repair: &model.Repair{Rerun: "act", Max: &maxAttempts}},
				shellStep("following", "echo following"),
			}}
			w.ApplyDefaults()
			dir := t.TempDir()
			result, err := RunWorkflow(&w, nil, &Options{
				ProcessRunner: runner, GlobExpander: &mockGlob{}, Log: &mockLog{}, SessionDir: dir,
				ProfileStore: testAgentProfileStore(),
			})
			if err != nil || result != ResultSuccess {
				t.Fatalf("RunWorkflow() = (%q, %v), want success", result, err)
			}
			if len(runner.calls) != tt.wantCalls {
				t.Errorf("process calls = %d, want %d (including following step)", len(runner.calls), tt.wantCalls)
			}
			if diff := cmp.Diff([]string{"sh", "-c", "echo following"}, runner.calls[len(runner.calls)-1]); diff != "" {
				t.Errorf("following step (-want +got):\n%s", diff)
			}
			state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !state.Completed || state.WarningCount != tt.wantWarnings {
				t.Errorf("completed = %v, warnings = %d; want true, %d", state.Completed, state.WarningCount, tt.wantWarnings)
			}
			body, err := os.ReadFile(filepath.Join(dir, "audit.log"))
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(body), `"status":"warning"`); got != tt.wantWarnings {
				t.Errorf("warning events = %d, want %d", got, tt.wantWarnings)
			}
		})
	}
}

func TestToleratedRerunRepairPersistsPendingRewind(t *testing.T) {
	for _, flag := range []string{"continue_on_failure", "warn_on_failure"} {
		t.Run(flag, func(t *testing.T) {
			workflowPath := writeWorkflowFile(t, t.TempDir(), "test-v1.0.yaml", strings.Replace(rerunRepairYAML, "    command: exit 1", "    command: exit 1\n    "+flag+": true", 1))
			w, err := loader.LoadWorkflow(workflowPath, loader.Options{})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			rs, err := initRunState(&w, nil, &Options{
				WorkflowFile: workflowPath, SessionDir: dir, ProfileStore: testAgentProfileStore(),
				ProcessRunner: &mockRunner{results: []exec.ProcessResult{
					{Stdout: claudeAgentOutput("acted")}, {ExitCode: 1, Stderr: "still failing"},
				}}, GlobExpander: &mockGlob{}, Log: &mockLog{},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { rs.auditLogger.Close(); runlock.Delete(dir) })
			for i := range w.Steps {
				_, err := runAndPersistTopLevelStep(rs, &w.Steps[i])
				if err != nil {
					t.Fatal(err)
				}
			}
			if rs.ctx.PendingRewind == nil {
				t.Fatal("expected pending rewind")
			}
			// Inspect the post-step write before AfterStepDispatch consumes the rewind.
			state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			nested := state.CurrentStep.Nested
			if nested == nil {
				t.Fatal("missing nested state")
			}
			if nested.Completed {
				t.Error("pending repair persisted as completed")
			}
			if nested.Repair == nil || nested.Repair.Phase != model.RepairPhaseReplaying {
				t.Fatalf("want replaying repair frame, got %+v", nested.Repair)
			}
			rs.auditLogger.Close()
			runlock.Delete(dir)
			// Resume re-enters the recorded check with the open repair budget.
			resumed := &mockRunner{results: []exec.ProcessResult{{ExitCode: 0}}}
			result, err := ResumeWorkflow(filepath.Join(dir, "state.json"), &Options{
				ProcessRunner: resumed, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: testAgentProfileStore(),
			})
			if err != nil || result != ResultSuccess {
				t.Fatalf("ResumeWorkflow() = (%q, %v), want success", result, err)
			}
			if len(resumed.calls) != 1 {
				t.Fatalf("resumed process calls = %d, want re-entered check", len(resumed.calls))
			}
			if diff := cmp.Diff([]string{"sh", "-c", "exit 1"}, resumed.calls[0]); diff != "" {
				t.Errorf("resumed check (-want +got):\n%s", diff)
			}
		})
	}
}

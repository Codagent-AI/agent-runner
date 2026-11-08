package runview

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runner"
	"github.com/codagent/agent-runner/internal/stateio"
)

// replayEvidenceRunner stubs only the process boundary; the runner, repair
// sequencer, persistence, audit logging, and historical projection are real.
type replayEvidenceRunner struct {
	t             *testing.T
	responses     []string
	checkFailures int
	failMidAt     int
	midCalls      int
	calls         []string
}

func (r *replayEvidenceRunner) RunAgent(_ *exec.AgentProcessOptions) (exec.ProcessResult, error) {
	r.calls = append(r.calls, "agent")
	if len(r.responses) == 0 {
		r.t.Fatal("unexpected agent invocation")
	}
	response := r.responses[0]
	r.responses = r.responses[1:]
	data, err := json.Marshal(map[string]string{"type": "result", "result": response})
	if err != nil {
		r.t.Fatal(err)
	}
	return exec.ProcessResult{Started: true, Stdout: string(data)}, nil
}

func (r *replayEvidenceRunner) RunShell(command string, _ bool, _ string) (exec.ProcessResult, error) {
	r.calls = append(r.calls, command)
	result := exec.ProcessResult{Started: true}
	switch command {
	case "prepare":
		r.midCalls++
		if r.midCalls == r.failMidAt {
			result.ExitCode, result.Stderr = 1, "preparation failed"
		}
	case "verify":
		if r.checkFailures > 0 {
			r.checkFailures--
			result.ExitCode, result.Stderr = 1, "no draft pull request found"
		}
	case "finalize":
	default:
		r.t.Fatalf("unexpected command %q", command)
	}
	return result, nil
}

func (r *replayEvidenceRunner) RunScript(_ string, _ []byte, _ bool, _ string) (exec.ProcessResult, error) {
	r.t.Fatal("unexpected script invocation")
	return exec.ProcessResult{}, nil
}

func TestRepairRunnerTerminalReplayHistoricalEvidenceAndResume(t *testing.T) {
	const blocked = "push rejected: token lacks workflow scope\nREPAIR_BLOCKED"
	for _, tc := range []struct {
		name          string
		responses     []string
		budget        int
		failMidAt     int
		savedStep     string
		priorAttempts int
	}{
		{"target blocks on first replay", []string{"no pr opened", blocked}, 1, 0, "open-draft-pr", 0},
		{"target blocks on later replay", []string{"no pr opened", "still no pr", blocked}, 3, 0, "open-draft-pr", 2},
		{"intermediate step exhausts budget", []string{"no pr opened", "retrying push"}, 1, 2, "prepare-pr", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			projectDir := filepath.Join(base, "project")
			sessionDir := filepath.Join(projectDir, "runs", "terminal-replay")
			workflowPath := filepath.Join(base, "workflows", "terminal-replay-v1.0.yaml")
			workflowYAML := `name: terminal-replay
steps:
  - id: open-draft-pr
    prompt: open the draft pr
    agent: implementor
    mode: autonomous
  - id: prepare-pr
    command: prepare
  - id: verify-draft-pr
    command: verify
    repair:
      rerun: open-draft-pr
      max: BUDGET
  - id: finalize
    command: finalize
`
			writeFile(t, workflowPath, strings.ReplaceAll(workflowYAML, "BUDGET", strconv.Itoa(tc.budget)))
			wf, err := loader.LoadWorkflow(workflowPath, loader.Options{})
			if err != nil {
				t.Fatal(err)
			}
			profiles := &config.Config{ActiveAgents: map[string]*config.Agent{"implementor": {CLI: "claude"}}}
			process := &replayEvidenceRunner{t: t, responses: slices.Clone(tc.responses), checkFailures: 10, failMidAt: tc.failMidAt}
			result, err := runner.RunWorkflow(&wf, nil, &runner.Options{
				WorkflowFile: workflowPath, SessionDir: sessionDir, WorkingDir: base, ProjectRoot: base,
				ProcessRunner: process, ProfileStore: profiles, Log: &runner.DiscardLogger{},
			})
			if err != nil || result != runner.ResultFailed {
				t.Fatalf("RunWorkflow = %q, %v; want failed", result, err)
			}
			state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if state.WorkflowHash == "" || state.CurrentStep.Nested == nil || state.CurrentStep.Nested.Repair == nil {
				t.Fatalf("missing persisted workflow hash or repair frame: %+v", state)
			}
			if nested := state.CurrentStep.Nested; nested.StepID != tc.savedStep || nested.Repair.Phase != model.RepairPhaseFailed {
				t.Errorf("saved position/frame = %s/%+v, want %s/failed", nested.StepID, nested.Repair, tc.savedStep)
			}
			assertEvidence := func(t *testing.T) {
				t.Helper()
				m, err := New(sessionDir, projectDir, FromInspect)
				if err != nil {
					t.Fatal(err)
				}
				check := childByID(m.tree.Root, "verify-draft-pr")
				if check == nil {
					t.Fatalf("check missing: %s", m.loadErr)
				}
				if tc.savedStep == "open-draft-pr" && m.selectedNode() != check {
					t.Errorf("historical selection = %v, want owning check", m.selectedNode())
				}
				m.setSelected(check)
				detail := stripANSI(m.selectedDetailDocument(100).renderCopy())
				wants := []string{"Failure evidence", "no draft pull request found"}
				if tc.savedStep == "open-draft-pr" {
					wants = append(wants, "push rejected: token lacks workflow scope", "REPAIR_BLOCKED")
				}
				for _, want := range wants {
					if !strings.Contains(detail, want) {
						t.Errorf("historical detail missing %q:\n%s", want, detail)
					}
				}
			}
			t.Run("persisted run", assertEvidence)
			t.Run("legacy replaying frame", func(t *testing.T) {
				legacy := state
				nested := *state.CurrentStep.Nested
				frame := *nested.Repair
				frame.Phase, frame.Attempts = model.RepairPhaseReplaying, tc.priorAttempts
				nested.Repair = &frame
				legacy.CurrentStep.Nested = &nested
				if err := stateio.WriteState(&legacy, sessionDir); err != nil {
					t.Fatal(err)
				}
				assertEvidence(t)
				if err := stateio.WriteState(&state, sessionDir); err != nil {
					t.Fatal(err)
				}
			})
			frame := *state.CurrentStep.Nested.Repair
			resolved, err := model.ResolveResumeStep(wf.Steps, state.CurrentStep.Nested.StepID, state.CurrentStep.Nested.Completed, &frame)
			if err != nil || resolved.StepID != "open-draft-pr" || frame.Attempts != 0 {
				t.Errorf("resume resolution = %+v, frame = %+v, error = %v; want target with fresh budget", resolved, frame, err)
			}
			resumed := &replayEvidenceRunner{t: t, responses: []string{"opened now"}}
			result, err = runner.ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &runner.Options{
				ProcessRunner: resumed, ProfileStore: profiles, Log: &runner.DiscardLogger{},
			})
			if err != nil || result != runner.ResultSuccess {
				t.Errorf("ResumeWorkflow = %q, %v; want success", result, err)
			}
			wantCalls := []string{"agent", "prepare", "verify", "finalize"}
			if !slices.Equal(resumed.calls, wantCalls) {
				t.Errorf("resume calls = %v, want %v", resumed.calls, wantCalls)
			}
		})
	}
}

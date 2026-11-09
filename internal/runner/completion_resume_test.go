package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

// INT-005 restores persisted child state through the sub-workflow resume path.
func TestCompletionEarlierLayoutResume(t *testing.T) {
	for _, removed := range []bool{true, false} {
		name := "missing response"
		if removed {
			name = "removed repair target"
		}
		t.Run(name, func(t *testing.T) {
			session := t.TempDir()
			parent := writeWorkflowFile(t, t.TempDir(), "parent-v1.0.yaml", `name: parent
steps:
  - id: implement-child
    workflow: builtin:core/implement-change-v2.0.yaml
    params:
      change_name: tiny
      change_label: change
      change_dir: change
      change_kind: spec-driven
      artifact_validation_instruction: ""
      skip_validator: "true"
`)
			child := &model.NestedStepState{StepID: "implement", Completed: true, CapturedVariables: map[string]model.CapturedValue{"implementation_start_head": model.NewCapturedString("abc")}}
			if removed {
				child.StepID = "check-evidence"
				child.Completed = false
				child.Repair = &model.RepairFrame{CheckID: "check-evidence", Form: "rerun", Target: "complete-evidence", Phase: model.RepairPhaseFailed}
			}
			state := model.RunState{RunID: "old-run", WorkflowFile: parent, WorkflowName: "parent", CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{StepID: "implement-child", Child: child}}}
			path := filepath.Join(session, "state.json")
			if e := stateio.WriteState(&state, session); e != nil {
				t.Fatal(e)
			}
			process := &completionResumeRunner{t: t, mockRunner: mockRunner{results: []exec.ProcessResult{{ExitCode: 0}, {ExitCode: 1, Stderr: "missing completion record"}, {ExitCode: 0}}}}
			result, e := ResumeWorkflow(path, &Options{ProcessRunner: process, GlobExpander: &mockGlob{}, Log: &mockLog{}})
			if result != ResultFailed {
				t.Fatalf("resume result %s err %v", result, e)
			}
			b, readErr := os.ReadFile(filepath.Join(session, "audit.log"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if removed {
				if !strings.Contains(string(b), `step \"complete-evidence\" not found`) {
					t.Fatalf("resume error %v audit %s", e, b)
				}
				if len(process.calls) != 0 {
					t.Fatalf("processes invoked %v", process.calls)
				}
				return
			}
			if !strings.Contains(string(b), "implementor_response") || !strings.Contains(string(b), "check-implementation-complete.sh") {
				t.Fatalf("resume audit %s", b)
			}
			saved, readErr := stateio.ReadState(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if saved.FailureKind != model.FailureStep {
				t.Fatalf("failure kind %s", saved.FailureKind)
			}

		})
	}
}

type completionResumeRunner struct {
	mockRunner
	t *testing.T
}

func (r *completionResumeRunner) RunAgent(_ *exec.AgentProcessOptions) (exec.ProcessResult, error) {
	r.t.Fatal("agent invoked before strict interpolation")
	return exec.ProcessResult{}, nil
}

package exec

import (
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
)

// finalizeRunner scripts the CI wait reports and records the order of the CI
// fix, validation, push, and head check. Gates run for real.
type finalizeRunner struct {
	t       *testing.T
	repo    string
	reports []string
	events  []string
}

func (r *finalizeRunner) RunShell(cmd string, _ bool, _ string) (ProcessResult, error) {
	if strings.Contains(cmd, "VALIDATION-MARK") {
		r.events = append(r.events, "validation")
	}
	return runProcess(osexec.Command("sh", "-c", cmd), r.repo, nil)
}

func (r *finalizeRunner) RunScript(path string, stdin []byte, _ bool, _ string) (ProcessResult, error) {
	name := filepath.Base(path)
	switch name {
	case "ci-wait.sh":
		r.events = append(r.events, "ci-wait")
		report := r.reports[0]
		if len(r.reports) > 1 {
			r.reports = r.reports[1:]
		}
		return ProcessResult{Started: true, Stdout: report}, nil
	case "ci-status-gate.sh", "ci-fix-needed-gate.sh":
		return runProcess(osexec.Command("sh", path), r.repo, stdin)
	case "acceptance-push.sh":
		r.events = append(r.events, "push")
		return ProcessResult{Started: true}, nil
	case "check-draft-pr.sh":
		r.events = append(r.events, "head-check:"+string(stdin))
		return ProcessResult{Started: true}, nil
	}
	r.t.Fatalf("unexpected script %s", name)
	return ProcessResult{}, nil
}

func (r *finalizeRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	if !strings.Contains(options.Prefix, "fix-pr") {
		r.t.Fatalf("unexpected agent step %s", options.Prefix)
	}
	r.events = append(r.events, "fix-pr")
	if err := os.WriteFile(filepath.Join(r.repo, "file.txt"), []byte("ci fix\n"), 0o600); err != nil {
		r.t.Fatal(err)
	}
	gitIn(r.t, r.repo, "commit", "-q", "-am", "ci fix")
	return ProcessResult{Started: true, Stdout: claudeUsageOutput("fixed", 0)}, nil
}

// runCIFixLoop dispatches finalize-pr's ci-fix-loop with the given CI reports
// and a custom validation workflow that records itself and ends as validationExit.
func runCIFixLoop(t *testing.T, reports []string, validationExit string) (outcome StepOutcome, events []string, warnings int) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	const ref = "builtin:core/finalize-pr-v2.0.yaml"
	workflow, err := loader.LoadWorkflow(ref, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var loop *model.Step
	for i := range workflow.Steps {
		if workflow.Steps[i].ID == "ci-fix-loop" {
			loop = &workflow.Steps[i]
		}
	}
	if loop == nil {
		t.Fatal("finalize-pr-v2.0 has no ci-fix-loop")
	}
	repo := newFeatureRepo(t)

	validation := writeCustomValidation(t, "  - id: mark\n    command: |\n      : VALIDATION-MARK\n      printf 'FAIL\\ncheck: x\\n' > \"{{result_file}}\"\n      exit "+validationExit+"\n")
	ctx := model.NewRootContext(&model.RootContextOptions{
		Params:            map[string]string{"ci_fix_cycles": "3", "review_bots": "", "validation_workflow": validation},
		WorkflowFile:      ref,
		SessionDir:        t.TempDir(),
		NamedSessionDecls: map[string]string{"lead-agent": "lead"},
	})
	runner := &finalizeRunner{t: t, repo: repo, reports: reports}
	outcome, err = DispatchStep(loop, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil {
		t.Fatalf("ci-fix-loop: %v (events %v)", err, runner.events)
	}
	return outcome, runner.events, ctx.WarningOrigins.Count()
}

// INT-004: a CI fix is committed, validated, pushed, and head-checked before
// CI is waited on again; a green first run skips the whole group.
func TestFinalizeCIFixValidatesBeforePush(t *testing.T) {
	failed, passed := "CI failed.\n\nCI_FAILED\n", "CI is green.\n\nCI_PASSED\n"
	t.Run("green first time runs no fix, validation, or push", func(t *testing.T) {
		outcome, events, warnings := runCIFixLoop(t, []string{passed}, "0")
		if outcome != OutcomeSuccess {
			t.Fatalf("outcome = %q; events %v", outcome, events)
		}
		if warnings != 0 {
			t.Errorf("warning origins = %d, want none", warnings)
		}
		if diff := cmp.Diff([]string{"ci-wait"}, events); diff != "" {
			t.Errorf("events (-want +got):\n%s", diff)
		}
	})
	t.Run("fix is validated before it is pushed", func(t *testing.T) {
		outcome, events, warnings := runCIFixLoop(t, []string{failed, passed}, "0")
		if outcome != OutcomeSuccess {
			t.Fatalf("outcome = %q; events %v", outcome, events)
		}
		if warnings != 0 {
			t.Errorf("warning origins = %d, want none for a passing validation", warnings)
		}
		want := []string{"ci-wait", "fix-pr", "validation", "push", `head-check:{"accept_ready":"true","attempts":"10","interval_seconds":"3"}`, "ci-wait"}
		if len(events) != len(want) {
			t.Fatalf("events = %v, want %v", events, want)
		}
		for i := range want {
			if i == 4 {
				if !strings.HasPrefix(events[i], "head-check:") || !strings.Contains(events[i], `"accept_ready":"true"`) {
					t.Errorf("event %d = %q, want a ready-accepting head check", i, events[i])
				}
				continue
			}
			if events[i] != want[i] {
				t.Errorf("event %d = %q, want %q (events %v)", i, events[i], want[i], events)
			}
		}
	})
	t.Run("red validation still pushes and CI runs on the fix", func(t *testing.T) {
		outcome, events, warnings := runCIFixLoop(t, []string{failed, passed}, "1")
		if outcome != OutcomeSuccess {
			t.Fatalf("outcome = %q; events %v", outcome, events)
		}
		if warnings != 1 {
			t.Errorf("warning origins = %d, want 1 for the red validation", warnings)
		}
		if countEvents(events, "push") != 1 || countEvents(events, "ci-wait") != 2 || countEvents(events, "validation") != 1 {
			t.Errorf("events = %v, want one validation, one push, two CI waits", events)
		}
	})
}

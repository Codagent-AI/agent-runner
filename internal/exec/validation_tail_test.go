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
	"github.com/codagent/agent-runner/internal/runlock"
)

// tailRunner runs shell and the handoff scripts for real in a temporary
// repository and stubs the push and pull-request check, recording their inputs.
type tailRunner struct {
	t      *testing.T
	repo   string
	events []string
	inputs map[string]string
}

func (r *tailRunner) RunShell(cmd string, _ bool, _ string) (ProcessResult, error) {
	r.events = append(r.events, "shell")
	return runProcess(osexec.Command("sh", "-c", cmd), r.repo, nil)
}

func (r *tailRunner) RunScript(path string, stdin []byte, _ bool, _ string) (ProcessResult, error) {
	name := filepath.Base(path)
	r.events = append(r.events, name)
	r.inputs[name] = string(stdin)
	switch name {
	case "validation-handoff.sh", "record-untested-commits.sh":
		return runProcess(osexec.Command("sh", path), r.repo, stdin)
	case "acceptance-push.sh", "check-draft-pr.sh":
		return ProcessResult{Started: true}, nil
	}
	r.t.Fatalf("unexpected script %s", name)
	return ProcessResult{}, nil
}

func (r *tailRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	if !strings.Contains(options.Prefix, "retest-validation-fixes") {
		r.t.Fatalf("unexpected agent step %s", options.Prefix)
	}
	r.events = append(r.events, "agent:retest-validation-fixes")
	return ProcessResult{Started: true, Stdout: claudeUsageOutput("no behavior change", 0)}, nil
}

// newFeatureRepo creates a repository on branch feature with one commit and
// makes it the working directory, as the shell skip conditions run there.
func newFeatureRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	initGitRepo(t, repo)
	gitIn(t, repo, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "file.txt")
	gitIn(t, repo, "commit", "-q", "-m", "initial")
	t.Chdir(repo)
	return repo
}

func writeCustomValidation(t *testing.T, steps string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "custom-validation-v1.0.yaml")
	body := "name: custom-validation\nparams:\n  - name: repair_session\n    required: false\n  - name: result_file\n    required: false\nsteps:\n" + steps
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type tailRun struct {
	outcome  StepOutcome
	events   []string
	inputs   map[string]string
	handoff  string
	status   string
	evidence string
}

// runValidationTail runs the validation tail of a v2.0 helper, starting at
// startID, against a repository whose tested revision is its initial commit.
func runValidationTail(t *testing.T, ref, startID, validation string) tailRun {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	workflow, err := loader.LoadWorkflow(ref, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	start := -1
	for i := range workflow.Steps {
		if workflow.Steps[i].ID == startID {
			start = i
		}
	}
	if start < 0 {
		t.Fatalf("%s has no step %s", ref, startID)
	}
	repo := newFeatureRepo(t)

	sessionDir := t.TempDir()
	if activePID, err := runlock.Acquire(sessionDir); err != nil || activePID != 0 {
		t.Fatalf("acquire run lock: active=%d err=%v", activePID, err)
	}
	t.Cleanup(func() { runlock.Delete(sessionDir) })
	evidence := filepath.Join(sessionDir, "output")
	if err := os.MkdirAll(evidence, 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(evidence, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("acceptance-tested-revision.txt", gitOut(t, repo, "rev-parse", "HEAD")+"\n")
	write("acceptance-handoff.md", "# Handoff\n\nready\n")
	write("acceptance-preparation-status.txt", "ACCEPTANCE_COMPLETE\n")

	params := map[string]string{"validation_workflow": validation, "change_name": "demo", "change_label": "change", "change_dir": "openspec/changes/demo"}
	for _, p := range workflow.Params {
		if _, ok := params[p.Name]; !ok {
			params[p.Name] = p.Default
		}
	}
	ctx := model.NewRootContext(&model.RootContextOptions{
		Params: params, WorkflowFile: ref, SessionDir: sessionDir, ProjectRoot: repo, WorkingDir: repo,
		NamedSessionDecls: map[string]string{"implementor-agent": "implementor", "lead-agent": "lead", "acceptance-tester": "tester"},
	})
	runner := &tailRunner{t: t, repo: repo, inputs: map[string]string{}}
	tail := model.Step{ID: "tail", Steps: workflow.Steps[start:]}
	outcome, err := DispatchStep(&tail, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil {
		t.Fatalf("tail: %v (events %v)", err, runner.events)
	}
	read := func(name string) string {
		body, _ := os.ReadFile(filepath.Join(evidence, name))
		return string(body)
	}
	return tailRun{outcome: outcome, events: runner.events, inputs: runner.inputs, handoff: read("acceptance-handoff.md"), status: read("acceptance-preparation-status.txt"), evidence: evidence}
}

// INT-002: validation runs after acceptance, its commits are pushed and the
// pull request head is checked (ready accepted), and the handoff records the
// result and the commits made after the tested revision.
func TestVerifyChangeValidationTail(t *testing.T) {
	const ref = "builtin:core/verify-change-v2.0.yaml"
	commitFix := "  - id: fix\n    command: |\n      printf 'fixed\\n' > file.txt\n      git commit -q -am 'validation fix'\n"
	for _, tc := range []struct {
		name, steps string
		wantWarning bool
		wantHandoff []string
	}{
		{"custom success without result file", "  - id: ok\n    command: \"true\"\n", false, []string{"recorded no result", "No commits were made"}},
		{"custom outright failure warns and continues", "  - id: boom\n    command: \"exit 3\"\n", true, []string{"recorded no result"}},
		{"failed validation with fixes", commitFix + "  - id: record\n    command: |\n      printf 'FAIL\\ncheck: lint\\n' > \"{{result_file}}\"\n", false, []string{"unresolved", "check: lint", "validation fix", "not exploratory-tested"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runValidationTail(t, ref, "record-validation-base", writeCustomValidation(t, tc.steps))
			if got.outcome != OutcomeSuccess {
				t.Fatalf("outcome = %q, want success; events %v", got.outcome, got.events)
			}
			var scripts []string
			for _, e := range got.events {
				if strings.HasSuffix(e, ".sh") {
					scripts = append(scripts, e)
				}
			}
			if diff := cmp.Diff([]string{"acceptance-push.sh", "check-draft-pr.sh", "validation-handoff.sh"}, scripts); diff != "" {
				t.Errorf("script order (-want +got):\n%s", diff)
			}
			if !strings.Contains(got.inputs["check-draft-pr.sh"], `"accept_ready":"true"`) {
				t.Errorf("head check inputs %q lack accept_ready", got.inputs["check-draft-pr.sh"])
			}
			if strings.TrimSpace(got.status) != "ACCEPTANCE_COMPLETE" {
				t.Errorf("status = %q, want ACCEPTANCE_COMPLETE", got.status)
			}
			for _, want := range tc.wantHandoff {
				if !strings.Contains(got.handoff, want) {
					t.Errorf("handoff lacks %q:\n%s", want, got.handoff)
				}
			}
		})
	}
}

// INT-001/INT-002: with the built-in none selection nothing runs between the
// base record and the push, and the handoff reports the skip.
func TestVerifyChangeValidationTailWithNone(t *testing.T) {
	got := runValidationTail(t, "builtin:core/verify-change-v2.0.yaml", "record-validation-base", "builtin:core/validation-none-v1.0.yaml")
	if got.outcome != OutcomeSuccess {
		t.Fatalf("outcome = %q; events %v", got.outcome, got.events)
	}
	if !strings.Contains(got.handoff, "Validation was skipped.") {
		t.Errorf("handoff lacks the skip note:\n%s", got.handoff)
	}
}

// INT-003: the second validation point retests only when validation committed,
// and commits made after the tested revision are recorded as untested.
func TestAcceptChangeValidationTail(t *testing.T) {
	const ref = "builtin:core/accept-change-v2.0.yaml"
	t.Run("no validation commits skips the retest and records nothing", func(t *testing.T) {
		got := runValidationTail(t, ref, "record-validation-base", "builtin:core/validation-none-v1.0.yaml")
		if got.outcome != OutcomeSuccess {
			t.Fatalf("outcome = %q; events %v", got.outcome, got.events)
		}
		if countEvents(got.events, "agent:retest-validation-fixes") != 0 {
			t.Errorf("retest ran without validation commits; events %v", got.events)
		}
		if strings.Contains(got.handoff, "not exploratory-tested") {
			t.Errorf("handoff records untested commits for a tested HEAD:\n%s", got.handoff)
		}
	})
	t.Run("validation commits trigger the retest and are recorded", func(t *testing.T) {
		steps := "  - id: fix\n    command: |\n      printf 'fixed\\n' > file.txt\n      git commit -q -am 'validation fix'\n"
		got := runValidationTail(t, ref, "record-validation-base", writeCustomValidation(t, steps))
		if got.outcome != OutcomeSuccess {
			t.Fatalf("outcome = %q; events %v", got.outcome, got.events)
		}
		if countEvents(got.events, "agent:retest-validation-fixes") != 1 {
			t.Errorf("retest count != 1; events %v", got.events)
		}
		for _, want := range []string{"## Commits not exploratory-tested", "validation fix"} {
			if !strings.Contains(got.handoff, want) {
				t.Errorf("handoff lacks %q:\n%s", want, got.handoff)
			}
		}
	})
}

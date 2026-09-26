package exec

import (
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/runlock"
)

const implementTaskRef = "builtin:core/implement-task-v1.0.yaml"

func loadBuiltinWorkflow(t *testing.T, ref string) model.Workflow {
	t.Helper()
	workflow, err := loader.LoadWorkflow(ref, loader.Options{})
	if err != nil {
		t.Fatalf("LoadWorkflow(%s): %v", ref, err)
	}
	return workflow
}

func findStep(steps []model.Step, id string) (step *model.Step, topLevelIndex int) {
	for i := range steps {
		if steps[i].ID == id {
			return &steps[i], i
		}
		if found, _ := findStep(steps[i].Steps, id); found != nil {
			return found, -1
		}
	}
	return nil, -1
}

func requireStep(t *testing.T, workflow *model.Workflow, id string) (step *model.Step, topLevelIndex int) {
	t.Helper()
	step, index := findStep(workflow.Steps, id)
	if step == nil {
		t.Fatalf("step %s not found", id)
	}
	return step, index
}

func requireContains(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s does not contain %q:\n%s", what, want, text)
		}
	}
}

// INT-004: the builtin workflows wire external task delivery together.
func TestImplementTaskExternalDeliveryWiring(t *testing.T) {
	t.Run("implement-task prepares the record and passes it to the gate", func(t *testing.T) {
		workflow := loadBuiltinWorkflow(t, implementTaskRef)
		_, recordStart := requireStep(t, &workflow, "record-task-start")
		prepare, prepareIndex := requireStep(t, &workflow, "prepare-task-delivery")
		_, generate := requireStep(t, &workflow, "generate-code")
		if recordStart >= prepareIndex || prepareIndex >= generate {
			t.Fatalf("step order record-task-start=%d prepare-task-delivery=%d generate-code=%d", recordStart, prepareIndex, generate)
		}
		if prepare.Script != "prepare-task-delivery.sh" || prepare.Capture != "task_delivery" || prepare.CaptureFormat != "json" {
			t.Fatalf("prepare-task-delivery = script %q capture %q format %q", prepare.Script, prepare.Capture, prepare.CaptureFormat)
		}
		wantPrepareInputs := map[string]string{
			"session_dir":   "{{session_dir}}",
			"task_file":     "{{task_file}}",
			"starting_head": "{{task_start_head}}",
		}
		if diff := cmp.Diff(wantPrepareInputs, prepare.ScriptInputs); diff != "" {
			t.Errorf("prepare-task-delivery inputs (-want +got):\n%s", diff)
		}

		verify, _ := requireStep(t, &workflow, "verify-task-commit")
		wantVerifyInputs := map[string]string{
			"starting_head": "{{task_start_head}}",
			"started_at":    "{{task_delivery.started_at}}",
			"record_path":   "{{task_delivery.record_path}}",
		}
		if diff := cmp.Diff(wantVerifyInputs, verify.ScriptInputs); diff != "" {
			t.Errorf("verify-task-commit inputs (-want +got):\n%s", diff)
		}
		if verify.Repair == nil {
			t.Fatal("verify-task-commit has no repair")
		}
		requireContains(t, "verify-task-commit repair prompt", verify.Repair.Prompt,
			"{{task_delivery.record_path}}",
			`"repository"`, `"commits"`,

			"Do not push, create or move branches, commit, or otherwise modify the external repository",
			"do not end with `REPAIR_BLOCKED`",
			"REPAIR_BLOCKED",
			"Never declare success yourself",
			"Do not create an empty or placeholder commit",
		)
	})

	t.Run("fix-violations skips externally directed work", func(t *testing.T) {
		workflow := loadBuiltinWorkflow(t, "builtin:core/run-validator-v1.0.yaml")
		step, _ := requireStep(t, &workflow, "fix-violations")
		requireContains(t, "fix-violations prompt", step.Prompt,
			"directs that work to a different repository",
			"do not re-implement it in this repository",
			`agent-validator update-review skip <#> "delivered in <repository>; see task file"`,
		)
	})

	t.Run("complete-task-index allows external delivery", func(t *testing.T) {
		workflow := loadBuiltinWorkflow(t, "builtin:core/implement-change-v1.0.yaml")
		step, _ := requireStep(t, &workflow, "complete-task-index")
		if strings.Contains(step.Prompt, "including its implementation commit") {
			t.Errorf("complete-task-index still claims every task has a local commit:\n%s", step.Prompt)
		}
		requireContains(t, "complete-task-index prompt", step.Prompt, "a verified delivery in another repository")
	})

	t.Run("open-draft-pr lists external deliveries", func(t *testing.T) {
		workflow := loadBuiltinWorkflow(t, verifyChangeRef)
		step, _ := requireStep(t, &workflow, "open-draft-pr")
		requireContains(t, "open-draft-pr prompt", step.Prompt,
			"{{session_dir}}/output/task-delivery/*.accepted",
			"Delivered in other repositories",
		)
	})
}

// taskDeliveryRunner runs implement-task's shell and script steps for real
// against a run repository and fakes its agents. generate-code can deliver the
// task as a commit in an external clone; the verify-task-commit repair writes
// the external delivery record at the path its prompt names.
type taskDeliveryRunner struct {
	t   *testing.T
	run string
	ext string
	// deliver makes generate-code commit in the external clone, pushing it
	// unless unpushed is set.
	deliver  bool
	unpushed bool
	// writeRecord makes the repair write a record listing the delivered commit.
	writeRecord bool
	// validatorFailures is how many validator runs report a compliance failure.
	validatorFailures int

	events              []string
	delivered           string
	repairRecordPath    string
	fixViolationsPrompt string
	lastGate            ProcessResult
}

var recordPathPattern = regexp.MustCompile("`(/[^`]*/output/task-delivery/[^`]*\\.json)`")

func (r *taskDeliveryRunner) RunShell(cmd string, _ bool, _ string) (ProcessResult, error) {
	return r.exec(osexec.Command("sh", "-c", cmd), nil)
}

func (r *taskDeliveryRunner) RunScript(path string, stdin []byte, _ bool, _ string) (ProcessResult, error) {
	name := filepath.Base(path)
	r.events = append(r.events, name)
	switch name {
	case "run-validator.sh":
		if r.validatorFailures > 0 {
			r.validatorFailures--
			return ProcessResult{Started: true, ExitCode: 1, Stdout: "REVIEW task-compliance: 1. task not implemented\n"}, nil
		}
		return ProcessResult{Started: true, ExitCode: 0, Stdout: "PASS\n"}, nil
	case "verify-task-commit.sh":
		result, err := r.exec(osexec.Command("sh", path), stdin)
		r.lastGate = result
		return result, err
	default:
		return r.exec(osexec.Command("sh", path), stdin)
	}
}

func (r *taskDeliveryRunner) RunAgent(options *AgentProcessOptions) (ProcessResult, error) {
	prompt := strings.Join(options.Args, "\n")
	result := "done"
	switch {
	case strings.Contains(prompt, "Implement the task described in"):
		r.events = append(r.events, "generate-code")
		if r.deliver {
			r.generate()
		}
	case strings.Contains(prompt, "This repository shows no commit for the task"):
		r.events = append(r.events, "repair")
		match := recordPathPattern.FindStringSubmatch(prompt)
		if match == nil {
			r.t.Fatalf("repair prompt names no record path:\n%s", prompt)
		}
		r.repairRecordPath = match[1]
		if r.writeRecord {
			body, err := json.Marshal(map[string]any{
				"repository": r.ext, "commits": []string{r.delivered}, "branch": "feature",
				"pull_request": "https://example.com/pr/7",
			})
			if err != nil {
				r.t.Fatal(err)
			}
			if err := os.WriteFile(r.repairRecordPath, body, 0o600); err != nil {
				r.t.Fatalf("write record: %v", err)
			}
		} else {
			result = "nothing to record\nREPAIR_BLOCKED"
		}
	case strings.Contains(prompt, "The validator found failures"):
		r.events = append(r.events, "fix-violations")
		r.fixViolationsPrompt = prompt
	default:
		r.t.Fatalf("unexpected agent step %s:\n%s", options.Prefix, prompt)
	}
	return ProcessResult{Started: true, ExitCode: 0, Stdout: claudeUsageOutput(result, 0)}, nil
}

func (r *taskDeliveryRunner) generate() {
	if err := os.WriteFile(filepath.Join(r.ext, "delivered.txt"), []byte("delivered\n"), 0o600); err != nil {
		r.t.Fatal(err)
	}
	gitIn(r.t, r.ext, "checkout", "-q", "-B", "feature")
	gitIn(r.t, r.ext, "add", "delivered.txt")
	gitIn(r.t, r.ext, "commit", "-q", "-m", "deliver task")
	if !r.unpushed {
		gitIn(r.t, r.ext, "push", "-q", "-u", "origin", "feature")
	}
	r.delivered = gitOut(r.t, r.ext, "rev-parse", "HEAD")
}

func (r *taskDeliveryRunner) exec(cmd *osexec.Cmd, stdin []byte) (ProcessResult, error) {
	cmd.Dir = r.run
	if stdin != nil {
		cmd.Stdin = strings.NewReader(string(stdin))
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if exitErr, ok := err.(*osexec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		return ProcessResult{}, err
	}
	return ProcessResult{Started: true, ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := osexec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "test@example.com")
	gitIn(t, dir, "config", "user.name", "Test")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
}

// taskDeliveryFixture is a run repository with task files, a bare remote, and
// an external clone whose origin/HEAD records main.
type taskDeliveryFixture struct {
	run        string
	ext        string
	sessionDir string
}

func newTaskDeliveryFixture(t *testing.T) *taskDeliveryFixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	f := &taskDeliveryFixture{run: t.TempDir(), sessionDir: t.TempDir()}
	if activePID, err := runlock.Acquire(f.sessionDir); err != nil || activePID != 0 {
		t.Fatalf("acquire run lock: active=%d err=%v", activePID, err)
	}
	t.Cleanup(func() { runlock.Delete(f.sessionDir) })
	initGitRepo(t, f.run)
	for _, name := range []string{"01-task.md", "02-task.md"} {
		if err := os.WriteFile(filepath.Join(f.run, name), []byte("Implement it in the external repository.\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, f.run, "add", ".")
	gitIn(t, f.run, "commit", "-q", "-m", "initial")

	remote := filepath.Join(t.TempDir(), "remote.git")
	gitIn(t, filepath.Dir(remote), "init", "-q", "--bare", "-b", "main", remote)
	seed := t.TempDir()
	initGitRepo(t, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("external\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, seed, "add", "README.md")
	gitIn(t, seed, "commit", "-q", "-m", "seed")
	gitIn(t, seed, "push", "-q", remote, "main")

	f.ext = filepath.Join(t.TempDir(), "ext")
	gitIn(t, filepath.Dir(f.ext), "clone", "-q", remote, f.ext)
	gitIn(t, f.ext, "config", "user.email", "test@example.com")
	gitIn(t, f.ext, "config", "user.name", "Test")
	gitIn(t, f.ext, "config", "commit.gpgsign", "false")
	return f
}

func (f *taskDeliveryFixture) runner(t *testing.T) *taskDeliveryRunner {
	return &taskDeliveryRunner{t: t, run: f.run, ext: f.ext}
}

// runImplementTask dispatches every implement-task step for taskFile.
func (f *taskDeliveryFixture) runImplementTask(t *testing.T, runner *taskDeliveryRunner, taskFile, skipValidator string) StepOutcome {
	t.Helper()
	workflow := loadBuiltinWorkflow(t, implementTaskRef)
	// generate-code is interactive; the fake agent has no terminal to hand off to.
	generate, _ := requireStep(t, &workflow, "generate-code")
	generate.Mode = model.ModeAutonomous
	ctx := model.NewRootContext(&model.RootContextOptions{
		Params: map[string]string{
			"task_file":          filepath.Join(f.run, taskFile),
			"skip_validator":     skipValidator,
			"run_session_report": "false",
		},
		WorkflowFile: implementTaskRef,
		SessionDir:   f.sessionDir,
		ProjectRoot:  f.run,
	})
	group := model.Step{ID: "implement-task", Steps: workflow.Steps}
	outcome, err := DispatchStep(&group, ctx, runner, &mockGlob{}, &mockLogger{})
	if err != nil {
		t.Fatalf("implement-task: %v (events %v)", err, runner.events)
	}
	return outcome
}

func countEvents(events []string, name string) int {
	n := 0
	for _, event := range events {
		if event == name {
			n++
		}
	}
	return n
}

// INT-001: a repair-recorded external delivery lets implement-task continue.
func TestImplementTaskAcceptsRepairRecordedExternalDelivery(t *testing.T) {
	t.Run("pushed delivery is recorded by the repair and accepted", func(t *testing.T) {
		f := newTaskDeliveryFixture(t)
		runner := f.runner(t)
		runner.deliver, runner.writeRecord = true, true
		headBefore := gitOut(t, f.run, "rev-parse", "HEAD")

		if outcome := f.runImplementTask(t, runner, "01-task.md", "true"); outcome != OutcomeSuccess {
			t.Fatalf("outcome = %q, want success; events %v\nlast gate stderr:\n%s", outcome, runner.events, runner.lastGate.Stderr)
		}
		wantTail := []string{"generate-code", "verify-task-commit.sh", "repair", "verify-task-commit.sh"}
		if got := runner.events[len(runner.events)-len(wantTail):]; !cmp.Equal(wantTail, got) {
			t.Fatalf("events = %v, want tail %v", runner.events, wantTail)
		}
		wantDir := filepath.Join(f.sessionDir, "output", "task-delivery") + string(filepath.Separator)
		if !strings.HasPrefix(runner.repairRecordPath, wantDir) {
			t.Fatalf("repair record path %q, want under %s", runner.repairRecordPath, wantDir)
		}
		extRoot := gitOut(t, f.ext, "rev-parse", "--show-toplevel")
		requireContains(t, "gate stdout", runner.lastGate.Stdout,
			"task delivered outside this repository",
			"repository: "+extRoot,
			"commit: "+runner.delivered+" (contained in refs/remotes/origin/feature)",
			"pull request (reported, unverified): https://example.com/pr/7",
			"note: this run's validator and task-compliance review did not cover the external work",
			"record: "+runner.repairRecordPath,
		)
		if got := gitOut(t, f.run, "rev-parse", "HEAD"); got != headBefore {
			t.Errorf("run repository HEAD moved from %s to %s", headBefore, got)
		}
		if status := gitOut(t, f.run, "status", "--porcelain"); status != "" {
			t.Errorf("run repository status = %q, want clean", status)
		}
		if _, err := os.Stat(runner.repairRecordPath); err != nil {
			t.Errorf("record not retained: %v", err)
		}
		if _, err := os.Stat(strings.TrimSuffix(runner.repairRecordPath, ".json") + ".accepted"); err != nil {
			t.Errorf("accepted marker not written: %v", err)
		}
	})

	t.Run("an unpushed delivery fails after the repair budget", func(t *testing.T) {
		f := newTaskDeliveryFixture(t)
		runner := f.runner(t)
		runner.deliver, runner.unpushed, runner.writeRecord = true, true, true

		if outcome := f.runImplementTask(t, runner, "01-task.md", "true"); outcome != OutcomeFailed {
			t.Fatalf("outcome = %q, want failed; events %v", outcome, runner.events)
		}
		if countEvents(runner.events, "repair") != 1 {
			t.Fatalf("events = %v, want exactly one repair", runner.events)
		}
		requireContains(t, "gate stderr", runner.lastGate.Stderr,
			"commit "+runner.delivered+": not reachable from any remote-tracking ref")
	})

	t.Run("validator repair skips externally directed work without local filler", func(t *testing.T) {
		f := newTaskDeliveryFixture(t)
		runner := f.runner(t)
		runner.deliver, runner.writeRecord, runner.validatorFailures = true, true, 1
		headBefore := gitOut(t, f.run, "rev-parse", "HEAD")

		if outcome := f.runImplementTask(t, runner, "01-task.md", "false"); outcome != OutcomeSuccess {
			t.Fatalf("outcome = %q, want success; events %v\nlast gate stderr:\n%s", outcome, runner.events, runner.lastGate.Stderr)
		}
		if countEvents(runner.events, "fix-violations") != 1 || countEvents(runner.events, "repair") != 1 {
			t.Fatalf("events = %v, want one fix-violations and one repair", runner.events)
		}
		requireContains(t, "fix-violations prompt", runner.fixViolationsPrompt,
			"directs that work to a different repository",
			"do not re-implement it in this repository",
		)
		if got := gitOut(t, f.run, "rev-parse", "HEAD"); got != headBefore {
			t.Errorf("run repository HEAD moved from %s to %s", headBefore, got)
		}
		requireContains(t, "gate stdout", runner.lastGate.Stdout, "task delivered outside this repository")
	})
}

// INT-002: an earlier task's record does not satisfy a later task.
func TestImplementTaskIsolatesExternalDeliveryRecords(t *testing.T) {
	f := newTaskDeliveryFixture(t)
	first := f.runner(t)
	first.deliver, first.writeRecord = true, true
	if outcome := f.runImplementTask(t, first, "01-task.md", "true"); outcome != OutcomeSuccess {
		t.Fatalf("first task outcome = %q, want success; events %v", outcome, first.events)
	}

	second := f.runner(t)
	if outcome := f.runImplementTask(t, second, "02-task.md", "true"); outcome != OutcomeFailed {
		t.Fatalf("second task outcome = %q, want failed; events %v", outcome, second.events)
	}
	if second.repairRecordPath == "" || second.repairRecordPath == first.repairRecordPath {
		t.Fatalf("second record path %q, want one distinct from %q", second.repairRecordPath, first.repairRecordPath)
	}
	requireContains(t, "second gate stderr", second.lastGate.Stderr,
		"did not produce a commit",
		"no external delivery record at "+second.repairRecordPath,
	)
	if _, err := os.Stat(first.repairRecordPath); err != nil {
		t.Errorf("first task's record removed: %v", err)
	}
}

package runner

import (
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
)

const (
	taskWorkflowRef        = "builtin:core/implement-change-v1.0.yaml"
	taskWorkflowChangeName = "demo-change"
	taskWorkflowChangeDir  = "openspec/changes/demo-change"
)

// taskWorkflowRunner executes the task-based change workflows against a
// temporary repository. Shell commands and bundled scripts run for real, with
// fake openspec and agent-validator executables on PATH. Agents are faked by
// the leaf step id in their execution prefix, so rewording a prompt or nesting
// a step under another sub-workflow does not change which fake runs. Only the
// GitHub-facing scripts are stubbed.
type taskWorkflowRunner struct {
	t          *testing.T
	repo       string
	sessionDir string
	events     []string
	// failGenerateCode makes the next n generate-code agents fail.
	failGenerateCode int
	taskCount        int
	indexPrompt      string
}

func (r *taskWorkflowRunner) RunShell(cmd string, _ bool, workdir string) (exec.ProcessResult, error) {
	r.events = append(r.events, "shell: "+strings.TrimSpace(strings.SplitN(cmd, "\n", 2)[0]))
	result, err := r.exec(osexec.Command("sh", "-c", cmd), nil, workdir)
	result.Stdout = strings.TrimSpace(result.Stdout)
	result.Stderr = strings.TrimSpace(result.Stderr)
	return result, err
}

func (r *taskWorkflowRunner) RunScript(path string, stdin []byte, _ bool, workdir string) (exec.ProcessResult, error) {
	name := filepath.Base(path)
	r.events = append(r.events, name)
	switch name {
	case "check-draft-pr.sh":
		return exec.ProcessResult{Started: true, Stdout: "https://github.com/example/project/pull/7"}, nil
	case "acceptance-push.sh":
		return exec.ProcessResult{Started: true}, nil
	}
	return r.exec(osexec.Command("sh", path), stdin, workdir)
}

func (r *taskWorkflowRunner) RunAgent(options *exec.AgentProcessOptions) (exec.ProcessResult, error) {
	event := leafStepID(options.Prefix)
	switch event {
	case "test", "review", "fix-violations", "session-report", "simplify", "open-draft-pr":
	case "generate-code":
		if r.failGenerateCode > 0 {
			r.failGenerateCode--
			r.events = append(r.events, event+" (failed)")
			return exec.ProcessResult{Started: true, ExitCode: 1, Stdout: claudeAgentOutput("crashed")}, nil
		}
		r.writeRepoFile("feature.txt", fmt.Sprintf("implemented %d\n", len(r.events)))
		gitRun(r.t, r.repo, "add", "feature.txt")
		gitRun(r.t, r.repo, "commit", "-q", "-m", "feat: implement the task")
	case "complete-task-index":
		r.indexPrompt = strings.Join(options.Args, "\n")
		index := filepath.Join(taskWorkflowChangeDir, "tasks.md")
		r.writeRepoFile(index, strings.ReplaceAll(r.readRepoFile(index), "- [ ]", "- [x]"))
		gitRun(r.t, r.repo, "commit", "-q", "-m", "chore: check task index", "--", index)
	case "review-assumptions":
		r.writeEvidence("acceptance-assumptions.md", "No unresolved assumptions or context gaps.\n")
	case "acceptance-test":
		head := gitRun(r.t, r.repo, "rev-parse", "HEAD")
		r.writeEvidence("exploration-log.md", "explored "+head+"\n")
		r.writeEvidence("acceptance-tested-revision.txt", head+"\n")
		r.writeEvidence("acceptance-handoff.md", "ready "+head+"\n")
		r.writeEvidence("acceptance-round-status.txt", "READY "+head+"\n")
	default:
		r.t.Errorf("unexpected agent step %s:\n%s", options.Prefix, strings.Join(options.Args, "\n"))
		return exec.ProcessResult{Started: true, ExitCode: 1}, nil
	}
	r.events = append(r.events, event)
	return exec.ProcessResult{Started: true, Stdout: claudeAgentOutput("done")}, nil
}

// leafStepID returns the innermost step id of an agent execution prefix such
// as "[plan, sub:plan-change, check-definition:0, repair-definition]".
func leafStepID(prefix string) string {
	segments := strings.Split(strings.Trim(prefix, "[]"), ", ")
	return segments[len(segments)-1]
}

func (r *taskWorkflowRunner) exec(cmd *osexec.Cmd, stdin []byte, workdir string) (exec.ProcessResult, error) {
	if workdir == "" {
		workdir = r.repo
	}
	cmd.Dir = workdir
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
		return exec.ProcessResult{}, err
	}
	return exec.ProcessResult{Started: true, ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}, nil
}

func (r *taskWorkflowRunner) writeTaskPlan() {
	index := "# Tasks\n\n"
	for i := 1; i <= max(1, r.taskCount); i++ {
		name := fmt.Sprintf("tasks/%02d-implement.md", i)
		r.writeRepoFile(filepath.Join(taskWorkflowChangeDir, name), "# Implement\n\nWrite feature.txt.\n")
		index += "- [ ] [Implement](" + name + ")\n"
	}
	r.writeRepoFile(filepath.Join(taskWorkflowChangeDir, "tasks.md"), index)
}

func (r *taskWorkflowRunner) writeRepoFile(rel, content string) {
	path := filepath.Join(r.repo, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *taskWorkflowRunner) readRepoFile(rel string) string {
	data, err := os.ReadFile(filepath.Join(r.repo, rel))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(data)
}

func (r *taskWorkflowRunner) writeEvidence(name, content string) {
	dir := filepath.Join(r.sessionDir, "output")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// repoGlob expands loop patterns relative to the run repository and returns
// repository-relative matches, as the CLI does from its working directory.
type repoGlob struct{ repo string }

func (g repoGlob) Expand(pattern string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(g.repo, pattern))
	if err != nil {
		return nil, err
	}
	rel := make([]string, 0, len(matches))
	for _, match := range matches {
		r, err := filepath.Rel(g.repo, match)
		if err != nil {
			return nil, err
		}
		rel = append(rel, r)
	}
	sort.Strings(rel)
	return rel, nil
}

type taskWorkflowFixture struct {
	repo         string
	sessionDir   string
	validatorLog string
}

// newTaskWorkflowFixture creates a repository holding a committed
// definition-only OpenSpec change (no tasks) on a feature branch, and puts
// fake openspec and agent-validator executables on PATH. The fake Validator
// records every invocation.
func newTaskWorkflowFixture(t *testing.T) taskWorkflowFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	bin := t.TempDir()
	validatorLog := filepath.Join(t.TempDir(), "agent-validator.log")
	fakeValidator := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + strconv.Quote(validatorLog) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "agent-validator"), []byte(fakeValidator), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "openspec"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE", filepath.Join(bin, "agent-validator"))

	repo := t.TempDir()
	initDeliveryRepo(t, repo)
	files := map[string]string{
		"README.md":                             "fixture\n",
		taskWorkflowChangeDir + "/proposal.md":  "# Proposal\n",
		taskWorkflowChangeDir + "/test-plan.md": "# Test plan\n",
		taskWorkflowChangeDir + "/design.md":    "# Design\n",
		// Config-gated validator steps run only in a configured project.
		".validator/config.yml": "checks: {}\n",
		taskWorkflowChangeDir + "/specs/demo/spec.md": "## ADDED Requirements\n\n### Requirement: Demo\n\nThe system SHALL demo.\n\n" +
			"#### Scenario: Demo\n- **WHEN** it runs\n- **THEN** it demos\n",
	}
	for rel, content := range files {
		path := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "docs: define demo change")
	gitRun(t, repo, "checkout", "-q", "-b", "demo-change")
	// skip_if shell conditions run in the process working directory.
	t.Chdir(repo)

	return taskWorkflowFixture{repo: repo, sessionDir: t.TempDir(), validatorLog: validatorLog}
}

func (f taskWorkflowFixture) options(process exec.ProcessRunner) *Options {
	autonomous := func(cli string) *config.Agent { return &config.Agent{CLI: cli, DefaultMode: "autonomous"} }
	return &Options{
		WorkflowFile: taskWorkflowRef, SessionDir: f.sessionDir, ProjectRoot: f.repo, WorkingDir: f.repo,
		ProcessRunner: process, GlobExpander: repoGlob{repo: f.repo}, Log: &mockLog{},
		ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{
			"lead": autonomous("claude"), "implementor": autonomous("claude"), "tester": autonomous("claude"),
		}},
	}
}

func (f taskWorkflowFixture) run(t *testing.T, process *taskWorkflowRunner, skipValidator string) WorkflowResult {
	t.Helper()
	workflow, err := loader.LoadWorkflow(taskWorkflowRef, loader.Options{})
	if err != nil {
		t.Fatalf("LoadWorkflow(%s): %v", taskWorkflowRef, err)
	}
	result, err := RunWorkflow(&workflow, map[string]string{
		"change_name":                     taskWorkflowChangeName,
		"change_dir":                      taskWorkflowChangeDir,
		"change_label":                    "change",
		"change_kind":                     "openspec",
		"artifact_validation_instruction": "Validate the OpenSpec artifacts.",
		"skip_validator":                  skipValidator,
	}, f.options(process))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return result
}

func (f taskWorkflowFixture) validatorCalls(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.validatorLog)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func (f taskWorkflowFixture) newRunner(t *testing.T) *taskWorkflowRunner {
	return &taskWorkflowRunner{t: t, repo: f.repo, sessionDir: f.sessionDir}
}

// INT-002: the task-based implement-change workflow validates multiple tasks without
// opting into task-compliance, including when final verification only warns.
func TestBuiltInChangesDisableTaskCompliance(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("validatorFails=%v", fail), func(t *testing.T) {
			f := newTaskWorkflowFixture(t)
			setTaskComplianceRunnerShim(t)
			if fail {
				path := os.Getenv("AGENT_RUNNER_VALIDATOR_EXECUTABLE")
				file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o700)
				if err != nil {
					t.Fatal(err)
				}
				_, err = file.WriteString("[ \"$1\" != run ] || exit 1\n")
				if err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			process := f.newRunner(t)
			process.taskCount = 2
			process.writeTaskPlan()
			gitRun(t, f.repo, "add", ".")
			gitRun(t, f.repo, "commit", "-qm", "plan")
			if result := f.run(t, process, "false"); !fail && result != ResultSuccess {
				t.Fatalf("result=%s events=%v", result, process.events)
			}
			calls := f.validatorCalls(t)
			if strings.Count(calls, "run --report") < 2 || strings.Contains(calls, "--enable-review") || strings.Contains(calls, "--context-file") {
				t.Fatalf("calls=%s", calls)
			}
			if process.indexPrompt == "" {
				t.Fatal("index not reached")
			}
			for _, forbidden := range []string{"task-compliance", "passed validation", "passed the validator"} {
				if strings.Contains(process.indexPrompt, forbidden) {
					t.Fatalf("prompt contains %s", forbidden)
				}
			}
			// Count only launches before the task index, excluding change-level
			// verification that follows it. Each exhausted task must launch
			// three retries plus final verification.
			launches := 0
			for _, event := range process.events {
				if event == "complete-task-index" {
					break
				}
				if event == "run-validator.sh" {
					launches++
				}
			}
			wantLaunches := 2
			if fail {
				wantLaunches = 8
			}
			if launches != wantLaunches {
				t.Fatalf("per-task launches=%d want=%d events=%v", launches, wantLaunches, process.events)
			}
		})
	}
}

func TestCompleteSimpleChangeDisablesTaskCompliance(t *testing.T) {
	f := newTaskWorkflowFixture(t)
	setTaskComplianceRunnerShim(t)
	process := f.newRunner(t)
	process.writeTaskPlan()
	gitRun(t, f.repo, "add", ".")
	gitRun(t, f.repo, "commit", "-qm", "plan")
	ref := "builtin:core/complete-simple-change-v1.0.yaml"
	workflow, err := loader.LoadWorkflow(ref, loader.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// The fake review agent has no interactive terminal.
	for i := range workflow.Steps {
		if workflow.Steps[i].ID == "review" {
			workflow.Steps[i].Mode = "autonomous"
		}
	}
	options := f.options(process)
	options.WorkflowFile = ref
	result, err := RunWorkflow(&workflow, map[string]string{"change_name": taskWorkflowChangeName, "change_dir": taskWorkflowChangeDir, "change_label": "change"}, options)
	if err != nil || result != ResultSuccess {
		t.Fatalf("result=%s err=%v events=%v", result, err, process.events)
	}
	calls := f.validatorCalls(t)
	if !strings.Contains(calls, "run --report") || strings.Contains(calls, "--enable-review") || strings.Contains(calls, "--context-file") {
		t.Fatalf("calls=%s", calls)
	}
}

func setTaskComplianceRunnerShim(t *testing.T) {
	t.Helper()
	jq, err := osexec.LookPath("jq")
	if err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(t.TempDir(), "runner")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexec "+jq+" -r --arg key \"$3\" '.[$key] // \"\"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_RUNNER_EXECUTABLE", shim)
}

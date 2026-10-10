package runner

import (
	"encoding/json"
	"io/fs"
	"os"
	osexec "os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/metrics"
	"github.com/codagent/agent-runner/internal/stateio"
)

const (
	planAndImplementChangeRef = "builtin:core/plan-and-implement-change-v1.0.yaml"
	planAndImplementName      = "demo-change"
	planAndImplementDir       = "openspec/changes/demo-change"
)

// planAndImplementRunner executes core:plan-and-implement-change against a
// temporary repository. Shell commands and bundled scripts run for real, with
// fake openspec and agent-validator executables on PATH. Agents are faked by
// what their prompts ask for, and only the GitHub-facing scripts are stubbed.
type planAndImplementRunner struct {
	t          *testing.T
	repo       string
	sessionDir string
	events     []string
	// failGenerateCode makes the next n generate-code agents fail.
	failGenerateCode int
}

func (r *planAndImplementRunner) RunShell(cmd string, _ bool, workdir string) (exec.ProcessResult, error) {
	r.events = append(r.events, "shell: "+strings.TrimSpace(strings.SplitN(cmd, "\n", 2)[0]))
	result, err := r.exec(osexec.Command("sh", "-c", cmd), nil, workdir)
	result.Stdout = strings.TrimSpace(result.Stdout)
	result.Stderr = strings.TrimSpace(result.Stderr)
	return result, err
}

func (r *planAndImplementRunner) RunScript(path string, stdin []byte, _ bool, workdir string) (exec.ProcessResult, error) {
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

func (r *planAndImplementRunner) RunAgent(options *exec.AgentProcessOptions) (exec.ProcessResult, error) {
	prompt := strings.Join(options.Args, "\n")
	event := ""
	switch {
	case strings.Contains(prompt, "did not pass deterministic planning validation"):
		event = "repair-definition"
	case strings.Contains(prompt, "Create the implementation task breakdown"):
		event = "tasks"
		r.writeTaskPlan()
	case strings.Contains(prompt, "independent task-plan review"):
		event = "review-tasks"
	case strings.Contains(prompt, "Implement the task described in"):
		event = "generate-code"
		if r.failGenerateCode > 0 {
			r.failGenerateCode--
			r.events = append(r.events, event+" (failed)")
			return exec.ProcessResult{Started: true, ExitCode: 1, Stdout: claudeAgentOutput("crashed")}, nil
		}
		r.writeRepoFile("feature.txt", "implemented\n")
		gitRun(r.t, r.repo, "add", "feature.txt")
		gitRun(r.t, r.repo, "commit", "-q", "-m", "feat: implement the task")
	case strings.Contains(prompt, "codagent:session-report"):
		event = "session-report"
	case strings.Contains(prompt, "Every implementation task for"):
		event = "complete-task-index"
		index := filepath.Join(planAndImplementDir, "tasks.md")
		r.writeRepoFile(index, strings.ReplaceAll(r.readRepoFile(index), "- [ ]", "- [x]"))
		gitRun(r.t, r.repo, "commit", "-q", "-m", "chore: check task index", "--", index)
	case strings.Contains(prompt, "Audit the risky and notable assumptions"):
		event = "review-assumptions"
		r.writeEvidence("acceptance-assumptions.md", "No unresolved assumptions or context gaps.\n")
	case strings.Contains(prompt, "maintainability pass"):
		event = "simplify"
	case strings.Contains(prompt, "create or update its pull request"):
		event = "open-draft-pr"
	case strings.Contains(prompt, "independent acceptance tester"):
		event = "acceptance-test"
		head := gitRun(r.t, r.repo, "rev-parse", "HEAD")
		r.writeEvidence("exploration-log.md", "explored "+head+"\n")
		r.writeEvidence("acceptance-tested-revision.txt", head+"\n")
		r.writeEvidence("acceptance-handoff.md", "ready "+head+"\n")
		r.writeEvidence("acceptance-round-status.txt", "READY "+head+"\n")
	default:
		r.t.Errorf("unexpected agent step %s:\n%s", options.Prefix, prompt)
		return exec.ProcessResult{Started: true, ExitCode: 1}, nil
	}
	r.events = append(r.events, event)
	return exec.ProcessResult{Started: true, Stdout: claudeAgentOutput("done")}, nil
}

func (r *planAndImplementRunner) exec(cmd *osexec.Cmd, stdin []byte, workdir string) (exec.ProcessResult, error) {
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

func (r *planAndImplementRunner) writeTaskPlan() {
	r.writeRepoFile(filepath.Join(planAndImplementDir, "tasks", "01-implement.md"), "# Implement\n\nWrite feature.txt.\n")
	r.writeRepoFile(filepath.Join(planAndImplementDir, "tasks.md"), "# Tasks\n\n- [ ] [Implement](tasks/01-implement.md)\n")
}

func (r *planAndImplementRunner) writeRepoFile(rel, content string) {
	path := filepath.Join(r.repo, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *planAndImplementRunner) readRepoFile(rel string) string {
	data, err := os.ReadFile(filepath.Join(r.repo, rel))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(data)
}

func (r *planAndImplementRunner) writeEvidence(name, content string) {
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

type planAndImplementFixture struct {
	repo         string
	sessionDir   string
	validatorLog string
}

// newPlanAndImplementFixture creates a repository holding a committed
// definition-only OpenSpec change (no tasks) on a feature branch, and puts
// fake openspec and agent-validator executables on PATH. The fake Validator
// records every invocation; definitionComplete=false omits design.md so
// planning validation fails.
func newPlanAndImplementFixture(t *testing.T, definitionComplete bool) planAndImplementFixture {
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
		"README.md":                           "fixture\n",
		planAndImplementDir + "/proposal.md":  "# Proposal\n",
		planAndImplementDir + "/test-plan.md": "# Test plan\n",
		planAndImplementDir + "/specs/demo/spec.md": "## ADDED Requirements\n\n### Requirement: Demo\n\nThe system SHALL demo.\n\n" +
			"#### Scenario: Demo\n- **WHEN** it runs\n- **THEN** it demos\n",
	}
	if definitionComplete {
		files[planAndImplementDir+"/design.md"] = "# Design\n"
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

	return planAndImplementFixture{repo: repo, sessionDir: t.TempDir(), validatorLog: validatorLog}
}

func (f planAndImplementFixture) options(process exec.ProcessRunner) *Options {
	autonomous := func(cli string) *config.Agent { return &config.Agent{CLI: cli, DefaultMode: "autonomous"} }
	return &Options{
		WorkflowFile: planAndImplementChangeRef, SessionDir: f.sessionDir, ProjectRoot: f.repo, WorkingDir: f.repo,
		ProcessRunner: process, GlobExpander: repoGlob{repo: f.repo}, Log: &mockLog{},
		ProfileStore: &config.Config{ActiveAgents: map[string]*config.Agent{
			"lead": autonomous("claude"), "implementor": autonomous("claude"), "tester": autonomous("claude"),
		}},
	}
}

func (f planAndImplementFixture) run(t *testing.T, process *planAndImplementRunner, skipValidator string) WorkflowResult {
	t.Helper()
	workflow, err := loader.LoadWorkflow(planAndImplementChangeRef, loader.Options{})
	if err != nil {
		t.Fatalf("LoadWorkflow(%s): %v", planAndImplementChangeRef, err)
	}
	result, err := RunWorkflow(&workflow, map[string]string{
		"change_name":                     planAndImplementName,
		"change_dir":                      planAndImplementDir,
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

func (f planAndImplementFixture) validatorCalls(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.validatorLog)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func (f planAndImplementFixture) newRunner(t *testing.T) *planAndImplementRunner {
	return &planAndImplementRunner{t: t, repo: f.repo, sessionDir: f.sessionDir}
}

// planOnlyEvents and implementOnlyEvents name events that only the plan or the
// implement step of the composed workflow produces.
var (
	planOnlyEvents      = []string{"repair-definition", "tasks", "review-tasks", "commit-change-plan.sh"}
	implementOnlyEvents = []string{"validate-change-name.sh", "prepare-task-delivery.sh", "generate-code", "verify-task-commit.sh", "complete-task-index", "acceptance-test"}
)

func containsAny(events, wanted []string) []string {
	var found []string
	for _, event := range events {
		for _, want := range wanted {
			if strings.HasPrefix(event, want) {
				found = append(found, event)
			}
		}
	}
	return found
}

// Scenario "Plan then implement from definition artifacts", "One metrics
// artifact covers both steps", and "Composed workflow skips every Validator
// path": the real composed workflow plans, commits, implements, and reaches
// the acceptance handoff with skip_validator=true and no Validator invocation.
func TestPlanAndImplementChangeRunsPlanThenImplementWithoutValidator(t *testing.T) {
	fixture := newPlanAndImplementFixture(t, true)
	process := fixture.newRunner(t)

	if result := fixture.run(t, process, "true"); result != ResultSuccess {
		t.Fatalf("result = %q, want success; events %v", result, process.events)
	}

	if calls := fixture.validatorCalls(t); calls != "" {
		t.Fatalf("agent-validator invoked with skip_validator=true:\n%s", calls)
	}
	for _, event := range process.events {
		if event == "run-validator.sh" || event == "validator-pr-gate.sh" {
			t.Fatalf("validator step %s ran with skip_validator=true; events %v", event, process.events)
		}
	}

	// Planning finishes, including its commit, before implementation begins.
	lastPlan, firstImplement := -1, -1
	for i, event := range process.events {
		if len(containsAny([]string{event}, planOnlyEvents)) > 0 {
			lastPlan = i
		}
		if firstImplement < 0 && len(containsAny([]string{event}, implementOnlyEvents)) > 0 {
			firstImplement = i
		}
	}
	if lastPlan < 0 || firstImplement < 0 || lastPlan > firstImplement {
		t.Fatalf("plan must finish before implement starts; events %v", process.events)
	}
	subjects := strings.Split(gitRun(t, fixture.repo, "log", "--format=%s", "main..HEAD"), "\n")
	wantSubjects := []string{
		"chore: check task index",
		"feat: implement the task",
		"[commit-plan] chore: add change documents for " + planAndImplementName,
	}
	if diff := cmp.Diff(wantSubjects, subjects); diff != "" {
		t.Errorf("branch commits mismatch (-want +got):\n%s", diff)
	}
	status, err := os.ReadFile(filepath.Join(fixture.sessionDir, "output", "acceptance-preparation-status.txt"))
	if err != nil {
		t.Fatalf("read acceptance status: %v", err)
	}
	if got := strings.TrimSpace(string(status)); got != "ACCEPTANCE_COMPLETE" {
		t.Fatalf("acceptance status = %q, want ACCEPTANCE_COMPLETE", got)
	}

	requireCombinedMetrics(t, fixture.sessionDir)
}

// requireCombinedMetrics checks the run's single run-metrics.json names the
// composed workflow and records sub-workflow attempts under both the plan and
// implement step prefixes.
func requireCombinedMetrics(t *testing.T, sessionDir string) {
	t.Helper()
	var files []string
	err := filepath.WalkDir(sessionDir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Name() == metrics.FileName {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]string{filepath.Join(sessionDir, metrics.FileName)}, files); diff != "" {
		t.Fatalf("metrics artifacts mismatch (-want +got):\n%s", diff)
	}
	data, err := os.ReadFile(filepath.Join(sessionDir, metrics.FileName))
	if err != nil {
		t.Fatalf("read %s: %v", metrics.FileName, err)
	}
	var artifact metrics.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("parse %s: %v", metrics.FileName, err)
	}
	if artifact.Workflow != "plan-and-implement-change" {
		t.Errorf("metrics workflow = %q, want plan-and-implement-change", artifact.Workflow)
	}
	if artifact.RunID != filepath.Base(sessionDir) {
		t.Errorf("metrics run_id = %q, want %q", artifact.RunID, filepath.Base(sessionDir))
	}
	recorded := map[string]bool{}
	for i := range artifact.Steps {
		step := &artifact.Steps[i]
		recorded[step.Prefix+" "+step.ID] = true
		if step.Prefix != "" && !strings.HasPrefix(step.Prefix, "plan/") && !strings.HasPrefix(step.Prefix, "implement/") {
			t.Errorf("metrics step %s has prefix %q outside plan/ and implement/", step.ID, step.Prefix)
		}
	}
	for _, want := range []string{
		" plan",
		" implement",
		"plan/sub:plan-change tasks",
		"plan/sub:plan-change/review-tasks/sub:review-tasks review-tasks",
		"plan/sub:plan-change commit-plan",
		"implement/sub:implement-change/implement-tasks:0/implement-single-task/sub:implement-task generate-code",
		"implement/sub:implement-change/verify-change/sub:verify-change/prepare-acceptance:0 acceptance-test",
	} {
		if !recorded[want] {
			t.Errorf("metrics have no step record %q; recorded %v", want, recorded)
		}
	}
}

// Scenario "Plan failure stops before implementation".
func TestPlanAndImplementChangePlanFailureStopsBeforeImplement(t *testing.T) {
	fixture := newPlanAndImplementFixture(t, false)
	process := fixture.newRunner(t)

	if result := fixture.run(t, process, "false"); result != ResultFailed {
		t.Fatalf("result = %q, want failed; events %v", result, process.events)
	}
	if started := containsAny(process.events, implementOnlyEvents); len(started) != 0 {
		t.Fatalf("implement ran after plan failed: %v; events %v", started, process.events)
	}
	if got := containsAny(process.events, []string{"repair-definition"}); len(got) == 0 {
		t.Fatalf("planning validation was not repaired before failing; events %v", process.events)
	}
	if got := containsAny(process.events, []string{"tasks"}); len(got) != 0 {
		t.Fatalf("tasks were planned from an invalid definition; events %v", process.events)
	}

	state, err := stateio.ReadState(filepath.Join(fixture.sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	current := state.CurrentStep.StepID
	if state.CurrentStep.Nested != nil {
		current = state.CurrentStep.Nested.StepID
	}
	if current != "plan" {
		t.Errorf("state current step = %q, want plan", current)
	}
	if !strings.Contains(state.FailureReason, "verify-definition") || !strings.Contains(state.FailureReason, "design.md") {
		t.Errorf("failure reason = %q, want the plan step's verify-definition failure", state.FailureReason)
	}
}

// Scenario "Resume continues inside the composed workflow".
func TestPlanAndImplementChangeResumeContinuesInsideImplement(t *testing.T) {
	fixture := newPlanAndImplementFixture(t, true)
	first := fixture.newRunner(t)
	first.failGenerateCode = 1

	if result := fixture.run(t, first, "true"); result != ResultFailed {
		t.Fatalf("first result = %q, want failed; events %v", result, first.events)
	}
	if got := containsAny(first.events, []string{"generate-code (failed)"}); len(got) != 1 {
		t.Fatalf("first run did not stop in generate-code; events %v", first.events)
	}
	planCommits := gitRun(t, fixture.repo, "rev-list", "--count", "main..HEAD")
	state, err := stateio.ReadState(filepath.Join(fixture.sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentStep.Nested == nil || state.CurrentStep.Nested.StepID != "implement" || state.CurrentStep.Nested.Child == nil {
		t.Fatalf("interrupted state current step = %+v, want a nested position inside implement", state.CurrentStep)
	}

	second := fixture.newRunner(t)
	result, err := ResumeWorkflow(filepath.Join(fixture.sessionDir, "state.json"), fixture.options(second))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("resumed result = %q, want success; events %v", result, second.events)
	}
	// validate-change-name.sh also opens verify-change, so it is checked by
	// count; the other events occur only before generate-code.
	if repeated := containsAny(second.events, append(planOnlyEvents, "validate-planning-artifacts.sh", "prepare-task-delivery.sh")); len(repeated) != 0 {
		t.Fatalf("resume repeated earlier steps %v; events %v", repeated, second.events)
	}
	if repeated := containsAny(second.events, []string{"validate-change-name.sh"}); len(repeated) != 1 {
		t.Fatalf("resume repeated earlier steps %v; events %v", repeated, second.events)
	}
	if len(second.events) == 0 || second.events[0] != "generate-code" {
		t.Fatalf("resume events = %v, want re-entry at generate-code", second.events)
	}
	if planCommits != "1" {
		t.Fatalf("plan commits before resume = %s, want 1", planCommits)
	}
	if n := gitRun(t, fixture.repo, "rev-list", "--count", "--grep", "commit-plan", "main..HEAD"); n != "1" {
		t.Fatalf("plan committed %s times, want once", n)
	}

	requireCombinedMetrics(t, fixture.sessionDir)
}

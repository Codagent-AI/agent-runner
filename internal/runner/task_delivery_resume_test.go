package runner

import (
	"encoding/json"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/config"
	"github.com/codagent/agent-runner/internal/exec"
	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
)

const implementTaskRef = "builtin:core/implement-task-v1.0.yaml"

// taskDeliveryProcessRunner runs shell and script steps for real in the run
// repository and fakes agents: generate-code does nothing, and the
// verify-task-commit repair declares REPAIR_BLOCKED.
type taskDeliveryProcessRunner struct {
	t        *testing.T
	repo     string
	events   []string
	lastGate exec.ProcessResult
}

// RunShell trims output like the real process runners, so captures such as
// task_start_head carry no trailing newline.
func (r *taskDeliveryProcessRunner) RunShell(cmd string, _ bool, _ string) (exec.ProcessResult, error) {
	r.events = append(r.events, "shell: "+strings.TrimSpace(strings.SplitN(cmd, "\n", 2)[0]))
	result, err := r.exec(osexec.Command("sh", "-c", cmd), nil)
	result.Stdout = strings.TrimSpace(result.Stdout)
	result.Stderr = strings.TrimSpace(result.Stderr)
	return result, err
}

func (r *taskDeliveryProcessRunner) RunScript(path string, stdin []byte, _ bool, _ string) (exec.ProcessResult, error) {
	name := filepath.Base(path)
	r.events = append(r.events, name)
	result, err := r.exec(osexec.Command("sh", path), stdin)
	if name == "verify-task-commit.sh" {
		r.lastGate = result
	}
	return result, err
}

func (r *taskDeliveryProcessRunner) RunAgent(options *exec.AgentProcessOptions) (exec.ProcessResult, error) {
	prompt := strings.Join(options.Args, "\n")
	switch {
	case strings.Contains(prompt, "Implement the task described in"):
		r.events = append(r.events, "generate-code")
		return exec.ProcessResult{Started: true, Stdout: claudeAgentOutput("implemented elsewhere")}, nil
	case strings.Contains(prompt, "This repository shows no commit for the task"):
		r.events = append(r.events, "repair")
		return exec.ProcessResult{Started: true, Stdout: claudeAgentOutput("commits are not pushed\nREPAIR_BLOCKED")}, nil
	default:
		r.t.Fatalf("unexpected agent step %s:\n%s", options.Prefix, prompt)
		return exec.ProcessResult{}, nil
	}
}

func (r *taskDeliveryProcessRunner) exec(cmd *osexec.Cmd, stdin []byte) (exec.ProcessResult, error) {
	cmd.Dir = r.repo
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

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := osexec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func initDeliveryRepo(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
}

func implementorProfileStore() *config.Config {
	return &config.Config{ActiveAgents: map[string]*config.Agent{
		"implementor": {CLI: "claude"},
	}}
}

// INT-003: a human-written record lets a blocked verify-task-commit pass on
// resume without re-running the task start steps.
func TestResumeTaskDeliveryAfterBlockedGateUsesHumanWrittenRecord(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	initDeliveryRepo(t, repo)
	taskFile := filepath.Join(repo, "01-task.md")
	if err := os.WriteFile(taskFile, []byte("Implement it in the external repository.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "initial")
	headBefore := gitRun(t, repo, "rev-parse", "HEAD")

	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, filepath.Dir(remote), "init", "-q", "--bare", "-b", "main", remote)
	ext := filepath.Join(t.TempDir(), "ext")
	gitRun(t, filepath.Dir(ext), "clone", "-q", remote, ext)
	initDeliveryRepo(t, ext)
	gitRun(t, ext, "checkout", "-q", "-b", "feature")

	sessionDir := t.TempDir()
	workflow, err := loader.LoadWorkflow(implementTaskRef, loader.Options{})
	if err != nil {
		t.Fatalf("load workflow: %v", err)
	}
	// generate-code is interactive; the fake agent has no terminal to hand off to.
	for i := range workflow.Steps {
		if workflow.Steps[i].ID == "generate-code" {
			workflow.Steps[i].Mode = model.ModeAutonomous
		}
	}
	runner1 := &taskDeliveryProcessRunner{t: t, repo: repo}
	result, err := RunWorkflow(&workflow, map[string]string{
		"task_file": taskFile, "skip_validator": "true", "run_session_report": "false",
	}, &Options{
		WorkflowFile: implementTaskRef, SessionDir: sessionDir, ProjectRoot: repo, WorkingDir: repo,
		ProcessRunner: runner1, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: implementorProfileStore(),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result != ResultFailed {
		t.Fatalf("result = %q, want failed; events %v", result, runner1.events)
	}

	match := regexp.MustCompile(`no external delivery record at (\S+)`).FindStringSubmatch(runner1.lastGate.Stderr)
	if match == nil {
		t.Fatalf("blocked gate stderr names no record path:\n%s", runner1.lastGate.Stderr)
	}
	recordPath := match[1]

	// The human pushes the external work and writes the record.
	if err := os.WriteFile(filepath.Join(ext, "delivered.txt"), []byte("delivered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, ext, "add", "delivered.txt")
	gitRun(t, ext, "commit", "-q", "-m", "deliver task")
	gitRun(t, ext, "push", "-q", "-u", "origin", "feature")
	delivered := gitRun(t, ext, "rev-parse", "HEAD")
	record, err := json.Marshal(map[string]any{"repository": ext, "commits": []string{delivered}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath, record, 0o600); err != nil {
		t.Fatalf("write record: %v", err)
	}

	runner2 := &taskDeliveryProcessRunner{t: t, repo: repo}
	result, err = ResumeWorkflow(filepath.Join(sessionDir, "state.json"), &Options{
		ProjectRoot: repo, WorkingDir: repo,
		ProcessRunner: runner2, GlobExpander: &mockGlob{}, Log: &mockLog{}, ProfileStore: implementorProfileStore(),
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if result != ResultSuccess {
		t.Fatalf("resumed result = %q, want success; events %v\ngate stderr:\n%s", result, runner2.events, runner2.lastGate.Stderr)
	}
	for _, event := range runner2.events {
		if event == "prepare-task-delivery.sh" || strings.Contains(event, "git rev-parse --verify HEAD") || event == "generate-code" {
			t.Fatalf("resume re-ran an earlier step: events %v", runner2.events)
		}
	}
	if len(runner2.events) == 0 || runner2.events[0] != "verify-task-commit.sh" {
		t.Fatalf("resume events = %v, want re-entry at verify-task-commit.sh", runner2.events)
	}
	if !strings.Contains(runner2.lastGate.Stdout, "task delivered outside this repository") {
		t.Fatalf("gate stdout = %q, want external delivery", runner2.lastGate.Stdout)
	}
	if _, err := os.Stat(recordPath); err != nil {
		t.Fatalf("human-written record removed: %v", err)
	}
	if got := gitRun(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("run repository HEAD moved from %s to %s", headBefore, got)
	}

	auditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	if !auditStepEndStdoutContains(t, auditData, "verify-task-commit", "task delivered outside this repository") {
		t.Fatalf("audit log has no verify-task-commit step_end whose stdout states external delivery:\n%s", auditData)
	}
}

// auditStepEndStdoutContains reports whether a top-level step_end for stepID
// records stdout containing want. Audit lines read "<ts> [<prefix>] <type> <json>".
func auditStepEndStdoutContains(t *testing.T, auditData []byte, stepID, want string) bool {
	t.Helper()
	marker := "[" + stepID + "] step_end "
	for _, line := range strings.Split(string(auditData), "\n") {
		_, payload, found := strings.Cut(line, marker)
		if !found {
			continue
		}
		var data struct {
			Stdout string `json:"stdout"`
		}
		if err := json.Unmarshal([]byte(payload), &data); err != nil {
			t.Fatalf("parse step_end payload %q: %v", payload, err)
		}
		if strings.Contains(data.Stdout, want) {
			return true
		}
	}
	return false
}

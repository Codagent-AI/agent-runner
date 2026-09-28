package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestRunRetentionE2E(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell workflow requires POSIX")
	}
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSmokeProfileConfig(t, home)
	workflow := filepath.Join(project, ".agent-runner", "workflows", "clean-v1.0.yaml")
	if err := os.MkdirAll(filepath.Dir(workflow), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: clean\nsteps:\n  - id: done\n    command: echo done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(tmp, "agent-runner")
	buildAgentRunner(t, findRepoRoot(t), bin)
	canonicalProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	runsDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(canonicalProject), "runs")
	if err := os.MkdirAll(runsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	seed := func(id string, finished bool, age time.Duration) {
		t.Helper()
		dir := filepath.Join(runsDir, id)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := stateio.WriteState(&model.RunState{RunID: id, WorkflowFile: workflow, Completed: finished}, dir); err != nil {
			t.Fatal(err)
		}
		log := filepath.Join(dir, "audit.log")
		if err := os.WriteFile(log, []byte("historical"), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := now.Add(-age)
		for _, p := range []string{log, filepath.Join(dir, "state.json"), dir} {
			if err := os.Chtimes(p, stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := 0; i < 5; i++ {
		seed(fmt.Sprintf("old-%d", i), true, 40*24*time.Hour)
	}
	seed("abandoned", false, 100*24*time.Hour)
	seed("resumable", false, 45*24*time.Hour)
	seed("recent", true, 2*24*time.Hour)
	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"--headless"}, args...)...)
		cmd.Dir = project
		cmd.Env = smokeCommandEnv(os.Environ(), "HOME="+home, "AGENT_RUNNER_NO_TUI=1")
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		err := cmd.Run()
		return output.String(), err
	}
	first, err := run("clean")
	if err != nil {
		t.Fatalf("first run: %v\n%s", err, first)
	}
	if !strings.Contains(first, "run retention enabled") || !strings.Contains(first, "30 days") || !strings.Contains(first, "90 days") || !strings.Contains(first, "100 finished runs") || !strings.Contains(first, "6 existing runs") || !strings.Contains(first, "run_retention.enabled: false") {
		t.Fatalf("activation notice: %s", first)
	}
	activationBody, err := os.ReadFile(filepath.Join(home, ".agent-runner", "retention", "state.json"))
	if err != nil || !bytes.Contains(activationBody, []byte(`"activated_at"`)) {
		t.Fatalf("activation not recorded after notice: %v %s", err, activationBody)
	}
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(runsDir, fmt.Sprintf("old-%d", i))); err != nil {
			t.Fatalf("deleted during grace: %v", err)
		}
	}
	second, err := run("clean")
	if err != nil || strings.Contains(second, "run retention enabled") {
		t.Fatalf("second run: %v\n%s", err, second)
	}
	statePath := filepath.Join(home, ".agent-runner", "retention", "state.json")
	if err := stateio.WriteJSONAtomic(statePath, map[string]time.Time{"activated_at": now.Add(-8 * 24 * time.Hour), "last_started_at": now.Add(-25 * time.Hour), "last_completed_at": now.Add(-25 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	third, err := run("clean")
	if err != nil {
		t.Fatalf("third run: %v\n%s", err, third)
	}
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(runsDir, fmt.Sprintf("old-%d", i))); !os.IsNotExist(err) {
			t.Errorf("old-%d survived: %v", i, err)
		}
	}
	if _, err := os.Stat(filepath.Join(runsDir, "abandoned")); !os.IsNotExist(err) {
		t.Errorf("abandoned survived: %v", err)
	}
	for _, id := range []string{"resumable", "recent"} {
		if _, err := os.Stat(filepath.Join(runsDir, id)); err != nil {
			t.Errorf("%s removed: %v", id, err)
		}
	}
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		t.Fatal(err)
	}
	newRuns := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "clean-") {
			newRuns++
			if _, err := os.Stat(filepath.Join(runsDir, entry.Name(), "audit.log")); err != nil {
				t.Errorf("new audit log missing: %v", err)
			}
		}
	}
	if newRuns != 3 {
		t.Errorf("fresh run count = %d, want 3", newRuns)
	}
	cmd := exec.Command(bin, "--resume", "old-0")
	cmd.Dir = project
	cmd.Env = smokeCommandEnv(os.Environ(), "HOME="+home, "AGENT_RUNNER_NO_TUI=1")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "session not found") {
		t.Fatalf("resume removed: %v %s", err, out)
	}
}

func TestRunRetentionOptOutE2E(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell workflow requires POSIX")
	}
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	project := filepath.Join(tmp, "project")
	workflow := filepath.Join(project, ".agent-runner", "workflows", "clean-v1.0.yaml")
	if err := os.MkdirAll(filepath.Dir(workflow), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: clean\nsteps:\n  - id: done\n    command: echo done\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeSmokeProfileConfig(t, home)
	bin := filepath.Join(tmp, "agent-runner")
	buildAgentRunner(t, findRepoRoot(t), bin)
	canonicalProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	runsDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(canonicalProject), "runs")
	if err := os.MkdirAll(runsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(runsDir, "old")
	if err := os.Mkdir(old, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "old", WorkflowFile: workflow, Completed: true}, old); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-40 * 24 * time.Hour)
	for _, p := range []string{filepath.Join(old, "state.json"), old} {
		if err := os.Chtimes(p, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	statePath := filepath.Join(home, ".agent-runner", "retention", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	dueState := func() {
		t.Helper()
		if err := stateio.WriteJSONAtomic(statePath, map[string]time.Time{"activated_at": time.Now().Add(-8 * 24 * time.Hour), "last_completed_at": time.Now().Add(-25 * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	dueState()
	settingsPath := filepath.Join(home, ".agent-runner", "settings.yaml")
	if err := os.WriteFile(settingsPath, []byte("run_retention: {enabled: false}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, error) {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"--headless"}, args...)...)
		cmd.Dir = project
		cmd.Env = smokeCommandEnv(os.Environ(), "HOME="+home, "AGENT_RUNNER_NO_TUI=1")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run("clean"); err != nil || strings.Contains(out, "run retention") {
		t.Fatalf("opt out: %v %s", err, out)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("opt out removed old run: %v", err)
	}
	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if out, err := run("run", "clean", "--session-dir", filepath.Join(tmp, "explicit")); err != nil || strings.Contains(out, "run retention") {
		t.Fatalf("explicit session: %v %s", err, out)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("explicit session triggered sweep: %v", err)
	}
	resumeDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(canonicalProject), "runs", "resume")
	if err := os.MkdirAll(resumeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := stateio.WriteState(&model.RunState{RunID: "resume", WorkflowFile: workflow, WorkflowName: "clean", CurrentStep: model.CurrentStep{Nested: &model.NestedStepState{StepID: "done"}}}, resumeDir); err != nil {
		t.Fatal(err)
	}
	resumeCmd := exec.Command(bin, "--headless", "--resume", "resume")
	resumeCmd.Dir = project
	resumeCmd.Env = smokeCommandEnv(os.Environ(), "HOME="+home, "AGENT_RUNNER_NO_TUI=1")
	resumeOut, resumeErr := resumeCmd.CombinedOutput()
	if resumeErr != nil || strings.Contains(string(resumeOut), "run retention") {
		t.Fatalf("resume triggered sweep: %v %s", resumeErr, resumeOut)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("resume removed old run: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("run_retention: {max_age_days: -5}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dueState()
	if out, err := run("clean"); err != nil || !strings.Contains(out, "max_age_days") {
		t.Fatalf("invalid limit: %v %s", err, out)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("default age did not remove old run: %v", err)
	}
	freshHome := filepath.Join(tmp, "fresh-home")
	writeSmokeProfileConfig(t, freshHome)
	freshSettings := filepath.Join(freshHome, ".agent-runner", "settings.yaml")
	if err := os.WriteFile(freshSettings, []byte("run_retention: {enabled: false}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--headless", "clean")
	cmd.Dir = project
	cmd.Env = smokeCommandEnv(os.Environ(), "HOME="+freshHome, "AGENT_RUNNER_NO_TUI=1")
	out, err := cmd.CombinedOutput()
	if err != nil || strings.Contains(string(out), "run retention") {
		t.Fatalf("disabled before activation: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(freshHome, ".agent-runner", "retention", "state.json")); !os.IsNotExist(err) {
		t.Fatalf("activation state written while disabled: %v", err)
	}
}

func TestRetentionTrashIDsRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := resolveResumeStatePath(".pruning-old"); err == nil || !strings.Contains(err.Error(), "invalid session ID") {
		t.Fatalf("resume trash ID: %v", err)
	}
	if _, _, err := resolveInspectSession(".pruning-old"); err == nil || !strings.Contains(err.Error(), "invalid run ID") {
		t.Fatalf("inspect trash ID: %v", err)
	}
}

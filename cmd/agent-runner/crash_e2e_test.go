package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/stateio"
)

func crashE2ESetup(t *testing.T, script string) (runnerBin, workdir, home string, env []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI requires POSIX shell")
	}
	tmp := t.TempDir()
	home, workdir, binDir := filepath.Join(tmp, "home"), filepath.Join(tmp, "project"), filepath.Join(tmp, "bin")
	runnerBin = filepath.Join(tmp, "agent-runner")
	for _, dir := range []string{home, workdir, binDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	workdir, err = filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatal(err)
	}
	writeSmokeProfileConfig(t, home)
	writeProjectSmokeConfig(t, workdir)
	if err := os.MkdirAll(filepath.Join(workdir, ".agent-runner", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	buildAgentRunner(t, findRepoRoot(t), runnerBin)
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	env = smokeCommandEnv(os.Environ(), "AGENT_RUNNER_NO_TUI=1", "HOME="+home, "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return runnerBin, workdir, home, env
}

func TestCrashInsideSubWorkflowE2E(t *testing.T) {
	runnerBin, workdir, home, env := crashE2ESetup(t, "echo 'Selected model is at capacity' >&2\nexit 1\n")
	workflowDir := filepath.Join(workdir, ".agent-runner", "workflows")
	writeWorkflowFile(t, workflowDir, "crash-child-v1.0.yaml", `name: crash-child
steps:
  - id: generate-code
    agent: claude_headless_smoke
    session: new
    prompt: "work"
`)
	signalFile := filepath.Join(workdir, "signals")
	writeWorkflowFile(t, workflowDir, "crash-parent-v1.0.yaml", `name: crash-parent
steps:
  - id: first
    workflow: crash-child-v1.0.yaml
    continue_on_failure: true
  - id: capture
    command: "printf '%s,%s' {{last_step_failure_kind}} {{last_step_crash_observed}} > `+signalFile+`"
  - id: final
    workflow: crash-child-v1.0.yaml
`)
	output := runRepairBlockedCLI(t, runnerBin, workdir, env, "--headless", "--profile", "smoke_test", "crash-parent")
	if output.exitCode == 0 {
		t.Fatalf("expected failure: %s", output.text)
	}
	signals, err := os.ReadFile(signalFile)
	if err != nil || string(signals) != "infrastructure,true" {
		t.Fatalf("signals = %q, %v; output: %s", signals, err, output.text)
	}
	matches, err := filepath.Glob(filepath.Join(home, ".agent-runner", "projects", "*", "runs", "crash-parent-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("run dirs = %v, %v", matches, err)
	}
	dir := matches[0]
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.FailureKind != model.FailureInfrastructure || !state.CrashObserved || len(state.Crashes) == 0 || !strings.Contains(state.FailureReason, "generate-code failed (infrastructure): Selected model is at capacity") {
		t.Fatalf("state = %+v", state)
	}
	if !strings.Contains(output.text, state.FailureReason) || !strings.Contains(output.text, "to resume:") {
		t.Fatalf("console: %s", output.text)
	}
	audit, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"failure_kind":"infrastructure"`, `"crash_observed":true`, `"failure_origin"`, "sub_workflow_end", "run_end"} {
		if !strings.Contains(string(audit), field) {
			t.Fatalf("audit missing %s", field)
		}
	}
	for _, event := range []string{" step_end ", " sub_workflow_end ", " run_end "} {
		line := lastCrashAuditLine(string(audit), event)
		if !strings.Contains(line, `"failure_kind":"infrastructure"`) || !strings.Contains(line, `"crash_observed":true`) || !strings.Contains(line, `"failure_origin"`) {
			t.Fatalf("%s lacks crash fields: %s", event, line)
		}
	}
}

func TestCrashResumeAndLoopRecoveryE2E(t *testing.T) {
	countFile := filepath.Join(t.TempDir(), "calls")
	script := `count=0
if [ -f "` + countFile + `" ]; then count=$(cat "` + countFile + `"); fi
count=$((count + 1))
echo "$count" > "` + countFile + `"
if [ "$count" -eq 1 ]; then echo 'Selected model is at capacity' >&2; exit 1; fi
printf '{"type":"result","result":"done"}\n'
`
	runnerBin, workdir, home, env := crashE2ESetup(t, script)
	workflowDir := filepath.Join(workdir, ".agent-runner", "workflows")
	writeWorkflowFile(t, workflowDir, "crash-resume-v1.0.yaml", `name: crash-resume
steps:
  - id: agent
    agent: claude_headless_smoke
    session: new
    prompt: "work"
`)
	first := runRepairBlockedCLI(t, runnerBin, workdir, env, "--headless", "--profile", "smoke_test", "crash-resume")
	if first.exitCode == 0 {
		t.Fatalf("first run succeeded: %s", first.text)
	}
	matches, err := filepath.Glob(filepath.Join(home, ".agent-runner", "projects", "*", "runs", "crash-resume-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("run dirs = %v, %v", matches, err)
	}
	dir := matches[0]
	resumed := runRepairBlockedCLI(t, runnerBin, workdir, env, "--headless", "--profile", "smoke_test", "--resume", filepath.Base(dir))
	if resumed.exitCode != 0 {
		t.Fatalf("resume failed: %s", resumed.text)
	}
	state, err := stateio.ReadState(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Completed || state.CrashObserved || len(state.Crashes) != 0 || state.FailureKind != "" {
		t.Fatalf("resumed state = %+v", state)
	}
	audit, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), `"failure_origin"`) {
		t.Fatal("original crash origin lost")
	}
	if line := lastCrashAuditLine(string(audit), " run_end "); !strings.Contains(line, `"crash_observed":false`) || strings.Contains(line, `"failure_kind"`) {
		t.Fatalf("resumed run_end=%s", line)
	}
	if err := os.Remove(countFile); err != nil {
		t.Fatal(err)
	}
	writeWorkflowFile(t, workflowDir, "crash-loop-v1.0.yaml", `name: crash-loop
steps:
  - id: cycle
    loop:
      max: 2
    steps:
      - id: agent
        agent: claude_headless_smoke
        session: new
        prompt: "work"
        continue_on_failure: true
      - id: gate
        command: "test $(cat `+countFile+`) -gt 1"
        continue_on_failure: true
        break_if: success
`)
	loop := runRepairBlockedCLI(t, runnerBin, workdir, env, "--headless", "--profile", "smoke_test", "crash-loop")
	if loop.exitCode != 0 {
		t.Fatalf("loop failed: %s", loop.text)
	}
	loopDirs, err := filepath.Glob(filepath.Join(home, ".agent-runner", "projects", "*", "runs", "crash-loop-*"))
	if err != nil || len(loopDirs) != 1 {
		t.Fatalf("loop dirs = %v, %v", loopDirs, err)
	}
	loopState, err := stateio.ReadState(filepath.Join(loopDirs[0], "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !loopState.Completed || !loopState.CrashObserved || len(loopState.Crashes) != 1 {
		t.Fatalf("loop state = %+v", loopState)
	}
	loopAudit, err := os.ReadFile(filepath.Join(loopDirs[0], "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(loopAudit), `"crash_observed":true`) {
		t.Fatal("loop audit lacks crash observation")
	}
	for _, event := range []string{"[cycle] step_end ", " run_end "} {
		if line := lastCrashAuditLine(string(loopAudit), event); !strings.Contains(line, `"crash_observed":true`) {
			t.Fatalf("%s=%s", event, line)
		}
	}
}

func lastCrashAuditLine(auditText, event string) string {
	var found string
	for _, line := range strings.Split(auditText, "\n") {
		if strings.Contains(line, event) {
			found = line
		}
	}
	return found
}

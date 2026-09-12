package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/stateio"
)

// TestRepairBlockedE2E proves the motivating blocked-draft-PR journey end to
// end: a run fails because a rerun-form repair's guarded agent declares
// REPAIR_BLOCKED, the failure names the right step, no repair attempt is
// recorded (blocked short-circuits the budget loop), and after a human fix
// (here: creating a marker file standing in for granting a missing scope)
// --resume re-enters at the rerun target with the evidence preface and
// completes.
func TestRepairBlockedE2E(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI stub is a POSIX shell script")
	}

	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	binDir := filepath.Join(tmp, "bin")
	workdir := filepath.Join(tmp, "project")
	runnerBin := filepath.Join(tmp, "agent-runner")

	for _, dir := range []string{home, binDir, workdir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	// --resume re-derives the project's encoded path from a fresh os.Getwd(),
	// which resolves symlinks (e.g. macOS's /tmp -> /private/tmp); starting
	// from the same canonical path here keeps the encoded project directory
	// consistent between the run and the resume.
	canonicalWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatalf("resolve workdir symlinks: %v", err)
	}
	workdir = canonicalWorkdir
	writeSmokeProfileConfig(t, home)
	writeProjectSmokeConfig(t, workdir)

	repoRoot := findRepoRoot(t)
	buildAgentRunner(t, repoRoot, runnerBin)

	capturedPrompts := filepath.Join(tmp, "captured-prompts.txt")
	scopeMarker := filepath.Join(tmp, "scope-granted")
	verifyReady := filepath.Join(tmp, "verify-ready")
	writeRepairBlockedFakeCLI(t, binDir, capturedPrompts, scopeMarker, verifyReady)

	workflowsDir := filepath.Join(workdir, ".agent-runner", "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("create workflows dir: %v", err)
	}
	writeWorkflowFile(t, workflowsDir, "child-v1.0.yaml", `name: child
steps:
  - id: child-step
    agent: claude_headless_smoke
    session: new
    prompt: "CHILD_STEP_MARKER: say hello"
`)
	writeWorkflowFile(t, workflowsDir, "repair-blocked-e2e-v1.0.yaml", `name: repair-blocked-e2e
steps:
  - id: act
    agent: claude_headless_smoke
    session: new
    prompt: "Open a draft PR"
  - id: shell-step
    command: "true"
  - id: sub
    workflow: child-v1.0.yaml
  - id: verify
    command: "test -f `+verifyReady+`"
    repair:
      rerun: act
`)

	env := smokeCommandEnv(os.Environ(),
		"AGENT_RUNNER_NO_TUI=1",
		"HOME="+home,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CAPTURED_PROMPTS="+capturedPrompts,
		"SCOPE_MARKER="+scopeMarker,
		"VERIFY_READY="+verifyReady,
	)

	firstOutput := runRepairBlockedCLI(t, runnerBin, workdir, env,
		"--headless", "--profile", "smoke_test", "repair-blocked-e2e")
	if firstOutput.exitCode == 0 {
		t.Fatalf("expected first run to fail, exit 0\n%s", firstOutput.text)
	}

	lines := strings.Split(firstOutput.text, "\n")
	blockedIdx, resumeIdx := -1, -1
	for i, line := range lines {
		if strings.Contains(line, "verify failed:") && strings.Contains(line, "blocked:") {
			blockedIdx = i
		}
		if strings.Contains(line, "to resume:") {
			resumeIdx = i
		}
	}
	if blockedIdx == -1 {
		t.Fatalf("missing 'verify failed: ... blocked: ...' line:\n%s", firstOutput.text)
	}
	if resumeIdx == -1 || resumeIdx <= blockedIdx {
		t.Fatalf("'to resume:' line missing or not after the blocked line:\n%s", firstOutput.text)
	}
	if !strings.Contains(lines[blockedIdx], "missing token scope") {
		t.Fatalf("blocked line does not carry act's own response: %q", lines[blockedIdx])
	}
	if strings.Contains(lines[blockedIdx], "child step distinguishable response") {
		t.Fatalf("blocked line incorrectly carries the sub-workflow agent's response: %q", lines[blockedIdx])
	}

	runID := parseResumeRunID(t, firstOutput.text)
	sessionDir := findSessionDir(t, home, runID)
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	if state.FailureReason == "" || !strings.Contains(state.FailureReason, "missing token scope") {
		t.Fatalf("state.json failureReason = %q, want it naming the blocked response", state.FailureReason)
	}

	auditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	if strings.Contains(string(auditData), "repair_attempt_start") {
		t.Fatalf("audit log unexpectedly contains repair_attempt_start for a blocked-on-first-failure check:\n%s", auditData)
	}

	// The human fix: grant the missing scope.
	if err := os.WriteFile(scopeMarker, []byte("granted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(capturedPrompts, 0); err != nil {
		t.Fatal(err)
	}

	resumedOutput := runRepairBlockedCLI(t, runnerBin, workdir, env,
		"--headless", "--profile", "smoke_test", "--resume", runID)
	if resumedOutput.exitCode != 0 {
		t.Fatalf("expected resume to succeed\n%s", resumedOutput.text)
	}

	promptsAfterResume, err := os.ReadFile(capturedPrompts)
	if err != nil {
		t.Fatalf("read captured prompts: %v", err)
	}
	if !strings.Contains(string(promptsAfterResume), "repair-evidence") {
		t.Fatalf("act's resumed prompt does not carry the evidence preface:\n%s", promptsAfterResume)
	}
	if !strings.Contains(string(promptsAfterResume), "Open a draft PR") {
		t.Fatalf("resumed prompt is not act's own prompt:\n%s", promptsAfterResume)
	}

	finalState, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read resumed state.json: %v", err)
	}
	if !finalState.Completed {
		t.Fatalf("expected resumed run to complete, got %+v", finalState)
	}
	if finalState.FailureReason != "" {
		t.Fatalf("expected failureReason cleared on a completed run, got %q", finalState.FailureReason)
	}

	finalAuditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatalf("read resumed audit.log: %v", err)
	}
	if !strings.Contains(string(finalAuditData), string(auditData)) {
		t.Fatalf("audit log is not append-only: the original failed run's events are missing after resume\noriginal:\n%s\nfinal:\n%s", auditData, finalAuditData)
	}
	if !strings.Contains(string(finalAuditData), "missing token scope") {
		t.Fatalf("the earlier blocked guarded evidence no longer appears in audit.log after resume:\n%s", finalAuditData)
	}
	if !strings.Contains(string(finalAuditData), "[verify] step_end") || !strings.Contains(string(finalAuditData), `"outcome":"success"`) {
		t.Fatalf("resumed audit.log missing a successful verify step_end:\n%s", finalAuditData)
	}
}

type repairBlockedCLIResult struct {
	text     string
	exitCode int
}

func runRepairBlockedCLI(t *testing.T, runnerBin, workdir string, env []string, args ...string) repairBlockedCLIResult {
	t.Helper()
	cmd := exec.Command(runnerBin, args...)
	cmd.Dir = workdir
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %v: %v\n%s", args, err, out.String())
		}
		exitCode = exitErr.ExitCode()
	}
	return repairBlockedCLIResult{text: out.String(), exitCode: exitCode}
}

var resumeLineRE = regexp.MustCompile(`to resume: agent-runner --resume (\S+)`)

func parseResumeRunID(t *testing.T, output string) string {
	t.Helper()
	m := resumeLineRE.FindStringSubmatch(output)
	if m == nil {
		t.Fatalf("could not find 'to resume: agent-runner --resume <id>' in output:\n%s", output)
	}
	return m[1]
}

// findSessionDir locates the run directory for runID under home's projects
// scope by globbing, rather than recomputing the project's encoded path:
// the runner canonicalizes the project root (resolving symlinks such as
// macOS's /tmp -> /private/tmp) before encoding it, which the test's own
// workdir string does not reflect.
func findSessionDir(t *testing.T, home, runID string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(home, ".agent-runner", "projects", "*", "runs", runID))
	if err != nil || len(matches) != 1 {
		t.Fatalf("find session dir for run %q: matches=%v err=%v", runID, matches, err)
	}
	return matches[0]
}

func writeWorkflowFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write workflow %s: %v", name, err)
	}
	return path
}

func writeProjectSmokeConfig(t *testing.T, workdir string) {
	t.Helper()
	dir := filepath.Join(workdir, ".agent-runner")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create project config dir: %v", err)
	}
	body := `profiles:
  smoke_test:
    extends: default
    agents:
      claude_headless_smoke:
        default_mode: autonomous
        cli: claude
        model: haiku
        effort: low
`
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}
}

// writeRepairBlockedFakeCLI writes a fake "claude" executable that: records
// every prompt it receives (its last argv) to capturedPrompts; gives a
// response distinguishable from act's whenever the prompt carries the
// sub-workflow's own marker; and otherwise (act's own prompt) reports
// REPAIR_BLOCKED until scopeMarker exists, at which point it creates
// verifyReady (standing in for the effect of a granted scope) and reports
// success instead.
func writeRepairBlockedFakeCLI(t *testing.T, binDir, capturedPrompts, scopeMarker, verifyReady string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
prompt=""
for arg in "$@"; do
  prompt=$arg
done
printf '%s\n---\n' "$prompt" >> "` + capturedPrompts + `"
case "$prompt" in
  *CHILD_STEP_MARKER*)
    result='child step distinguishable response'
    ;;
  *)
    if [ -f "` + scopeMarker + `" ]; then
      : > "` + verifyReady + `"
      result='now able to open the PR'
    else
      result='looked into it: missing token scope\nREPAIR_BLOCKED'
    fi
    ;;
esac
printf '{"type":"result","result":"%s"}\n' "$result"
`
	path := filepath.Join(binDir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
}

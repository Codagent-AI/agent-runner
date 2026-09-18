package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/stateio"
)

// TestRepairInlineRecoveryE2E proves the inline-form repair journey end to
// end on the built binary: a check fails, the inline repair agent (a fake
// CLI) creates the file the check needs, the same check reruns and passes,
// and the run completes with the audit sequence the spec requires and no
// warning.
func TestRepairInlineRecoveryE2E(t *testing.T) {
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
	canonicalWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatalf("resolve workdir symlinks: %v", err)
	}
	workdir = canonicalWorkdir
	writeSmokeProfileConfig(t, home)
	writeProjectSmokeConfig(t, workdir)
	buildAgentRunner(t, findRepoRoot(t), runnerBin)

	capturedPrompts := filepath.Join(tmp, "captured-prompts.txt")
	required := filepath.Join(tmp, "required-file")
	writeRepairInlineFakeCLI(t, binDir, capturedPrompts, required)

	workflowsDir := filepath.Join(workdir, ".agent-runner", "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("create workflows dir: %v", err)
	}
	writeWorkflowFile(t, workflowsDir, "repair-inline-e2e-v1.0.yaml", `name: repair-inline-e2e
steps:
  - id: check
    command: "test -f `+required+`"
    repair:
      agent: claude_headless_smoke
      max: 1
      prompt: "Create the required file."
`)

	env := smokeCommandEnv(os.Environ(),
		"AGENT_RUNNER_NO_TUI=1",
		"HOME="+home,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output := runRepairBlockedCLI(t, runnerBin, workdir, env,
		"--headless", "--profile", "smoke_test", "repair-inline-e2e")
	if output.exitCode != 0 {
		t.Fatalf("expected the repaired run to succeed\n%s", output.text)
	}

	prompts, err := os.ReadFile(capturedPrompts)
	if err != nil {
		t.Fatalf("read captured prompts: %v", err)
	}
	if !strings.Contains(string(prompts), "Create the required file.") || !strings.Contains(string(prompts), "repair-evidence") {
		t.Fatalf("repair prompt missing the block prompt or the evidence block:\n%s", prompts)
	}

	sessionDirs, err := filepath.Glob(filepath.Join(home, ".agent-runner", "projects", "*", "runs", "repair-inline-e2e-*"))
	if err != nil || len(sessionDirs) != 1 {
		t.Fatalf("find session dir: matches=%v err=%v", sessionDirs, err)
	}
	sessionDir := sessionDirs[0]

	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	if !state.Completed || state.WarningCount != 0 || state.FailureReason != "" {
		t.Fatalf("expected a completed run with no warning and no failure reason, got %+v", state)
	}

	auditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	audit := string(auditData)
	for _, want := range []string{
		"[check] repair_attempt_start",
		"[check, attempt:1, repair] step_start",
		"[check, attempt:1, check] step_end",
		"[check] repair_attempt_end",
	} {
		if !strings.Contains(audit, want) {
			t.Fatalf("audit log missing %q:\n%s", want, audit)
		}
	}
	if strings.Count(audit, "repair_attempt_start") != 1 || strings.Count(audit, "repair_attempt_end") != 1 {
		t.Fatalf("expected exactly one repair attempt in audit log:\n%s", audit)
	}
	if !strings.Contains(audit, "[check] step_end") || !strings.Contains(audit, `"outcome":"success"`) {
		t.Fatalf("audit log missing the owning check's successful step_end:\n%s", audit)
	}
	if strings.Count(audit, "[check] step_start") != 1 || strings.Count(audit, "[check] step_end") != 1 {
		t.Fatalf("owning check must emit exactly one step_start and one step_end:\n%s", audit)
	}
}

// writeRepairInlineFakeCLI writes a fake "claude" executable that records
// its prompt and creates the required file, standing in for a repair agent
// that fixes the mechanical omission the check reported.
func writeRepairInlineFakeCLI(t *testing.T, binDir, capturedPrompts, required string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
prompt=""
for arg in "$@"; do
  prompt=$arg
done
printf '%s\n---\n' "$prompt" >> "` + capturedPrompts + `"
: > "` + required + `"
printf '{"type":"result","result":"created the required file"}\n'
`
	path := filepath.Join(binDir, "claude")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
}

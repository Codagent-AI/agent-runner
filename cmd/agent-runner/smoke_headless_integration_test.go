package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestSmokeTestHeadlessWorkflowIntegration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI stubs are POSIX shell scripts")
	}
	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	binDir := filepath.Join(tmp, "bin")
	smokeDir := filepath.Join(tmp, "smoke-files")
	runnerBin := filepath.Join(tmp, "agent-runner")

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}
	if err := os.MkdirAll(smokeDir, 0o755); err != nil {
		t.Fatalf("create smoke dir: %v", err)
	}
	writeSmokeProfileConfig(t, home)

	buildAgentRunner(t, repoRoot, runnerBin)
	writeFakeAgentCLIs(t, binDir, fakeExecutableNames(t))

	cmd := exec.Command(runnerBin, "--headless", "smoke-test-headless", "smoke_dir="+smokeDir)
	cmd.Dir = repoRoot
	cmd.Env = smokeCommandEnv(os.Environ(),
		"AGENT_RUNNER_NO_TUI=1",
		"HOME="+home,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("smoke-test-headless failed: %v\n%s", err, out.String())
	}

	sessionDir := latestRunDir(t, home, repoRoot)
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read run state: %v", err)
	}
	if !state.Completed {
		t.Fatalf("expected completed run state, got %+v", state)
	}

	wantSteps := headlessSmokeStepIDs()
	for _, stepID := range wantSteps {
		path := filepath.Join(smokeDir, stepID+".txt")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s to write %s: %v", stepID, path, err)
		}
		if got, want := string(data), "smoke-test value"; got != want {
			t.Fatalf("%s content = %q, want %q", path, got, want)
		}
	}

	assertSuccessfulHeadlessSteps(t, filepath.Join(sessionDir, "audit.log"), wantSteps)
}

func TestSmokeCommandEnvOverridesInheritedValues(t *testing.T) {
	env := smokeCommandEnv([]string{"HOME=/developer", "PATH=/original", "KEEP=value"}, "HOME=/sandbox", "PATH=/fake:/original")
	got := map[string]string{}
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("malformed environment entry %q", entry)
		}
		if _, exists := got[name]; exists {
			t.Fatalf("duplicate environment variable %q", name)
		}
		got[name] = value
	}
	if got["HOME"] != "/sandbox" || got["PATH"] != "/fake:/original" || got["KEEP"] != "value" {
		t.Fatalf("environment = %#v", got)
	}
}

// smokeCommandEnv replaces inherited variables instead of appending duplicate
// entries. A child process can otherwise resolve the first HOME entry and read
// a developer's config rather than the test sandbox.
func smokeCommandEnv(base []string, overrides ...string) []string {
	overrideNames := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		name, _, _ := strings.Cut(entry, "=")
		overrideNames[name] = struct{}{}
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrideNames[name]; !replaced {
			result = append(result, entry)
		}
	}
	return append(result, overrides...)
}

// writeSmokeProfileConfig completes the project-local smoke profile without
// relying on a developer's global Agent Runner config. The checked-in project
// config selects codex-kimi while defining the test-only smoke_test profile.
func writeSmokeProfileConfig(t *testing.T, home string) {
	t.Helper()
	path := filepath.Join(home, ".agent-runner", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create smoke config directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("profiles:\n  codex-kimi:\n    extends: smoke_test\n"), 0o600); err != nil {
		t.Fatalf("write smoke config: %v", err)
	}
}

func buildAgentRunner(t *testing.T, repoRoot, runnerBin string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", runnerBin, "./cmd/agent-runner")
	cmd.Dir = repoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("build agent-runner: %v\n%s", err, out.String())
	}
}

func headlessSmokeStepIDs() []string {
	names := cli.KnownCLIs()
	sort.Strings(names)
	steps := make([]string, 0, len(names))
	for _, name := range names {
		steps = append(steps, name+"-headless")
	}
	return steps
}

func fakeExecutableNames(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, name := range cli.KnownCLIs() {
		adapter, err := cli.Get(name)
		if err != nil {
			t.Fatalf("get CLI adapter %q: %v", name, err)
		}
		exe := cli.ExecutableName(name, adapter)
		if !seen[exe] {
			seen[exe] = true
			out = append(out, exe)
		}
	}
	sort.Strings(out)
	return out
}

func writeFakeAgentCLIs(t *testing.T, binDir string, executableNames []string) {
	t.Helper()
	script := `#!/bin/sh
set -eu

name=$(basename "$0")
prompt=""
if [ "$name" = "copilot" ]; then
  prev=""
  for arg in "$@"; do
    if [ "$prev" = "-p" ]; then
      prompt=$arg
      break
    fi
    prev=$arg
  done
else
  for arg in "$@"; do
    prompt=$arg
  done
fi

flat_prompt=$(printf '%s' "$prompt" | tr '\n' ' ')
target_file=$(printf '%s\n' "$flat_prompt" | sed -n 's/.*Create this exact file: \([^ ]*\) and write.*/\1/p')
value=$(printf '%s\n' "$flat_prompt" | sed -n 's/.*and write this exact value into it: \([^.]*\)\. Then reply.*/\1/p')
if [ -z "$target_file" ] || [ -z "$value" ]; then
  echo "fake $name: could not parse prompt: $flat_prompt" >&2
  exit 2
fi

mkdir -p "$(dirname "$target_file")"
printf '%s' "$value" > "$target_file"

case "$name" in
  claude)
    printf 'smoke-test value: %s\n' "$value"
    ;;
  codex)
    printf '{"type":"thread.started","thread_id":"codex-smoke-session"}\n'
    printf '{"type":"item.completed","item":{"type":"agent_message","text":"smoke-test value: %s"}}\n' "$value"
    printf '{"type":"turn.completed"}\n'
    ;;
  copilot)
    session_dir="$HOME/.copilot/session-state/copilot-smoke-session"
    mkdir -p "$session_dir"
    printf 'cwd: %s\n' "$PWD" > "$session_dir/workspace.yaml"
    printf 'smoke-test value: %s\n' "$value"
    ;;
  agent)
    printf '{"session_id":"cursor-smoke-session"}\n'
    printf '{"type":"result","result":"smoke-test value: %s"}\n' "$value"
    ;;
  opencode)
    printf '{"sessionID":"opencode-smoke-session"}\n'
    printf '{"type":"text","part":{"type":"text","text":"smoke-test value: %s"}}\n' "$value"
    ;;
  *)
    echo "unexpected fake CLI name: $name" >&2
    exit 2
    ;;
esac
`
	for _, name := range executableNames {
		path := filepath.Join(binDir, name)
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
}

func latestRunDir(t *testing.T, home, repoRoot string) string {
	t.Helper()
	runsDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(repoRoot), "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		t.Fatalf("read runs dir %s: %v", runsDir, err)
	}
	var latest os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "smoke-test-headless-") {
			continue
		}
		if latest == nil || entry.Name() > latest.Name() {
			latest = entry
		}
	}
	if latest == nil {
		t.Fatalf("no smoke-test-headless run found in %s", runsDir)
	}
	return filepath.Join(runsDir, latest.Name())
}

func assertSuccessfulHeadlessSteps(t *testing.T, auditPath string, stepIDs []string) {
	t.Helper()
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	successes := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		for _, stepID := range stepIDs {
			if strings.Contains(line, "["+stepID+"] step_end ") && strings.Contains(line, `"outcome":"success"`) {
				successes[stepID] = true
			}
		}
	}
	for _, stepID := range stepIDs {
		if !successes[stepID] {
			t.Fatalf("missing successful step_end for %s in %s:\n%s", stepID, auditPath, string(data))
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

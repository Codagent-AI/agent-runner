//go:build e2e_agents

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	gopty "github.com/creack/pty"
	"github.com/google/uuid"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/stateio"
)

const realAgentTimeout = 5 * time.Minute

func TestClaudeHeadlessRealAgentE2E(t *testing.T) {
	runRealHeadlessAgentE2E(t, "claude")
}

func TestCodexHeadlessRealAgentE2E(t *testing.T) {
	runRealHeadlessAgentE2E(t, "codex")
}

func TestCopilotHeadlessRealAgentE2E(t *testing.T) {
	runRealHeadlessAgentE2E(t, "copilot")
}

func TestCursorHeadlessRealAgentE2E(t *testing.T) {
	runRealHeadlessAgentE2E(t, "cursor")
}

func TestOpenCodeHeadlessRealAgentE2E(t *testing.T) {
	runRealHeadlessAgentE2E(t, "opencode")
}

func TestClaudeInteractiveRealAgentE2E(t *testing.T) {
	runRealInteractiveAgentE2E(t, "claude")
}

func TestCodexInteractiveRealAgentE2E(t *testing.T) {
	runRealInteractiveAgentE2E(t, "codex")
}

func TestCopilotInteractiveRealAgentE2E(t *testing.T) {
	runRealInteractiveAgentE2E(t, "copilot")
}

func TestCursorInteractiveRealAgentE2E(t *testing.T) {
	runRealInteractiveAgentE2E(t, "cursor")
}

func TestOpenCodeInteractiveRealAgentE2E(t *testing.T) {
	runRealInteractiveAgentE2E(t, "opencode")
}

func TestRealAgentTestEnvUsesFileCredentialStore(t *testing.T) {
	t.Setenv("AGENT_CLI_CREDENTIAL_STORE", "keychain")

	env := realAgentTestEnv(false)

	for _, entry := range env {
		if entry == "AGENT_CLI_CREDENTIAL_STORE=file" {
			return
		}
	}
	t.Fatalf("real-agent E2E environment does not select Cursor's file credential store: %q", env)
}

func runRealHeadlessAgentE2E(t *testing.T, agent string) {
	t.Helper()
	_, workdir, runnerBin := prepareRealAgentE2E(t, agent)
	token := "headless-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	workflowName := "real-" + agent + "-headless-e2e"
	freshPath := filepath.Join(workdir, "fresh-token.txt")
	resumePath := filepath.Join(workdir, "resumed-token.txt")
	workflowPath := filepath.Join(workdir, "workflow.yaml")
	workflow := fmt.Sprintf(`name: %s
description: "Real %s headless compatibility test"
steps:
  - id: %s-headless-fresh
    agent: %s_headless_smoke
    session: new
    prompt: "Write the exact value %s into %s. Remember that value for the next turn."
  - id: remove-headless-artifact
    command: "rm -f %s"
  - id: %s-headless-resume
    session: resume
    prompt: "Using the value you were asked to remember in the previous turn, write that exact value into %s."
`, workflowName, agent, agent, agent, token, freshPath, freshPath, agent, resumePath)
	writeRealAgentTestFile(t, workflowPath, []byte(workflow))

	ctx, cancel := context.WithTimeout(context.Background(), realAgentTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, runnerBin, "--headless", workflowPath)
	cmd.Dir = workdir
	cmd.Env = realAgentTestEnv(false)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("real %s headless E2E timed out after %s\n%s", agent, realAgentTimeout, output)
	}
	if err != nil {
		t.Fatalf("real %s headless E2E failed: %v\n%s", agent, err, output)
	}

	data, err := os.ReadFile(resumePath)
	if err != nil {
		t.Fatalf("real %s resumed session did not create %s: %v\n%s", agent, resumePath, err, output)
	}
	if got := strings.TrimSpace(string(data)); got != token {
		t.Fatalf("real %s resumed token = %q, want %q\n%s", agent, got, token, output)
	}
	assertRealAgentRunCompleted(t, workdir, workflowName, []string{agent + "-headless-fresh", "remove-headless-artifact", agent + "-headless-resume"})
}

func runRealInteractiveAgentE2E(t *testing.T, agent string) {
	t.Helper()
	_, workdir, runnerBin := prepareRealAgentE2E(t, agent)
	recallPhrase := strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	upperAgent := strings.ToUpper(agent)
	freshReady := "AR_" + upperAgent + "_FRESH_" + recallPhrase
	resumeReady := "AR_" + upperAgent + "_RESUME_" + recallPhrase
	workflowName := "real-" + agent + "-interactive-e2e"
	workflowPath := filepath.Join(t.TempDir(), "workflow.yaml")
	workflow := fmt.Sprintf(`name: %s
description: "Real %s interactive compatibility test"
steps:
  - id: %s-interactive-fresh
    agent: %s_interactive_smoke
    session: new
    prompt: "The recall phrase is %s. Remember it for the next turn. Reply with one line made by concatenating AR_, %s, _FRESH_, and the recall phrase."
`, workflowName, agent, agent, agent, recallPhrase, upperAgent)
	phases := []realAgentPTYPhase{{ready: freshReady}}
	stepIDs := []string{agent + "-interactive-fresh"}
	workflow += fmt.Sprintf(`  - id: %s-interactive-resume
    session: resume
    prompt: "Recall the phrase from the prior turn. Reply with one line made by concatenating AR_, %s, _RESUME_, and that phrase."
`, agent, upperAgent)
	phases = append(phases, realAgentPTYPhase{ready: resumeReady})
	stepIDs = append(stepIDs, agent+"-interactive-resume")
	writeRealAgentTestFile(t, workflowPath, []byte(workflow))
	cleanupNewRealAgentRuns(t, workdir, workflowName)

	cmd := exec.Command(runnerBin, "--headless", workflowPath)
	cmd.Dir = workdir
	cmd.Env = realAgentTestEnv(true)
	result, err := runRealAgentWorkflowInPTY(cmd, agent, phases, realAgentTimeout)
	if err != nil {
		t.Fatalf("real %s interactive E2E failed: %v\n%s", agent, err, result.output)
	}
	if len(phases) > 1 {
		resumedOutputObserved := terminalTextContains(ansi.Strip(result.output), resumeReady)
		if agent == "copilot" {
			resumedOutputObserved = resumedOutputObserved || copilotSessionContains(resumeReady)
		}
		if !resumedOutputObserved {
			t.Fatalf("real %s resumed output did not recall the prior token; want %q\n%s", agent, resumeReady, result.output)
		}
	}
	assertRealAgentRunCompleted(t, workdir, workflowName, stepIDs)
}

func prepareRealAgentE2E(t *testing.T, agent string) (repoRoot, workdir, runnerBin string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("real-agent PTY smoke tests require a POSIX terminal")
	}
	if !realAgentSelected(agent, os.Getenv("E2E_AGENTS")) {
		t.Skipf("%s is not selected by E2E_AGENTS", agent)
	}
	adapter, err := cli.Get(agent)
	if err != nil {
		t.Fatalf("get %s adapter: %v", agent, err)
	}
	executable := cli.ExecutableName(agent, adapter)
	if _, err := exec.LookPath(executable); err != nil {
		t.Fatalf("real-agent E2E requires %q for the %s adapter: %v", executable, agent, err)
	}

	repoRoot = findRepoRoot(t)
	workdir, err = os.MkdirTemp(repoRoot, ".agent-runner-e2e-")
	if err != nil {
		t.Fatalf("create real-agent workdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(workdir); err != nil {
			t.Errorf("clean real-agent workdir %s: %v", workdir, err)
		}
	})
	// macOS reports temporary directories through both /var and /private/var.
	// Canonicalize before Agent Runner encodes the project path so assertions
	// and cleanup address the same state directory the subprocess uses.
	canonicalWorkdir, err := filepath.EvalSymlinks(workdir)
	if err != nil {
		t.Fatalf("canonicalize real-agent workdir: %v", err)
	}
	workdir = canonicalWorkdir
	configData, err := os.ReadFile(filepath.Join(repoRoot, ".agent-runner", "config.yaml"))
	if err != nil {
		t.Fatalf("read smoke-test config: %v", err)
	}
	writeRealAgentTestFile(t, filepath.Join(workdir, ".agent-runner", "config.yaml"), configData)
	runnerBin = filepath.Join(t.TempDir(), "agent-runner")
	buildAgentRunner(t, repoRoot, runnerBin)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home directory: %v", err)
	}
	projectStateDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(workdir))
	t.Cleanup(func() {
		if err := os.RemoveAll(projectStateDir); err != nil {
			t.Errorf("clean real-agent E2E state %s: %v", projectStateDir, err)
		}
	})
	return repoRoot, workdir, runnerBin
}

func realAgentSelected(agent, selection string) bool {
	selection = strings.TrimSpace(selection)
	if selection == "" {
		return true
	}
	for _, selected := range strings.Split(selection, ",") {
		if strings.EqualFold(strings.TrimSpace(selected), agent) {
			return true
		}
	}
	return false
}

func realAgentTestEnv(interactive bool) []string {
	overrides := map[string]string{
		"AGENT_CLI_CREDENTIAL_STORE": "file",
		"AGENT_RUNNER_NO_TUI":        "1",
	}
	if interactive {
		overrides["TERM"] = "xterm-256color"
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			env = append(env, entry)
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

func writeRealAgentTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s parent: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

type realAgentPTYResult struct {
	output string
}

type realAgentPTYPhase struct {
	ready string
}

func runRealAgentWorkflowInPTY(cmd *exec.Cmd, agent string, phases []realAgentPTYPhase, timeout time.Duration) (realAgentPTYResult, error) {
	ptmx, err := gopty.StartWithSize(cmd, &gopty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return realAgentPTYResult{}, err
	}
	defer func() { _ = ptmx.Close() }()
	var transcript *os.File
	if path := os.Getenv("E2E_TRANSCRIPT"); path != "" {
		transcript, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return realAgentPTYResult{}, fmt.Errorf("open E2E transcript: %w", err)
		}
		defer func() { _ = transcript.Close() }()
	}

	type readResult struct {
		data []byte
		err  error
	}
	reads := make(chan readResult, 16)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, readErr := ptmx.Read(buf)
			result := readResult{err: readErr}
			if n > 0 {
				result.data = append([]byte(nil), buf[:n]...)
			}
			reads <- result
			if readErr != nil {
				return
			}
		}
	}()

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	probeTicker := time.NewTicker(200 * time.Millisecond)
	defer probeTicker.Stop()

	var raw strings.Builder
	ready := make([]bool, len(phases))
	trustHandled := make([]bool, len(phases))
	phaseBoundary := 0
	approvalBoundary := 0
	copilotApprovals := 0
	readDone := false
	waitDone := false
	var waitErr error
	var lastOpenCodeProbe time.Time
	advancePhase := func(i, boundary int) {
		ready[i] = true
		phaseBoundary = boundary
	}
	for !readDone || !waitDone {
		select {
		case result := <-reads:
			if len(result.data) > 0 {
				raw.Write(result.data)
				if transcript != nil {
					_, _ = transcript.Write(result.data)
				}
				plain := ansi.Strip(raw.String())
				currentPhase := 0
				for currentPhase < len(ready) && ready[currentPhase] {
					currentPhase++
				}
				if currentPhase < len(phases) && !trustHandled[currentPhase] &&
					strings.Contains(plain[phaseBoundary:], "Confirm folder trust") {
					trustHandled[currentPhase] = true
					// Accept Copilot's one-session default without persisting trust
					// for the user's repository.
					_, _ = ptmx.Write([]byte("\r"))
				}
				if agent == "copilot" && copilotApprovals < len(phases) &&
					strings.Contains(plain[approvalBoundary:], "Do you want to run this command?") {
					copilotApprovals++
					approvalBoundary = len(plain)
					// The adapter's exact command permission is the only approval
					// presented; accept it once for this invocation.
					_, _ = ptmx.Write([]byte("\r"))
				}
				for i, expected := range phases {
					if !ready[i] && terminalTextContains(plain, expected.ready) {
						advancePhase(i, len(plain))
					}
				}
			}
			if result.err != nil {
				readDone = true
			}
		case waitErr = <-waitCh:
			waitDone = true
		case <-probeTicker.C:
			if agent == "copilot" || agent == "opencode" {
				if agent == "opencode" && time.Since(lastOpenCodeProbe) < time.Second {
					continue
				}
				lastOpenCodeProbe = time.Now()
				for i, expected := range phases {
					var durableResponseObserved bool
					switch agent {
					case "copilot":
						durableResponseObserved = copilotSessionContains(expected.ready)
					case "opencode":
						durableResponseObserved = openCodeSessionContains(expected.ready)
					}
					if !ready[i] && durableResponseObserved {
						advancePhase(i, len(ansi.Strip(raw.String())))
						break
					}
				}
			}
		case <-timer.C:
			_ = cmd.Process.Kill()
			_ = ptmx.Close()
			return realAgentPTYResult{output: raw.String()}, fmt.Errorf("timed out after %s; readiness=%v", timeout, ready)
		}
	}

	result := realAgentPTYResult{output: raw.String()}
	for i := range phases {
		if !ready[i] {
			return result, fmt.Errorf("real-agent phase %d incomplete: ready=false", i)
		}
	}
	return result, waitErr
}

func copilotSessionContains(expected string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	paths, err := filepath.Glob(filepath.Join(home, ".copilot", "session-state", "*", "events.jsonl"))
	if err != nil {
		return false
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err == nil && bytes.Contains(data, []byte(expected)) {
			return true
		}
	}
	return false
}

func openCodeSessionContains(expected string) bool {
	since := time.Now().Add(-realAgentTimeout).UnixMilli()
	escapedExpected := strings.ReplaceAll(expected, "'", "''")
	query := fmt.Sprintf(
		"SELECT data FROM part WHERE time_created >= %d AND instr(data, '%s') > 0 LIMIT 1",
		since, escapedExpected,
	)
	output, err := exec.Command("opencode", "db", query, "--format", "json").Output()
	return err == nil && bytes.Contains(output, []byte(expected))
}

func terminalTextContains(rendered, expected string) bool {
	if strings.Contains(rendered, expected) {
		return true
	}
	compact := func(value string) string {
		return strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z':
				return r
			case r >= 'A' && r <= 'Z':
				return r
			case r >= '0' && r <= '9':
				return r
			case r == '_':
				return r
			default:
				return -1
			}
		}, value)
	}
	return strings.Contains(compact(rendered), compact(expected))
}

func assertRealAgentRunCompleted(t *testing.T, workdir, workflowName string, stepIDs []string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home directory: %v", err)
	}
	sessionDir := latestWorkflowRunDir(t, home, workdir, workflowName)
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read real-agent run state: %v", err)
	}
	if !state.Completed {
		t.Fatalf("expected completed real-agent run state, got %+v", state)
	}
	assertSuccessfulInteractiveSteps(t, filepath.Join(sessionDir, "audit.log"), stepIDs)
}

func cleanupNewRealAgentRuns(t *testing.T, workdir, workflowName string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home directory: %v", err)
	}
	runsDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(workdir), "runs")
	before := map[string]bool{}
	entries, err := os.ReadDir(runsDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read existing real-agent runs: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), workflowName+"-") {
			before[entry.Name()] = true
		}
	}
	t.Cleanup(func() {
		entries, err := os.ReadDir(runsDir)
		if err != nil {
			if !os.IsNotExist(err) {
				t.Errorf("read real-agent runs during cleanup: %v", err)
			}
			return
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), workflowName+"-") && !before[entry.Name()] {
				if err := os.RemoveAll(filepath.Join(runsDir, entry.Name())); err != nil {
					t.Errorf("clean real-agent E2E run %s: %v", entry.Name(), err)
				}
			}
		}
	})
}

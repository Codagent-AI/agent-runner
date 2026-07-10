//go:build e2e

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	cterm "github.com/charmbracelet/x/term"
	gopty "github.com/creack/pty"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/cli"
	"github.com/codagent/agent-runner/internal/stateio"
)

const (
	interactiveFixtureEnv = "AGENT_RUNNER_INTERACTIVE_E2E_FIXTURE"
	interactiveFixtureLog = "AGENT_RUNNER_INTERACTIVE_E2E_LOG"
)

var (
	continuationSuffixRE = regexp.MustCompile(`\b[0-9a-f]{32}\b`)
	sessionUUIDRE        = regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
)

func TestSmokeTestInteractiveAgentsWorkflowE2E(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive PTY smoke test requires a POSIX terminal")
	}

	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	binDir := filepath.Join(tmp, "bin")
	runnerBin := filepath.Join(tmp, "agent-runner")
	fixtureLog := filepath.Join(tmp, "interactive-fixture.jsonl")

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}

	buildAgentRunner(t, repoRoot, runnerBin)
	writeInteractiveAgentFixtures(t, binDir, fakeExecutableNames(t))

	cmd := exec.Command(runnerBin, "--headless", "smoke-test-interactive-agents")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"AGENT_RUNNER_NO_TUI=1",
		"HOME="+home,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		interactiveFixtureEnv+"=1",
		interactiveFixtureLog+"="+fixtureLog,
	)

	output, err := runInteractiveWorkflowInPTY(cmd, 45*time.Second)
	if err != nil {
		t.Fatalf("smoke-test-interactive-agents failed: %v\n%s", err, output)
	}
	if strings.Contains(output, "AGENT_RUNNER_CONTINUE_") {
		t.Fatalf("continuation marker leaked into terminal output:\n%s", output)
	}

	wantSteps := interactiveSmokeStepIDs()
	sessionDir := latestWorkflowRunDir(t, home, repoRoot, "smoke-test-interactive-agents")
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatalf("read run state: %v", err)
	}
	if !state.Completed {
		t.Fatalf("expected completed run state, got %+v", state)
	}
	assertSuccessfulInteractiveSteps(t, filepath.Join(sessionDir, "audit.log"), wantSteps)
	assertInteractiveFixtureInvocations(t, fixtureLog)
}

// TestInteractiveAgentFixtureProcess is re-executed by the fake CLI wrappers.
// In a normal go test process the environment guard makes it a no-op.
func TestInteractiveAgentFixtureProcess(t *testing.T) {
	if os.Getenv(interactiveFixtureEnv) != "1" {
		return
	}
	if err := runInteractiveAgentFixture(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "interactive agent fixture: %v\n", err)
		os.Exit(2)
	}
	os.Exit(0)
}

type interactiveFixtureInvocation struct {
	CLI       string   `json:"cli"`
	Resume    bool     `json:"resume"`
	SessionID string   `json:"session_id"`
	Args      []string `json:"args"`
}

func runInteractiveAgentFixture() error {
	name := os.Getenv("AGENT_RUNNER_INTERACTIVE_E2E_NAME")
	args := argsAfterDoubleDash(os.Args)
	resume := isFixtureResume(name, args)
	sessionID, err := prepareInteractiveFixtureSession(name, args, resume)
	if err != nil {
		return err
	}
	if err := appendInteractiveFixtureInvocation(interactiveFixtureInvocation{
		CLI: name, Resume: resume, SessionID: sessionID, Args: args,
	}); err != nil {
		return err
	}

	joined := strings.Join(args, "\n")
	suffix := continuationSuffixRE.FindString(joined)
	if suffix == "" {
		return fmt.Errorf("%s prompt did not contain a continuation suffix", name)
	}

	if !resume {
		_, _ = fmt.Fprintf(os.Stdout, "FAKE_AGENT_READY %s new\r\n", name)
		// Split and style the agent-generated marker to exercise the real PTY
		// output scanner across reads and ANSI cursor styling.
		_, _ = io.WriteString(os.Stdout, "AGENT_RUNNER_")
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(os.Stdout, "\x1b[32m\x1b[2CCONTINUE_")
		time.Sleep(20 * time.Millisecond)
		_, _ = io.WriteString(os.Stdout, suffix+"\x1b[0m\r\n")
		return waitForFixtureTermination()
	}

	oldState, err := cterm.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return fmt.Errorf("make fixture stdin raw: %w", err)
	}
	defer func() { _ = cterm.Restore(os.Stdin.Fd(), oldState) }()

	// Real full-screen agents enable mouse reporting before they can receive
	// wheel events. The E2E controller sends SS3, CSI, and a wheel report and
	// waits for this fixture to prove that all three reached the child.
	_, _ = io.WriteString(os.Stdout, "\x1b[?1000h\x1b[?1006h")
	_, _ = fmt.Fprintf(os.Stdout, "FAKE_AGENT_READY %s resume\r\n", name)
	wantInput := []byte("\x1bOA\x1b[B\x1b[<64;10;5M")
	gotInput := make([]byte, 0, len(wantInput))
	buf := make([]byte, 64)
	for !bytes.Contains(gotInput, wantInput) {
		n, readErr := os.Stdin.Read(buf)
		if n > 0 {
			gotInput = append(gotInput, buf[:n]...)
		}
		if readErr != nil {
			return fmt.Errorf("read terminal input: %w", readErr)
		}
	}
	_, _ = fmt.Fprintf(os.Stdout, "FAKE_AGENT_INPUT_OK %s\r\n", name)
	return waitForFixtureTermination()
}

func waitForFixtureTermination() error {
	_, err := io.Copy(io.Discard, os.Stdin)
	return err
}

func writeInteractiveAgentFixtures(t *testing.T, binDir string, executableNames []string) {
	t.Helper()
	testBinary := currentTestBinary(t)
	for _, name := range executableNames {
		script := fmt.Sprintf(`#!/bin/sh
set -eu
export AGENT_RUNNER_INTERACTIVE_E2E_NAME=%q
exec %q -test.run='^TestInteractiveAgentFixtureProcess$' -- "$@"
`, name, testBinary)
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755); err != nil {
			t.Fatalf("write interactive fake %s: %v", name, err)
		}
	}
}

func currentTestBinary(t *testing.T) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	return path
}

func runInteractiveWorkflowInPTY(cmd *exec.Cmd, timeout time.Duration) (string, error) {
	ptmx, err := gopty.StartWithSize(cmd, &gopty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return "", err
	}
	defer func() { _ = ptmx.Close() }()

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
	var output strings.Builder
	seenReady := map[string]bool{}
	seenInputOK := map[string]bool{}
	readDone := false
	waitDone := false
	var waitErr error

	for !readDone || !waitDone {
		select {
		case result := <-reads:
			if len(result.data) > 0 {
				output.Write(result.data)
				text := output.String()
				for _, executable := range []string{"claude", "codex", "copilot", "agent", "opencode"} {
					ready := "FAKE_AGENT_READY " + executable + " resume"
					if strings.Contains(text, ready) && !seenReady[executable] {
						seenReady[executable] = true
						// Deliberately split SS3 at the outer terminal boundary, then
						// send CSI and an SGR wheel event while mouse tracking is on.
						_, _ = ptmx.Write([]byte("\x1bO"))
						time.Sleep(10 * time.Millisecond)
						_, _ = ptmx.Write([]byte("A\x1b[B\x1b[<64;10;5M"))
					}
					inputOK := "FAKE_AGENT_INPUT_OK " + executable
					if strings.Contains(text, inputOK) && !seenInputOK[executable] {
						seenInputOK[executable] = true
						// The focused input-parser regression owns SS3 line-state
						// semantics. Clear the synthetic input here so this workflow
						// test independently exercises user-triggered continuation.
						_, _ = ptmx.Write([]byte("\x15/next\r"))
					}
				}
			}
			if result.err != nil {
				readDone = true
			}
		case waitErr = <-waitCh:
			waitDone = true
		case <-timer.C:
			_ = cmd.Process.Kill()
			_ = ptmx.Close()
			return output.String(), fmt.Errorf("timed out after %s", timeout)
		}
	}

	if len(seenReady) != len(cli.KnownCLIs()) {
		return output.String(), fmt.Errorf("received terminal readiness from %d agents, want %d", len(seenReady), len(cli.KnownCLIs()))
	}
	if len(seenInputOK) != len(cli.KnownCLIs()) {
		return output.String(), fmt.Errorf("verified terminal input for %d agents, want %d", len(seenInputOK), len(cli.KnownCLIs()))
	}
	return output.String(), waitErr
}

func argsAfterDoubleDash(args []string) []string {
	for i, arg := range args {
		if arg == "--" {
			return append([]string(nil), args[i+1:]...)
		}
	}
	return nil
}

func isFixtureResume(name string, args []string) bool {
	for _, arg := range args {
		switch name {
		case "claude":
			if arg == "--resume" {
				return true
			}
		case "codex":
			if arg == "resume" {
				return true
			}
		case "copilot", "agent":
			if strings.HasPrefix(arg, "--resume=") {
				return true
			}
		case "opencode":
			if arg == "-s" {
				return true
			}
		}
	}
	return false
}

func prepareInteractiveFixtureSession(name string, args []string, resume bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	if resume {
		switch name {
		case "claude":
			return valueAfter(args, "--resume"), nil
		case "codex":
			return sessionUUIDRE.FindString(strings.Join(args, "\n")), nil
		case "copilot", "agent":
			return valueWithPrefix(args, "--resume="), nil
		case "opencode":
			return valueAfter(args, "-s"), nil
		}
	}

	switch name {
	case "claude":
		id := valueAfter(args, "--session-id")
		encodedCWD := strings.NewReplacer("/", "-", ".", "-", "_", "-").Replace(cwd)
		path := filepath.Join(home, ".claude", "projects", encodedCWD, id+".jsonl")
		return id, writeFixtureFile(path, []byte("{}\n"))
	case "codex":
		id := "11111111-1111-4111-8111-111111111111"
		path := filepath.Join(home, ".codex", "sessions", "2026", "07", "10", "rollout-"+id+".jsonl")
		payload := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q}}`+"\n", id, cwd)
		return id, writeFixtureFile(path, []byte(payload))
	case "copilot":
		id := "copilot-interactive-smoke"
		path := filepath.Join(home, ".copilot", "session-state", id, "workspace.yaml")
		return id, writeFixtureFile(path, []byte("cwd: "+cwd+"\n"))
	case "agent":
		id := "22222222-2222-4222-8222-222222222222"
		path := filepath.Join(home, ".cursor", "chats", "smoke-workspace", id, "store.db")
		return id, writeFixtureFile(path, []byte("Workspace Path: "+cwd+"\n"))
	case "opencode":
		id := "ses_interactive_smoke"
		path := filepath.Join(home, ".local", "share", "opencode", "storage", "session_diff", id+".json")
		return id, writeFixtureFile(path, []byte("{}\n"))
	default:
		return "", fmt.Errorf("unsupported fake executable %q", name)
	}
}

func writeFixtureFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func valueAfter(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func valueWithPrefix(args []string, prefix string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	return ""
}

func appendInteractiveFixtureInvocation(invocation interactiveFixtureInvocation) error {
	path := os.Getenv(interactiveFixtureLog)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return json.NewEncoder(file).Encode(invocation)
}

func interactiveSmokeStepIDs() []string {
	names := cli.KnownCLIs()
	sort.Strings(names)
	steps := make([]string, 0, len(names)*2)
	for _, name := range names {
		steps = append(steps, name+"-interactive", name+"-interactive-resume")
	}
	return steps
}

func latestWorkflowRunDir(t *testing.T, home, repoRoot, workflow string) string {
	t.Helper()
	runsDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(repoRoot), "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		t.Fatalf("read runs dir %s: %v", runsDir, err)
	}
	var latest os.DirEntry
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), workflow+"-") {
			continue
		}
		if latest == nil || entry.Name() > latest.Name() {
			latest = entry
		}
	}
	if latest == nil {
		t.Fatalf("no %s run found in %s", workflow, runsDir)
	}
	return filepath.Join(runsDir, latest.Name())
}

func assertSuccessfulInteractiveSteps(t *testing.T, auditPath string, stepIDs []string) {
	t.Helper()
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	for _, stepID := range stepIDs {
		if !auditContainsSuccessfulStep(string(data), stepID) {
			t.Fatalf("missing successful step_end for %s in %s:\n%s", stepID, auditPath, data)
		}
	}
}

func auditContainsSuccessfulStep(data, stepID string) bool {
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		if strings.Contains(line, "["+stepID+"] step_end ") && strings.Contains(line, `"outcome":"success"`) {
			return true
		}
	}
	return false
}

func assertInteractiveFixtureInvocations(t *testing.T, logPath string) {
	t.Helper()
	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open interactive fixture log: %v", err)
	}
	defer func() { _ = file.Close() }()

	decoder := json.NewDecoder(file)
	counts := map[string]map[bool]int{}
	sessions := map[string]map[bool]string{}
	for {
		var invocation interactiveFixtureInvocation
		if err := decoder.Decode(&invocation); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode interactive fixture log: %v", err)
		}
		if counts[invocation.CLI] == nil {
			counts[invocation.CLI] = map[bool]int{}
			sessions[invocation.CLI] = map[bool]string{}
		}
		counts[invocation.CLI][invocation.Resume]++
		sessions[invocation.CLI][invocation.Resume] = invocation.SessionID
		if invocation.SessionID == "" {
			t.Errorf("%s resume=%t received no session ID; args=%v", invocation.CLI, invocation.Resume, invocation.Args)
		}
	}

	for _, executable := range []string{"claude", "codex", "copilot", "agent", "opencode"} {
		if got := counts[executable][false]; got != 1 {
			t.Errorf("%s fresh invocations = %d, want 1", executable, got)
		}
		if got := counts[executable][true]; got != 1 {
			t.Errorf("%s resume invocations = %d, want 1", executable, got)
		}
		if fresh, resumed := sessions[executable][false], sessions[executable][true]; fresh != resumed {
			t.Errorf("%s resumed session %q, want fresh session %q", executable, resumed, fresh)
		}
	}
}

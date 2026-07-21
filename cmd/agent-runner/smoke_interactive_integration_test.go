package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	cterm "github.com/charmbracelet/x/term"
	gopty "github.com/creack/pty"
	"golang.org/x/sys/unix"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/interactive"
	"github.com/codagent/agent-runner/internal/liverun"
	"github.com/codagent/agent-runner/internal/stateio"
)

const (
	interactiveFixtureEnv     = "AGENT_RUNNER_INTERACTIVE_E2E_FIXTURE"
	interactiveFixtureLog     = "AGENT_RUNNER_INTERACTIVE_E2E_LOG"
	jobControlFixtureEnv      = "AGENT_RUNNER_JOB_CONTROL_FIXTURE"
	terminalLeaseFixtureEnv   = "AGENT_RUNNER_TERMINAL_LEASE_E2E_FIXTURE"
	terminalLeaseWorkflowEnv  = "AGENT_RUNNER_TERMINAL_LEASE_E2E_WORKFLOW"
	durabilityRetryFixtureEnv = "AGENT_RUNNER_DURABILITY_RETRY_E2E_FIXTURE"
	directShellFixtureEnv     = "AGENT_RUNNER_DIRECT_SHELL_E2E_FIXTURE"
	directShellRunnerEnv      = "AGENT_RUNNER_DIRECT_SHELL_E2E_RUNNER"
	directShellWorkflowEnv    = "AGENT_RUNNER_DIRECT_SHELL_E2E_WORKFLOW"
	directShellTTYDeviceEnv   = "AGENT_RUNNER_DIRECT_SHELL_TTY_DEVICE"
	directShellChildEnv       = "AGENT_RUNNER_DIRECT_SHELL_CHILD"
)

var (
	sessionUUIDRE = regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
)

func TestInteractiveDirectHandoffWorkflowIntegration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive direct-handoff smoke test requires a POSIX terminal")
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
	writeInteractiveAgentFixtures(t, binDir, []string{"claude"})
	workflowPath := writeInteractiveDirectHandoffWorkflow(t, tmp)

	cmd := exec.Command(runnerBin, "--headless", workflowPath)
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
		t.Fatalf("interactive direct-handoff workflow failed: %v\n%s", err, output)
	}

	wantSteps := []string{"claude-interactive", "claude-interactive-resume"}
	sessionDir := latestWorkflowRunDir(t, home, repoRoot, "interactive-direct-handoff-integration")
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

func TestInteractiveDirectHandoffJobControl(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("job control requires a POSIX terminal")
	}
	shell, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is required for the real-shell job-control test")
	}
	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()
	runnerBin := filepath.Join(tmp, "agent-runner")
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildAgentRunner(t, repoRoot, runnerBin)
	writeInteractiveAgentFixtures(t, binDir, []string{"claude"})

	for _, mode := range []string{"cooperative", "external"} {
		t.Run(mode, func(t *testing.T) {
			home := filepath.Join(tmp, "home-"+mode)
			workflow := writeJobControlWorkflow(t, t.TempDir())
			command := exec.Command(shell, "-f")
			command.Dir = repoRoot
			command.Env = append(os.Environ(), "PS1=JOB_SHELL_READY> ", "HOME="+home,
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			ptmx, err := gopty.StartWithSize(command, &gopty.Winsize{Rows: 40, Cols: 120})
			if err != nil {
				t.Fatal(err)
			}
			defer ptmx.Close()
			output := make(chan string, 64)
			go scanPTYText(ptmx, output)
			waitForPTYText(t, output, "JOB_SHELL_READY>", 5*time.Second)
			launch := fmt.Sprintf("AGENT_RUNNER_NO_TUI=1 %s=1 %s=%s %s --headless %s\r",
				interactiveFixtureEnv, jobControlFixtureEnv, mode, runnerBin, workflow)
			_, _ = ptmx.WriteString(launch)
			ready := waitForPTYText(t, output, "JOB_CHILD_READY "+mode+" ", 30*time.Second)
			match := regexp.MustCompile(`JOB_CHILD_READY ` + mode + ` ([0-9]+)`).FindStringSubmatch(ready)
			if len(match) != 2 {
				t.Fatalf("parse child readiness from %q", ready)
			}
			pid, parseErr := strconv.Atoi(match[1])
			if parseErr != nil {
				t.Fatalf("parse child pid from %q: %v", ready, parseErr)
			}
			if mode == "external" {
				if err := unix.Kill(pid, unix.SIGSTOP); err != nil {
					t.Fatalf("stop child: %v", err)
				}
			}
			waitForPTYText(t, output, "suspended", 10*time.Second)
			if mode == "cooperative" {
				_, _ = ptmx.WriteString("bg\r")
				waitForPTYText(t, output, "JOB_SHELL_READY>", 5*time.Second)
				state, stateErr := exec.Command("ps", "-o", "state=", "-p", strconv.Itoa(pid)).Output()
				if stateErr != nil || !strings.HasPrefix(strings.TrimSpace(string(state)), "T") {
					t.Fatalf("child state after bg = %q, err %v; want stopped", state, stateErr)
				}
			}
			_, _ = ptmx.WriteString("fg\r")
			time.Sleep(100 * time.Millisecond)
			_, _ = ptmx.WriteString("R")
			waitForPTYText(t, output, "JOB_CHILD_RESUMED "+mode, 10*time.Second)
			waitForPTYText(t, output, "JOB_SHELL_READY>", 15*time.Second)
			_, _ = ptmx.WriteString("exit\r")
			_ = command.Wait()
		})
	}
}

func TestInteractiveShellDirectTerminalHandoffE2E(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("direct terminal handoff requires a POSIX terminal")
	}
	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()
	home := t.TempDir()
	runnerBin := filepath.Join(tmp, "agent-runner")
	buildAgentRunner(t, repoRoot, runnerBin)
	workflowPath := writeDirectShellWorkflow(t, tmp, currentTestBinary(t))

	command := exec.Command(currentTestBinary(t), "-test.run=^TestInteractiveShellDirectHandoffFixtureProcess$")
	command.Dir = repoRoot
	command.Env = append(os.Environ(),
		"HOME="+home,
		directShellFixtureEnv+"=1",
		directShellRunnerEnv+"="+runnerBin,
		directShellWorkflowEnv+"="+workflowPath,
	)
	output, err := runCommandInPTY(command, 30*time.Second)
	if err != nil {
		t.Fatalf("interactive shell direct-handoff workflow failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "DIRECT_SHELL_TTY_OK") {
		t.Fatalf("interactive shell did not confirm direct terminal inheritance:\n%s", output)
	}
	latestWorkflowRunDir(t, home, repoRoot, "interactive-shell-direct-handoff")
}

// TestInteractiveShellDirectHandoffFixtureProcess records the outer terminal
// device before launching Agent Runner. The workflow's shell child must inherit
// that same device rather than receiving a runner-created nested PTY.
func TestInteractiveShellDirectHandoffFixtureProcess(t *testing.T) {
	if os.Getenv(directShellFixtureEnv) == "" {
		return
	}
	device, err := terminalDeviceID(os.Stdin)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "inspect outer terminal: %v\n", err)
		os.Exit(2)
	}
	command := exec.Command(os.Getenv(directShellRunnerEnv), "--headless", os.Getenv(directShellWorkflowEnv))
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.Env = append(os.Environ(), directShellTTYDeviceEnv+"="+device)
	if err := command.Run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "run direct-shell workflow: %v\n", err)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestInteractiveShellDirectChildProcess(t *testing.T) {
	if os.Getenv(directShellChildEnv) == "" {
		return
	}
	want := os.Getenv(directShellTTYDeviceEnv)
	for name, file := range map[string]*os.File{"stdin": os.Stdin, "stdout": os.Stdout, "stderr": os.Stderr} {
		got, statErr := terminalDeviceID(file)
		if statErr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "inspect %s terminal: %v\n", name, statErr)
			os.Exit(2)
		}
		if got != want {
			_, _ = fmt.Fprintf(os.Stderr, "%s terminal device = %s, want inherited device %s\n", name, got, want)
			os.Exit(2)
		}
	}
	_, _ = fmt.Fprintln(os.Stdout, "DIRECT_SHELL_TTY_OK")
	os.Exit(0)
}

func terminalDeviceID(file *os.File) (string, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return "", err
	}
	return fmt.Sprint(stat.Rdev), nil
}

func TestInteractiveTerminalLeaseFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive terminal lease smoke test requires a POSIX terminal")
	}
	repoRoot := findRepoRoot(t)

	for _, mode := range []string{"release", "restore"} {
		t.Run(mode, func(t *testing.T) {
			tmp := t.TempDir()
			home := filepath.Join(tmp, "home")
			binDir := filepath.Join(tmp, "bin")
			fixtureLog := filepath.Join(tmp, "interactive-fixture.jsonl")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(home, ".agent-runner"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".agent-runner", "settings.yaml"), []byte("theme: dark\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			writeInteractiveAgentFixtures(t, binDir, []string{"claude"})
			workflowName := "interactive-terminal-lease-" + mode
			workflowPath := writeTerminalLeaseWorkflow(t, tmp, workflowName)

			cmd := exec.Command(currentTestBinary(t), "-test.run=^TestInteractiveTerminalLeaseFixtureProcess$")
			cmd.Dir = repoRoot
			cmd.Env = append(os.Environ(),
				"HOME="+home,
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TERM=xterm-256color",
				interactiveFixtureEnv+"=1",
				interactiveFixtureLog+"="+fixtureLog,
				terminalLeaseFixtureEnv+"="+mode,
				terminalLeaseWorkflowEnv+"="+workflowPath,
			)
			output, err := runCommandInPTY(cmd, 30*time.Second)
			if err == nil {
				t.Fatalf("%s failure injection unexpectedly succeeded\n%s", mode, output)
			}
			sessionDir := latestWorkflowRunDir(t, home, repoRoot, workflowName)
			if mode == "release" {
				if data, readErr := os.ReadFile(fixtureLog); readErr == nil && len(bytes.TrimSpace(data)) != 0 {
					t.Fatalf("fake CLI spawned despite release failure: %s", data)
				} else if readErr != nil && !os.IsNotExist(readErr) {
					t.Fatalf("read fixture log: %v", readErr)
				}
				return
			}

			assertSuccessfulInteractiveSteps(t, filepath.Join(sessionDir, "audit.log"), []string{"interactive"})
			assertInteractiveFixtureInvocationCount(t, fixtureLog, 1)
		})
	}
}

// TestInteractiveTerminalLeaseFixtureProcess runs the live TUI with a
// deterministic terminal program decorator. The outer test launches this
// helper in a PTY so Bubble Tea exercises the real release/restore wiring.
func TestInteractiveTerminalLeaseFixtureProcess(t *testing.T) {
	mode := os.Getenv(terminalLeaseFixtureEnv)
	if mode == "" {
		return
	}
	runnerExecutable, err := exec.LookPath("agent-runner")
	if err != nil {
		t.Fatalf("locate testscript agent-runner executable: %v", err)
	}
	if err := os.Setenv(agentRunnerExecutableEnv, runnerExecutable); err != nil {
		t.Fatalf("set runner executable: %v", err)
	}
	liveRunCoordinatorFactory = func(program *tea.Program, sessionDir string) *liverun.Coordinator {
		return liverun.NewCoordinator(&terminalFaultProgram{Program: program, mode: mode}, sessionDir)
	}
	result := handleRunWithResult([]string{os.Getenv(terminalLeaseWorkflowEnv)}, liveTUIOptions{quitOnDone: true, startInAltScreen: true})
	os.Exit(result.exitCode)
}

type terminalFaultProgram struct {
	*tea.Program
	mode string
}

func (p *terminalFaultProgram) ReleaseTerminal() error {
	if p.mode == "release" {
		return errors.New("injected release failure")
	}
	return p.Program.ReleaseTerminal()
}

func (p *terminalFaultProgram) RestoreTerminal() error {
	err := p.Program.RestoreTerminal()
	if p.mode == "restore" {
		return errors.Join(err, errors.New("injected restore failure"))
	}
	return err
}

func writeTerminalLeaseWorkflow(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name+".yaml")
	data := []byte(fmt.Sprintf(`name: %s
steps:
  - id: interactive
    agent: claude_interactive_smoke
    session: new
    prompt: "Complete through the control channel."
`, name))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeJobControlWorkflow(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "interactive-job-control.yaml")
	data := []byte(`name: interactive-job-control
steps:
  - id: job-control
    agent: claude_interactive_smoke
    session: new
    prompt: "Exercise job control."
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeDirectShellWorkflow(t *testing.T, dir, testBinary string) string {
	t.Helper()
	path := filepath.Join(dir, "interactive-shell-direct-handoff.yaml")
	data := []byte(fmt.Sprintf(`name: interactive-shell-direct-handoff
steps:
  - id: direct-shell
    command: |
      %s=1 %q -test.run='^TestInteractiveShellDirectChildProcess$'
    mode: interactive
`, directShellChildEnv, testBinary))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func scanPTYText(reader io.Reader, output chan<- string) {
	buffer := make([]byte, 4096)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			output <- string(buffer[:n])
		}
		if err != nil {
			close(output)
			return
		}
	}
}

func waitForPTYText(t *testing.T, output <-chan string, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var text strings.Builder
	for {
		select {
		case chunk, ok := <-output:
			if !ok {
				t.Fatalf("PTY closed before %q; output: %s", want, text.String())
			}
			text.WriteString(chunk)
			if strings.Contains(text.String(), want) {
				return text.String()
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for %q; output: %s", want, text.String())
		}
	}
}

func writeInteractiveDirectHandoffWorkflow(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "interactive-direct-handoff-integration.yaml")
	data := []byte(`name: interactive-direct-handoff-integration
description: "Deterministic direct terminal handoff fixture"
steps:
  - id: claude-interactive
    agent: claude_interactive_smoke
    session: new
    prompt: "Wait for the integration controller."
  - id: claude-interactive-resume
    session: resume
    prompt: "Wait for the integration controller again."
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write direct-handoff integration workflow: %v", err)
	}
	return path
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
	Token     string   `json:"token"`
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
	if mode := os.Getenv(jobControlFixtureEnv); mode != "" {
		return runJobControlFixture(mode)
	}
	invocationNumber, err := appendInteractiveFixtureInvocation(&interactiveFixtureInvocation{
		CLI: name, Resume: resume, SessionID: sessionID,
		Token: os.Getenv(interactive.EnvControlToken), Args: args,
	})
	if err != nil {
		return err
	}

	for _, key := range []string{interactive.EnvControlSocket, interactive.EnvRunID, interactive.EnvStepID, interactive.EnvControlToken} {
		if os.Getenv(key) == "" {
			return fmt.Errorf("%s did not receive %s", name, key)
		}
	}

	if !resume {
		_, _ = fmt.Fprintf(os.Stdout, "FAKE_AGENT_READY %s new\r\n", name)
		if os.Getenv(durabilityRetryFixtureEnv) == "1" && invocationNumber == 1 {
			if _, err := interactive.SendControlEventFromEnvironment(context.Background(), interactive.MessageCompleteStep, os.Getenv); err != nil {
				return fmt.Errorf("send completion without committed turn: %w", err)
			}
			return waitForFixtureTermination()
		}
		if err := completeInteractiveFixture(); err != nil {
			return err
		}
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
	_, _ = os.Stdout.WriteString("\x1b[?1000h\x1b[?1006h")
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
	if err := completeInteractiveFixture(); err != nil {
		return err
	}
	return waitForFixtureTermination()
}

func runJobControlFixture(mode string) error {
	oldState, err := cterm.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return err
	}
	defer func() { _ = cterm.Restore(os.Stdin.Fd(), oldState) }()
	_, _ = fmt.Fprintf(os.Stdout, "JOB_CHILD_READY %s %d\r\n", mode, os.Getpid())
	if mode == "cooperative" {
		if err := unix.Kill(-unix.Getpgrp(), unix.SIGSTOP); err != nil {
			return err
		}
	}
	input := []byte{0}
	if _, err := io.ReadFull(os.Stdin, input); err != nil {
		return err
	}
	if input[0] != 'R' {
		return fmt.Errorf("terminal mode was not restored for child: read %q", input)
	}
	_, _ = fmt.Fprintf(os.Stdout, "JOB_CHILD_RESUMED %s\r\n", mode)
	if err := completeInteractiveFixture(); err != nil {
		return err
	}
	return waitForFixtureTermination()
}

func completeInteractiveFixture() error {
	if _, err := interactive.SendControlEventFromEnvironment(context.Background(), interactive.MessageCompleteStep, os.Getenv); err != nil {
		return fmt.Errorf("send completion: %w", err)
	}
	if _, err := interactive.SendControlEventFromEnvironment(context.Background(), interactive.MessageTurnCommitted, os.Getenv); err != nil {
		return fmt.Errorf("send committed turn: %w", err)
	}
	return nil
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
				for _, executable := range []string{"claude"} {
					ready := "FAKE_AGENT_READY " + executable + " resume"
					if strings.Contains(text, ready) && !seenReady[executable] {
						seenReady[executable] = true
						// Deliberately split SS3 at the outer terminal boundary, then
						// send CSI and an SGR wheel event while mouse tracking is on.
						_, _ = ptmx.WriteString("\x1bO")
						time.Sleep(10 * time.Millisecond)
						_, _ = ptmx.WriteString("A\x1b[B\x1b[<64;10;5M")
					}
					inputOK := "FAKE_AGENT_INPUT_OK " + executable
					if strings.Contains(text, inputOK) && !seenInputOK[executable] {
						seenInputOK[executable] = true
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

	if len(seenReady) != 1 {
		return output.String(), fmt.Errorf("received terminal readiness from %d agents, want 1", len(seenReady))
	}
	if len(seenInputOK) != 1 {
		return output.String(), fmt.Errorf("verified terminal input for %d agents, want 1", len(seenInputOK))
	}
	return output.String(), waitErr
}

func runCommandInPTY(cmd *exec.Cmd, timeout time.Duration) (string, error) {
	ptmx, err := gopty.StartWithSize(cmd, &gopty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return "", err
	}
	defer func() { _ = ptmx.Close() }()

	outputCh := make(chan []byte, 1)
	go func() {
		output, _ := io.ReadAll(ptmx)
		outputCh <- output
	}()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var output []byte
	var waitErr error
	for outputCh != nil || waitCh != nil {
		select {
		case output = <-outputCh:
			outputCh = nil
		case waitErr = <-waitCh:
			waitCh = nil
		case <-timer.C:
			_ = cmd.Process.Kill()
			_ = ptmx.Close()
			select {
			case output = <-outputCh:
			case <-time.After(time.Second):
			}
			return string(output), fmt.Errorf("timed out after %s", timeout)
		}
	}
	return string(output), waitErr
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

func appendInteractiveFixtureInvocation(invocation *interactiveFixtureInvocation) (int, error) {
	path := os.Getenv(interactiveFixtureLog)
	invocationNumber := 1
	if data, err := os.ReadFile(path); err == nil {
		invocationNumber += bytes.Count(data, []byte{'\n'})
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()
	if err := json.NewEncoder(file).Encode(invocation); err != nil {
		return 0, err
	}
	return invocationNumber, nil
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

	for _, executable := range []string{"claude"} {
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

func assertInteractiveFixtureInvocationCount(t *testing.T, logPath string, want int) {
	t.Helper()
	invocations := readInteractiveFixtureInvocations(t, logPath)
	if len(invocations) != want {
		t.Fatalf("interactive fixture invocations = %d, want %d", len(invocations), want)
	}
}

func readInteractiveFixtureInvocations(t *testing.T, logPath string) []interactiveFixtureInvocation {
	t.Helper()
	file, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open interactive fixture log: %v", err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	var invocations []interactiveFixtureInvocation
	for {
		var invocation interactiveFixtureInvocation
		if err := decoder.Decode(&invocation); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode interactive fixture log: %v", err)
		}
		invocations = append(invocations, invocation)
	}
	return invocations
}

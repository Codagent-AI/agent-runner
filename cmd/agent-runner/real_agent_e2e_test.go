//go:build e2e_agents

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

func TestClaudeAgentCallRealAgentE2E(t *testing.T) {
	_, workdir, runnerBin := prepareRealAgentE2E(t, "claude")
	workflowName := "real-claude-agent-call-e2e"
	childPath := filepath.Join(workdir, "call-child-response.txt")
	parentPath := filepath.Join(workdir, "call-parent-response.txt")
	reusePath := filepath.Join(workdir, "call-reused-session.txt")
	workflowPath := filepath.Join(workdir, "workflow.yaml")
	workflow := fmt.Sprintf(`name: %s
description: "Real Runner-owned call_agent tool and named-session reuse test"
sessions:
  - name: called-claude
    agent: claude_headless_smoke
steps:
  - id: claude-call-parent
    agent: claude_headless_smoke
    session: new
    prompt: |
      Use call_agent exactly once with the named session called-claude. Ask the called agent to invent a token made of exactly two unusual lowercase words joined by one underscore, write only that token into %s, remember it, and reply with only that token. After call_agent returns, write its exact response into %s and reply with only that same response. Do not use a proprietary subagent tool.
  - id: claude-call-session-reuse
    session: called-claude
    prompt: "Write only the exact token you invented in the previous turn into %s, then reply with only that token."
`, workflowName, childPath, parentPath, reusePath)
	writeRealAgentTestFile(t, workflowPath, []byte(workflow))
	cleanupNewRealAgentRuns(t, workdir, workflowName)

	ctx, cancel := context.WithTimeout(context.Background(), realAgentTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, runnerBin, "--headless", workflowPath)
	cmd.Dir = workdir
	cmd.Env = realAgentTestEnv(false)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("real Claude agent-call E2E timed out after %s\n%s", realAgentTimeout, output)
	}
	if err != nil {
		t.Fatalf("real Claude agent-call E2E failed: %v\n%s", err, output)
	}

	child := readRealAgentToken(t, childPath, output)
	parent := readRealAgentToken(t, parentPath, output)
	reused := readRealAgentToken(t, reusePath, output)
	if child != parent || child != reused {
		t.Fatalf("agent-call response/session reuse mismatch: child=%q parent=%q reused=%q\n%s", child, parent, reused, output)
	}
	if !regexp.MustCompile(`^[a-z]{2,32}_[a-z]{2,32}$`).MatchString(child) {
		t.Fatalf("agent-call token = %q, want two lowercase words joined by underscore\n%s", child, output)
	}
	assertRealAgentRunCompleted(t, workdir, workflowName, []string{"claude-call-parent", "claude-call-session-reuse"})
}

func readRealAgentToken(t *testing.T, path string, output []byte) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read real-agent token %s: %v\n%s", path, err, output)
	}
	return strings.TrimSpace(string(data))
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

// TestOpenCodeInteractiveRealAgentE2E proves the runner refuses interactive
// OpenCode steps up front. Interactive OpenCode is deliberately unsupported:
// a resumed OpenCode TUI never submits the runner's prompt
// (anomalyco/opencode#37536), which stalls workflows silently, so the adapter
// rejects interactive invocations before any TUI spawns.
func TestOpenCodeInteractiveRealAgentE2E(t *testing.T) {
	_, workdir, runnerBin := prepareRealAgentE2E(t, "opencode")
	workflowName := "real-opencode-interactive-e2e"
	workflowPath := filepath.Join(t.TempDir(), "workflow.yaml")
	workflow := fmt.Sprintf(`name: %s
description: "Real opencode interactive rejection test"
steps:
  - id: opencode-interactive-fresh
    agent: opencode_interactive_smoke
    session: new
    prompt: "Say hello."
`, workflowName)
	writeRealAgentTestFile(t, workflowPath, []byte(workflow))
	cleanupNewRealAgentRuns(t, workdir, workflowName)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, runnerBin, "--headless", workflowPath)
	cmd.Dir = workdir
	cmd.Env = realAgentTestEnv(false)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("opencode interactive rejection E2E timed out\n%s", output)
	}
	if err == nil {
		t.Fatalf("expected interactive opencode workflow to fail fast, got success\n%s", output)
	}
	if !strings.Contains(string(output), "opencode does not support interactive steps") {
		t.Fatalf("run output does not name the interactive opencode rejection (exit err %v):\n%s", err, output)
	}
}

// TestOpenCodeCompletionSurfacesInteractiveRealAgentE2E exercises OpenCode's
// real TUI directly because Agent Runner rejects interactive OpenCode workflows
// until anomalyco/opencode#37536 is fixed. Fresh sessions can still prove that
// both completion surfaces are correctly injected into the adapter invocation.
func TestOpenCodeCompletionSurfacesInteractiveRealAgentE2E(t *testing.T) {
	_, workdir, runnerBin := prepareRealAgentE2E(t, "opencode")
	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "slash command", input: "/agent-runner:next"},
		{name: "prompted continuation", input: "Please continue to the next workflow step now."},
	} {
		t.Run(test.name, func(t *testing.T) {
			runOpenCodeCompletionSurfaceE2E(t, workdir, runnerBin, test.input)
		})
	}
}

func runOpenCodeCompletionSurfaceE2E(t *testing.T, workdir, runnerBin, input string) {
	t.Helper()
	socketDir, err := os.MkdirTemp("/tmp", "ar-oc-")
	if err != nil {
		t.Fatalf("create short OpenCode control directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	socketPath := filepath.Join(socketDir, "control.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake control endpoint: %v", err)
	}
	defer func() { _ = listener.Close() }()
	type controlRequest struct {
		Type      string `json:"type"`
		RunID     string `json:"run_id"`
		StepID    string `json:"step_id"`
		Token     string `json:"token"`
		RequestID string `json:"request_id"`
	}
	requests := make(chan controlRequest, 1)
	serveErrors := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serveErrors <- err
			return
		}
		defer func() { _ = connection.Close() }()
		var request controlRequest
		if err := json.NewDecoder(connection).Decode(&request); err != nil {
			serveErrors <- err
			return
		}
		if err := json.NewEncoder(connection).Encode(map[string]any{"ok": true, "receipt": request.RequestID}); err != nil {
			serveErrors <- err
			return
		}
		requests <- request
	}()

	const runID = "opencode-surface-run"
	const stepID = "opencode-surface-step"
	const token = "opencode-surface-token"
	marker := "AR_OPENCODE_SURFACE_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10] + "_READY"
	command := &cli.CompletionCommand{Executable: runnerBin, Args: []string{"step", "complete"}}
	prompt := fmt.Sprintf("Print %s on its own line, then wait for the user. When the user asks you to continue, run the exact shell command %s and finish the response.", marker, command.ShellCommand())
	args := (&cli.OpenCodeAdapter{}).BuildArgs(&cli.BuildArgsInput{
		Prompt:            prompt,
		Context:           cli.ContextInteractive,
		CompletionCommand: command,
	})
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = workdir
	cmd.Env = append(realAgentTestEnv(true),
		"AGENT_RUNNER_CONTROL_SOCKET="+socketPath,
		"AGENT_RUNNER_RUN_ID="+runID,
		"AGENT_RUNNER_STEP_ID="+stepID,
		"AGENT_RUNNER_CONTROL_TOKEN="+token,
	)
	ptmx, err := gopty.StartWithSize(cmd, &gopty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		t.Fatalf("start real OpenCode TUI: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = ptmx.Close()
		_ = cmd.Wait()
	}()

	output := make(chan []byte, 32)
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, readErr := ptmx.Read(buffer)
			if n > 0 {
				output <- append([]byte(nil), buffer[:n]...)
			}
			if readErr != nil {
				close(output)
				return
			}
		}
	}()

	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	var transcript strings.Builder
	ready := false
	for {
		select {
		case chunk, ok := <-output:
			if !ok {
				t.Fatalf("real OpenCode TUI exited before completion event\n%s", transcript.String())
			}
			transcript.Write(chunk)
			if !ready && strings.Contains(ansi.Strip(transcript.String()), marker) {
				ready = true
				if strings.HasPrefix(input, "/") {
					_, _ = ptmx.Write([]byte("/"))
					time.Sleep(200 * time.Millisecond)
					_, _ = ptmx.Write([]byte(strings.TrimPrefix(input, "/")))
				} else {
					_, _ = ptmx.Write([]byte(input))
				}
				time.Sleep(100 * time.Millisecond)
				_, _ = ptmx.Write([]byte("\r"))
			}
		case request := <-requests:
			if request.Type != "complete_step" || request.RunID != runID || request.StepID != stepID || request.Token != token || request.RequestID == "" {
				t.Fatalf("OpenCode completion request = %#v", request)
			}
			return
		case err := <-serveErrors:
			t.Fatalf("serve OpenCode control request: %v\n%s", err, transcript.String())
		case <-timer.C:
			t.Fatalf("real OpenCode %q completion timed out; ready=%v\n%s", input, ready, transcript.String())
		}
	}
}

func TestRealAgentTestEnvUsesFileCredentialStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("AGENT_CLI_CREDENTIAL_STORE", "keychain")

	env := realAgentTestEnv(false)

	for _, entry := range env {
		if entry == "AGENT_CLI_CREDENTIAL_STORE=file" {
			return
		}
	}
	t.Fatalf("real-agent E2E environment does not select Cursor's file credential store: %q", env)
}

func TestRealAgentTestEnvLoadsClaudeOAuthTokenFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	writeRealAgentTestFile(t, filepath.Join(home, ".config", "agent-runner", "claude-oauth-token"), []byte("test-setup-token\n"))

	env := realAgentTestEnv(false)

	for _, entry := range env {
		if entry == "CLAUDE_CODE_OAUTH_TOKEN=test-setup-token" {
			return
		}
	}
	t.Fatal("real-agent E2E environment did not load the configured Claude OAuth token")
}

func TestRealAgentTestEnvScrubsEnclosingSessionState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "enclosing-session")
	t.Setenv("AGENT_RUNNER_EXECUTABLE", "/somewhere/else/agent-runner")

	scrubbed := map[string]bool{
		"CLAUDECODE":                true,
		"CLAUDE_CODE_CHILD_SESSION": true,
		"CLAUDE_CODE_ENTRYPOINT":    true,
		"CLAUDE_CODE_SESSION_ID":    true,
		"AGENT_RUNNER_EXECUTABLE":   true,
	}
	for _, entry := range realAgentTestEnv(true) {
		key, _, _ := strings.Cut(entry, "=")
		if scrubbed[key] {
			t.Errorf("real-agent E2E environment leaks %s", entry)
		}
	}
}

func TestDurabilityTimeoutResumeUsesFreshCredentialE2E(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("durability retry E2E requires a POSIX terminal")
	}
	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	binDir := filepath.Join(tmp, "bin")
	runnerBin := filepath.Join(tmp, "agent-runner")
	fixtureLog := filepath.Join(tmp, "interactive-fixture.jsonl")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildAgentRunner(t, repoRoot, runnerBin)
	writeInteractiveAgentFixtures(t, binDir, []string{"claude"})
	workflowName := "durability-timeout-resume-e2e"
	workflowPath := writeTerminalLeaseWorkflow(t, tmp, workflowName)
	env := append(os.Environ(),
		"AGENT_RUNNER_NO_TUI=1",
		"HOME="+home,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		interactiveFixtureEnv+"=1",
		interactiveFixtureLog+"="+fixtureLog,
		durabilityRetryFixtureEnv+"=1",
	)

	first := exec.Command(runnerBin, "--headless", workflowPath)
	first.Dir = repoRoot
	first.Env = env
	firstOutput, firstErr := runCommandInPTY(first, 45*time.Second)
	if firstErr == nil {
		t.Fatalf("first attempt succeeded, want durability timeout\n%s", firstOutput)
	}

	sessionDir := latestWorkflowRunDir(t, home, repoRoot, workflowName)
	auditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(auditData), "durability_failure") {
		t.Fatalf("first attempt did not record a durability timeout:\n%s", auditData)
	}
	resumed := exec.Command(runnerBin, "--headless", "--resume", filepath.Base(sessionDir))
	resumed.Dir = repoRoot
	resumed.Env = env
	resumedOutput, resumedErr := runCommandInPTY(resumed, 30*time.Second)
	if resumedErr != nil {
		t.Fatalf("resumed durability attempt failed: %v\n%s", resumedErr, resumedOutput)
	}
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Completed {
		t.Fatalf("resumed workflow did not complete: %#v", state)
	}

	invocations := readInteractiveFixtureInvocations(t, fixtureLog)
	if len(invocations) != 2 {
		t.Fatalf("fixture invocations = %d, want 2", len(invocations))
	}
	if invocations[0].Token == "" || invocations[1].Token == "" || invocations[0].Token == invocations[1].Token {
		t.Fatalf("completion credentials were not freshly issued: first=%q resumed=%q", invocations[0].Token, invocations[1].Token)
	}
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

// runRealInteractiveAgentE2E proves completing-turn durability: the agent
// INVENTS a recall phrase during the same turn that runs the completion
// command, so the resumed session can only repeat it if the completing turn
// itself survived process termination. A phrase supplied in the prompt would
// only prove the prompt turn was persisted. The harness never learns the
// phrase in advance; it captures whatever the agent stated after the
// per-phase marker prefix (terminal output first, session store fallback).
func runRealInteractiveAgentE2E(t *testing.T, agent string) {
	t.Helper()
	_, workdir, runnerBin := prepareRealAgentE2E(t, agent)
	nonce := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	upperAgent := strings.ToUpper(agent)
	// The prompts spell the marker parts as prose ("AR, then CLAUDE, then
	// ...") so neither the raw prompt echo nor its compacted form ever
	// assembles the contiguous marker prefix; only the agent's answer does.
	freshPrefix := "AR_" + upperAgent + "_FRESH_" + nonce + "_"
	resumePrefix := "AR_" + upperAgent + "_RESUME_" + nonce + "_"
	freshPrompt := fmt.Sprintf("Invent a recall phrase made of exactly two unusual lowercase English words that appear nowhere in these instructions. State the phrase exactly once as a single line built from these seven parts joined by single underscores. AR, then %s, then FRESH, then %s, then the first word, then the second word, then END. After stating that line, wait for the user. Do not run the Agent Runner completion command yourself; the user will invoke the native Agent Runner completion command.", upperAgent, nonce)
	resumePrompt := fmt.Sprintf("Reply with a single line built from these seven parts joined by single underscores. AR, then %s, then RESUME, then %s, then the first word, then the second word of the recall phrase you invented in the previous turn, then END. Then wait for the user without running the Agent Runner completion command yourself.", upperAgent, nonce)
	workflowName := "real-" + agent + "-interactive-e2e"
	workflowPath := filepath.Join(t.TempDir(), "workflow.yaml")
	workflow := fmt.Sprintf(`name: %s
description: "Real %s interactive compatibility test"
steps:
  - id: %s-interactive-fresh
    agent: %s_interactive_smoke
    session: new
    prompt: "%s"
  - id: %s-interactive-resume
    session: resume
    prompt: "%s"
`, workflowName, agent, agent, agent, freshPrompt, agent, resumePrompt)
	phases := []realAgentPTYPhase{
		{markerPrefix: freshPrefix, afterReady: realAgentExplicitCompletionCommand(agent)},
		{markerPrefix: resumePrefix, afterReady: "Please continue to the next workflow step now."},
	}
	stepIDs := []string{agent + "-interactive-fresh", agent + "-interactive-resume"}
	writeRealAgentTestFile(t, workflowPath, []byte(workflow))
	cleanupNewRealAgentRuns(t, workdir, workflowName)

	cmd := exec.Command(runnerBin, "--headless", workflowPath)
	cmd.Dir = workdir
	cmd.Env = realAgentTestEnv(true)
	result, err := runRealAgentWorkflowInPTY(cmd, agent, phases, realAgentTimeout)
	if err != nil {
		t.Fatalf("real %s interactive E2E failed: %v\n%s", agent, err, result.output)
	}
	if agent == "cursor" && result.commandApprovals != 0 {
		t.Fatalf("real cursor interactive run surfaced %d approval prompt(s) (Run this command?); the narrow Shell(<abs>:step complete) pre-approval in the private CURSOR_CONFIG_DIR must cover the completion command without prompting\n%s", result.commandApprovals, result.output)
	}
	plain := ansi.Strip(result.output)
	freshPhrase := capturedRealAgentPhrase(agent, plain, freshPrefix)
	if freshPhrase == "" {
		t.Fatalf("real %s fresh turn never stated an invented recall phrase after marker %q\n%s", agent, freshPrefix, result.output)
	}
	resumePhrase := capturedRealAgentPhrase(agent, plain, resumePrefix)
	if resumePhrase == "" {
		t.Fatalf("real %s resumed turn never answered with marker %q\n%s", agent, resumePrefix, result.output)
	}
	if !strings.Contains(resumePhrase, freshPhrase) && !strings.Contains(freshPhrase, resumePhrase) {
		t.Fatalf("real %s resumed answer did not recall the phrase invented in the completing turn: fresh=%q resume=%q\n%s", agent, freshPhrase, resumePhrase, result.output)
	}
	assertRealAgentRunCompleted(t, workdir, workflowName, stepIDs)
}

func realAgentExplicitCompletionCommand(agent string) string {
	if agent == "codex" {
		// Current Codex does not expose plugin commands or custom prompts as
		// slash commands. Skills are its supported explicit invocation surface.
		return "$agent-runner-next"
	}
	return "/agent-runner:next"
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
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			tokenPath := filepath.Join(home, ".config", "agent-runner", "claude-oauth-token")
			if data, err := os.ReadFile(tokenPath); err == nil {
				if token := strings.TrimSpace(string(data)); token != "" {
					overrides["CLAUDE_CODE_OAUTH_TOKEN"] = token
				}
			}
		}
	}
	if interactive {
		overrides["TERM"] = "xterm-256color"
	}
	// The suite must be hermetic no matter which terminal launches it. An
	// inherited executable override would point the completion client and
	// watchdog at an installed binary instead of the freshly built one, and
	// enclosing-session markers make a spawned interactive CLI silently change
	// behavior (Claude skips transcript persistence, breaking the resume
	// steps). The marker names come from the adapters' own drop lists so the
	// harness cannot drift from what production spawns scrub.
	scrubbed := map[string]bool{"AGENT_RUNNER_EXECUTABLE": true}
	for _, name := range cli.KnownCLIs() {
		if adapter, err := cli.Get(name); err == nil {
			for _, key := range cli.DropSpawnEnvVars(adapter) {
				scrubbed[key] = true
			}
		}
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; replaced || scrubbed[key] {
			continue
		}
		env = append(env, entry)
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
	// commandApprovals counts CLI-native shell-command approval prompts:
	// answered for Copilot, and only counted for Cursor — where the narrow
	// pre-approval means any prompt at all is a regression.
	commandApprovals int
}

type realAgentPTYPhase struct {
	// markerPrefix is the known head of the phase's answer line; the agent
	// completes it with an invented two-word phrase the harness captures.
	markerPrefix string
	afterReady   string
}

// realAgentMarkerRegexp matches a phase marker: the known prefix followed by
// the agent's invented two-lowercase-word phrase joined by an underscore. The
// first submatch captures the phrase.
func realAgentMarkerRegexp(prefix string) *regexp.Regexp {
	return regexp.MustCompile(regexp.QuoteMeta(prefix) + `([a-z]{2,32}_[a-z]{2,32})_END`)
}

// longestMarkerPhrase returns the longest captured phrase among all matches.
// Terminal transcripts accumulate partial streaming frames, so an early
// truncated rendering of the marker stays in the transcript forever; the
// longest occurrence is the fully streamed one.
func longestMarkerPhrase(text string, re *regexp.Regexp) string {
	longest := ""
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		if len(match[1]) > len(longest) {
			longest = match[1]
		}
	}
	return longest
}

// captureMarkerPhrase extracts the invented phrase from rendered terminal
// text, falling back to the compacted form only when line wrapping broke the
// marker across rows.
func captureMarkerPhrase(rendered string, re *regexp.Regexp) string {
	if phrase := longestMarkerPhrase(rendered, re); phrase != "" {
		return phrase
	}
	return longestMarkerPhrase(compactTerminalText(rendered), re)
}

// capturedRealAgentPhrase extracts a phase's invented phrase from the final
// transcript, consulting the CLI's session store for agents whose TUIs do not
// reliably render the response.
func capturedRealAgentPhrase(agent, plain, prefix string) string {
	re := realAgentMarkerRegexp(prefix)
	if phrase := captureMarkerPhrase(plain, re); phrase != "" {
		return phrase
	}
	return realAgentSessionCapture(agent, re)
}

func realAgentSessionCapture(agent string, re *regexp.Regexp) string {
	if agent == "copilot" {
		return copilotSessionCapture(re)
	}
	return ""
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
	captures := make([]*regexp.Regexp, len(phases))
	for i := range phases {
		captures[i] = realAgentMarkerRegexp(phases[i].markerPrefix)
	}
	ready := make([]bool, len(phases))
	readyAt := make([]time.Time, len(phases))
	afterReadySubmitted := make([]bool, len(phases))
	trustHandled := make([]bool, len(phases))
	phaseBoundary := 0
	approvalBoundary := 0
	commandApprovals := 0
	hookTrustHandled := false
	readDone := false
	waitDone := false
	var waitErr error
	advancePhase := func(i, boundary int) {
		ready[i] = true
		readyAt[i] = time.Now()
		phaseBoundary = boundary
	}
	handleInteractivePrompts := func(plain string) {
		if agent == "codex" && !hookTrustHandled && strings.Contains(plain, "Hooks need review") {
			hookTrustHandled = true
			// The generated notify hook contains the test binary's unique path.
			// Trust it once in the process-local CODEX_HOME; the resumed step
			// must reuse the state written by this choice.
			_, _ = ptmx.Write([]byte("2\r"))
		}
		currentPhase := 0
		for currentPhase < len(ready) && ready[currentPhase] {
			currentPhase++
		}
		if currentPhase < len(phases) && !trustHandled[currentPhase] &&
			strings.Contains(plain[phaseBoundary:], "Confirm folder trust") {
			trustHandled[currentPhase] = true
			_, _ = ptmx.Write([]byte("\r"))
		}
		if commandApprovals >= len(phases) {
			return
		}
		approvalText := plain[approvalBoundary:]
		switch {
		case agent == "copilot" && strings.Contains(approvalText, "Do you want to run this command?"):
			commandApprovals++
			approvalBoundary = len(plain)
			_, _ = ptmx.Write([]byte("\r"))
		case agent == "cursor" && strings.Contains(approvalText, "Run this command?"):
			// Never answered: the narrow pre-approval in the private config
			// must cover the completion command, so a prompt is a regression.
			// Counting it lets the caller fail with a precise message.
			commandApprovals++
			approvalBoundary = len(plain)
		}
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
				handleInteractivePrompts(plain)
				for i := range phases {
					if !ready[i] && captureMarkerPhrase(plain, captures[i]) != "" {
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
			plain := ansi.Strip(raw.String())
			handleInteractivePrompts(plain)
			for i := range phases {
				if phases[i].afterReady != "" && ready[i] && !afterReadySubmitted[i] && time.Since(readyAt[i]) >= 2*time.Second {
					afterReadySubmitted[i] = true
					input := phases[i].afterReady
					if agent == "codex" && strings.HasPrefix(input, "$") {
						// Codex only opens its skill-completion menu for typed input,
						// not for a whole mention pasted atomically. Type the sigil,
						// accept the match, then submit it as the user's message.
						_, _ = ptmx.Write([]byte("$"))
						time.Sleep(250 * time.Millisecond)
						_, _ = ptmx.Write([]byte(strings.TrimPrefix(input, "$")))
						time.Sleep(250 * time.Millisecond)
						_, _ = ptmx.Write([]byte("\t\r"))
						continue
					}
					_, _ = ptmx.Write([]byte(input))
					time.Sleep(100 * time.Millisecond)
					_, _ = ptmx.Write([]byte("\r"))
				}
			}
			if agent == "copilot" {
				for i := range phases {
					if !ready[i] && realAgentSessionCapture(agent, captures[i]) != "" {
						advancePhase(i, len(plain))
						break
					}
				}
			}
		case <-timer.C:
			_ = cmd.Process.Kill()
			_ = ptmx.Close()
			return realAgentPTYResult{output: raw.String(), commandApprovals: commandApprovals}, fmt.Errorf("timed out after %s; readiness=%v", timeout, ready)
		}
	}

	result := realAgentPTYResult{output: raw.String(), commandApprovals: commandApprovals}
	for i := range phases {
		if !ready[i] {
			return result, fmt.Errorf("real-agent phase %d incomplete: ready=false", i)
		}
	}
	return result, waitErr
}

func copilotSessionCapture(re *regexp.Regexp) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	paths, err := filepath.Glob(filepath.Join(home, ".copilot", "session-state", "*", "events.jsonl"))
	if err != nil {
		return ""
	}
	longest := ""
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if phrase := longestMarkerPhrase(string(data), re); len(phrase) > len(longest) {
			longest = phrase
		}
	}
	return longest
}

// compactTerminalText strips everything but marker-safe characters so markers
// broken across rendered lines still match contiguously.
func compactTerminalText(value string) string {
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

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/stateio"
	gopty "github.com/creack/pty"
	"github.com/google/go-cmp/cmp"
)

// E2E-001 proves the public headless CLI can coordinate from one Git-backed
// workspace while mutating and validating independent selected repositories.
func TestMultiRepositoryWorkspacePublicCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("multi-repository workflow fixture uses POSIX shell commands")
	}
	repoRoot := findRepoRoot(t)
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	binDir := filepath.Join(temp, "bin")
	clientLog := filepath.Join(temp, "clients.log")
	workspace := filepath.Join(temp, "foo")
	backend := filepath.Join(temp, "backend")
	frontend := filepath.Join(temp, "frontend")
	docs := filepath.Join(temp, "docs")
	for _, dir := range []string{workspace, backend, frontend, docs} {
		initE2EGitRepository(t, dir)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	canonicalBackend, err := filepath.EvalSymlinks(backend)
	if err != nil {
		t.Fatal(err)
	}
	canonicalFrontend, err := filepath.EvalSymlinks(frontend)
	if err != nil {
		t.Fatal(err)
	}
	writeSmokeProfileConfig(t, home)
	writeMultiRepositoryClientFixtures(t, binDir)
	for marker, dir := range map[string]string{"workspace-validator": workspace, "backend-validator": backend, "frontend-validator": frontend} {
		if err := os.WriteFile(filepath.Join(dir, ".validator-marker"), []byte(marker+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	configPath := filepath.Join(workspace, ".agent-runner", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "repositories:\n  backend:\n    path: ../backend\n  frontend:\n    path: ../frontend\n  docs:\n    path: ../docs\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(workspace, ".agent-runner", "workflows", "multi-repo-proof-v1.0.yaml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	workflow := `name: multi-repo-proof
scope: workspace
params:
  - name: repositories
steps:
  - id: plan
    command: printf 'plan:%s\n' {{workspace_dir}} > order.log
  - id: implement-task-groups
    scope: repositories
    steps:
      - id: modify
        command: printf '%s\n' {{repository_name}} > changed.txt; printf '%s\n' {{repository_name}} >> {{workspace_dir}}/order.log
      - id: validate
        command: test "$(cat changed.txt)" = {{repository_name}} && agent-validator run --report
      - id: finalize-repository-pr
        command: gh pr view --json url --jq .url
        capture: pr_url
  - id: validate-workspace
    command: agent-validator run --report
  - id: finalize-workspace-pr
    command: gh pr view --json url --jq .url
    capture: pr_url
  - id: accept
    command: test -s {{workspace_dir}}/order.log && test ! -e "` + docs + `/changed.txt"
`
	if err := os.WriteFile(workflowPath, []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	runnerBin := filepath.Join(temp, "agent-runner")
	buildAgentRunner(t, repoRoot, runnerBin)
	command := exec.Command(runnerBin, "--headless", "multi-repo-proof", "repositories=backend,frontend")
	command.Dir = workspace
	command.Env = smokeCommandEnv(os.Environ(),
		"AGENT_RUNNER_NO_TUI=1",
		"HOME="+home,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"MULTI_REPO_CLIENT_LOG="+clientLog,
	)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		t.Fatalf("multi-repository CLI run failed: %v\n%s", err, output.String())
	}

	for name, dir := range map[string]string{"backend": backend, "frontend": frontend} {
		data, err := os.ReadFile(filepath.Join(dir, "changed.txt"))
		if err != nil || strings.TrimSpace(string(data)) != name {
			t.Fatalf("%s marker = %q, %v", name, data, err)
		}
	}
	order, err := os.ReadFile(filepath.Join(workspace, "order.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(order)), "\n")
	if diff := cmp.Diff([]string{"plan:" + canonicalWorkspace, "backend", "frontend"}, lines); diff != "" {
		t.Fatalf("execution order mismatch (-want +got):\n%s", diff)
	}

	sessionDir := latestNamedRunDir(t, home, canonicalWorkspace, "multi-repo-proof-")
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Completed || len(state.SelectedRepositories) != 2 {
		t.Fatalf("completed state = %#v", state)
	}
	if diff := cmp.Diff("https://example.test/foo/42", state.WorkspacePullRequestURL); diff != "" {
		t.Fatalf("workspace PR URL mismatch (-want +got):\n%s", diff)
	}
	wantRepositoryPRs := map[string]string{
		"backend":  "https://example.test/backend/42",
		"frontend": "https://example.test/frontend/42",
	}
	if diff := cmp.Diff(wantRepositoryPRs, state.RepositoryPullRequestURLs); diff != "" {
		t.Fatalf("repository PR URLs mismatch (-want +got):\n%s", diff)
	}
	clientData, err := os.ReadFile(clientLog)
	if err != nil {
		t.Fatal(err)
	}
	clientLines := strings.Split(strings.TrimSpace(string(clientData)), "\n")
	wantClientLines := []string{
		"validator|" + canonicalBackend + "|backend-validator|run --report",
		"gh|" + canonicalBackend + "|pr view --json url --jq .url",
		"validator|" + canonicalFrontend + "|frontend-validator|run --report",
		"gh|" + canonicalFrontend + "|pr view --json url --jq .url",
		"validator|" + canonicalWorkspace + "|workspace-validator|run --report",
		"gh|" + canonicalWorkspace + "|pr view --json url --jq .url",
	}
	if diff := cmp.Diff(wantClientLines, clientLines); diff != "" {
		t.Fatalf("controlled client calls mismatch (-want +got):\n%s", diff)
	}
	auditData, err := os.ReadFile(filepath.Join(sessionDir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{
		"[implement-task-groups, repo:backend, modify]",
		"[implement-task-groups, repo:frontend, validate]",
	} {
		if !bytes.Contains(auditData, []byte(prefix)) {
			t.Errorf("audit log missing %s", prefix)
		}
	}
}

// E2E-002 proves a repository fan-out nested inside a workspace sub-workflow
// resumes at the failed repository's deepest child step in a new process.
func TestMultiRepositoryNestedResumePublicCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("multi-repository workflow fixture uses POSIX shell commands")
	}
	repoRoot := findRepoRoot(t)
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	workspaceRoot := symlinkedE2EWorkspaceRoot(t, temp)
	workspace := filepath.Join(workspaceRoot, "foo")
	repositories := map[string]string{
		"backend":  filepath.Join(workspaceRoot, "backend"),
		"frontend": filepath.Join(workspaceRoot, "frontend"),
		"docs":     filepath.Join(workspaceRoot, "docs"),
	}
	initE2EGitRepository(t, workspace)
	for _, dir := range repositories {
		initE2EGitRepository(t, dir)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	writeSmokeProfileConfig(t, home)

	configPath := filepath.Join(workspace, ".agent-runner", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "repositories:\n  backend:\n    path: ../backend\n  frontend:\n    path: ../frontend\n  docs:\n    path: ../docs\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	workflowDir := filepath.Join(workspace, ".agent-runner", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parentWorkflow := `name: nested-repository-resume
scope: workspace
params:
  - name: repositories
steps:
  - id: coordinate
    workflow: workspace-child-v1.0.yaml
    params:
      repositories: "{{repositories}}"
`
	workspaceChild := `name: workspace-child
scope: workspace
params:
  - name: repositories
steps:
  - id: prepare
    command: touch prepared.txt
  - id: implement-repositories
    workflow: repository-child-v1.0.yaml
    params:
      repositories: "{{repositories}}"
`
	repositoryChild := `name: repository-child
scope: repositories
params:
  - name: repositories
steps:
  - id: first
    command: printf '%s:first\n' {{repository_name}} >> {{workspace_dir}}/trace.log
  - id: second
    command: printf '%s:second\n' {{repository_name}} >> {{workspace_dir}}/trace.log; if [ {{repository_name}} = frontend ] && [ ! -e {{workspace_dir}}/failed-once ]; then touch {{workspace_dir}}/failed-once; exit 1; fi
`
	for name, contents := range map[string]string{
		"nested-repository-resume-v1.0.yaml": parentWorkflow,
		"workspace-child-v1.0.yaml":          workspaceChild,
		"repository-child-v1.0.yaml":         repositoryChild,
	} {
		if err := os.WriteFile(filepath.Join(workflowDir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	runnerBin := filepath.Join(temp, "agent-runner")
	buildAgentRunner(t, repoRoot, runnerBin)
	env := smokeCommandEnv(os.Environ(), "AGENT_RUNNER_NO_TUI=1", "HOME="+home, "PWD="+workspace)
	first := exec.Command(runnerBin, "--headless", "nested-repository-resume", "repositories=backend,frontend,docs")
	first.Dir = workspace
	first.Env = env
	firstOutput, firstErr := first.CombinedOutput()
	if firstErr == nil {
		t.Fatalf("first run succeeded, want controlled frontend failure\n%s", firstOutput)
	}
	sessionDir := latestNamedRunDir(t, home, canonicalWorkspace, "nested-repository-resume-")

	resumed := exec.Command(runnerBin, "--headless", "--resume", filepath.Base(sessionDir))
	resumed.Dir = workspace
	resumed.Env = env
	if output, runErr := resumed.CombinedOutput(); runErr != nil {
		t.Fatalf("resumed run failed: %v\n%s", runErr, output)
	}

	traceData, err := os.ReadFile(filepath.Join(workspace, "trace.log"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(traceData)), "\n")
	want := []string{
		"backend:first", "backend:second",
		"frontend:first", "frontend:second", "frontend:second",
		"docs:first", "docs:second",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("resume execution mismatch (-want +got):\n%s", diff)
	}
	state, err := stateio.ReadState(filepath.Join(sessionDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !state.Completed {
		t.Fatalf("resumed workflow did not complete: %#v", state)
	}
}

// E2E-003 proves both the live and saved public run views expose the same
// single repository hierarchy level through a real PTY.
func TestMultiRepositoryRunViewPublicPTY(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("run-view PTY fixture requires a POSIX terminal")
	}
	repoRoot := findRepoRoot(t)
	temp := t.TempDir()
	home := filepath.Join(temp, "home")
	workspaceRoot := symlinkedE2EWorkspaceRoot(t, temp)
	workspace := filepath.Join(workspaceRoot, "foo")
	backend := filepath.Join(workspaceRoot, "backend")
	frontend := filepath.Join(workspaceRoot, "frontend")
	for _, dir := range []string{workspace, backend, frontend} {
		initE2EGitRepository(t, dir)
	}
	canonicalWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	writeSmokeProfileConfig(t, home)
	if err := os.WriteFile(filepath.Join(home, ".agent-runner", "settings.yaml"), []byte("theme: dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(workspace, ".agent-runner", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "repositories:\n  backend:\n    path: ../backend\n  frontend:\n    path: ../frontend\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	workflowDir := filepath.Join(workspace, ".agent-runner", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	workflow := `name: multi-repo-run-view
scope: repositories
params:
  - name: repositories
steps:
  - id: implement
    command: printf '%s-output\n' {{repository_name}}; sleep 0.3
`
	if err := os.WriteFile(filepath.Join(workflowDir, "multi-repo-run-view-v1.0.yaml"), []byte(workflow), 0o600); err != nil {
		t.Fatal(err)
	}
	runnerBin := filepath.Join(temp, "agent-runner")
	buildAgentRunner(t, repoRoot, runnerBin)
	env := smokeCommandEnv(os.Environ(), "HOME="+home, "PWD="+workspace, "TERM=xterm-256color")

	live := exec.Command(runnerBin, "multi-repo-run-view", "repositories=backend,frontend")
	live.Dir, live.Env = workspace, env
	liveScreen := runPTYUntilRepositoryScreen(t, live)
	assertRepositoryRunViewScreen(t, "live", liveScreen)

	sessionDir := latestNamedRunDir(t, home, canonicalWorkspace, "multi-repo-run-view-")
	inspect := exec.Command(runnerBin, "--inspect", filepath.Base(sessionDir))
	inspect.Dir, inspect.Env = workspace, env
	inspectScreen := runPTYUntilRepositoryScreen(t, inspect)
	assertRepositoryRunViewScreen(t, "inspect", inspectScreen)
}

func assertRepositoryRunViewScreen(t *testing.T, phase, screen string) {
	t.Helper()
	for _, marker := range []string{"multi-repo-run-view", "backend", "frontend", "completed"} {
		if !strings.Contains(screen, marker) {
			t.Errorf("%s run-view screen missing %q:\n%s", phase, marker, screen)
		}
	}
}

func symlinkedE2EWorkspaceRoot(t *testing.T, temp string) string {
	t.Helper()
	realRoot := filepath.Join(temp, "workspace-real")
	logicalRoot := filepath.Join(temp, "workspace-logical")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, logicalRoot); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	return logicalRoot
}

func runPTYUntilRepositoryScreen(t *testing.T, command *exec.Cmd) string {
	t.Helper()
	repositoryMarkers := []string{"multi-repo-run-view", "backend", "frontend", "completed"}
	ptmx, err := gopty.StartWithSize(command, &gopty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ptmx.Close() }()
	chunks := make(chan []byte, 16)
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, readErr := ptmx.Read(buffer)
			if n > 0 {
				chunks <- append([]byte(nil), buffer[:n]...)
			}
			if readErr != nil {
				close(chunks)
				return
			}
		}
	}()
	waitCh := make(chan error, 1)
	go func() { waitCh <- command.Wait() }()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	screen := newTestTerminalScreen(40, 120)
	lastScreen := ""
	quitSent := false
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				chunks = nil
				continue
			}
			screen.Write(chunk)
			current := screen.String()
			if containsAll(current, repositoryMarkers) {
				lastScreen = current
				if !quitSent {
					_, _ = ptmx.WriteString("q")
					quitSent = true
				}
			}
		case waitErr := <-waitCh:
			if waitErr != nil {
				t.Fatalf("PTY command failed: %v\n%s", waitErr, screen.String())
			}
			if lastScreen == "" {
				t.Fatalf("PTY screen never contained %v:\n%s", repositoryMarkers, screen.String())
			}
			return lastScreen
		case <-timer.C:
			_ = command.Process.Kill()
			t.Fatalf("PTY screen timed out waiting for %v:\n%s", repositoryMarkers, screen.String())
		}
	}
}

func containsAll(text string, markers []string) bool {
	for _, marker := range markers {
		if !strings.Contains(text, marker) {
			return false
		}
	}
	return true
}

type testTerminalScreen struct {
	rows, cols int
	row, col   int
	cells      [][]byte
	escape     []byte
}

func newTestTerminalScreen(rows, cols int) *testTerminalScreen {
	cells := make([][]byte, rows)
	for row := range cells {
		cells[row] = bytes.Repeat([]byte{' '}, cols)
	}
	return &testTerminalScreen{rows: rows, cols: cols, cells: cells}
}

func (s *testTerminalScreen) Write(data []byte) {
	for _, character := range data {
		if s.escape != nil {
			s.escape = append(s.escape, character)
			if s.escape[0] != '[' || len(s.escape) > 1 && character >= 0x40 && character <= 0x7e {
				s.applyEscape()
				s.escape = nil
			}
			continue
		}
		switch character {
		case 0x1b:
			s.escape = make([]byte, 0, 16)
		case '\r':
			s.col = 0
		case '\n':
			s.row = min(s.row+1, s.rows-1)
		case '\b':
			s.col = max(s.col-1, 0)
		default:
			if character >= 0x20 && s.row < s.rows && s.col < s.cols {
				s.cells[s.row][s.col] = character
				s.col++
			}
		}
	}
}

func (s *testTerminalScreen) applyEscape() {
	if len(s.escape) < 2 || s.escape[0] != '[' {
		return
	}
	final := s.escape[len(s.escape)-1]
	raw := strings.TrimLeft(string(s.escape[1:len(s.escape)-1]), "?")
	params := []int{0}
	if raw != "" {
		params = params[:0]
		for _, part := range strings.Split(raw, ";") {
			value, _ := strconv.Atoi(part)
			params = append(params, value)
		}
	}
	value := params[0]
	if value == 0 {
		value = 1
	}
	switch final {
	case 'A':
		s.row = max(s.row-value, 0)
	case 'B':
		s.row = min(s.row+value, s.rows-1)
	case 'C':
		s.col = min(s.col+value, s.cols-1)
	case 'D':
		s.col = max(s.col-value, 0)
	case 'G':
		s.col = min(max(value-1, 0), s.cols-1)
	case 'H', 'f':
		row, col := 1, 1
		if len(params) > 0 && params[0] > 0 {
			row = params[0]
		}
		if len(params) > 1 && params[1] > 0 {
			col = params[1]
		}
		s.row, s.col = min(row-1, s.rows-1), min(col-1, s.cols-1)
	case 'J':
		if params[0] == 2 || params[0] == 3 {
			for row := range s.cells {
				for col := range s.cells[row] {
					s.cells[row][col] = ' '
				}
			}
		}
	case 'K':
		for col := s.col; col < s.cols; col++ {
			s.cells[s.row][col] = ' '
		}
	}
}

func (s *testTerminalScreen) String() string {
	lines := make([]string, len(s.cells))
	for row := range s.cells {
		lines[row] = strings.TrimRight(string(s.cells[row]), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func initE2EGitRepository(t *testing.T, dir string) {
	t.Helper()
	command := exec.Command("git", "init", "-q", dir)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", dir, err, output)
	}
}

func writeMultiRepositoryClientFixtures(t *testing.T, binDir string) {
	t.Helper()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	validator := `#!/bin/sh
set -eu
marker=$(cat .validator-marker)
printf 'validator|%s|%s|%s\n' "$PWD" "$marker" "$*" >> "$MULTI_REPO_CLIENT_LOG"
`
	gh := `#!/bin/sh
set -eu
printf 'gh|%s|%s\n' "$PWD" "$*" >> "$MULTI_REPO_CLIENT_LOG"
printf 'https://example.test/%s/42\n' "$(basename "$PWD")"
`
	for name, contents := range map[string]string{"agent-validator": validator, "gh": gh} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(contents), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func latestNamedRunDir(t *testing.T, home, workspace, prefix string) string {
	t.Helper()
	runsDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(workspace), "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		t.Fatal(err)
	}
	for index := len(entries) - 1; index >= 0; index-- {
		if entries[index].IsDir() && strings.HasPrefix(entries[index].Name(), prefix) {
			return filepath.Join(runsDir, entries[index].Name())
		}
	}
	t.Fatalf("no run with prefix %q in %s", prefix, runsDir)
	return ""
}

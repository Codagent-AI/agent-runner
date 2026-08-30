package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/stateio"
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
	writeSmokeProfileConfig(t, home)

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
        command: test "$(cat changed.txt)" = {{repository_name}}
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
	command.Env = smokeCommandEnv(os.Environ(), "AGENT_RUNNER_NO_TUI=1", "HOME="+home)
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

func initE2EGitRepository(t *testing.T, dir string) {
	t.Helper()
	command := exec.Command("git", "init", "-q", dir)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", dir, err, output)
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

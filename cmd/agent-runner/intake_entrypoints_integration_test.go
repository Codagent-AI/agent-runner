package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/stateio"
)

func TestIntakeEntryPointsRequireTTYIntegration(t *testing.T) {
	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	runnerBin := filepath.Join(tmp, "agent-runner")
	buildAgentRunner(t, repoRoot, runnerBin)

	runsDir := filepath.Join(home, ".agent-runner", "projects", audit.EncodePath(repoRoot), "runs")
	assertNoIntakeRuns := func() {
		t.Helper()
		if entries, err := os.ReadDir(runsDir); err == nil && len(entries) > 0 {
			t.Fatalf("unexpected intake run directories: %v", entries)
		} else if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read runs directory: %v", err)
		}
	}
	run := func(env []string, args ...string) (string, error) {
		t.Helper()
		cmd := exec.Command(runnerBin, args...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), append([]string{"HOME=" + home}, env...)...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		return out.String(), err
	}

	for _, tt := range []struct {
		name string
		env  []string
		args []string
		want string
	}{
		{name: "headless flag", args: []string{"--headless", "-i"}, want: "mutually exclusive"},
		{name: "headless intake by name", args: []string{"--headless", "core:intake"}, want: "intake requires an interactive terminal"},
		{name: "no TUI bypass with direct intake", env: []string{"AGENT_RUNNER_NO_TUI=1"}, args: []string{"-i"}, want: "intake requires an interactive terminal"},
		{name: "no TUI bypass by name", env: []string{"AGENT_RUNNER_NO_TUI=1"}, args: []string{"core:intake"}, want: "intake requires an interactive terminal"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, err := run(tt.env, tt.args...)
			if err == nil {
				t.Fatalf("command succeeded, output:\n%s", out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("output = %q, want %q", out, tt.want)
			}
			assertNoIntakeRuns()
		})
	}

	workflowDir := filepath.Join(home, ".agent-runner", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "unaffected-v1.0.yaml"), []byte("name: unaffected\nsteps:\n  - id: done\n    command: echo unaffected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := run([]string{"AGENT_RUNNER_NO_TUI=1"}, "--headless", "unaffected")
	if err != nil {
		t.Fatalf("non-intake workflow failed: %v\n%s", err, out)
	}
	entries, err := os.ReadDir(runsDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("non-intake runs = %v, %v; want one run", entries, err)
	}
	state, err := stateio.ReadState(filepath.Join(runsDir, entries[0].Name(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.WorkflowName != "unaffected" || !state.Completed {
		t.Fatalf("non-intake state = %+v, want completed unaffected run", state)
	}
}

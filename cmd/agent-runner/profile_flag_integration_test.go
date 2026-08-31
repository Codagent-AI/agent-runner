package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive the real binary because the --profile surface is split
// across two parsers: the standard flag package (which consumes occurrences
// before the first positional) and extractProfileArgs (which handles the rest).
// Unit tests against either half individually cannot detect a disagreement
// between them.
func writeProfileTestWorkflow(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "wf-v1.0.yaml")
	body := "name: wf\nsteps:\n  - id: s1\n    command: \"true\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	return path
}

func runProfileCLI(t *testing.T, runnerBin, dir, home string, args ...string) (stdoutText, stderrText string, exitCode int) {
	t.Helper()
	cmd := exec.Command(runnerBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+home, "AGENT_RUNNER_NO_TUI=1")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), code
}

func TestProfileFlagDuplicateRejectedInEveryOrderingIntegration(t *testing.T) {
	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create home: %v", err)
	}
	runnerBin := filepath.Join(tmp, "agent-runner")
	buildAgentRunner(t, repoRoot, runnerBin)
	writeProfileTestWorkflow(t, tmp)

	const wantErr = "--profile may only be specified once"

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "both before the positional",
			args: []string{"--profile", "aaa", "--profile", "bbb", "--validate", "wf-v1.0.yaml"},
		},
		{
			name: "both after the positional",
			args: []string{"--validate", "wf-v1.0.yaml", "--profile", "aaa", "--profile", "bbb"},
		},
		{
			name: "one before and one after the positional",
			args: []string{"--profile", "aaa", "--validate", "wf-v1.0.yaml", "--profile", "bbb"},
		},
		{
			name: "equals form before the positional",
			args: []string{"--profile=aaa", "--profile=bbb", "--validate", "wf-v1.0.yaml"},
		},
		{
			name: "single dash form before the positional",
			args: []string{"-profile", "aaa", "-profile", "bbb", "--validate", "wf-v1.0.yaml"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runProfileCLI(t, runnerBin, tmp, home, tc.args...)
			if code == 0 {
				t.Fatalf("exit code = 0, want non-zero\nstdout: %s\nstderr: %s", stdout, stderr)
			}
			if !strings.Contains(stderr, wantErr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr, wantErr)
			}
		})
	}
}

func TestProfileFlagSingleOccurrenceAcceptedInEveryOrderingIntegration(t *testing.T) {
	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create home: %v", err)
	}
	runnerBin := filepath.Join(tmp, "agent-runner")
	buildAgentRunner(t, repoRoot, runnerBin)
	writeProfileTestWorkflow(t, tmp)

	tests := []struct {
		name string
		args []string
	}{
		{name: "before the positional", args: []string{"--profile", "default", "--validate", "wf-v1.0.yaml"}},
		{name: "after the positional", args: []string{"--validate", "wf-v1.0.yaml", "--profile", "default"}},
		{name: "equals form", args: []string{"--profile=default", "--validate", "wf-v1.0.yaml"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runProfileCLI(t, runnerBin, tmp, home, tc.args...)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if want := "workflow is valid (profile set: default)"; !strings.Contains(stdout, want) {
				t.Fatalf("stdout = %q, want it to contain %q", stdout, want)
			}
		})
	}
}

// The resolved profile set is always reportable, so validation names it whether
// or not the caller passed an override.
func TestValidateNamesResolvedProfileSetWithoutOverrideIntegration(t *testing.T) {
	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create home: %v", err)
	}
	runnerBin := filepath.Join(tmp, "agent-runner")
	buildAgentRunner(t, repoRoot, runnerBin)
	writeProfileTestWorkflow(t, tmp)

	stdout, stderr, code := runProfileCLI(t, runnerBin, tmp, home, "--validate", "wf-v1.0.yaml")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if want := "workflow is valid (profile set: default)"; !strings.Contains(stdout, want) {
		t.Fatalf("stdout = %q, want it to contain %q", stdout, want)
	}
}

func TestValidateNamesConfigSelectedProfileSetIntegration(t *testing.T) {
	repoRoot := findRepoRoot(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create home: %v", err)
	}
	runnerBin := filepath.Join(tmp, "agent-runner")
	buildAgentRunner(t, repoRoot, runnerBin)
	writeProfileTestWorkflow(t, tmp)

	projectConfigDir := filepath.Join(tmp, ".agent-runner")
	if err := os.MkdirAll(projectConfigDir, 0o755); err != nil {
		t.Fatalf("create project config dir: %v", err)
	}
	config := "active_profile: work\nprofiles:\n  work:\n    agents: {}\n"
	if err := os.WriteFile(filepath.Join(projectConfigDir, "config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	stdout, stderr, code := runProfileCLI(t, runnerBin, tmp, home, "--validate", "wf-v1.0.yaml")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if want := "workflow is valid (profile set: work)"; !strings.Contains(stdout, want) {
		t.Fatalf("stdout = %q, want it to contain %q", stdout, want)
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	iexec "github.com/codagent/agent-runner/internal/exec"
	"github.com/google/go-cmp/cmp"
)

func TestResolveWorkflowArg(t *testing.T) {
	t.Run("rejects existing workflow YAML path", func(t *testing.T) {
		t.Chdir(t.TempDir())
		path := filepath.Join("custom", "workflow-v1.0.yaml")
		writeTestFile(t, path, "name: workflow\nsteps:\n  - id: s\n    command: echo ok\n")

		_, err := resolveWorkflowArg(path)
		if err == nil || !strings.Contains(err.Error(), `launch logical workflow "custom/workflow"`) {
			t.Fatalf("resolveWorkflowArg error = %v, want logical-name guidance", err)
		}
	})

	t.Run("rejected nested workflow path preserves logical directory", func(t *testing.T) {
		t.Chdir(t.TempDir())
		path := filepath.Join(".agent-runner", "workflows", "team", "deploy-v1.0.yaml")
		writeTestFile(t, path, "name: deploy\nsteps:\n  - id: s\n    command: echo ok\n")

		_, err := resolveWorkflowArg(path)
		if err == nil || !strings.Contains(err.Error(), `launch logical workflow "team/deploy"`) {
			t.Fatalf("resolveWorkflowArg error = %v, want nested logical-name guidance", err)
		}
	})

	t.Run("resolves bare user workflow from dot-agent-runner directory", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "my-workflow-v1.0.yaml"), "name: my-workflow\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("my-workflow")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(".agent-runner", "workflows", "my-workflow-v1.0.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("falls back to yml for bare user workflow", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "my-workflow-v1.0.yml"), "name: my-workflow\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("my-workflow")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(".agent-runner", "workflows", "my-workflow-v1.0.yml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("falls back to global yaml for bare user workflow", func(t *testing.T) {
		repo := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "my-workflow-v1.0.yaml"), "name: my-workflow\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("my-workflow")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(home, ".agent-runner", "workflows", "my-workflow-v1.0.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("falls back to global yml for bare user workflow", func(t *testing.T) {
		repo := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "my-workflow-v1.0.yml"), "name: my-workflow\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("my-workflow")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(home, ".agent-runner", "workflows", "my-workflow-v1.0.yml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("resolves nested bare user workflow", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "team", "deploy-v1.0.yaml"), "name: deploy\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("team/deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(".agent-runner", "workflows", "team", "deploy-v1.0.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("falls back to nested global workflow", func(t *testing.T) {
		repo := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "team", "deploy-v1.0.yaml"), "name: deploy\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("team/deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(home, ".agent-runner", "workflows", "team", "deploy-v1.0.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("resolves namespaced builtin workflow", func(t *testing.T) {
		got, err := resolveWorkflowArg("core:finalize-pr")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		if got != "builtin:core/finalize-pr-v1.0.yaml" {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, "builtin:core/finalize-pr-v1.0.yaml")
		}
	})

	t.Run("namespaced workflow does not fall back to disk", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "missing", "workflow.yaml"), "name: test\nsteps:\n  - id: s\n    command: echo ok\n")

		_, err := resolveWorkflowArg("missing:workflow")
		if err == nil {
			t.Fatal("expected missing builtin to return an error")
		}
		if !strings.Contains(err.Error(), `workflow "missing:workflow" not found`) {
			t.Fatalf("expected workflow-not-found error, got %v", err)
		}
	})

	t.Run("namespaced local workflow path is ignored in favor of builtin namespace", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "team", "deploy.yaml"), "name: test\nsteps:\n  - id: s\n    command: echo ok\n")

		_, err := resolveWorkflowArg("team:deploy")
		if err == nil {
			t.Fatal("expected missing builtin to return an error")
		}
		if !strings.Contains(err.Error(), `workflow "team:deploy" not found`) {
			t.Fatalf("expected workflow-not-found error, got %v", err)
		}
	})

	t.Run("namespaced workflow does not fall back to global directory", func(t *testing.T) {
		repo := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "missing", "workflow.yaml"), "name: test\nsteps:\n  - id: s\n    command: echo ok\n")

		_, err := resolveWorkflowArg("missing:workflow")
		if err == nil {
			t.Fatal("expected missing builtin to return an error")
		}
		if !strings.Contains(err.Error(), `workflow "missing:workflow" not found`) {
			t.Fatalf("expected workflow-not-found error, got %v", err)
		}
	})

	t.Run("project workflow shadows global workflow with same name", func(t *testing.T) {
		repo := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "my-workflow-v1.0.yaml"), "name: my-workflow\nsteps:\n  - id: s\n    command: echo ok\n")
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "my-workflow-v2.0.yaml"), "name: my-workflow\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("my-workflow")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(".agent-runner", "workflows", "my-workflow-v1.0.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("project nested workflow shadows global workflow with same path", func(t *testing.T) {
		repo := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "team", "deploy-v1.0.yaml"), "name: deploy\nsteps:\n  - id: s\n    command: echo ok\n")
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "team", "deploy-v2.0.yaml"), "name: deploy\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("team/deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(".agent-runner", "workflows", "team", "deploy-v1.0.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("bare workflow does not fall back to builtins", func(t *testing.T) {
		t.Chdir(t.TempDir())

		_, err := resolveWorkflowArg("finalize-pr")
		if err == nil {
			t.Fatal("expected missing local workflow to return an error")
		}
		if !strings.Contains(err.Error(), `workflow "finalize-pr" not found`) {
			t.Fatalf("expected workflow-not-found error, got %v", err)
		}
	})

	t.Run("home directory lookup failure still returns workflow not found", func(t *testing.T) {
		original := userHomeDir
		userHomeDir = func() (string, error) { return "", fmt.Errorf("home unavailable") }
		t.Cleanup(func() { userHomeDir = original })

		t.Chdir(t.TempDir())
		_, err := resolveWorkflowArg("my-workflow")
		if err == nil {
			t.Fatal("expected missing local workflow to return an error")
		}
		if !strings.Contains(err.Error(), `workflow "my-workflow" not found`) {
			t.Fatalf("expected workflow-not-found error, got %v", err)
		}
		if strings.Contains(err.Error(), "home directory") {
			t.Fatalf("expected home-directory failure to be hidden, got %v", err)
		}
	})

	t.Run("top-level workflows directory is ignored", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeTestFile(t, filepath.Join("workflows", "my-workflow-v1.0.yaml"), "name: my-workflow\nsteps:\n  - id: s\n    command: echo ok\n")

		_, err := resolveWorkflowArg("my-workflow")
		if err == nil {
			t.Fatal("expected top-level workflows directory to be ignored")
		}
		if !strings.Contains(err.Error(), `workflow "my-workflow" not found`) {
			t.Fatalf("expected workflow-not-found error, got %v", err)
		}
	})

	t.Run("rejects invalid workflow names", func(t *testing.T) {
		for _, arg := range []string{"my-workflow.yaml", "core:team/deploy", "/team/deploy"} {
			t.Run(arg, func(t *testing.T) {
				_, err := resolveWorkflowArg(arg)
				if err == nil {
					t.Fatal("expected invalid workflow name error")
				}
				if !strings.Contains(err.Error(), "invalid workflow name") {
					t.Fatalf("expected invalid workflow name error, got %v", err)
				}
			})
		}
	})
}

func TestValidateIntakeInvocation(t *testing.T) {
	tests := []struct {
		name string
		opts intakeInvocationOptions
		want string
	}{
		{name: "headless", opts: intakeInvocationOptions{intake: true, headless: true}, want: "mutually exclusive"},
		{name: "list", opts: intakeInvocationOptions{intake: true, list: true}, want: "mutually exclusive"},
		{name: "resume", opts: intakeInvocationOptions{intake: true, resume: true}, want: "mutually exclusive"},
		{name: "inspect", opts: intakeInvocationOptions{intake: true, inspect: "run-1"}, want: "mutually exclusive"},
		{name: "validate", opts: intakeInvocationOptions{intake: true, validate: true}, want: "mutually exclusive"},
		{name: "onboarding", opts: intakeInvocationOptions{intake: true, onboardingFrom: "setup"}, want: "mutually exclusive"},
		{name: "workflow", opts: intakeInvocationOptions{intake: true, args: []string{"spec-driven:change"}}, want: "cannot be combined with an explicit workflow"},
		{name: "override requires intake", opts: intakeInvocationOptions{cli: "codex"}, want: "require -i"},
		{name: "model requires intake", opts: intakeInvocationOptions{model: "gpt-5.2"}, want: "require -i"},
		{name: "unknown CLI", opts: intakeInvocationOptions{intake: true, cli: "not-a-real-cli"}, want: "accepted values"},
		{name: "noninteractive CLI", opts: intakeInvocationOptions{intake: true, cli: "opencode"}, want: "interactive-capable"},
		{name: "valid override", opts: intakeInvocationOptions{intake: true, cli: "codex", model: "gpt-5.2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIntakeInvocation(&tt.opts)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("validateIntakeInvocation() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateIntakeInvocation() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateIntakeRunInvocationRejectsHeadlessByName(t *testing.T) {
	if err := validateIntakeRunInvocation("builtin:core/intake-v1.0.yaml", true); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("headless intake error = %v, want interactive-terminal rejection", err)
	}
	if err := validateIntakeRunInvocation("builtin:core/finalize-pr-v1.0.yaml", true); err != nil {
		t.Fatalf("headless non-intake error = %v, want nil", err)
	}
}

func TestResolveValidateWorkflowArgAcceptsExistingYAMLPath(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join("workflows", "custom-v1.0.yaml")
	writeTestFile(t, path, "name: custom\nsteps:\n  - id: s\n    command: echo ok\n")

	got, err := resolveValidateWorkflowArg(path)
	if err != nil {
		t.Fatalf("resolveValidateWorkflowArg returned error: %v", err)
	}
	if got != path {
		t.Fatalf("resolveValidateWorkflowArg = %q, want %q", got, path)
	}
}

func TestResolveValidateWorkflowArgAcceptsExactBuiltinRef(t *testing.T) {
	const ref = "builtin:core/debug-v1.0.yaml"

	got, err := resolveValidateWorkflowArg(ref)
	if err != nil {
		t.Fatalf("resolveValidateWorkflowArg returned error: %v", err)
	}
	if got != ref {
		t.Fatalf("resolveValidateWorkflowArg = %q, want %q", got, ref)
	}
}

func TestResolveValidateWorkflowArgReportsMissingYAMLPath(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join("workflows", "missing-v1.0.yaml")

	_, err := resolveValidateWorkflowArg(path)
	if err == nil {
		t.Fatal("resolveValidateWorkflowArg error = nil, want missing-file error")
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("resolveValidateWorkflowArg error = %v, want validation-specific missing-file guidance", err)
	}
	if strings.Contains(err.Error(), "for execution") {
		t.Fatalf("resolveValidateWorkflowArg error = %v, must not describe validation as execution", err)
	}
}

func TestResolveValidateWorkflowArgResolvesLogicalNameToLatest(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	writeTestFile(t, filepath.Join(".agent-runner", "workflows", "deploy-v1.0.yaml"), "name: deploy\nsteps:\n  - id: old\n    command: echo old\n")
	want := filepath.Join(".agent-runner", "workflows", "deploy-v2.0.yaml")
	writeTestFile(t, want, "name: deploy\nsteps:\n  - id: new\n    command: echo new\n")

	got, err := resolveValidateWorkflowArg("deploy")
	if err != nil {
		t.Fatalf("resolveValidateWorkflowArg returned error: %v", err)
	}
	if got != want {
		t.Fatalf("resolveValidateWorkflowArg = %q, want %q", got, want)
	}
}

func TestExtractProfileArgs_AcceptsProfileAfterResumeID(t *testing.T) {
	args, profile, set, err := extractProfileArgs([]string{"run-123", "--profile", " copilot "})
	if err != nil {
		t.Fatalf("extractProfileArgs() error = %v", err)
	}
	if diff := cmp.Diff([]string{"run-123"}, args); diff != "" {
		t.Fatalf("args mismatch (-want +got):\n%s", diff)
	}
	if !set || profile != "copilot" {
		t.Fatalf("profile = %q, set = %t; want copilot, true", profile, set)
	}
}

func TestExtractProfileArgs_RejectsDuplicateProfileFlag(t *testing.T) {
	_, _, _, err := extractProfileArgs([]string{"workflow", "--profile=copilot", "-profile", "work"})
	if err == nil || !strings.Contains(err.Error(), "may only be specified once") {
		t.Fatalf("extractProfileArgs() error = %v, want duplicate profile error", err)
	}
}

func TestExtractProfileArgs_RejectsOptionAsProfileValue(t *testing.T) {
	_, _, _, err := extractProfileArgs([]string{"workflow", "--profile", "--until", "step"})
	if err == nil || !strings.Contains(err.Error(), "requires a profile set name") {
		t.Fatalf("extractProfileArgs() error = %v, want missing profile value error", err)
	}
}

func TestHandleValidateArgsBindsOptionalParamsForYAMLPath(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	writeTestFile(t, filepath.Join("workflows", "green-v1.0.yaml"), "name: green\nsteps:\n  - id: s\n    command: echo ok\n")
	root := filepath.Join("workflows", "root-v1.0.yaml")
	writeTestFile(t, root, `
name: root
params:
  - name: flavor
steps:
  - id: call
    workflow: "{{flavor}}.yaml"
`)

	if code := handleValidateArgs([]string{root, "flavor=green-v1.0"}); code != 0 {
		t.Fatalf("handleValidateArgs returned %d, want 0", code)
	}
}

func TestHandleValidateArgsPrintsLegacyAgentDeprecationToStderr(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	writeTestFile(t, filepath.Join(".agent-runner", "config.yaml"), `profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
`)
	workflow := filepath.Join("workflows", "shell-v1.0.yaml")
	writeTestFile(t, workflow, "name: shell\nsteps:\n  - id: s\n    command: echo ok\n")

	originalStderr := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = originalStderr })

	code := handleValidateArgs([]string{workflow})
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	stderr, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if code != 0 {
		t.Fatalf("handleValidateArgs() = %d, want 0; stderr:\n%s", code, stderr)
	}
	const warning = `agent profile "planner" is deprecated; use "lead"`
	if count := strings.Count(string(stderr), warning); count != 1 {
		t.Fatalf("warning count = %d, want 1; stderr:\n%s", count, stderr)
	}
}

func TestRealProcessRunner_RunAgentDoesNotInheritStdin(t *testing.T) {
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.WriteString("leaked\n"); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	result, err := (&realProcessRunner{}).RunAgent(&iexec.AgentProcessOptions{
		Context: context.Background(), Args: []string{"sh", "-c", `if read x; then printf "read:%s" "$x"; else printf "eof"; fi`}, CaptureStdout: true,
	})
	if err != nil {
		t.Fatalf("RunAgent returned error: %v", err)
	}
	if result.Stdout != "eof" {
		t.Fatalf("RunAgent inherited stdin, stdout = %q", result.Stdout)
	}
}

func TestRealProcessRunner_RunAgentCanceledBeforeStartIsNotLaunched(t *testing.T) {
	for _, captureStdout := range []bool{false, true} {
		t.Run(fmt.Sprintf("capture=%t", captureStdout), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			result, err := (&realProcessRunner{}).RunAgent(&iexec.AgentProcessOptions{
				Context: ctx, Args: []string{"sh", "-c", "exit 0"}, CaptureStdout: captureStdout,
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("RunAgent() error = %v, want context canceled", err)
			}
			if result.Started {
				t.Fatalf("RunAgent() result = %#v, want Started=false", result)
			}
		})
	}
}

func TestRealProcessRunner_RunShellExposesCurrentExecutable(t *testing.T) {
	originalExecutable := currentExecutable
	t.Cleanup(func() {
		currentExecutable = originalExecutable
	})
	currentExecutable = func() (string, error) {
		return "/tmp/source-build/agent-runner", nil
	}

	result, err := (&realProcessRunner{}).RunShell(`printf %s "$AGENT_RUNNER_EXECUTABLE"`, true, "")
	if err != nil {
		t.Fatalf("RunShell returned error: %v", err)
	}
	if result.Stdout != "/tmp/source-build/agent-runner" {
		t.Fatalf("AGENT_RUNNER_EXECUTABLE = %q, want current executable", result.Stdout)
	}
}

func TestEnsureAgentRunnerExecutableEnvSetsParentEnvironment(t *testing.T) {
	originalExecutable := currentExecutable
	t.Cleanup(func() {
		currentExecutable = originalExecutable
	})
	t.Setenv(agentRunnerExecutableEnv, "")
	currentExecutable = func() (string, error) {
		return "/tmp/source-build/agent-runner", nil
	}

	ensureAgentRunnerExecutableEnv()

	if got := os.Getenv(agentRunnerExecutableEnv); got != "/tmp/source-build/agent-runner" {
		t.Fatalf("%s = %q, want current executable", agentRunnerExecutableEnv, got)
	}
}

func TestEnsureAgentRunnerExecutableEnvReplacesInheritedValue(t *testing.T) {
	originalExecutable := currentExecutable
	t.Cleanup(func() {
		currentExecutable = originalExecutable
	})
	t.Setenv(agentRunnerExecutableEnv, "/outer/agent-runner")
	currentExecutable = func() (string, error) {
		return "/tmp/source-build/agent-runner", nil
	}

	ensureAgentRunnerExecutableEnv()

	if got := os.Getenv(agentRunnerExecutableEnv); got != "/tmp/source-build/agent-runner" {
		t.Fatalf("%s = %q, want current executable", agentRunnerExecutableEnv, got)
	}
}

func TestEnsureAgentRunnerExecutableEnvFallsBackToArgv0(t *testing.T) {
	originalExecutable := currentExecutable
	originalArgs := os.Args
	t.Cleanup(func() {
		currentExecutable = originalExecutable
		os.Args = originalArgs
	})
	t.Setenv(agentRunnerExecutableEnv, "")
	currentExecutable = func() (string, error) {
		return "", errors.New("unavailable")
	}
	path := filepath.Join(t.TempDir(), "agent-runner")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write fallback executable: %v", err)
	}
	os.Args = []string{path}

	ensureAgentRunnerExecutableEnv()

	if got := os.Getenv(agentRunnerExecutableEnv); got != path {
		t.Fatalf("%s = %q, want argv[0] fallback", agentRunnerExecutableEnv, got)
	}
}

func TestRealProcessRunner_RunScriptPreservesCapturedStdout(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '  value\\n\\n'\n"), 0o700); err != nil {
		t.Fatalf("write script: %v", err)
	}

	result, err := (&realProcessRunner{}).RunScript(script, nil, true, "")
	if err != nil {
		t.Fatalf("RunScript returned error: %v", err)
	}
	if result.Stdout != "  value\n\n" {
		t.Fatalf("stdout = %q, want preserved bytes", result.Stdout)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

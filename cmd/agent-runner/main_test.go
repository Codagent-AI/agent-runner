package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWorkflowArg(t *testing.T) {
	t.Run("resolves bare user workflow from dot-agent-runner directory", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "my-workflow.yaml"), "name: test\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("my-workflow")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(".agent-runner", "workflows", "my-workflow.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("falls back to yml for bare user workflow", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "my-workflow.yml"), "name: test\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("my-workflow")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(".agent-runner", "workflows", "my-workflow.yml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("falls back to global yaml for bare user workflow", func(t *testing.T) {
		repo := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "my-workflow.yaml"), "name: test\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("my-workflow")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(home, ".agent-runner", "workflows", "my-workflow.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("falls back to global yml for bare user workflow", func(t *testing.T) {
		repo := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "my-workflow.yml"), "name: test\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("my-workflow")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(home, ".agent-runner", "workflows", "my-workflow.yml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("resolves nested bare user workflow", func(t *testing.T) {
		t.Chdir(t.TempDir())
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "team", "deploy.yaml"), "name: test\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("team/deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(".agent-runner", "workflows", "team", "deploy.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("falls back to nested global workflow", func(t *testing.T) {
		repo := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "team", "deploy.yaml"), "name: test\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("team/deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(home, ".agent-runner", "workflows", "team", "deploy.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("resolves namespaced builtin workflow", func(t *testing.T) {
		got, err := resolveWorkflowArg("core:finalize-pr")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		if got != "builtin:core/finalize-pr.yaml" {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, "builtin:core/finalize-pr.yaml")
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
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "my-workflow.yaml"), "name: local\nsteps:\n  - id: s\n    command: echo ok\n")
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "my-workflow.yaml"), "name: global\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("my-workflow")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(".agent-runner", "workflows", "my-workflow.yaml")
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("project nested workflow shadows global workflow with same path", func(t *testing.T) {
		repo := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		t.Setenv("HOME", home)
		t.Chdir(repo)
		writeTestFile(t, filepath.Join(".agent-runner", "workflows", "team", "deploy.yaml"), "name: local\nsteps:\n  - id: s\n    command: echo ok\n")
		writeTestFile(t, filepath.Join(home, ".agent-runner", "workflows", "team", "deploy.yaml"), "name: global\nsteps:\n  - id: s\n    command: echo ok\n")

		got, err := resolveWorkflowArg("team/deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		want := filepath.Join(".agent-runner", "workflows", "team", "deploy.yaml")
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
		writeTestFile(t, filepath.Join("workflows", "my-workflow.yaml"), "name: test\nsteps:\n  - id: s\n    command: echo ok\n")

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

func TestResolveValidateWorkflowArgAcceptsExistingYAMLPath(t *testing.T) {
	t.Chdir(t.TempDir())
	path := filepath.Join("workflows", "custom.yaml")
	writeTestFile(t, path, "name: custom\nsteps:\n  - id: s\n    command: echo ok\n")

	got, err := resolveValidateWorkflowArg(path)
	if err != nil {
		t.Fatalf("resolveValidateWorkflowArg returned error: %v", err)
	}
	if got != path {
		t.Fatalf("resolveValidateWorkflowArg = %q, want %q", got, path)
	}
}

func TestHandleValidateArgsBindsOptionalParamsForYAMLPath(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestFile(t, filepath.Join("workflows", "green.yaml"), "name: green\nsteps:\n  - id: s\n    command: echo ok\n")
	root := filepath.Join("workflows", "root.yaml")
	writeTestFile(t, root, `
name: root
params:
  - name: flavor
steps:
  - id: call
    workflow: "{{flavor}}.yaml"
`)

	if code := handleValidateArgs([]string{root, "flavor=green"}); code != 0 {
		t.Fatalf("handleValidateArgs returned %d, want 0", code)
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

	result, err := (&realProcessRunner{}).RunAgent([]string{"sh", "-c", `if read x; then printf "read:%s" "$x"; else printf "eof"; fi`}, true, "")
	if err != nil {
		t.Fatalf("RunAgent returned error: %v", err)
	}
	if result.Stdout != "eof" {
		t.Fatalf("RunAgent inherited stdin, stdout = %q", result.Stdout)
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

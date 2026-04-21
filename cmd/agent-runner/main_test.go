package main

import (
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

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

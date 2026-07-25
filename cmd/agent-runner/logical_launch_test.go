package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/workflowcatalog"
)

func TestResolveWorkflowArgUsesLogicalVersionCatalogs(t *testing.T) {
	t.Run("selects latest project version numerically", func(t *testing.T) {
		project, _ := setupLogicalLaunchDirs(t)
		writeLogicalWorkflow(t, filepath.Join(project, ".agent-runner", "workflows", "deploy-v2.9.yaml"), "deploy")
		want := filepath.Join(".agent-runner", "workflows", "deploy-v2.10.yaml")
		writeLogicalWorkflow(t, filepath.Join(project, want), "deploy")

		got, err := resolveWorkflowArg("deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("selects arbitrary-size nested yml version", func(t *testing.T) {
		project, _ := setupLogicalLaunchDirs(t)
		writeLogicalWorkflow(t, filepath.Join(project, ".agent-runner", "workflows", "team", "deploy-v999999999999999999999999.1.yaml"), "deploy")
		want := filepath.Join(".agent-runner", "workflows", "team", "deploy-v1000000000000000000000000.0.yml")
		writeLogicalWorkflow(t, filepath.Join(project, want), "deploy")

		got, err := resolveWorkflowArg("team/deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("project group shadows newer global group", func(t *testing.T) {
		project, home := setupLogicalLaunchDirs(t)
		want := filepath.Join(".agent-runner", "workflows", "deploy-v1.0.yaml")
		writeLogicalWorkflow(t, filepath.Join(project, want), "deploy")
		writeLogicalWorkflow(t, filepath.Join(home, ".agent-runner", "workflows", "deploy-v3.0.yaml"), "deploy")

		got, err := resolveWorkflowArg("deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want project path %q", got, want)
		}
	})

	t.Run("falls back to latest global group", func(t *testing.T) {
		_, home := setupLogicalLaunchDirs(t)
		writeLogicalWorkflow(t, filepath.Join(home, ".agent-runner", "workflows", "team", "deploy-v1.0.yaml"), "deploy")
		want := filepath.Join(home, ".agent-runner", "workflows", "team", "deploy-v1.8.yml")
		writeLogicalWorkflow(t, want, "deploy")

		got, err := resolveWorkflowArg("team/deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("invalid project group blocks global fallback", func(t *testing.T) {
		project, home := setupLogicalLaunchDirs(t)
		writeLogicalWorkflow(t, filepath.Join(project, ".agent-runner", "workflows", "deploy.yaml"), "deploy")
		writeLogicalWorkflow(t, filepath.Join(home, ".agent-runner", "workflows", "deploy-v3.0.yaml"), "deploy")

		_, err := resolveWorkflowArg("deploy")
		var filenameErr *workflowcatalog.FilenameError
		if !errors.As(err, &filenameErr) {
			t.Fatalf("resolveWorkflowArg error = %v, want FilenameError", err)
		}
		if want := filepath.Join(".agent-runner", "workflows", "deploy.yaml"); !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want invalid project file", err)
		}
	})

	t.Run("unrelated invalid project group does not block global fallback", func(t *testing.T) {
		project, home := setupLogicalLaunchDirs(t)
		writeLogicalWorkflow(t, filepath.Join(project, ".agent-runner", "workflows", "verify.yaml"), "verify")
		want := filepath.Join(home, ".agent-runner", "workflows", "deploy-v1.0.yaml")
		writeLogicalWorkflow(t, want, "deploy")

		got, err := resolveWorkflowArg("deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("duplicate project version blocks fallback", func(t *testing.T) {
		project, home := setupLogicalLaunchDirs(t)
		writeLogicalWorkflow(t, filepath.Join(project, ".agent-runner", "workflows", "deploy-v1.0.yaml"), "deploy")
		writeLogicalWorkflow(t, filepath.Join(project, ".agent-runner", "workflows", "deploy-v1.0.yml"), "deploy")
		writeLogicalWorkflow(t, filepath.Join(home, ".agent-runner", "workflows", "deploy-v2.0.yaml"), "deploy")

		_, err := resolveWorkflowArg("deploy")
		var duplicateErr *workflowcatalog.DuplicateVersionError
		if !errors.As(err, &duplicateErr) {
			t.Fatalf("resolveWorkflowArg error = %v, want DuplicateVersionError", err)
		}
	})

	t.Run("real logical name ending in dotless version resolves", func(t *testing.T) {
		project, _ := setupLogicalLaunchDirs(t)
		want := filepath.Join(".agent-runner", "workflows", "deploy-v1-v2.0.yaml")
		writeLogicalWorkflow(t, filepath.Join(project, want), "deploy-v1")

		got, err := resolveWorkflowArg("deploy-v1")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})

	t.Run("unreadable unrelated directory does not block root group", func(t *testing.T) {
		project, _ := setupLogicalLaunchDirs(t)
		want := filepath.Join(".agent-runner", "workflows", "deploy-v1.0.yaml")
		writeLogicalWorkflow(t, filepath.Join(project, want), "deploy")
		unrelated := filepath.Join(project, ".agent-runner", "workflows", "unrelated")
		if err := os.MkdirAll(unrelated, 0o755); err != nil {
			t.Fatalf("mkdir unrelated workflow directory: %v", err)
		}
		if err := os.Chmod(unrelated, 0); err != nil {
			t.Fatalf("make unrelated workflow directory unreadable: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(unrelated, 0o755) })
		if _, err := os.ReadDir(unrelated); err == nil {
			t.Skip("test process can read mode-000 directories")
		}

		got, err := resolveWorkflowArg("deploy")
		if err != nil {
			t.Fatalf("resolveWorkflowArg returned error: %v", err)
		}
		if got != want {
			t.Fatalf("resolveWorkflowArg = %q, want %q", got, want)
		}
	})
}

func TestResolveWorkflowArgValidatesFreshLaunchName(t *testing.T) {
	project, _ := setupLogicalLaunchDirs(t)
	existing := filepath.Join(project, "workflows", "deploy-v1.0.yaml")
	writeLogicalWorkflow(t, existing, "deploy")

	tests := []struct {
		name string
		arg  string
		want string
	}{
		{name: "existing exact path", arg: existing, want: `launch logical workflow "deploy"`},
		{name: "versioned filename", arg: "deploy-v1.0.yaml", want: `launch logical workflow "deploy"`},
		{name: "version-bearing name", arg: "deploy-v2.0", want: `launch logical workflow "deploy"`},
		{name: "version-bearing path-style name", arg: "team/deploy-v2.0", want: `launch logical workflow "team/deploy"`},
		{name: "uppercase", arg: "Deploy", want: "must be lowercase"},
		{name: "mixed namespace and path", arg: "core:team/deploy", want: "invalid workflow name"},
		{name: "leading slash", arg: "/team/deploy", want: "invalid workflow name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveWorkflowArg(tt.arg)
			if err == nil {
				t.Fatal("resolveWorkflowArg error = nil, want rejection")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestResolveWorkflowArgNotFoundGuidance(t *testing.T) {
	t.Run("names searched sources", func(t *testing.T) {
		_, home := setupLogicalLaunchDirs(t)
		_, err := resolveWorkflowArg("missing")
		if err == nil {
			t.Fatal("resolveWorkflowArg error = nil, want not found")
		}
		for _, want := range []string{
			`logical workflow "missing" not found`,
			filepath.Join(".agent-runner", "workflows"),
			filepath.Join(home, ".agent-runner", "workflows"),
		} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %q, want substring %q", err, want)
			}
		}
	})

	t.Run("dotless version attempt suggests version-free name after lookup", func(t *testing.T) {
		setupLogicalLaunchDirs(t)
		_, err := resolveWorkflowArg("deploy-v1")
		if err == nil || !strings.Contains(err.Error(), `launch logical workflow "deploy"`) {
			t.Fatalf("resolveWorkflowArg error = %v, want logical-name hint", err)
		}
	})

	t.Run("namespaced dotless version attempt suggests version-free name", func(t *testing.T) {
		setupLogicalLaunchDirs(t)
		_, err := resolveWorkflowArg("core:missing-v1")
		if err == nil || !strings.Contains(err.Error(), `launch logical workflow "core:missing"`) {
			t.Fatalf("resolveWorkflowArg error = %v, want namespaced logical-name hint", err)
		}
	})

	t.Run("bare name never resolves builtin", func(t *testing.T) {
		setupLogicalLaunchDirs(t)
		_, err := resolveWorkflowArg("finalize-pr")
		if err == nil || !strings.Contains(err.Error(), `logical workflow "finalize-pr" not found`) {
			t.Fatalf("resolveWorkflowArg error = %v, want disk-only not found", err)
		}
	})
}

func TestFreshCommandFormsRejectExactWorkflowPaths(t *testing.T) {
	project, _ := setupLogicalLaunchDirs(t)
	path := filepath.Join(project, "deploy-v1.0.yaml")
	writeLogicalWorkflow(t, path, "deploy")

	for _, args := range [][]string{{path}, {"run", path}} {
		normalized, _, err := parseRunCommandArgs(args)
		if err != nil {
			t.Fatalf("parseRunCommandArgs(%v): %v", args, err)
		}
		_, err = resolveWorkflowArg(normalized[0])
		if err == nil || !strings.Contains(err.Error(), `launch logical workflow "deploy"`) {
			t.Fatalf("fresh command %v error = %v, want logical-name guidance", args, err)
		}
	}
}

func setupLogicalLaunchDirs(t *testing.T) (project, home string) {
	t.Helper()
	project = t.TempDir()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(project)
	return project, home
}

func writeLogicalWorkflow(t *testing.T, filePath, name string) {
	t.Helper()
	writeTestFile(t, filePath, "name: "+name+"\nsteps:\n  - id: ok\n    command: echo ok\n")
}

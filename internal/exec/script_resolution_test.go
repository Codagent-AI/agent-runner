package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
)

func chdirTo(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func TestResolveScriptPath_EmbeddedWorkflowUsesContainingNamespace(t *testing.T) {
	sessionDir := t.TempDir()
	ctx := model.NewRootContext(&model.RootContextOptions{
		WorkflowFile: "builtin:openspec/plan-change-v1.0.yaml",
		SessionDir:   sessionDir,
	})

	got, err := resolveScriptPath("create-change.sh", ctx)
	if err != nil {
		t.Fatalf("resolveScriptPath: %v", err)
	}
	want := filepath.Join(sessionDir, "bundled", "openspec", "create-change.sh")
	if got != want {
		t.Fatalf("script path = %q, want %q", got, want)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read materialized script: %v", err)
	}
	if !strings.Contains(string(data), "change") {
		t.Fatalf("materialized script does not look like openspec/create-change.sh")
	}
}

func TestResolveScriptPath_EmbeddedWorkflowDoesNotFallbackToDisk(t *testing.T) {
	projectDir := t.TempDir()
	chdirTo(t, projectDir)
	diskScript := filepath.Join(projectDir, ".agent-runner", "workflows", "openspec", "missing.sh")
	if err := os.MkdirAll(filepath.Dir(diskScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(diskScript, []byte("#!/bin/sh\necho disk\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := model.NewRootContext(&model.RootContextOptions{
		WorkflowFile: "builtin:openspec/plan-change-v1.0.yaml",
		SessionDir:   t.TempDir(),
	})

	got, err := resolveScriptPath("missing.sh", ctx)
	if err == nil {
		t.Fatalf("resolveScriptPath = %q, want embedded-asset error", got)
	}
}

func TestResolveScriptPath_DiskWorkflowUsesContainingDirectory(t *testing.T) {
	workflowDir := t.TempDir()
	workflowPath := filepath.Join(workflowDir, "deploy-v1.0.yaml")
	scriptPath := filepath.Join(workflowDir, "helper.sh")
	if err := os.WriteFile(workflowPath, []byte("name: deploy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho disk\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := model.NewRootContext(&model.RootContextOptions{WorkflowFile: workflowPath})

	got, err := resolveScriptPath("helper.sh", ctx)
	if err != nil {
		t.Fatalf("resolveScriptPath: %v", err)
	}
	want, err := filepath.EvalSymlinks(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("script path = %q, want %q", got, want)
	}
}

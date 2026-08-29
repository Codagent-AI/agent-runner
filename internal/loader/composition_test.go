package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeWorkflow writes a YAML string to a file inside dir.
func writeWorkflow(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestValidateComposition(t *testing.T) {
	t.Run("enforces scoped parent child matrix", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "workspace-child-v1.0.yaml", `
name: workspace-child
scope: workspace
steps:
  - id: run
    command: true
`)
		root := writeWorkflow(t, dir, "repository-parent-v1.0.yaml", `
name: repository-parent
scope: repositories
params:
  - name: repositories
steps:
  - id: call
    workflow: workspace-child-v1.0.yaml
`)
		err := ValidateComposition(root)
		if err == nil || !strings.Contains(err.Error(), "workspace-scoped child") {
			t.Fatalf("ValidateComposition() error = %v, want scope matrix error", err)
		}
	})

	t.Run("accepts root with no sub-workflows", func(t *testing.T) {
		dir := t.TempDir()
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: s1
    prompt: plan
    session: planner
`)
		if err := ValidateComposition(root); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts compatible declarations across files", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "sub-v1.0.yaml", `
name: sub
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: inner
    prompt: plan
    session: planner
`)
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: call
    workflow: sub-v1.0.yaml
`)
		if err := ValidateComposition(root); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("accepts hidden sub-workflow references", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "sub-v1.0.yaml", `
name: sub
hidden: true
steps:
  - id: inner
    command: echo sub
`)
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
steps:
  - id: call
    workflow: sub-v1.0.yaml
`)
		if err := ValidateComposition(root); err != nil {
			t.Fatalf("hidden sub-workflow should validate normally: %v", err)
		}
	})

	t.Run("rejects incompatible declarations across files", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "sub-v1.0.yaml", `
name: sub
sessions:
  - name: planner
    agent: implementor-profile
steps:
  - id: inner
    prompt: plan
    session: planner
`)
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: call
    workflow: sub-v1.0.yaml
`)
		err := ValidateComposition(root)
		if err == nil {
			t.Fatal("expected error for incompatible declarations")
		}
		msg := err.Error()
		if !strings.Contains(msg, "planner") {
			t.Errorf("expected error to name the conflicting session; got: %v", err)
		}
		if !strings.Contains(msg, "root-v1.0.yaml") || !strings.Contains(msg, "sub-v1.0.yaml") {
			t.Errorf("expected error to reference both source files; got: %v", err)
		}
		if !strings.Contains(msg, "planner-profile") || !strings.Contains(msg, "implementor-profile") {
			t.Errorf("expected error to name both agent values; got: %v", err)
		}
	})

	t.Run("walks nested sub-workflows", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "inner-v1.0.yaml", `
name: inner
sessions:
  - name: planner
    agent: implementor-profile
steps:
  - id: leaf
    prompt: leaf
    session: planner
`)
		writeWorkflow(t, dir, "mid-v1.0.yaml", `
name: mid
steps:
  - id: call-inner
    workflow: inner-v1.0.yaml
`)
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: call-mid
    workflow: mid-v1.0.yaml
`)
		err := ValidateComposition(root)
		if err == nil {
			t.Fatal("expected error for conflict in grandchild sub-workflow")
		}
		if !strings.Contains(err.Error(), "planner") {
			t.Errorf("expected 'planner' in error: %v", err)
		}
	})

	t.Run("keeps exact versioned child path", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "child-v1.0.yaml", `
name: child
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: leaf
    prompt: leaf
    session: planner
`)
		writeWorkflow(t, dir, "child-v2.0.yaml", `
name: child
sessions:
  - name: planner
    agent: conflicting-profile
steps:
  - id: leaf
    prompt: leaf
    session: planner
`)
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: call
    workflow: child-v1.0.yaml
`)
		if err := ValidateComposition(root); err != nil {
			t.Fatalf("ValidateComposition selected a different child version: %v", err)
		}
	})

	t.Run("skips interpolated sub-workflow paths", func(t *testing.T) {
		dir := t.TempDir()
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
params:
  - name: sub_name
    default: "sub"
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: call
    workflow: "{{sub_name}}.yaml"
`)
		// The referenced sub-workflow doesn't exist on disk, but we expect
		// ValidateComposition to skip interpolated paths rather than fail.
		if err := ValidateComposition(root); err != nil {
			t.Fatalf("expected interpolated workflow path to be skipped; got: %v", err)
		}
	})

	t.Run("does not infinite-loop on cycles", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "a-v1.0.yaml", `
name: a
steps:
  - id: call-b
    workflow: b-v1.0.yaml
`)
		writeWorkflow(t, dir, "b-v1.0.yaml", `
name: b
steps:
  - id: call-a
    workflow: a-v1.0.yaml
`)
		if err := ValidateComposition(filepath.Join(dir, "a-v1.0.yaml")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("walks sub-workflows nested in loops", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "sub-v1.0.yaml", `
name: sub
sessions:
  - name: planner
    agent: implementor-profile
steps:
  - id: inner
    prompt: p
    session: planner
`)
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `
name: root
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: loop
    loop:
      max: 3
    steps:
      - id: call-sub
        workflow: sub-v1.0.yaml
`)
		err := ValidateComposition(root)
		if err == nil {
			t.Fatal("expected error for conflict in sub-workflow nested inside a loop")
		}
	})

	t.Run("returns load error for invalid root", func(t *testing.T) {
		dir := t.TempDir()
		root := writeWorkflow(t, dir, "root-v1.0.yaml", `not: yaml: valid: :`)
		err := ValidateComposition(root)
		if err == nil {
			t.Fatal("expected error for invalid YAML")
		}
	})
}

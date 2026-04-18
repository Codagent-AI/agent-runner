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
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestValidateComposition(t *testing.T) {
	t.Run("accepts root with no sub-workflows", func(t *testing.T) {
		dir := t.TempDir()
		root := writeWorkflow(t, dir, "root.yaml", `
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
		writeWorkflow(t, dir, "sub.yaml", `
name: sub
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: inner
    prompt: plan
    session: planner
`)
		root := writeWorkflow(t, dir, "root.yaml", `
name: root
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: call
    workflow: sub.yaml
`)
		if err := ValidateComposition(root); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects incompatible declarations across files", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "sub.yaml", `
name: sub
sessions:
  - name: planner
    agent: implementor-profile
steps:
  - id: inner
    prompt: plan
    session: planner
`)
		root := writeWorkflow(t, dir, "root.yaml", `
name: root
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: call
    workflow: sub.yaml
`)
		err := ValidateComposition(root)
		if err == nil {
			t.Fatal("expected error for incompatible declarations")
		}
		msg := err.Error()
		if !strings.Contains(msg, "planner") {
			t.Errorf("expected error to name the conflicting session; got: %v", err)
		}
		if !strings.Contains(msg, "root.yaml") || !strings.Contains(msg, "sub.yaml") {
			t.Errorf("expected error to reference both source files; got: %v", err)
		}
		if !strings.Contains(msg, "planner-profile") || !strings.Contains(msg, "implementor-profile") {
			t.Errorf("expected error to name both agent values; got: %v", err)
		}
	})

	t.Run("walks nested sub-workflows", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "inner.yaml", `
name: inner
sessions:
  - name: planner
    agent: implementor-profile
steps:
  - id: leaf
    prompt: leaf
    session: planner
`)
		writeWorkflow(t, dir, "mid.yaml", `
name: mid
steps:
  - id: call-inner
    workflow: inner.yaml
`)
		root := writeWorkflow(t, dir, "root.yaml", `
name: root
sessions:
  - name: planner
    agent: planner-profile
steps:
  - id: call-mid
    workflow: mid.yaml
`)
		err := ValidateComposition(root)
		if err == nil {
			t.Fatal("expected error for conflict in grandchild sub-workflow")
		}
		if !strings.Contains(err.Error(), "planner") {
			t.Errorf("expected 'planner' in error: %v", err)
		}
	})

	t.Run("skips interpolated sub-workflow paths", func(t *testing.T) {
		dir := t.TempDir()
		root := writeWorkflow(t, dir, "root.yaml", `
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
		writeWorkflow(t, dir, "a.yaml", `
name: a
steps:
  - id: call-b
    workflow: b.yaml
`)
		writeWorkflow(t, dir, "b.yaml", `
name: b
steps:
  - id: call-a
    workflow: a.yaml
`)
		if err := ValidateComposition(filepath.Join(dir, "a.yaml")); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("walks sub-workflows nested in loops", func(t *testing.T) {
		dir := t.TempDir()
		writeWorkflow(t, dir, "sub.yaml", `
name: sub
sessions:
  - name: planner
    agent: implementor-profile
steps:
  - id: inner
    prompt: p
    session: planner
`)
		root := writeWorkflow(t, dir, "root.yaml", `
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
        workflow: sub.yaml
`)
		err := ValidateComposition(root)
		if err == nil {
			t.Fatal("expected error for conflict in sub-workflow nested inside a loop")
		}
	})

	t.Run("returns load error for invalid root", func(t *testing.T) {
		dir := t.TempDir()
		root := writeWorkflow(t, dir, "root.yaml", `not: yaml: valid: :`)
		err := ValidateComposition(root)
		if err == nil {
			t.Fatal("expected error for invalid YAML")
		}
	})
}

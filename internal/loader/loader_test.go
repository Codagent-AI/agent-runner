package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWorkflow(t *testing.T) {
	// testdata is at the repo root
	testdata := filepath.Join("..", "..", "testdata")

	t.Run("loads a valid workflow from YAML", func(t *testing.T) {
		w, err := LoadWorkflow(filepath.Join(testdata, "valid-workflow.yaml"), Options{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.Name == "" {
			t.Fatal("expected workflow name")
		}
		if len(w.Steps) == 0 {
			t.Fatal("expected steps")
		}
	})

	t.Run("loads a minimal workflow with defaults", func(t *testing.T) {
		w, err := LoadWorkflow(filepath.Join(testdata, "minimal-workflow.yaml"), Options{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.Agent != "claude" {
			t.Fatalf("expected default agent 'claude', got %q", w.Agent)
		}
		if w.Params == nil {
			t.Fatal("expected params to be initialized")
		}
	})

	t.Run("throws for workflow with empty steps", func(t *testing.T) {
		_, err := LoadWorkflow(filepath.Join(testdata, "invalid-no-steps.yaml"), Options{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("throws for shell step without command", func(t *testing.T) {
		_, err := LoadWorkflow(filepath.Join(testdata, "invalid-shell-no-command.yaml"), Options{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("throws for non-existent file", func(t *testing.T) {
		_, err := LoadWorkflow("/nonexistent/workflow.yaml", Options{})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestInterpolateParams(t *testing.T) {
	t.Run("replaces all placeholders", func(t *testing.T) {
		result, err := InterpolateParams("hello {{name}} and {{thing}}", map[string]string{
			"name":  "world",
			"thing": "stuff",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "hello world and stuff" {
			t.Fatalf("expected 'hello world and stuff', got %q", result)
		}
	})

	t.Run("returns string unchanged when no placeholders", func(t *testing.T) {
		result, err := InterpolateParams("no placeholders", map[string]string{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "no placeholders" {
			t.Fatalf("expected unchanged string, got %q", result)
		}
	})

	t.Run("throws for missing parameter", func(t *testing.T) {
		_, err := InterpolateParams("{{missing}}", map[string]string{})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "missing parameter") {
			t.Fatalf("expected 'missing parameter' error, got: %v", err)
		}
	})

	t.Run("replaces duplicate placeholders", func(t *testing.T) {
		result, err := InterpolateParams("{{x}} and {{x}}", map[string]string{"x": "val"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "val and val" {
			t.Fatalf("expected 'val and val', got %q", result)
		}
	})

	t.Run("replaces file placeholders", func(t *testing.T) {
		dir := t.TempDir()
		fpath := filepath.Join(dir, "test.txt")
		os.WriteFile(fpath, []byte("file content"), 0o644)

		result, err := InterpolateParams("before {{file:myfile}} after", map[string]string{
			"myfile": fpath,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "file content") {
			t.Fatalf("expected file content in result, got: %q", result)
		}
		if !strings.Contains(result, "<file path=") {
			t.Fatalf("expected file XML tag in result, got: %q", result)
		}
	})
}

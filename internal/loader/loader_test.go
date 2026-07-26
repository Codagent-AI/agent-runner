package loader

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/workflowcatalog"
)

func TestLoadWorkflow(t *testing.T) {
	// testdata is at the repo root
	testdata := filepath.Join("..", "..", "testdata")

	t.Run("loads a valid workflow from YAML", func(t *testing.T) {
		w, err := LoadWorkflow(filepath.Join(testdata, "valid-workflow-v1.0.yaml"), Options{})
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
		w, err := LoadWorkflow(filepath.Join(testdata, "minimal-workflow-v1.0.yaml"), Options{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.Params == nil {
			t.Fatal("expected params to be initialized")
		}
	})

	t.Run("throws for workflow with empty steps", func(t *testing.T) {
		_, err := LoadWorkflow(filepath.Join(testdata, "invalid-no-steps-v1.0.yaml"), Options{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("throws for shell step without command", func(t *testing.T) {
		_, err := LoadWorkflow(filepath.Join(testdata, "invalid-shell-no-command-v1.0.yaml"), Options{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("throws for non-existent file", func(t *testing.T) {
		_, err := LoadWorkflow("/nonexistent/workflow-v1.0.yaml", Options{})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("loads an embedded builtin workflow", func(t *testing.T) {
		w, err := LoadWorkflow("builtin:core/finalize-pr-v1.0.yaml", Options{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if w.Name != "finalize-pr" {
			t.Fatalf("expected builtin workflow name finalize-pr, got %q", w.Name)
		}
		if !w.Hidden {
			t.Fatal("expected hidden builtin workflow to load normally")
		}
	})

	t.Run("loads hidden workflow field", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "hidden-v1.0.yaml")
		if err := os.WriteFile(path, []byte(`name: hidden
hidden: true
steps:
  - id: step1
    command: echo hidden
`), 0o600); err != nil {
			t.Fatalf("write workflow: %v", err)
		}

		w, err := LoadWorkflow(path, Options{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !w.Hidden {
			t.Fatal("expected hidden workflow field to round-trip")
		}
	})
}

func TestLoadWorkflowEnforcesVersionedSourceIdentity(t *testing.T) {
	t.Run("rejects an unversioned path before reading it", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "deploy.yaml")

		_, err := LoadWorkflow(missing, Options{})
		var filenameErr *workflowcatalog.FilenameError
		if !errors.As(err, &filenameErr) {
			t.Fatalf("error = %T %v, want *workflowcatalog.FilenameError", err, err)
		}
		if filenameErr.Path != missing {
			t.Fatalf("filename error path = %q, want %q", filenameErr.Path, missing)
		}
		if strings.Contains(err.Error(), "cannot read workflow file") {
			t.Fatalf("error = %v, want filename guidance before file read", err)
		}
	})

	t.Run("retains builtin source in filename error", func(t *testing.T) {
		const ref = "builtin:core/missing.yaml"

		_, err := LoadWorkflow(ref, Options{})
		var filenameErr *workflowcatalog.FilenameError
		if !errors.As(err, &filenameErr) {
			t.Fatalf("error = %T %v, want *workflowcatalog.FilenameError", err, err)
		}
		if filenameErr.Path != ref {
			t.Fatalf("filename error path = %q, want %q", filenameErr.Path, ref)
		}
	})

	for _, test := range []struct {
		name       string
		sourcePath string
		yamlName   string
		want       string
	}{
		{
			name:       "directory-qualified YAML name",
			sourcePath: filepath.Join("team", "deploy-v2.0.yaml"),
			yamlName:   "team/deploy",
			want:       `expected "deploy", got "team/deploy"`,
		},
		{
			name:       "version-bearing YAML name",
			sourcePath: "deploy-v2.0.yaml",
			yamlName:   "deploy-v2.0",
			want:       `expected "deploy", got "deploy-v2.0"`,
		},
		{
			name:       "uppercase YAML name",
			sourcePath: "deploy-v2.0.yaml",
			yamlName:   "Deploy",
			want:       `expected "deploy", got "Deploy"`,
		},
	} {
		t.Run("rejects "+test.name, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, test.sourcePath)
			if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
				t.Fatalf("make workflow directory: %v", err)
			}
			data := []byte("name: " + test.yamlName + "\nsteps:\n  - id: run\n    command: echo ok\n")
			if err := os.WriteFile(filePath, data, 0o600); err != nil {
				t.Fatalf("write workflow: %v", err)
			}

			_, err := LoadWorkflow(filePath, Options{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestParseWorkflowAgentTools(t *testing.T) {
	t.Run("loads call_agent on an agent step", func(t *testing.T) {
		w, err := ParseWorkflow([]byte(`
name: tool-test
steps:
  - id: review
    agent: reviewer
    tools: [call_agent]
    prompt: Review the change.
`), Options{})
		if err != nil {
			t.Fatalf("ParseWorkflow() error = %v", err)
		}
		encoded, err := json.Marshal(w.Steps[0])
		if err != nil {
			t.Fatalf("marshal loaded step: %v", err)
		}
		if !strings.Contains(string(encoded), `"tools":["call_agent"]`) {
			t.Fatalf("loaded step = %s, want call_agent in enabled tools", encoded)
		}
	})

	for _, tt := range []struct {
		name  string
		tools string
	}{
		{name: "omitted tools", tools: ""},
		{name: "empty tools", tools: "    tools: []\n"},
	} {
		t.Run(tt.name+" enables no tools", func(t *testing.T) {
			w, err := ParseWorkflow([]byte(`
name: tool-test
steps:
  - id: review
    agent: reviewer
`+tt.tools+`    prompt: Review the change.
`), Options{})
			if err != nil {
				t.Fatalf("ParseWorkflow() error = %v", err)
			}
			encoded, err := json.Marshal(w.Steps[0])
			if err != nil {
				t.Fatalf("marshal loaded step: %v", err)
			}
			if strings.Contains(string(encoded), `"tools"`) {
				t.Fatalf("loaded step = %s, want no enabled tools", encoded)
			}
		})
	}

	for _, tt := range []struct {
		name        string
		declaration string
		wantError   string
	}{
		{
			name:        "unknown tool",
			declaration: "    tools: [other]\n",
			wantError:   "unknown tool",
		},
		{
			name:        "duplicate tool",
			declaration: "    tools: [call_agent, call_agent]\n",
			wantError:   "duplicate tool",
		},
		{
			name:        "scalar declaration",
			declaration: "    tools: call_agent\n",
			wantError:   "tools",
		},
		{
			name:        "placeholder declaration",
			declaration: "    tools: ['{{tool_name}}']\n",
			wantError:   "unknown tool",
		},
	} {
		t.Run("rejects "+tt.name, func(t *testing.T) {
			_, err := ParseWorkflow([]byte(`
name: tool-test
steps:
  - id: review
    agent: reviewer
`+tt.declaration+`    prompt: Review the change.
`), Options{})
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("ParseWorkflow() error = %v, want error containing %q", err, tt.wantError)
			}
		})
	}
}

func TestParseWorkflowSourceEnforcesNameWhileParseWorkflowRemainsContentOnly(t *testing.T) {
	data := []byte("name: other\nsteps:\n  - id: run\n    command: echo ok\n")

	if _, err := ParseWorkflow(data, Options{}); err != nil {
		t.Fatalf("ParseWorkflow content-only parse returned error: %v", err)
	}

	_, err := ParseWorkflowSource(data, "nested/deploy-v1.0.yml", Options{})
	if err == nil || !strings.Contains(err.Error(), `expected "deploy", got "other"`) {
		t.Fatalf("ParseWorkflowSource error = %v, want name-alignment error", err)
	}
}

func TestParseWorkflowRejectsToolsOnNonAgentSteps(t *testing.T) {
	for _, tt := range []struct {
		name        string
		declaration string
	}{
		{name: "call_agent", declaration: "    tools: [call_agent]\n"},
		{name: "empty sequence", declaration: "    tools: []\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseWorkflow([]byte(`
name: tool-test
steps:
  - id: shell
    command: echo hi
`+tt.declaration), Options{})
			if err == nil || !strings.Contains(err.Error(), `"tools" is only allowed on agent steps`) {
				t.Fatalf("ParseWorkflow() error = %v, want agent-only tools error", err)
			}
		})
	}

	t.Run("nested shell declaration", func(t *testing.T) {
		_, err := ParseWorkflow([]byte(`
name: tool-test
steps:
  - id: repeated
    loop:
      max: 1
    steps:
      - id: shell
        command: echo hi
        tools: []
`), Options{})
		if err == nil || !strings.Contains(err.Error(), `"tools" is only allowed on agent steps`) {
			t.Fatalf("ParseWorkflow() error = %v, want nested agent-only tools error", err)
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

func TestValidateComposition_EmbeddedBuiltinUsesEmbeddedSubworkflow(t *testing.T) {
	t.Chdir(t.TempDir())
	writeWorkflow(t, filepath.Join(".agent-runner", "workflows"), "plan-change-v1.0.yaml", `
name: plan-change
steps:
  - id: local
    command: not valid builtin syntax
`)

	if err := ValidateComposition("builtin:spec-driven/change-v1.0.yaml"); err != nil {
		t.Fatalf("expected embedded composition to resolve embedded sub-workflows, got %v", err)
	}
}

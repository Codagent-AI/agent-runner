package exec

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
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

func TestResolveScriptPath_EmbeddedScriptBringsItsSiblingScripts(t *testing.T) {
	sessionDir := t.TempDir()
	ctx := model.NewRootContext(&model.RootContextOptions{
		WorkflowFile: "builtin:openspec/archive-change-v1.0.yaml",
		SessionDir:   sessionDir,
	})

	got, err := resolveScriptPath("archive-transition.sh", ctx)
	if err != nil {
		t.Fatalf("resolveScriptPath: %v", err)
	}
	sibling := filepath.Join(filepath.Dir(got), "validate-change-name.sh")
	info, err := os.Stat(sibling)
	if err != nil {
		t.Fatalf("archive-transition.sh calls validate-change-name.sh, which was not materialized: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("sibling script mode = %v, want executable", info.Mode())
	}
}

// Every "$script_dir/<name>" call in an embedded script must resolve once that
// script alone is materialized, because steps reference only the entry script.
func TestResolveScriptPath_EveryEmbeddedSiblingCallResolves(t *testing.T) {
	call := regexp.MustCompile(`"\$script_dir/([^"]+)"`)
	err := fs.WalkDir(builtinworkflows.FS, ".", func(assetPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || path.Ext(assetPath) != ".sh" {
			return walkErr
		}
		namespace, rel, ok := strings.Cut(assetPath, "/")
		if !ok {
			return nil
		}
		data, err := fs.ReadFile(builtinworkflows.FS, assetPath)
		if err != nil {
			return err
		}
		calls := call.FindAllStringSubmatch(string(data), -1)
		if len(calls) == 0 {
			return nil
		}
		sessionDir := t.TempDir()
		ctx := model.NewRootContext(&model.RootContextOptions{
			WorkflowFile: "builtin:" + namespace + "/workflow-v1.0.yaml",
			SessionDir:   sessionDir,
		})
		got, err := resolveScriptPath(rel, ctx)
		if err != nil {
			t.Errorf("resolveScriptPath(%s): %v", assetPath, err)
			return nil
		}
		for _, c := range calls {
			if _, err := os.Stat(filepath.Join(filepath.Dir(got), filepath.FromSlash(c[1]))); err != nil {
				t.Errorf("%s calls %s, which is missing after materialization: %v", assetPath, c[1], err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

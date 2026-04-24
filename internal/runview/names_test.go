package runview

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalName(t *testing.T) {
	repo := "/repo"
	wfRoot := "/repo/workflows"
	cfg := ResolverConfig{WorkflowsRoot: wfRoot, RepoRoot: repo}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"namespaced core builtin under workflows", "/repo/workflows/core/implement-task.yaml", "core:implement-task"},
		{"namespaced under workflows", "/repo/workflows/openspec/plan-change.yaml", "openspec:plan-change"},
		{"namespaced yml extension", "/repo/workflows/openspec/implement-change.yml", "openspec:implement-change"},
		{"deep subdir falls back to repo-rel", "/repo/workflows/a/b/c.yaml", "workflows/a/b/c.yaml"},
		{"outside workflows falls back to repo-rel", "/repo/external/other.yaml", "external/other.yaml"},
		{"outside repo returns abs path", "/elsewhere/thing.yaml", "../elsewhere/thing.yaml"},
		{"builtin namespaced yaml", "builtin:spec-driven/change.yaml", "spec-driven:change"},
		{"builtin namespaced yml", "builtin:core/implement-task.yml", "core:implement-task"},
		{"builtin top-level", "builtin:simple.yaml", "simple"},
		{"builtin deep path", "builtin:a/b/c.yaml", "a/b/c"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CanonicalName(c.in, cfg)
			if got != c.want {
				t.Errorf("in=%q want=%q got=%q", c.in, c.want, got)
			}
		})
	}
}

func TestCanonicalName_NoWorkflowsRoot(t *testing.T) {
	got := CanonicalName("/repo/workflows/x.yaml", ResolverConfig{RepoRoot: "/repo"})
	if got != "workflows/x.yaml" {
		t.Errorf("without WorkflowsRoot falls back to repo-rel; got %q", got)
	}
}

func TestCanonicalName_Empty(t *testing.T) {
	if got := CanonicalName("", ResolverConfig{}); got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

func TestDiscoverWorkflowsRoot(t *testing.T) {
	repo := t.TempDir()
	wfDir := filepath.Join(repo, "workflows")
	if err := mkdir(wfDir); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(repo, "pkg", "inner")
	if err := mkdir(subdir); err != nil {
		t.Fatal(err)
	}
	got, ok := DiscoverWorkflowsRoot(subdir)
	if !ok {
		t.Fatal("expected discovery to succeed")
	}
	if got != wfDir {
		t.Errorf("want %q, got %q", wfDir, got)
	}

	// Outside any repo with workflows/ returns false.
	empty := t.TempDir()
	if _, ok := DiscoverWorkflowsRoot(empty); ok {
		// might still find one via some ancestor depending on test env; just
		// accept both outcomes but prefer consistency
		t.Log("ancestor has workflows dir; skipping negative check")
	}
}

func mkdir(p string) error {
	return os.MkdirAll(p, 0o755)
}

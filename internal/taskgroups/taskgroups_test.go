package taskgroups

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseConfiguredGroupsPreservesRepositoryAndTaskOrder(t *testing.T) {
	workspace := t.TempDir()
	changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks.md"), `## Repository: backend

- [ ] [Create API](tasks/backend/02-api.md)
- [ ] [Add storage](tasks/backend/10-storage.md)

## Repository: frontend

- [ ] [Build UI](tasks/frontend/01-ui.md)
`)
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "backend", "02-api.md"), "# API\n")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "backend", "10-storage.md"), "# Storage\n")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "frontend", "01-ui.md"), "# UI\n")
	canonicalChangeDir, err := filepath.EvalSymlinks(changeDir)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := Parse(Options{
		WorkspaceDir: workspace,
		ChangeDir:    changeDir,
		PlanKind:     Full,
		Repositories: []string{"backend", "frontend"},
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if diff := cmp.Diff([]string{"backend", "frontend"}, plan.Repositories); diff != "" {
		t.Fatalf("repositories mismatch (-want +got):\n%s", diff)
	}
	wantBackend := []string{
		filepath.Join(canonicalChangeDir, "tasks", "backend", "02-api.md"),
		filepath.Join(canonicalChangeDir, "tasks", "backend", "10-storage.md"),
	}
	if diff := cmp.Diff(wantBackend, plan.Groups[0].Tasks); diff != "" {
		t.Fatalf("backend tasks mismatch (-want +got):\n%s", diff)
	}
	if got, want := plan.TaskPattern("backend"), filepath.Join(canonicalChangeDir, "tasks", "backend", "*.md"); got != want {
		t.Fatalf("TaskPattern(backend) = %q, want %q", got, want)
	}
	if len(plan.Snapshot.Groups) == 0 || plan.Fingerprint == "" {
		t.Fatalf("snapshot/fingerprint must be populated: %#v", plan)
	}
}

func TestParseConfiguredGroupsRejectsUnlinkedTaskFile(t *testing.T) {
	workspace := t.TempDir()
	changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks.md"), `## Repository: backend

- [ ] [Linked](tasks/backend/01-linked.md)
`)
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "backend", "01-linked.md"), "# Linked\n")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "backend", "02-unlinked.md"), "# Unlinked\n")

	_, err := Parse(Options{
		WorkspaceDir: workspace,
		ChangeDir:    changeDir,
		PlanKind:     Full,
		Repositories: []string{"backend"},
	})
	if err == nil || !strings.Contains(err.Error(), "unlinked") {
		t.Fatalf("Parse() error = %v, want unlinked task failure", err)
	}
}

func TestParseImplicitPlanShapes(t *testing.T) {
	workspace := t.TempDir()
	changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "10-last.md"), "# Last\n")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "02-first.md"), "# First\n")

	full, err := Parse(Options{WorkspaceDir: workspace, ChangeDir: changeDir, PlanKind: Full})
	if err != nil {
		t.Fatalf("Parse(full) error = %v", err)
	}
	if diff := cmp.Diff([]string{"default"}, full.Repositories); diff != "" {
		t.Fatalf("full repositories mismatch (-want +got):\n%s", diff)
	}
	if got, want := filepath.Base(full.Groups[0].Tasks[0]), "02-first.md"; got != want {
		t.Fatalf("first full task = %q, want %q", got, want)
	}

	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks.md"), "# One small task\n")
	simple, err := Parse(Options{WorkspaceDir: workspace, ChangeDir: changeDir, PlanKind: Simple})
	if err != nil {
		t.Fatalf("Parse(simple) error = %v", err)
	}
	if got, want := filepath.Base(simple.Groups[0].Tasks[0]), "tasks.md"; got != want {
		t.Fatalf("simple task = %q, want %q", got, want)
	}
}

func TestTaskPatternEscapesLiteralWorkspacePath(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace[one]")
	changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks.md"), `## Repository: backend

- [ ] [API](tasks/backend/01-api.md)
`)
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "backend", "01-api.md"), "# API\n")

	plan, err := Parse(Options{
		WorkspaceDir: workspace,
		ChangeDir:    changeDir,
		PlanKind:     Full,
		Repositories: []string{"backend"},
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	matches, err := filepath.Glob(plan.TaskPattern("backend"))
	if err != nil {
		t.Fatalf("Glob(%q) error = %v", plan.TaskPattern("backend"), err)
	}
	if diff := cmp.Diff(plan.Groups[0].Tasks, matches); diff != "" {
		t.Fatalf("literal-safe pattern mismatch (-want +got):\n%s", diff)
	}
}

func TestParseConfiguredGroupRejectsTasksFromDifferentDirectories(t *testing.T) {
	workspace := t.TempDir()
	changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks.md"), `## Repository: backend

- [ ] [API](tasks/backend/api/01-api.md)
- [ ] [Database](tasks/backend/db/02-database.md)
`)
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "backend", "api", "01-api.md"), "# API\n")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "backend", "db", "02-database.md"), "# Database\n")

	_, err := Parse(Options{
		WorkspaceDir: workspace,
		ChangeDir:    changeDir,
		PlanKind:     Full,
		Repositories: []string{"backend"},
	})
	if err == nil || !strings.Contains(err.Error(), "same directory") {
		t.Fatalf("Parse() error = %v, want single-directory task group failure", err)
	}
}

func TestParseConfiguredSimplePlanAcceptsMonolithicTaskFile(t *testing.T) {
	workspace := t.TempDir()
	changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks.md"), "# Small change\n")

	plan, err := Parse(Options{
		WorkspaceDir: workspace,
		ChangeDir:    changeDir,
		PlanKind:     Simple,
		Repositories: []string{"backend"},
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if diff := cmp.Diff([]string{"default"}, plan.Repositories); diff != "" {
		t.Fatalf("repositories mismatch (-want +got):\n%s", diff)
	}
	if got, want := filepath.Base(plan.Groups[0].Tasks[0]), "tasks.md"; got != want {
		t.Fatalf("task file = %q, want %q", got, want)
	}
}

func TestParseConfiguredSimplePlanIgnoresOrdinaryRepositorySectionHeading(t *testing.T) {
	workspace := t.TempDir()
	changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks.md"), "## Repository setup\n\n# Small change\n")

	plan, err := Parse(Options{
		WorkspaceDir: workspace,
		ChangeDir:    changeDir,
		PlanKind:     Simple,
		Repositories: []string{"backend"},
	})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := filepath.Base(plan.Groups[0].Tasks[0]), "tasks.md"; got != want {
		t.Fatalf("task file = %q, want %q", got, want)
	}
}

func TestParseConfiguredGroupsRejectsInvalidOwnership(t *testing.T) {
	tests := []struct {
		name         string
		repositories []string
		index        string
		files        map[string]string
	}{
		{
			name:         "unknown repository",
			repositories: []string{"backend"},
			index:        "## Repository: frontend\n\n- [ ] [UI](tasks/frontend/01-ui.md)\n",
			files:        map[string]string{"tasks/frontend/01-ui.md": "# UI\n"},
		},
		{
			name:         "repeated repository",
			repositories: []string{"backend"},
			index: "## Repository: backend\n\n- [ ] [API](tasks/backend/01-api.md)\n\n" +
				"## Repository: backend\n\n- [ ] [More](tasks/backend/02-more.md)\n",
			files: map[string]string{"tasks/backend/01-api.md": "# API\n", "tasks/backend/02-more.md": "# More\n"},
		},
		{
			name:         "task without group",
			repositories: []string{"backend"},
			index:        "- [ ] [API](tasks/backend/01-api.md)\n",
			files:        map[string]string{"tasks/backend/01-api.md": "# API\n"},
		},
		{
			name:         "cross group task",
			repositories: []string{"backend", "frontend"},
			index: "## Repository: backend\n\n- [ ] [UI](tasks/frontend/01-ui.md)\n\n" +
				"## Repository: frontend\n\n- [ ] [Actual UI](tasks/frontend/02-ui.md)\n",
			files: map[string]string{"tasks/frontend/01-ui.md": "# UI\n", "tasks/frontend/02-ui.md": "# UI\n"},
		},
		{
			name:         "duplicate task link",
			repositories: []string{"backend"},
			index: "## Repository: backend\n\n- [ ] [API](tasks/backend/01-api.md)\n" +
				"- [ ] [API again](tasks/backend/01-api.md)\n",
			files: map[string]string{"tasks/backend/01-api.md": "# API\n"},
		},
		{
			name:         "empty task",
			repositories: []string{"backend"},
			index:        "## Repository: backend\n\n- [ ] [API](tasks/backend/01-api.md)\n",
			files:        map[string]string{"tasks/backend/01-api.md": ""},
		},
		{
			name:         "traversal task link",
			repositories: []string{"backend"},
			index:        "## Repository: backend\n\n- [ ] [API](tasks/backend/%2e%2e/backend/01-api.md)\n",
			files:        map[string]string{"tasks/backend/01-api.md": "# API\n"},
		},
		{
			name:         "malformed heading",
			repositories: []string{"backend"},
			index:        "## Repository: backend extra words\n\n- [ ] [API](tasks/backend/01-api.md)\n",
			files:        map[string]string{"tasks/backend/01-api.md": "# API\n"},
		},
		{
			name:         "empty group",
			repositories: []string{"backend"},
			index:        "## Repository: backend\n",
			files:        map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
			writeTaskGroupFile(t, filepath.Join(changeDir, "tasks.md"), tt.index)
			for relative, content := range tt.files {
				writeTaskGroupFile(t, filepath.Join(changeDir, filepath.FromSlash(relative)), content)
			}
			if _, err := Parse(Options{WorkspaceDir: workspace, ChangeDir: changeDir, PlanKind: Full, Repositories: tt.repositories}); err == nil {
				t.Fatal("Parse() succeeded, want invalid ownership failure")
			}
		})
	}
}

func TestPlanSnapshotChangesForTaskGroupDrift(t *testing.T) {
	parse := func(t *testing.T, index string, files map[string]string) Plan {
		t.Helper()
		workspace := t.TempDir()
		changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
		writeTaskGroupFile(t, filepath.Join(changeDir, "tasks.md"), index)
		for relative, content := range files {
			writeTaskGroupFile(t, filepath.Join(changeDir, filepath.FromSlash(relative)), content)
		}
		plan, err := Parse(Options{WorkspaceDir: workspace, ChangeDir: changeDir, PlanKind: Full, Repositories: []string{"backend", "frontend"}})
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}

	baseline := parse(t, "## Repository: backend\n\n- [ ] [API](tasks/backend/01-api.md)\n\n## Repository: frontend\n\n- [ ] [UI](tasks/frontend/01-ui.md)\n", map[string]string{
		"tasks/backend/01-api.md": "# API\n", "tasks/frontend/01-ui.md": "# UI\n",
	})
	identical := parse(t, "## Repository: backend\n\n- [ ] [API](tasks/backend/01-api.md)\n\n## Repository: frontend\n\n- [ ] [UI](tasks/frontend/01-ui.md)\n", map[string]string{
		"tasks/backend/01-api.md": "# API\n", "tasks/frontend/01-ui.md": "# UI\n",
	})
	reordered := parse(t, "## Repository: frontend\n\n- [ ] [UI](tasks/frontend/01-ui.md)\n\n## Repository: backend\n\n- [ ] [API](tasks/backend/01-api.md)\n", map[string]string{
		"tasks/backend/01-api.md": "# API\n", "tasks/frontend/01-ui.md": "# UI\n",
	})
	changedTaskSet := parse(t, "## Repository: backend\n\n- [ ] [API](tasks/backend/02-api.md)\n\n## Repository: frontend\n\n- [ ] [UI](tasks/frontend/01-ui.md)\n", map[string]string{
		"tasks/backend/02-api.md": "# API\n", "tasks/frontend/01-ui.md": "# UI\n",
	})

	if baseline.Fingerprint == reordered.Fingerprint || baseline.Fingerprint == changedTaskSet.Fingerprint {
		t.Fatalf("fingerprint did not detect task-group drift: baseline=%s reordered=%s changed=%s", baseline.Fingerprint, reordered.Fingerprint, changedTaskSet.Fingerprint)
	}
	if baseline.Fingerprint != identical.Fingerprint {
		t.Fatalf("identical task plans have different fingerprints: %s vs %s", baseline.Fingerprint, identical.Fingerprint)
	}
	if diff := cmp.Diff(baseline.Snapshot, identical.Snapshot); diff != "" {
		t.Fatalf("identical snapshot was not stable (-want +got):\n%s", diff)
	}
}

func TestParseConfiguredGroupsRejectsTaskSymlinkOutsideGroup(t *testing.T) {
	workspace := t.TempDir()
	changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
	outside := filepath.Join(workspace, "outside.md")
	writeTaskGroupFile(t, outside, "# Outside\n")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks.md"), "## Repository: backend\n\n- [ ] [API](tasks/backend/01-api.md)\n")
	if err := os.MkdirAll(filepath.Join(changeDir, "tasks", "backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(changeDir, "tasks", "backend", "01-api.md")); err != nil {
		t.Fatal(err)
	}

	if _, err := Parse(Options{WorkspaceDir: workspace, ChangeDir: changeDir, PlanKind: Full, Repositories: []string{"backend"}}); err == nil {
		t.Fatal("Parse() succeeded for task symlink outside the repository group")
	}
}

func TestParseConfiguredGroupsRejectsTaskIndexSymlinkOutsideChange(t *testing.T) {
	workspace := t.TempDir()
	changeDir := filepath.Join(workspace, "openspec", "changes", "demo")
	outsideIndex := filepath.Join(workspace, "outside-tasks.md")
	writeTaskGroupFile(t, outsideIndex, "## Repository: backend\n\n- [ ] [API](tasks/backend/01-api.md)\n")
	writeTaskGroupFile(t, filepath.Join(changeDir, "tasks", "backend", "01-api.md"), "# API\n")
	if err := os.Symlink(outsideIndex, filepath.Join(changeDir, "tasks.md")); err != nil {
		t.Fatal(err)
	}

	_, err := Parse(Options{WorkspaceDir: workspace, ChangeDir: changeDir, PlanKind: Full, Repositories: []string{"backend"}})
	if err == nil || !strings.Contains(err.Error(), "task index") {
		t.Fatalf("Parse() error = %v, want task index symlink failure", err)
	}
}

func writeTaskGroupFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

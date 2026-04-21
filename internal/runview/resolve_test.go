package runview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codagent/agent-runner/internal/model"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

// writeFile is a tiny t.Fatal-on-error helper used throughout the resolver
// tests to keep fixtures terse.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeMeta(t *testing.T, projectDir, originCwd string) {
	t.Helper()
	data, err := json.Marshal(map[string]string{"path": originCwd})
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	writeFile(t, filepath.Join(projectDir, "meta.json"), string(data))
}

const minimalWorkflowYAML = `name: example
steps:
  - id: only
    command: echo ok
`

// scenarioDirs sets up a repo layout at root with one workflow and a
// project dir containing a session directory. It does not touch meta.json.
type scenarioDirs struct {
	root        string // simulated repo root
	workflow    string // absolute path to the workflow file
	projectDir  string // absolute path to ~/.agent-runner/projects/<encoded>
	sessionDir  string // absolute path to .../runs/<sessionID>
	sessionName string // bare session ID
}

func scenarioA(t *testing.T) scenarioDirs {
	t.Helper()
	base := realPath(t, t.TempDir())
	s := scenarioDirs{
		root:        filepath.Join(base, "repo"),
		projectDir:  filepath.Join(base, "projects", "encoded"),
		sessionName: "example-2026-04-11T09-14-00-000000000Z",
	}
	s.workflow = filepath.Join(s.root, "workflows", "example.yaml")
	s.sessionDir = filepath.Join(s.projectDir, "runs", s.sessionName)
	writeFile(t, s.workflow, minimalWorkflowYAML)
	if err := os.MkdirAll(s.sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session: %v", err)
	}
	return s
}

// realPath resolves symlinks so tests can compare paths produced by
// os.Getwd() (canonicalized on macOS: /var → /private/var) against paths
// composed via filepath.Join from t.TempDir() (which is not canonical).
func realPath(t *testing.T, p string) string {
	t.Helper()
	out, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("eval symlinks %s: %v", p, err)
	}
	return out
}

// chdirTo temporarily changes the process cwd, restoring it on test cleanup.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

func TestResolveWorkflow_AbsolutePathInState(t *testing.T) {
	s := scenarioA(t)
	// Put process cwd somewhere irrelevant to ensure absolute wins.
	chdirTo(t, t.TempDir())

	state := model.RunState{WorkflowFile: s.workflow, WorkflowName: "example"}
	got, ok := ResolveWorkflow(s.sessionDir, s.projectDir, &state)
	if !ok {
		t.Fatal("expected ResolveWorkflow to succeed for absolute path")
	}
	if got.AbsPath != filepath.Clean(s.workflow) {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, s.workflow)
	}
	wantRoot := filepath.Join(s.root, "workflows")
	if got.WorkflowsRoot != wantRoot {
		t.Fatalf("WorkflowsRoot = %q, want %q", got.WorkflowsRoot, wantRoot)
	}
	if got.RepoRoot != s.root {
		t.Fatalf("RepoRoot = %q, want %q", got.RepoRoot, s.root)
	}
}

func TestResolveWorkflow_RelativeViaMetaJSON(t *testing.T) {
	s := scenarioA(t)
	writeMeta(t, s.projectDir, s.root)

	// Process cwd is elsewhere — forces the resolver to consult meta.json.
	chdirTo(t, t.TempDir())

	state := model.RunState{WorkflowFile: "workflows/example.yaml", WorkflowName: "example"}
	got, ok := ResolveWorkflow(s.sessionDir, s.projectDir, &state)
	if !ok {
		t.Fatal("expected ResolveWorkflow to succeed via meta.json")
	}
	if got.OriginCwd != s.root {
		t.Fatalf("OriginCwd = %q, want %q", got.OriginCwd, s.root)
	}
	if got.AbsPath != filepath.Clean(s.workflow) {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, s.workflow)
	}
}

func TestReadMetaCwd_RejectsNonAbsolute(t *testing.T) {
	// meta.json with a relative path would, if accepted, cause resolution
	// to silently depend on the process cwd — contradicting the documented
	// "true absolute path or nothing" invariant. readMetaCwd MUST reject it.
	projectDir := t.TempDir()
	writeMeta(t, projectDir, "relative/path")

	if got := readMetaCwd(projectDir); got != "" {
		t.Fatalf("readMetaCwd with relative path = %q, want %q", got, "")
	}
}

func TestResolveWorkflow_RelativeViaProcessCwd(t *testing.T) {
	s := scenarioA(t)
	// No meta.json this time.
	chdirTo(t, s.root)

	state := model.RunState{WorkflowFile: "workflows/example.yaml", WorkflowName: "example"}
	got, ok := ResolveWorkflow(s.sessionDir, s.projectDir, &state)
	if !ok {
		t.Fatal("expected ResolveWorkflow to succeed via process cwd")
	}
	if got.AbsPath != filepath.Clean(s.workflow) {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, s.workflow)
	}
}

func TestResolveWorkflow_DiscoveryByName_WhenFileMoved(t *testing.T) {
	base := realPath(t, t.TempDir())
	root := filepath.Join(base, "repo")
	// Workflow lives under a namespace subdir...
	movedWorkflow := filepath.Join(root, "workflows", "openspec", "plan-change.yaml")
	writeFile(t, movedWorkflow, minimalWorkflowYAML)

	projectDir := filepath.Join(base, "projects", "encoded")
	sessionDir := filepath.Join(projectDir, "runs", "plan-change-2026-04-11T09-14-00-000000000Z")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMeta(t, projectDir, root)
	chdirTo(t, t.TempDir())

	// ...but state still records the OLD location.
	state := model.RunState{
		WorkflowFile: "workflows/plan-change.yaml",
		WorkflowName: "plan-change",
	}
	got, ok := ResolveWorkflow(sessionDir, projectDir, &state)
	if !ok {
		t.Fatal("expected ResolveWorkflow to fall back to name discovery")
	}
	if got.AbsPath != filepath.Clean(movedWorkflow) {
		t.Fatalf("AbsPath = %q, want %q (discovered by name)", got.AbsPath, movedWorkflow)
	}
	wantRoot := filepath.Join(root, "workflows")
	if got.WorkflowsRoot != wantRoot {
		t.Fatalf("WorkflowsRoot = %q, want %q", got.WorkflowsRoot, wantRoot)
	}
}

func TestResolveWorkflow_DiscoveryByNamespacedName(t *testing.T) {
	base := realPath(t, t.TempDir())
	root := filepath.Join(base, "repo")
	namespaced := filepath.Join(root, "workflows", "openspec", "plan-change.yaml")
	writeFile(t, namespaced, minimalWorkflowYAML)

	projectDir := filepath.Join(base, "projects", "encoded")
	sessionDir := filepath.Join(projectDir, "runs", "openspec-plan-change-2026-04-11T09-14-00-000000000Z")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMeta(t, projectDir, root)
	chdirTo(t, t.TempDir())

	state := model.RunState{WorkflowName: "openspec:plan-change"}
	got, ok := ResolveWorkflow(sessionDir, projectDir, &state)
	if !ok {
		t.Fatal("expected namespaced discovery to succeed")
	}
	if got.AbsPath != filepath.Clean(namespaced) {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, namespaced)
	}
}

func TestResolveWorkflow_FallbackToSessionIDName(t *testing.T) {
	s := scenarioA(t)
	writeMeta(t, s.projectDir, s.root)
	chdirTo(t, t.TempDir())

	// state.WorkflowName is empty — forces the resolver to recover the name
	// from the session ID itself ("example-...Z" → "example").
	state := model.RunState{}
	got, ok := ResolveWorkflow(s.sessionDir, s.projectDir, &state)
	if !ok {
		t.Fatal("expected ResolveWorkflow to recover name from session ID")
	}
	if got.AbsPath != filepath.Clean(s.workflow) {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, s.workflow)
	}
}

func TestResolveWorkflow_NoMatch_StillReportsRoots(t *testing.T) {
	base := realPath(t, t.TempDir())
	root := filepath.Join(base, "repo")
	// Workflows dir exists but contains no matching file.
	if err := os.MkdirAll(filepath.Join(root, "workflows"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	projectDir := filepath.Join(base, "projects", "encoded")
	sessionDir := filepath.Join(projectDir, "runs", "absent-2026-04-11T09-14-00-000000000Z")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMeta(t, projectDir, root)
	chdirTo(t, t.TempDir())

	got, ok := ResolveWorkflow(sessionDir, projectDir, &model.RunState{})
	if ok {
		t.Fatalf("expected ResolveWorkflow to report miss, got %+v", got)
	}
	// Even on miss, roots should be populated from the origin cwd.
	wantRoot := filepath.Join(root, "workflows")
	if got.WorkflowsRoot != wantRoot {
		t.Fatalf("WorkflowsRoot = %q, want %q", got.WorkflowsRoot, wantRoot)
	}
	if got.RepoRoot != root {
		t.Fatalf("RepoRoot = %q, want %q", got.RepoRoot, root)
	}
}

// TestNew_BuildsTreeFromResolvedWorkflow exercises the end-to-end path from
// the list TUI: sessionDir and projectDir point into a fake agent-runner
// projects layout; meta.json records the run's original cwd; the process cwd
// is elsewhere (simulating "viewing a worktree run from the main repo").
func TestNew_BuildsTreeFromResolvedWorkflow(t *testing.T) {
	base := realPath(t, t.TempDir())
	root := filepath.Join(base, "repo")

	workflowYAML := `name: implement-change
steps:
  - id: first
    command: echo first
  - id: second
    command: echo second
`
	wfPath := filepath.Join(root, "workflows", "implement-change.yaml")
	writeFile(t, wfPath, workflowYAML)

	projectDir := filepath.Join(base, "projects", "encoded")
	sessionID := "implement-change-2026-04-11T09-14-00-000000000Z"
	sessionDir := filepath.Join(projectDir, "runs", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session: %v", err)
	}

	// Write state.json with a cwd-RELATIVE workflow path — the bug case.
	state := model.RunState{
		WorkflowFile: "workflows/implement-change.yaml",
		WorkflowName: "implement-change",
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	writeFile(t, filepath.Join(sessionDir, "state.json"), string(data))
	writeMeta(t, projectDir, root)

	// Process cwd is somewhere unrelated — this reproduces the original bug.
	chdirTo(t, realPath(t, t.TempDir()))

	m, err := New(sessionDir, projectDir, FromList)
	if err != nil {
		t.Fatalf("runview.New: %v", err)
	}
	if m.loadErr != "" {
		t.Fatalf("unexpected load error: %s", m.loadErr)
	}
	if m.tree == nil || m.tree.Root == nil {
		t.Fatal("expected a populated tree")
	}
	if n := len(m.tree.Root.Children); n != 2 {
		t.Fatalf("tree root children = %d, want 2", n)
	}
	if m.tree.Root.Children[0].ID != "first" || m.tree.Root.Children[1].ID != "second" {
		t.Fatalf("unexpected child IDs: %q, %q",
			m.tree.Root.Children[0].ID, m.tree.Root.Children[1].ID)
	}
	if m.resolverCfg.WorkflowsRoot != filepath.Join(root, "workflows") {
		t.Fatalf("resolverCfg.WorkflowsRoot = %q, want %q",
			m.resolverCfg.WorkflowsRoot, filepath.Join(root, "workflows"))
	}
}

// TestNew_ReportsErrorWhenWorkflowMissing verifies the improved error
// surfacing: instead of silent "No steps to display", the load error is now
// populated so the UI can tell the user what went wrong.
func TestNew_ReportsErrorWhenWorkflowMissing(t *testing.T) {
	base := realPath(t, t.TempDir())
	projectDir := filepath.Join(base, "projects", "encoded")
	sessionDir := filepath.Join(projectDir, "runs", "missing-2026-04-11T09-14-00-000000000Z")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	state := model.RunState{
		WorkflowFile: "workflows/gone.yaml",
		WorkflowName: "gone",
	}
	data, _ := json.Marshal(state)
	writeFile(t, filepath.Join(sessionDir, "state.json"), string(data))
	chdirTo(t, realPath(t, t.TempDir()))

	m, err := New(sessionDir, projectDir, FromList)
	if err != nil {
		t.Fatalf("runview.New returned error: %v", err)
	}
	if m.loadErr == "" {
		t.Fatal("expected loadErr to be set when workflow cannot be resolved")
	}
}

func TestResolveWorkflow_BacktracksFromStatePath(t *testing.T) {
	// When state.WorkflowFile points to a path inside workflows/, verify
	// WorkflowsRoot is derived from that absolute path (not from the
	// origin-cwd guess), so the CanonicalName resolver works even when
	// origin-cwd is an ancestor of multiple repos.
	base := realPath(t, t.TempDir())
	root := filepath.Join(base, "repo")
	wfPath := filepath.Join(root, "workflows", "example.yaml")
	writeFile(t, wfPath, minimalWorkflowYAML)

	projectDir := filepath.Join(base, "projects", "encoded")
	sessionDir := filepath.Join(projectDir, "runs", "example-2026-04-11T09-14-00-000000000Z")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMeta(t, projectDir, root)
	chdirTo(t, t.TempDir())

	state := model.RunState{WorkflowFile: wfPath, WorkflowName: "example"}
	got, ok := ResolveWorkflow(sessionDir, projectDir, &state)
	if !ok {
		t.Fatal("expected ResolveWorkflow to succeed")
	}
	wantRoot := filepath.Join(root, "workflows")
	if got.WorkflowsRoot != wantRoot {
		t.Fatalf("WorkflowsRoot = %q, want %q", got.WorkflowsRoot, wantRoot)
	}
	if got.RepoRoot != root {
		t.Fatalf("RepoRoot = %q, want %q", got.RepoRoot, root)
	}
}

func TestResolveWorkflow_DiscoveryByName_AgentRunnerWorkflowsDir(t *testing.T) {
	base := realPath(t, t.TempDir())
	root := filepath.Join(base, "repo")

	// Workflow lives under .agent-runner/workflows/ (user-defined workflows).
	wfPath := filepath.Join(root, ".agent-runner", "workflows", "smoke-test.yaml")
	writeFile(t, wfPath, minimalWorkflowYAML)

	projectDir := filepath.Join(base, "projects", "encoded")
	sessionDir := filepath.Join(projectDir, "runs", "smoke-test-2026-04-11T09-14-00-000000000Z")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMeta(t, projectDir, root)
	chdirTo(t, t.TempDir())

	// State records the .agent-runner/workflows/ relative path, but the
	// process cwd is elsewhere so direct file resolution fails. The resolver
	// must fall back to name-based discovery under .agent-runner/workflows/.
	state := model.RunState{
		WorkflowFile: ".agent-runner/workflows/smoke-test.yaml",
		WorkflowName: "smoke-test",
	}
	got, ok := ResolveWorkflow(sessionDir, projectDir, &state)
	if !ok {
		t.Fatal("expected ResolveWorkflow to find workflow under .agent-runner/workflows/")
	}
	if got.AbsPath != filepath.Clean(wfPath) {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, wfPath)
	}
}

func TestResolveWorkflow_BuiltinRef(t *testing.T) {
	base := realPath(t, t.TempDir())
	projectDir := filepath.Join(base, "projects", "encoded")
	sessionDir := filepath.Join(projectDir, "runs", "change-2026-04-11T09-14-00-000000000Z")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// No meta.json, process cwd is unrelated — forces the resolver to
	// recognise the builtin: prefix without any filesystem search.
	chdirTo(t, realPath(t, t.TempDir()))

	// Pick a builtin workflow that we know is embedded.
	ref := builtinworkflows.Ref("openspec/change.yaml")

	state := model.RunState{WorkflowFile: ref, WorkflowName: "change"}
	got, ok := ResolveWorkflow(sessionDir, projectDir, &state)
	if !ok {
		t.Fatalf("expected ResolveWorkflow to succeed for builtin ref %q", ref)
	}
	if got.AbsPath != ref {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, ref)
	}
}

func TestNew_BuiltinWorkflowShowsSteps(t *testing.T) {
	base := realPath(t, t.TempDir())
	projectDir := filepath.Join(base, "projects", "encoded")
	sessionDir := filepath.Join(projectDir, "runs", "change-2026-04-11T09-14-00-000000000Z")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ref := builtinworkflows.Ref("openspec/change.yaml")
	state := model.RunState{WorkflowFile: ref, WorkflowName: "change"}
	data, _ := json.Marshal(state)
	writeFile(t, filepath.Join(sessionDir, "state.json"), string(data))
	chdirTo(t, realPath(t, t.TempDir()))

	m, err := New(sessionDir, projectDir, FromList)
	if err != nil {
		t.Fatalf("runview.New: %v", err)
	}
	if m.loadErr != "" {
		t.Fatalf("unexpected load error: %s", m.loadErr)
	}
	if m.tree == nil || m.tree.Root == nil {
		t.Fatal("expected a populated tree")
	}
	if len(m.tree.Root.Children) == 0 {
		t.Fatal("expected steps in tree but got none (\"No steps to display\" bug)")
	}
}

func TestResolveWorkflow_DiscoveryByName_OldPathFallsBackToAgentRunnerDir(t *testing.T) {
	base := realPath(t, t.TempDir())
	root := filepath.Join(base, "repo")

	// Workflow is now under .agent-runner/workflows/ but state has the old
	// workflows/ relative path (from before the builtin workflows migration).
	wfPath := filepath.Join(root, ".agent-runner", "workflows", "smoke-test.yaml")
	writeFile(t, wfPath, minimalWorkflowYAML)

	projectDir := filepath.Join(base, "projects", "encoded")
	sessionDir := filepath.Join(projectDir, "runs", "smoke-test-2026-04-11T09-14-00-000000000Z")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeMeta(t, projectDir, root)
	chdirTo(t, t.TempDir())

	// Old-style state: file path points to workflows/smoke-test.yaml which
	// no longer exists. Name-based discovery should find it under
	// .agent-runner/workflows/.
	state := model.RunState{
		WorkflowFile: "workflows/smoke-test.yaml",
		WorkflowName: "smoke-test",
	}
	got, ok := ResolveWorkflow(sessionDir, projectDir, &state)
	if !ok {
		t.Fatal("expected ResolveWorkflow to fall back to .agent-runner/workflows/ by name")
	}
	if got.AbsPath != filepath.Clean(wfPath) {
		t.Fatalf("AbsPath = %q, want %q", got.AbsPath, wfPath)
	}
}

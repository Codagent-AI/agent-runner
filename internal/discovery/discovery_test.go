package discovery_test

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/codagent/agent-runner/internal/discovery"
)

// minimalYAML returns a minimal valid workflow YAML with name and optional description.
func workflowYAML(description string) []byte {
	if description == "" {
		return []byte("name: test-workflow\nsteps:\n  - name: step1\n    prompt: do something\n")
	}
	return []byte("name: test-workflow\ndescription: " + description + "\nsteps:\n  - name: step1\n    prompt: do something\n")
}

func malformedYAML() []byte {
	return []byte("{{{not valid yaml")
}

func makeFakeFSWithBuiltins() fstest.MapFS {
	return fstest.MapFS{
		"core/finalize-pr.yaml":    {Data: workflowYAML("Finalize a pull request")},
		"core/implement-task.yaml": {Data: workflowYAML("Implement a task")},
		"openspec/change.yaml":     {Data: workflowYAML("OpenSpec change workflow")},
	}
}

// TestEnumerate_BuiltinsOnly returns builtin entries when no project/user dirs exist.
func TestEnumerate_BuiltinsOnly(t *testing.T) {
	builtinFS := makeFakeFSWithBuiltins()
	entries := discovery.Enumerate(builtinFS, "", "")

	if len(entries) == 0 {
		t.Fatal("expected builtin entries, got none")
	}

	// All returned entries should be builtin scope.
	for _, e := range entries {
		if e.Scope != discovery.ScopeBuiltin {
			t.Errorf("entry %q: expected ScopeBuiltin, got %v", e.CanonicalName, e.Scope)
		}
	}
}

// TestEnumerate_BuiltinCanonicalNames verifies namespace:name format for builtins.
func TestEnumerate_BuiltinCanonicalNames(t *testing.T) {
	builtinFS := makeFakeFSWithBuiltins()
	entries := discovery.Enumerate(builtinFS, "", "")

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.CanonicalName] = true
	}

	wantNames := []string{"core:finalize-pr", "core:implement-task", "openspec:change"}
	for _, want := range wantNames {
		if !names[want] {
			t.Errorf("expected canonical name %q in results, not found", want)
		}
	}
}

// TestEnumerate_BuiltinDescription verifies description is extracted from YAML.
func TestEnumerate_BuiltinDescription(t *testing.T) {
	builtinFS := makeFakeFSWithBuiltins()
	entries := discovery.Enumerate(builtinFS, "", "")

	for _, e := range entries {
		if e.CanonicalName == "core:finalize-pr" {
			if e.Description != "Finalize a pull request" {
				t.Errorf("core:finalize-pr description = %q, want %q", e.Description, "Finalize a pull request")
			}
			return
		}
	}
	t.Error("core:finalize-pr entry not found")
}

// TestEnumerate_BuiltinNamespaceSorting verifies same-namespace entries are grouped together.
func TestEnumerate_BuiltinNamespaceSorting(t *testing.T) {
	builtinFS := makeFakeFSWithBuiltins()
	entries := discovery.Enumerate(builtinFS, "", "")

	var namespaces []string
	lastNS := ""
	for _, e := range entries {
		if e.Scope != discovery.ScopeBuiltin {
			continue
		}
		if e.Namespace != lastNS {
			namespaces = append(namespaces, e.Namespace)
			lastNS = e.Namespace
		}
	}

	// core should appear before openspec alphabetically.
	if len(namespaces) < 2 {
		t.Fatalf("expected at least 2 namespaces, got %v", namespaces)
	}
	if namespaces[0] != "core" {
		t.Errorf("first namespace = %q, want %q", namespaces[0], "core")
	}
	if namespaces[1] != "openspec" {
		t.Errorf("second namespace = %q, want %q", namespaces[1], "openspec")
	}
}

// TestEnumerate_ProjectWorkflowsFirst verifies project entries appear before builtins.
func TestEnumerate_ProjectWorkflowsFirst(t *testing.T) {
	builtinFS := makeFakeFSWithBuiltins()

	// Create a temp dir with a project workflow.
	tmp := t.TempDir()
	wfDir := filepath.Join(tmp, ".agent-runner", "workflows")
	if err := mkdirAll(wfDir); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(wfDir, "deploy.yaml"), workflowYAML("")); err != nil {
		t.Fatal(err)
	}

	entries := discovery.Enumerate(builtinFS, tmp, "")

	if len(entries) == 0 {
		t.Fatal("expected entries")
	}
	if entries[0].Scope != discovery.ScopeProject {
		t.Errorf("first entry scope = %v, want ScopeProject", entries[0].Scope)
	}
	if entries[0].CanonicalName != "deploy" {
		t.Errorf("first entry name = %q, want %q", entries[0].CanonicalName, "deploy")
	}
}

// TestEnumerate_UserWorkflowsBetweenProjectAndBuiltin verifies ordering.
func TestEnumerate_UserWorkflowsBetweenProjectAndBuiltin(t *testing.T) {
	builtinFS := makeFakeFSWithBuiltins()

	// Create a temp project with project workflow.
	projectTmp := t.TempDir()
	projWfDir := filepath.Join(projectTmp, ".agent-runner", "workflows")
	if err := mkdirAll(projWfDir); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(projWfDir, "proj-workflow.yaml"), workflowYAML("")); err != nil {
		t.Fatal(err)
	}

	// Create a temp user workflows dir.
	userTmp := t.TempDir()
	if err := writeFile(filepath.Join(userTmp, "user-workflow.yaml"), workflowYAML("")); err != nil {
		t.Fatal(err)
	}

	entries := discovery.Enumerate(builtinFS, projectTmp, userTmp)

	var scopes []discovery.Scope
	lastScope := discovery.Scope(-1)
	for _, e := range entries {
		if e.Scope != lastScope {
			scopes = append(scopes, e.Scope)
			lastScope = e.Scope
		}
	}

	if len(scopes) < 3 {
		t.Fatalf("expected 3 scope groups, got %d: %v", len(scopes), scopes)
	}
	if scopes[0] != discovery.ScopeProject {
		t.Errorf("first scope = %v, want ScopeProject", scopes[0])
	}
	if scopes[1] != discovery.ScopeUser {
		t.Errorf("second scope = %v, want ScopeUser", scopes[1])
	}
	if scopes[2] != discovery.ScopeBuiltin {
		t.Errorf("third scope = %v, want ScopeBuiltin", scopes[2])
	}
}

// TestEnumerate_MalformedWorkflow verifies ParseError is set for unreadable files.
func TestEnumerate_MalformedWorkflow(t *testing.T) {
	builtinFS := fstest.MapFS{
		"core/good.yaml":      {Data: workflowYAML("Good workflow")},
		"core/malformed.yaml": {Data: malformedYAML()},
	}

	entries := discovery.Enumerate(builtinFS, "", "")

	var malformed *discovery.WorkflowEntry
	for i := range entries {
		if entries[i].CanonicalName == "core:malformed" {
			malformed = &entries[i]
			break
		}
	}
	if malformed == nil {
		t.Fatal("expected malformed entry in results")
	}
	if malformed.ParseError == "" {
		t.Error("expected ParseError to be set for malformed workflow")
	}
}

// TestEnumerate_NoDescription verifies entry with no description has empty Description.
func TestEnumerate_NoDescription(t *testing.T) {
	builtinFS := fstest.MapFS{
		"core/nodesc.yaml": {Data: workflowYAML("")},
	}
	entries := discovery.Enumerate(builtinFS, "", "")

	for _, e := range entries {
		if e.CanonicalName == "core:nodesc" {
			if e.Description != "" {
				t.Errorf("expected empty description, got %q", e.Description)
			}
			return
		}
	}
	t.Error("core:nodesc entry not found")
}

// TestEnumerate_EmptyBuiltinFS returns empty slice for empty builtin FS.
func TestEnumerate_EmptyBuiltinFS(t *testing.T) {
	entries := discovery.Enumerate(fstest.MapFS{}, "", "")
	if len(entries) != 0 {
		t.Errorf("expected empty result for empty FS, got %d entries", len(entries))
	}
}

// TestWorkflowEntry_SourcePath verifies source path is set for all entries.
func TestWorkflowEntry_SourcePath(t *testing.T) {
	builtinFS := makeFakeFSWithBuiltins()
	entries := discovery.Enumerate(builtinFS, "", "")

	for _, e := range entries {
		if e.SourcePath == "" {
			t.Errorf("entry %q: SourcePath is empty", e.CanonicalName)
		}
	}
}

// TestEnumerate_Params_Populated verifies that Params are extracted from workflow YAML.
func TestEnumerate_Params_Populated(t *testing.T) {
	yaml := []byte(`name: my-workflow
description: A workflow with params
params:
  - name: task_file
    required: true
  - name: branch
    required: false
    default: main
  - name: tag
steps:
  - id: step1
    command: echo hello
`)
	builtinFS := fstest.MapFS{
		"core/my-workflow.yaml": {Data: yaml},
	}
	entries := discovery.Enumerate(builtinFS, "", "")

	var entry *discovery.WorkflowEntry
	for i := range entries {
		if entries[i].CanonicalName == "core:my-workflow" {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		t.Fatal("core:my-workflow not found")
	}
	if len(entry.Params) != 3 {
		t.Fatalf("expected 3 params, got %d", len(entry.Params))
	}
	if entry.Params[0].Name != "task_file" {
		t.Errorf("params[0].Name = %q, want %q", entry.Params[0].Name, "task_file")
	}
	if entry.Params[0].Required == nil || !*entry.Params[0].Required {
		t.Errorf("params[0].Required should be true")
	}
	if entry.Params[1].Name != "branch" {
		t.Errorf("params[1].Name = %q, want %q", entry.Params[1].Name, "branch")
	}
	if entry.Params[1].Default != "main" {
		t.Errorf("params[1].Default = %q, want %q", entry.Params[1].Default, "main")
	}
	// params[2] has no required/default — nil required (defaults to required=true in app logic)
	if entry.Params[2].Name != "tag" {
		t.Errorf("params[2].Name = %q, want %q", entry.Params[2].Name, "tag")
	}
}

// TestEnumerate_Params_EmptyWhenNone verifies Params is nil/empty when workflow has no params.
func TestEnumerate_Params_EmptyWhenNone(t *testing.T) {
	builtinFS := fstest.MapFS{
		"core/nodesc.yaml": {Data: workflowYAML("")},
	}
	entries := discovery.Enumerate(builtinFS, "", "")
	for _, e := range entries {
		if e.CanonicalName == "core:nodesc" {
			if len(e.Params) != 0 {
				t.Errorf("expected no params, got %d", len(e.Params))
			}
			return
		}
	}
	t.Error("core:nodesc not found")
}

// Helpers.
func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

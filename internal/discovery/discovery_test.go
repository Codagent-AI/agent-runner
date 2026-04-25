package discovery_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/codagent/agent-runner/internal/discovery"
)

func TestEnumerate_AllScopesOrderedWithMetadata(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "build.yaml"), validWorkflowYAML("Build the project"))
	writeWorkflow(t, filepath.Join(userDir, "deploy.yaml"), workflowWithParamsYAML("Deploy the app"))

	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, userDir)

	gotNames := canonicalNames(entries)
	wantNames := []string{
		"build",
		"deploy",
		"core:finalize-pr",
		"core:implement-task",
		"spec-driven:change",
	}
	if fmt.Sprint(gotNames) != fmt.Sprint(wantNames) {
		t.Fatalf("canonical names = %v, want %v", gotNames, wantNames)
	}

	build := entryByName(t, entries, "build")
	if build.Scope != discovery.ScopeProject {
		t.Fatalf("build scope = %v, want %v", build.Scope, discovery.ScopeProject)
	}
	if build.Description != "Build the project" {
		t.Fatalf("build description = %q, want %q", build.Description, "Build the project")
	}

	deploy := entryByName(t, entries, "deploy")
	if deploy.Scope != discovery.ScopeUser {
		t.Fatalf("deploy scope = %v, want %v", deploy.Scope, discovery.ScopeUser)
	}
	if deploy.Description != "Deploy the app" {
		t.Fatalf("deploy description = %q, want %q", deploy.Description, "Deploy the app")
	}
	if len(deploy.Params) != 3 {
		t.Fatalf("deploy params len = %d, want 3", len(deploy.Params))
	}
	if deploy.Params[0].Name != "environment" {
		t.Fatalf("deploy first param = %q, want %q", deploy.Params[0].Name, "environment")
	}
	if deploy.Params[0].Required == nil || !*deploy.Params[0].Required {
		t.Fatalf("deploy first param required = %v, want true", deploy.Params[0].Required)
	}
	if deploy.Params[1].Default != "main" {
		t.Fatalf("deploy second param default = %q, want %q", deploy.Params[1].Default, "main")
	}

	builtin := entryByName(t, entries, "core:finalize-pr")
	if builtin.Scope != discovery.ScopeBuiltin {
		t.Fatalf("builtin scope = %v, want %v", builtin.Scope, discovery.ScopeBuiltin)
	}
	if builtin.Namespace != "core" {
		t.Fatalf("builtin namespace = %q, want %q", builtin.Namespace, "core")
	}
	if builtin.SourcePath == "" {
		t.Fatal("builtin source path should not be empty")
	}
}

func TestEnumerate_MissingProjectDirectoryContributesNoEntries(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()
	writeWorkflow(t, filepath.Join(userDir, "deploy.yaml"), validWorkflowYAML("Deploy"))

	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, userDir)

	for _, entry := range entries {
		if entry.Scope == discovery.ScopeProject {
			t.Fatalf("unexpected project entry: %+v", entry)
		}
	}

	gotNames := canonicalNames(entries)
	wantNames := []string{"deploy", "core:finalize-pr", "core:implement-task", "spec-driven:change"}
	if fmt.Sprint(gotNames) != fmt.Sprint(wantNames) {
		t.Fatalf("canonical names = %v, want %v", gotNames, wantNames)
	}
}

func TestEnumerate_ProjectShadowsUserWorkflow(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "deploy.yaml"), validWorkflowYAML("Project deploy"))
	writeWorkflow(t, filepath.Join(userDir, "deploy.yaml"), validWorkflowYAML("User deploy"))

	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, userDir)

	if countByName(entries, "deploy") != 1 {
		t.Fatalf("deploy count = %d, want 1", countByName(entries, "deploy"))
	}

	deploy := entryByName(t, entries, "deploy")
	if deploy.Scope != discovery.ScopeProject {
		t.Fatalf("deploy scope = %v, want %v", deploy.Scope, discovery.ScopeProject)
	}
	if deploy.Description != "Project deploy" {
		t.Fatalf("deploy description = %q, want %q", deploy.Description, "Project deploy")
	}
}

func TestEnumerate_BuiltinNamesAreNotShadowedByDiskWorkflows(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "finalize-pr.yaml"), validWorkflowYAML("Project finalize"))
	writeWorkflow(t, filepath.Join(userDir, "core", "finalize-pr.yaml"), validWorkflowYAML("User builtin-style path"))

	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, userDir)

	gotNames := canonicalNames(entries)
	wantNames := []string{
		"finalize-pr",
		"core/finalize-pr",
		"core:finalize-pr",
		"core:implement-task",
		"spec-driven:change",
	}
	if fmt.Sprint(gotNames) != fmt.Sprint(wantNames) {
		t.Fatalf("canonical names = %v, want %v", gotNames, wantNames)
	}
}

func TestEnumerate_ReportsMalformedFilesWithoutBlockingOtherEntries(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "broken.yaml"), malformedYAML())
	writeWorkflow(t, filepath.Join(userDir, "bad-syntax.yaml"), malformedYAML())
	writeWorkflow(t, filepath.Join(userDir, "good.yaml"), validWorkflowYAML("Good workflow"))
	writeWorkflow(t, filepath.Join(userDir, "also-good.yaml"), validWorkflowYAML("Also good"))

	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, userDir)

	broken := entryByName(t, entries, "broken")
	if broken.Scope != discovery.ScopeProject {
		t.Fatalf("broken scope = %v, want %v", broken.Scope, discovery.ScopeProject)
	}
	if broken.ParseError == "" {
		t.Fatal("broken workflow should include a parse error")
	}
	if broken.Description != "" {
		t.Fatalf("broken description = %q, want empty", broken.Description)
	}
	if len(broken.Params) != 0 {
		t.Fatalf("broken params len = %d, want 0", len(broken.Params))
	}

	badSyntax := entryByName(t, entries, "bad-syntax")
	if badSyntax.Scope != discovery.ScopeUser {
		t.Fatalf("bad-syntax scope = %v, want %v", badSyntax.Scope, discovery.ScopeUser)
	}
	if badSyntax.ParseError == "" {
		t.Fatal("bad-syntax workflow should include a parse error")
	}

	good := entryByName(t, entries, "good")
	if good.Description != "Good workflow" {
		t.Fatalf("good description = %q, want %q", good.Description, "Good workflow")
	}

	alsoGood := entryByName(t, entries, "also-good")
	if alsoGood.Description != "Also good" {
		t.Fatalf("also-good description = %q, want %q", alsoGood.Description, "Also good")
	}
}

func TestEnumerate_UsesWorkflowLoaderValidation(t *testing.T) {
	projectDir := t.TempDir()

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "needs-agent.yaml"), invalidWorkflowYAML())

	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, "")

	entry := entryByName(t, entries, "needs-agent")
	if entry.ParseError == "" {
		t.Fatal("needs-agent workflow should include a validation error")
	}
	if !strings.Contains(entry.ParseError, `"agent" is required`) {
		t.Fatalf("parse error = %q, want validation error mentioning missing agent", entry.ParseError)
	}
}

func TestEnumerate_PrefersYAMLOverYMLForDuplicateNames(t *testing.T) {
	projectDir := t.TempDir()

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "deploy.yml"), validWorkflowYAML("from yml"))
	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "deploy.yaml"), validWorkflowYAML("from yaml"))

	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, "")

	if countByName(entries, "deploy") != 1 {
		t.Fatalf("deploy count = %d, want 1", countByName(entries, "deploy"))
	}

	deploy := entryByName(t, entries, "deploy")
	if deploy.Description != "from yaml" {
		t.Fatalf("deploy description = %q, want %q", deploy.Description, "from yaml")
	}
	if !strings.HasSuffix(filepath.ToSlash(deploy.SourcePath), "deploy.yaml") {
		t.Fatalf("deploy source path = %q, want .yaml file", deploy.SourcePath)
	}
}

func fakeBuiltinFS() fstest.MapFS {
	return fstest.MapFS{
		"core/finalize-pr.yaml":    {Data: validWorkflowYAML("Finalize a pull request")},
		"core/implement-task.yaml": {Data: validWorkflowYAML("Implement a task")},
		"spec-driven/change.yaml":  {Data: validWorkflowYAML("Spec-driven change")},
	}
}

func validWorkflowYAML(description string) []byte {
	return []byte(fmt.Sprintf(`name: test-workflow
description: %s
steps:
  - id: step1
    command: echo hello
`, description))
}

func workflowWithParamsYAML(description string) []byte {
	return []byte(fmt.Sprintf(`name: deploy-workflow
description: %s
params:
  - name: environment
    required: true
  - name: branch
    required: false
    default: main
  - name: tag
steps:
  - id: step1
    command: echo deploy
`, description))
}

func invalidWorkflowYAML() []byte {
	return []byte(`name: invalid-workflow
steps:
  - id: step1
    prompt: do the thing
`)
}

func malformedYAML() []byte {
	return []byte("{{{not valid yaml")
}

func writeWorkflow(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func canonicalNames(entries []discovery.WorkflowEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.CanonicalName)
	}
	return names
}

func countByName(entries []discovery.WorkflowEntry, name string) int {
	count := 0
	for _, entry := range entries {
		if entry.CanonicalName == name {
			count++
		}
	}
	return count
}

func entryByName(t *testing.T, entries []discovery.WorkflowEntry, name string) discovery.WorkflowEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.CanonicalName == name {
			return entry
		}
	}
	t.Fatalf("entry %q not found in %v", name, canonicalNames(entries))
	return discovery.WorkflowEntry{}
}

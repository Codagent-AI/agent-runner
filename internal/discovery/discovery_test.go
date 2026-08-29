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

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "build-v1.0.yaml"), validWorkflowYAML("Build the project"))
	writeWorkflow(t, filepath.Join(userDir, "deploy-v1.0.yaml"), workflowWithParamsYAML("Deploy the app"))

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

func TestEnumerate_CopiesHiddenField(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "hidden-local-v1.0.yaml"), hiddenWorkflowYAML("Hidden local"))
	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, userDir)

	local := entryByName(t, entries, "hidden-local")
	if !local.Hidden {
		t.Fatal("local hidden workflow should be marked hidden in discovery")
	}

	builtin := entryByName(t, entries, "core:implement-task")
	if !builtin.Hidden {
		t.Fatal("builtin hidden workflow should be marked hidden in discovery")
	}
}

func TestEnumerate_SkipsUnderscorePrefixedWorkflowFiles(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "_local.yaml"), validWorkflowYAML("Local metadata"))
	writeWorkflow(t, filepath.Join(userDir, "_user.yaml"), validWorkflowYAML("User metadata"))

	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, userDir)

	for _, name := range []string{"_local", "_user", "core:_group"} {
		if countByName(entries, name) != 0 {
			t.Fatalf("underscore-prefixed workflow %q should not be enumerated: %v", name, canonicalNames(entries))
		}
	}
}

func TestEnumerate_SkipsTopLevelBuiltinWorkflowFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"top-level.yaml":             {Data: validWorkflowYAML("Top level")},
		"core/finalize-pr-v1.0.yaml": {Data: namedWorkflowYAML("finalize-pr", "Finalize")},
	}

	entries := discovery.Enumerate(fsys, "", "")

	if countByName(entries, "top-level") != 0 {
		t.Fatalf("top-level builtin workflow should not be enumerated: %v", canonicalNames(entries))
	}
	if countByName(entries, "core:finalize-pr") != 1 {
		t.Fatalf("expected namespaced builtin workflow, got %v", canonicalNames(entries))
	}
}

func TestEnumerateGroups_LoadsBuiltinMetadata(t *testing.T) {
	entries := discovery.Enumerate(fakeBuiltinFS(), "", "")

	groups := discovery.EnumerateGroups(fakeBuiltinFS(), entries)

	core := groupByScopeNamespace(t, groups, discovery.ScopeBuiltin, "core")
	if core.DisplayName != "Core Tools" {
		t.Fatalf("core display name = %q, want Core Tools", core.DisplayName)
	}
	if core.Description != "Shared implementation workflows." {
		t.Fatalf("core description = %q, want metadata description", core.Description)
	}
}

func TestEnumerateGroups_DefaultsWhenMetadataAbsent(t *testing.T) {
	fsys := fstest.MapFS{
		"extra/deploy-v1.0.yaml": {Data: namedWorkflowYAML("deploy", "Deploy")},
	}
	entries := discovery.Enumerate(fsys, "", "")

	groups := discovery.EnumerateGroups(fsys, entries)

	extra := groupByScopeNamespace(t, groups, discovery.ScopeBuiltin, "extra")
	if extra.DisplayName != "extra" {
		t.Fatalf("display name = %q, want namespace fallback", extra.DisplayName)
	}
	if extra.Description != "" {
		t.Fatalf("description = %q, want empty fallback", extra.Description)
	}
}

func TestEnumerateGroups_DefaultsWhenMetadataMalformed(t *testing.T) {
	fsys := fstest.MapFS{
		"broken/deploy-v1.0.yaml": {Data: namedWorkflowYAML("deploy", "Deploy")},
		"broken/_group.yaml":      {Data: malformedYAML()},
	}
	entries := discovery.Enumerate(fsys, "", "")

	groups := discovery.EnumerateGroups(fsys, entries)

	broken := groupByScopeNamespace(t, groups, discovery.ScopeBuiltin, "broken")
	if broken.DisplayName != "broken" {
		t.Fatalf("display name = %q, want namespace fallback", broken.DisplayName)
	}
	if broken.Description != "" {
		t.Fatalf("description = %q, want empty fallback", broken.Description)
	}
}

func TestEnumerate_MissingProjectDirectoryContributesNoEntries(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()
	writeWorkflow(t, filepath.Join(userDir, "deploy-v1.0.yaml"), validWorkflowYAML("Deploy"))

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

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "deploy-v1.0.yaml"), validWorkflowYAML("Project deploy"))
	writeWorkflow(t, filepath.Join(userDir, "deploy-v1.0.yaml"), validWorkflowYAML("User deploy"))

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

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "finalize-pr-v1.0.yaml"), validWorkflowYAML("Project finalize"))
	writeWorkflow(t, filepath.Join(userDir, "core", "finalize-pr-v1.0.yaml"), validWorkflowYAML("User builtin-style path"))

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

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "broken-v1.0.yaml"), malformedYAML())
	writeWorkflow(t, filepath.Join(userDir, "bad-syntax-v1.0.yaml"), malformedYAML())
	writeWorkflow(t, filepath.Join(userDir, "good-v1.0.yaml"), validWorkflowYAML("Good workflow"))
	writeWorkflow(t, filepath.Join(userDir, "also-good-v1.0.yaml"), validWorkflowYAML("Also good"))

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

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "needs-agent-v1.0.yaml"), invalidWorkflowYAML())

	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, "")

	entry := entryByName(t, entries, "needs-agent")
	if entry.ParseError == "" {
		t.Fatal("needs-agent workflow should include a validation error")
	}
	if !strings.Contains(entry.ParseError, `"agent" is required`) {
		t.Fatalf("parse error = %q, want validation error mentioning missing agent", entry.ParseError)
	}
}

func TestEnumerate_BuiltinUsesSourceAwareNameValidation(t *testing.T) {
	fsys := fstest.MapFS{
		"core/deploy-v1.0.yml": {
			Data: []byte("name: other\nsteps:\n  - id: run\n    command: echo ok\n"),
		},
	}

	entries := discovery.Enumerate(fsys, "", "")

	deploy := entryByName(t, entries, "core:deploy")
	if !strings.Contains(deploy.ParseError, `expected "deploy", got "other"`) {
		t.Fatalf("parse error = %q, want source-aware name mismatch", deploy.ParseError)
	}
}

func TestEnumerate_DuplicateVersionGroupIsInvalid(t *testing.T) {
	projectDir := t.TempDir()

	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "deploy-v1.0.yml"), validWorkflowYAML("from yml"))
	writeWorkflow(t, filepath.Join(projectDir, ".agent-runner", "workflows", "deploy-v1.0.yaml"), validWorkflowYAML("from yaml"))

	entries := discovery.Enumerate(fakeBuiltinFS(), projectDir, "")

	if countByName(entries, "deploy") != 1 {
		t.Fatalf("deploy count = %d, want 1", countByName(entries, "deploy"))
	}

	deploy := entryByName(t, entries, "deploy")
	for _, filename := range []string{"deploy-v1.0.yaml", "deploy-v1.0.yml"} {
		if !strings.Contains(deploy.ParseError, filename) {
			t.Fatalf("deploy parse error = %q, want duplicate diagnostic naming %q", deploy.ParseError, filename)
		}
	}
	if deploy.SourcePath != "" {
		t.Fatalf("deploy source path = %q, want no launchable path", deploy.SourcePath)
	}
}

func TestEnumerate_SelectsLatestVersionMetadataAndExactPath(t *testing.T) {
	projectDir := t.TempDir()
	root := filepath.Join(projectDir, ".agent-runner", "workflows")
	writeWorkflow(t, filepath.Join(root, "deploy-v1.0.yaml"), validWorkflowYAML("Older visible deploy"))
	writeWorkflow(t, filepath.Join(root, "deploy-v2.0.yaml"), []byte(`name: deploy
description: Latest hidden deploy
hidden: true
params:
  - name: environment
    default: production
steps:
  - id: run
    command: echo deploy
`))

	entries := discovery.Enumerate(nil, projectDir, "")

	if got := canonicalNames(entries); fmt.Sprint(got) != fmt.Sprint([]string{"deploy"}) {
		t.Fatalf("canonical names = %v, want one version-free deploy row", got)
	}
	deploy := entryByName(t, entries, "deploy")
	if deploy.Description != "Latest hidden deploy" || !deploy.Hidden {
		t.Fatalf("latest metadata = (%q, hidden=%v), want latest hidden metadata", deploy.Description, deploy.Hidden)
	}
	if len(deploy.Params) != 1 || deploy.Params[0].Name != "environment" || deploy.Params[0].Default != "production" {
		t.Fatalf("latest params = %+v, want environment default production", deploy.Params)
	}
	wantPath := filepath.Join(root, "deploy-v2.0.yaml")
	if deploy.SourcePath != wantPath {
		t.Fatalf("source path = %q, want exact latest path %q", deploy.SourcePath, wantPath)
	}
}

func TestEnumerate_InvalidOlderSiblingMakesLogicalGroupNonLaunchable(t *testing.T) {
	projectDir := t.TempDir()
	root := filepath.Join(projectDir, ".agent-runner", "workflows")
	writeWorkflow(t, filepath.Join(root, "deploy-v1.0.yaml"), malformedYAML())
	writeWorkflow(t, filepath.Join(root, "deploy-v2.0.yaml"), validWorkflowYAML("Latest deploy"))

	entries := discovery.Enumerate(nil, projectDir, "")

	deploy := entryByName(t, entries, "deploy")
	if deploy.ParseError == "" || !strings.Contains(deploy.ParseError, "deploy-v1.0.yaml") {
		t.Fatalf("parse error = %q, want older sibling validation error", deploy.ParseError)
	}
	if deploy.SourcePath != "" {
		t.Fatalf("source path = %q, want invalid group to be non-launchable", deploy.SourcePath)
	}
}

func TestEnumerate_InvalidUnversionedSiblingMakesLogicalGroupNonLaunchable(t *testing.T) {
	projectDir := t.TempDir()
	root := filepath.Join(projectDir, ".agent-runner", "workflows")
	writeWorkflow(t, filepath.Join(root, "deploy.yaml"), validWorkflowYAML("Legacy deploy"))
	writeWorkflow(t, filepath.Join(root, "deploy-v2.0.yaml"), validWorkflowYAML("Latest deploy"))

	entries := discovery.Enumerate(nil, projectDir, "")

	deploy := entryByName(t, entries, "deploy")
	if deploy.ParseError == "" {
		t.Fatal("deploy should contain an actionable filename error")
	}
	for _, want := range []string{"deploy.yaml", "<logical-name>-v<major>.<minor>.yaml", "deploy-v1.0.yaml"} {
		if !strings.Contains(deploy.ParseError, want) {
			t.Fatalf("parse error = %q, want %q", deploy.ParseError, want)
		}
	}
	if deploy.SourcePath != "" {
		t.Fatalf("source path = %q, want invalid group to be non-launchable", deploy.SourcePath)
	}
}

func TestEnumerate_ProjectGroupShadowsNewerUserGroupBeforeVersionSelection(t *testing.T) {
	projectDir := t.TempDir()
	userDir := t.TempDir()
	projectPath := filepath.Join(projectDir, ".agent-runner", "workflows", "deploy-v1.0.yaml")
	writeWorkflow(t, projectPath, validWorkflowYAML("Project deploy"))
	writeWorkflow(t, filepath.Join(userDir, "deploy-v9.0.yaml"), validWorkflowYAML("User deploy"))

	entries := discovery.Enumerate(nil, projectDir, userDir)

	if got := canonicalNames(entries); fmt.Sprint(got) != fmt.Sprint([]string{"deploy"}) {
		t.Fatalf("canonical names = %v, want one shadowed deploy group", got)
	}
	deploy := entryByName(t, entries, "deploy")
	if deploy.Scope != discovery.ScopeProject || deploy.SourcePath != projectPath {
		t.Fatalf("deploy = %+v, want project v1.0 to shadow user v9.0", deploy)
	}
}

func TestEnumerate_BuiltinCatalogSelectsLatestAndKeepsNamespace(t *testing.T) {
	fsys := fstest.MapFS{
		"core/team/deploy-v1.0.yaml": {Data: namedWorkflowYAML("deploy", "Older deploy")},
		"core/team/deploy-v2.0.yml":  {Data: namedWorkflowYAML("deploy", "Latest deploy")},
	}

	entries := discovery.Enumerate(fsys, "", "")

	if got := canonicalNames(entries); fmt.Sprint(got) != fmt.Sprint([]string{"core:team/deploy"}) {
		t.Fatalf("canonical names = %v, want namespaced logical name", got)
	}
	deploy := entryByName(t, entries, "core:team/deploy")
	if deploy.Namespace != "core" || deploy.Description != "Latest deploy" {
		t.Fatalf("builtin deploy = %+v, want latest metadata in core namespace", deploy)
	}
	if deploy.SourcePath != "builtin:core/team/deploy-v2.0.yml" {
		t.Fatalf("source path = %q, want exact latest builtin ref", deploy.SourcePath)
	}
}

func TestEnumerate_UnversionedBuiltinSiblingInvalidatesLogicalGroup(t *testing.T) {
	fsys := fstest.MapFS{
		"core/deploy.yaml":      {Data: namedWorkflowYAML("deploy", "Legacy deploy")},
		"core/deploy-v2.0.yaml": {Data: namedWorkflowYAML("deploy", "Latest deploy")},
	}

	entries := discovery.Enumerate(fsys, "", "")

	if got := canonicalNames(entries); fmt.Sprint(got) != fmt.Sprint([]string{"core:deploy"}) {
		t.Fatalf("canonical names = %v, want one version-free builtin group", got)
	}
	deploy := entryByName(t, entries, "core:deploy")
	if deploy.ParseError == "" || !strings.Contains(deploy.ParseError, "deploy.yaml") {
		t.Fatalf("parse error = %q, want unversioned builtin diagnostic", deploy.ParseError)
	}
	if deploy.SourcePath != "" {
		t.Fatalf("source path = %q, want invalid builtin group to be non-launchable", deploy.SourcePath)
	}
}

func fakeBuiltinFS() fstest.MapFS {
	return fstest.MapFS{
		"core/finalize-pr-v1.0.yaml":    {Data: namedWorkflowYAML("finalize-pr", "Finalize a pull request")},
		"core/implement-task-v1.0.yaml": {Data: namedHiddenWorkflowYAML("implement-task", "Implement a task")},
		"core/_group.yaml":              {Data: []byte("display_name: Core Tools\ndescription: Shared implementation workflows.\n")},
		"spec-driven/change-v1.0.yaml":  {Data: namedWorkflowYAML("change", "Spec-driven change")},
	}
}

func namedWorkflowYAML(name, description string) []byte {
	return []byte(fmt.Sprintf(`name: %s
description: %s
steps:
  - id: step1
    command: echo hello
`, name, description))
}

func namedHiddenWorkflowYAML(name, description string) []byte {
	return []byte(fmt.Sprintf(`name: %s
description: %s
hidden: true
steps:
  - id: step1
    command: echo hello
`, name, description))
}

func validWorkflowYAML(description string) []byte {
	return []byte(fmt.Sprintf(`name: test-workflow
description: %s
steps:
  - id: step1
    command: echo hello
`, description))
}

func hiddenWorkflowYAML(description string) []byte {
	return []byte(fmt.Sprintf(`name: test-workflow
description: %s
hidden: true
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
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if version := strings.LastIndex(stem, "-v"); version >= 0 {
		stem = stem[:version]
	}
	if newline := strings.IndexByte(string(data), '\n'); newline >= 0 && strings.HasPrefix(string(data), "name: ") {
		data = []byte("name: " + stem + string(data[newline:]))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func canonicalNames(entries []discovery.WorkflowEntry) []string {
	names := make([]string, 0, len(entries))
	for i := range entries {
		names = append(names, entries[i].CanonicalName)
	}
	return names
}

func countByName(entries []discovery.WorkflowEntry, name string) int {
	count := 0
	for i := range entries {
		if entries[i].CanonicalName == name {
			count++
		}
	}
	return count
}

func entryByName(t *testing.T, entries []discovery.WorkflowEntry, name string) discovery.WorkflowEntry {
	t.Helper()
	for i := range entries {
		if entries[i].CanonicalName == name {
			return entries[i]
		}
	}
	t.Fatalf("entry %q not found in %v", name, canonicalNames(entries))
	return discovery.WorkflowEntry{}
}

func groupByScopeNamespace(t *testing.T, groups []discovery.GroupMetadata, scope discovery.Scope, namespace string) discovery.GroupMetadata {
	t.Helper()
	for _, group := range groups {
		if group.Scope == scope && group.Namespace == namespace {
			return group
		}
	}
	t.Fatalf("group (%v, %q) not found in %+v", scope, namespace, groups)
	return discovery.GroupMetadata{}
}

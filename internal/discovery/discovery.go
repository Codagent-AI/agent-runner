// Package discovery enumerates workflow definitions from project, user, and builtin sources.
package discovery

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/workflowcatalog"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
	"gopkg.in/yaml.v3"
)

// Scope identifies where a workflow was found.
type Scope int

const (
	ScopeProject Scope = iota
	ScopeUser
	ScopeBuiltin
)

// WorkflowEntry describes a discovered workflow definition.
type WorkflowEntry struct {
	CanonicalName string        // e.g. "core:finalize-pr" or "deploy"
	Description   string        // from the workflow YAML description field
	Hidden        bool          // from the workflow YAML hidden field
	Params        []model.Param // declared parameters, in order
	SourcePath    string        // exact selected workflow path
	Namespace     string        // builtin namespace (e.g. "core"), empty for project/user
	Scope         Scope
	ParseError    string // non-empty if the file could not be loaded or parsed
}

// GroupMetadata describes the display metadata for a workflow group.
type GroupMetadata struct {
	Namespace   string
	Scope       Scope
	DisplayName string
	Description string
}

// StartRunMsg is a bubbletea message emitted when the user requests to start
// a run for a workflow (e.g. pressing r on the new tab or in the definition view).
// The handler that launches the actual run is wired separately.
type StartRunMsg struct {
	Entry  WorkflowEntry
	Params map[string]string
}

// ViewDefinitionMsg is a bubbletea message emitted when the user opens a
// workflow's definition view (e.g. pressing Enter on the new tab).
type ViewDefinitionMsg struct {
	Entry WorkflowEntry
}

// StartIntakeMsg asks the top-level workflow browser to start the built-in
// intake workflow directly.
type StartIntakeMsg struct{}

// UserWorkflowsDir returns the user-scope workflow directory. An unresolvable
// home directory yields an empty path, which Enumerate treats as "no user
// scope" rather than an error.
func UserWorkflowsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agent-runner", "workflows")
}

// EnumerateForProject discovers workflows across every scope the user can
// launch from. Callers that must agree on what is launchable — the workflow
// browser and intake route resolution — share this rather than assembling the
// scope list themselves, so a scope added here cannot reach one and not the
// other.
func EnumerateForProject(projectDir string) []WorkflowEntry {
	return Enumerate(builtinworkflows.FS, projectDir, UserWorkflowsDir())
}

// Enumerate discovers workflows from three sources in order:
//  1. Project-local: <projectDir>/.agent-runner/workflows/
//  2. User-home: userWorkflowsDir (e.g. ~/.agent-runner/workflows/)
//  3. Builtins: builtinFS (an embed.FS whose root contains namespace subdirectories)
//
// projectDir and userWorkflowsDir may be empty to skip that source.
// Results are ordered: project, user, builtin (builtins sorted by namespace then name).
func Enumerate(builtinFS fs.FS, projectDir, userWorkflowsDir string) []WorkflowEntry {
	var projectEntries []WorkflowEntry
	if projectDir != "" {
		projectEntries = enumerateLocalDir(filepath.Join(projectDir, ".agent-runner", "workflows"), ScopeProject)
	}

	var userEntries []WorkflowEntry
	if userWorkflowsDir != "" {
		userEntries = enumerateLocalDir(userWorkflowsDir, ScopeUser)
	}

	builtinEntries := enumerateBuiltinFS(builtinFS)

	if len(projectEntries) != 0 && len(userEntries) != 0 {
		shadowed := make(map[string]struct{}, len(projectEntries))
		for _, entry := range projectEntries {
			shadowed[entry.CanonicalName] = struct{}{}
		}

		filtered := userEntries[:0]
		for _, entry := range userEntries {
			if _, ok := shadowed[entry.CanonicalName]; ok {
				continue
			}
			filtered = append(filtered, entry)
		}
		userEntries = filtered
	}

	entries := make([]WorkflowEntry, 0, len(projectEntries)+len(userEntries)+len(builtinEntries))
	entries = append(entries, projectEntries...)
	entries = append(entries, userEntries...)
	entries = append(entries, builtinEntries...)
	return entries
}

// enumerateLocalDir walks dir and returns one WorkflowEntry per logical
// workflow group.
func enumerateLocalDir(dir string, scope Scope) []WorkflowEntry {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}

	var candidatePaths []string
	_ = filepath.WalkDir(dir, func(filePath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), "_") {
			return nil
		}
		if !workflowcatalog.HasYAMLExtension(d.Name()) {
			return nil
		}

		rel, err := filepath.Rel(dir, filePath)
		if err != nil {
			return nil
		}
		candidatePaths = append(candidatePaths, filepath.ToSlash(rel))
		return nil
	})

	return loadLocalEntries(dir, scope, workflowcatalog.Build(candidatePaths))
}

// enumerateBuiltinFS walks the embedded FS and returns one entry per logical
// builtin workflow group.
func enumerateBuiltinFS(fsys fs.FS) []WorkflowEntry {
	if fsys == nil {
		return nil
	}

	var candidatePaths []string
	_ = fs.WalkDir(fsys, ".", func(relPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(path.Base(relPath), "_") || !strings.Contains(relPath, "/") {
			return nil
		}
		if !workflowcatalog.HasYAMLExtension(relPath) {
			return nil
		}

		candidatePaths = append(candidatePaths, relPath)
		return nil
	})

	return loadBuiltinEntries(fsys, workflowcatalog.Build(candidatePaths))
}

func loadLocalEntries(dir string, scope Scope, catalog workflowcatalog.Catalog) []WorkflowEntry {
	entries := make([]WorkflowEntry, 0, len(catalog.Groups))
	for _, group := range catalog.Groups {
		entry := WorkflowEntry{
			CanonicalName: group.CanonicalName,
			Scope:         scope,
		}

		var loadProblems []string
		var selectedWorkflow *model.Workflow
		for _, definition := range group.Definitions {
			sourcePath := filepath.Join(dir, filepath.FromSlash(definition.Path))
			workflow, err := loader.LoadWorkflow(sourcePath, loader.Options{})
			if err != nil {
				loadProblems = append(loadProblems, fmt.Sprintf("%s: %v", sourcePath, err))
				continue
			}
			if group.Selected != nil && definition.Path == group.Selected.Path {
				entry.SourcePath = sourcePath
				selectedWorkflow = &workflow
			}
		}

		completeWorkflowEntry(&entry, group, selectedWorkflow, loadProblems)
		entries = append(entries, entry)
	}

	return entries
}

func loadBuiltinEntries(fsys fs.FS, catalog workflowcatalog.Catalog) []WorkflowEntry {
	entries := make([]WorkflowEntry, 0, len(catalog.Groups))
	for _, group := range catalog.Groups {
		namespace, logicalName, ok := strings.Cut(group.CanonicalName, "/")
		if !ok {
			continue
		}
		entry := WorkflowEntry{
			CanonicalName: namespace + ":" + logicalName,
			Namespace:     namespace,
			Scope:         ScopeBuiltin,
		}

		var loadProblems []string
		var selectedWorkflow *model.Workflow
		for _, definition := range group.Definitions {
			sourcePath := builtinworkflows.Ref(definition.Path)
			data, err := fs.ReadFile(fsys, definition.Path)
			if err != nil {
				loadProblems = append(loadProblems, fmt.Sprintf("%s: %v", sourcePath, err))
				continue
			}
			workflow, err := loader.ParseWorkflowSource(data, sourcePath, loader.Options{})
			if err != nil {
				loadProblems = append(loadProblems, fmt.Sprintf("%s: %v", sourcePath, err))
				continue
			}
			if group.Selected != nil && definition.Path == group.Selected.Path {
				entry.SourcePath = sourcePath
				selectedWorkflow = &workflow
			}
		}

		completeWorkflowEntry(&entry, group, selectedWorkflow, loadProblems)
		entries = append(entries, entry)
	}

	return entries
}

func completeWorkflowEntry(
	entry *WorkflowEntry,
	group workflowcatalog.Group,
	selectedWorkflow *model.Workflow,
	loadProblems []string,
) {
	problems := make([]string, 0, len(loadProblems)+1)
	if group.Err != nil {
		problems = append(problems, group.Err.Error())
	}
	problems = append(problems, loadProblems...)

	switch {
	case len(problems) > 0:
		entry.SourcePath = ""
		entry.ParseError = strings.Join(problems, "; ")
	case group.Selected == nil || selectedWorkflow == nil || entry.SourcePath == "":
		entry.ParseError = fmt.Sprintf("logical workflow %q has no selectable definition", entry.CanonicalName)
	default:
		entry.Description = selectedWorkflow.Description
		entry.Hidden = selectedWorkflow.Hidden
		entry.Params = selectedWorkflow.Params
	}
}

// EnumerateGroups returns display metadata for every scope/namespace group
// represented in entries. Builtin metadata comes from workflows/<ns>/_group.yaml.
func EnumerateGroups(builtinFS fs.FS, entries []WorkflowEntry) []GroupMetadata {
	type groupKey struct {
		scope Scope
		ns    string
	}

	seen := make(map[groupKey]bool)
	var keys []groupKey
	for _, entry := range entries {
		key := groupKey{scope: entry.Scope, ns: entry.Namespace}
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}

	groups := make([]GroupMetadata, 0, len(keys))
	for _, key := range keys {
		switch key.scope {
		case ScopeProject:
			groups = append(groups, GroupMetadata{
				Scope:       ScopeProject,
				DisplayName: "Project workflows",
				Description: "Workflows defined in this project's .agent-runner directory.",
			})
		case ScopeUser:
			groups = append(groups, GroupMetadata{
				Scope:       ScopeUser,
				DisplayName: "User workflows",
				Description: "Workflows from your home .agent-runner directory.",
			})
		case ScopeBuiltin:
			groups = append(groups, loadBuiltinGroupMetadata(builtinFS, key.ns))
		}
	}
	return groups
}

func loadBuiltinGroupMetadata(builtinFS fs.FS, namespace string) GroupMetadata {
	group := GroupMetadata{
		Scope:       ScopeBuiltin,
		Namespace:   namespace,
		DisplayName: namespace,
	}
	if builtinFS == nil || namespace == "" {
		return group
	}

	data, err := fs.ReadFile(builtinFS, path.Join(namespace, "_group.yaml"))
	if err != nil {
		return group
	}

	var metadata struct {
		DisplayName string `yaml:"display_name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		log.Printf("discovery: malformed _group.yaml in builtin namespace %q: %v", namespace, err)
		return group
	}
	if metadata.DisplayName != "" {
		group.DisplayName = metadata.DisplayName
	}
	group.Description = metadata.Description
	return group
}

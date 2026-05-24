// Package discovery enumerates workflow definitions from project, user, and builtin sources.
package discovery

import (
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codagent/agent-runner/internal/loader"
	"github.com/codagent/agent-runner/internal/model"
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
	SourcePath    string        // file path for search matching
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

type workflowCandidate struct {
	canonicalName string
	namespace     string
	sourcePath    string
	extPriority   int
}

// StartRunMsg is a bubbletea message emitted when the user requests to start
// a run for a workflow (e.g. pressing r on the new tab or in the definition view).
// The handler that launches the actual run is wired separately.
type StartRunMsg struct {
	Entry WorkflowEntry
}

// ViewDefinitionMsg is a bubbletea message emitted when the user opens a
// workflow's definition view (e.g. pressing Enter on the new tab).
type ViewDefinitionMsg struct {
	Entry WorkflowEntry
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

// enumerateLocalDir walks dir and returns a WorkflowEntry for each canonical
// workflow name, preferring .yaml over .yml when both exist.
func enumerateLocalDir(dir string, scope Scope) []WorkflowEntry {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}

	candidates := make(map[string]workflowCandidate)
	_ = filepath.WalkDir(dir, func(filePath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), "_") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		rel, err := filepath.Rel(dir, filePath)
		if err != nil {
			return nil
		}
		canonicalName := stripExt(filepath.ToSlash(rel))
		candidate := workflowCandidate{
			canonicalName: canonicalName,
			sourcePath:    filePath,
			extPriority:   extensionPriority(ext),
		}
		recordCandidate(candidates, candidate)
		return nil
	})

	return loadLocalEntries(scope, candidates)
}

// enumerateBuiltinFS walks the embedded FS and returns entries for all canonical
// builtin workflow names, preferring .yaml over .yml when both exist.
func enumerateBuiltinFS(fsys fs.FS) []WorkflowEntry {
	if fsys == nil {
		return nil
	}

	candidates := make(map[string]workflowCandidate)
	_ = fs.WalkDir(fsys, ".", func(relPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if strings.HasPrefix(path.Base(relPath), "_") || !strings.Contains(relPath, "/") {
			return nil
		}
		ext := strings.ToLower(path.Ext(relPath))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		canonicalName, namespace := builtinCanonical(relPath)
		candidate := workflowCandidate{
			canonicalName: canonicalName,
			namespace:     namespace,
			sourcePath:    builtinworkflows.Ref(relPath),
			extPriority:   extensionPriority(ext),
		}
		recordCandidate(candidates, candidate)
		return nil
	})

	return loadBuiltinEntries(fsys, candidates)
}

func loadLocalEntries(scope Scope, candidates map[string]workflowCandidate) []WorkflowEntry {
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]WorkflowEntry, 0, len(names))
	for _, name := range names {
		candidate := candidates[name]
		entry := WorkflowEntry{
			CanonicalName: candidate.canonicalName,
			Scope:         scope,
			SourcePath:    candidate.sourcePath,
		}

		workflow, err := loader.LoadWorkflow(candidate.sourcePath, loader.Options{})
		if err != nil {
			entry.ParseError = err.Error()
		} else {
			entry.Description = workflow.Description
			entry.Hidden = workflow.Hidden
			entry.Params = workflow.Params
		}

		entries = append(entries, entry)
	}

	return entries
}

func loadBuiltinEntries(fsys fs.FS, candidates map[string]workflowCandidate) []WorkflowEntry {
	ordered := make([]workflowCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].namespace != ordered[j].namespace {
			return ordered[i].namespace < ordered[j].namespace
		}
		return ordered[i].canonicalName < ordered[j].canonicalName
	})

	entries := make([]WorkflowEntry, 0, len(ordered))
	for _, candidate := range ordered {
		entry := WorkflowEntry{
			CanonicalName: candidate.canonicalName,
			Namespace:     candidate.namespace,
			Scope:         ScopeBuiltin,
			SourcePath:    candidate.sourcePath,
		}

		relPath, err := builtinworkflows.RefPath(candidate.sourcePath)
		if err != nil {
			entry.ParseError = err.Error()
			entries = append(entries, entry)
			continue
		}

		data, err := fs.ReadFile(fsys, relPath)
		if err != nil {
			entry.ParseError = err.Error()
			entries = append(entries, entry)
			continue
		}

		workflow, err := loader.ParseWorkflow(data, loader.Options{})
		if err != nil {
			entry.ParseError = err.Error()
		} else {
			entry.Description = workflow.Description
			entry.Hidden = workflow.Hidden
			entry.Params = workflow.Params
		}

		entries = append(entries, entry)
	}

	return entries
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

func recordCandidate(candidates map[string]workflowCandidate, candidate workflowCandidate) {
	existing, ok := candidates[candidate.canonicalName]
	if !ok || candidate.extPriority < existing.extPriority || (candidate.extPriority == existing.extPriority && candidate.sourcePath < existing.sourcePath) {
		candidates[candidate.canonicalName] = candidate
	}
}

func extensionPriority(ext string) int {
	if strings.EqualFold(ext, ".yaml") {
		return 0
	}
	return 1
}

// builtinCanonical converts a path like "core/finalize-pr.yaml" to
// canonical name "core:finalize-pr" and namespace "core".
func builtinCanonical(relPath string) (name, namespace string) {
	relPath = path.Clean(filepath.ToSlash(relPath))
	parts := strings.SplitN(relPath, "/", 2)
	if len(parts) == 1 {
		return stripExt(parts[0]), ""
	}
	namespace = parts[0]
	base := stripExt(parts[1])
	return namespace + ":" + base, namespace
}

func stripExt(name string) string {
	ext := filepath.Ext(name)
	if strings.EqualFold(ext, ".yaml") || strings.EqualFold(ext, ".yml") {
		return name[:len(name)-len(ext)]
	}
	return name
}

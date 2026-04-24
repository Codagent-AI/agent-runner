// Package discovery enumerates workflow definitions from project, user, and builtin sources.
package discovery

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	CanonicalName string // e.g. "core:finalize-pr" or "deploy"
	Description   string // from the workflow YAML description field
	SourcePath    string // file path for search matching
	Namespace     string // builtin namespace (e.g. "core"), empty for project/user
	Scope         Scope
	ParseError    string // non-empty if the file could not be loaded or parsed
}

type workflowHeader struct {
	Description string `yaml:"description"`
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
	var entries []WorkflowEntry

	if projectDir != "" {
		projWFDir := filepath.Join(projectDir, ".agent-runner", "workflows")
		entries = append(entries, enumerateLocalDir(projWFDir, ScopeProject)...)
	}

	if userWorkflowsDir != "" {
		entries = append(entries, enumerateLocalDir(userWorkflowsDir, ScopeUser)...)
	}

	if builtinFS != nil {
		entries = append(entries, enumerateBuiltinFS(builtinFS)...)
	}

	return entries
}

// enumerateLocalDir walks dir and returns a WorkflowEntry for each .yaml/.yml file.
// The canonical name is the relative path from dir with the extension stripped,
// using "/" as separator (e.g. "deploy" or "team/deploy").
func enumerateLocalDir(dir string, scope Scope) []WorkflowEntry {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}

	var entries []WorkflowEntry
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		name := stripExt(rel)

		entry := WorkflowEntry{
			CanonicalName: name,
			SourcePath:    path,
			Scope:         scope,
		}

		data, readErr := os.ReadFile(path) // #nosec G304 G122 -- path is from a controlled workflow directory walk
		if readErr != nil {
			entry.ParseError = readErr.Error()
		} else {
			var h workflowHeader
			if yamlErr := yaml.Unmarshal(data, &h); yamlErr != nil {
				entry.ParseError = yamlErr.Error()
			} else {
				entry.Description = h.Description
			}
		}

		entries = append(entries, entry)
		return nil
	})

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CanonicalName < entries[j].CanonicalName
	})
	return entries
}

// enumerateBuiltinFS walks the embedded FS and returns entries for all .yaml/.yml files.
// The canonical name follows namespace:name format (one level of subdirectory).
// Files at the root (no subdirectory) use just the name.
// Entries are sorted by namespace then name.
func enumerateBuiltinFS(fsys fs.FS) []WorkflowEntry {
	var entries []WorkflowEntry

	_ = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		name, ns := builtinCanonical(path)
		entry := WorkflowEntry{
			CanonicalName: name,
			SourcePath:    "builtin:" + path,
			Namespace:     ns,
			Scope:         ScopeBuiltin,
		}

		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			entry.ParseError = readErr.Error()
		} else {
			var h workflowHeader
			if yamlErr := yaml.Unmarshal(data, &h); yamlErr != nil {
				entry.ParseError = yamlErr.Error()
			} else {
				entry.Description = h.Description
			}
		}

		entries = append(entries, entry)
		return nil
	})

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Namespace != entries[j].Namespace {
			return entries[i].Namespace < entries[j].Namespace
		}
		return entries[i].CanonicalName < entries[j].CanonicalName
	})
	return entries
}

// builtinCanonical converts a path like "core/finalize-pr.yaml" to
// canonical name "core:finalize-pr" and namespace "core".
func builtinCanonical(path string) (name, ns string) {
	path = filepath.ToSlash(path)
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 1 {
		return stripExt(parts[0]), ""
	}
	ns = parts[0]
	base := stripExt(parts[1])
	return ns + ":" + base, ns
}

func stripExt(name string) string {
	ext := filepath.Ext(name)
	if strings.EqualFold(ext, ".yaml") || strings.EqualFold(ext, ".yml") {
		return name[:len(name)-len(ext)]
	}
	return name
}

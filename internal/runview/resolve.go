package runview

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/workflowcatalog"
	builtinworkflows "github.com/codagent/agent-runner/workflows"
)

// ResolvedWorkflow is the output of ResolveWorkflow: the absolute path to
// the run's top-level workflow YAML plus the roots needed to render canonical
// names and to bound sub-workflow lookups.
type ResolvedWorkflow struct {
	// AbsPath is the absolute, validated path to the workflow YAML.
	AbsPath string
	// WorkflowsRoot is the absolute path to the project's workflows/ dir.
	WorkflowsRoot string
	// RepoRoot is the absolute path to the project's repo root (parent of
	// WorkflowsRoot when that is under a repo).
	RepoRoot string
	// OriginCwd is the cwd recorded in meta.json, when available.
	OriginCwd string
}

// ResolveWorkflow locates the workflow YAML for a run. It tries, in order:
//
//  1. state.WorkflowFile as an absolute path;
//  2. state.WorkflowFile relative to the run's recorded cwd from meta.json;
//  3. state.WorkflowFile relative to the current process cwd;
//  4. discovery by state.WorkflowName (or the name parsed from the session
//     ID) under a discovered workflows/ root.
//
// Roots (WorkflowsRoot, RepoRoot) are filled whenever they can be derived
// from the resolved workflow path or from a discoverable workflows/ dir in
// one of the candidate base directories. Returns ok=false when no workflow
// file could be located; the caller is expected to surface a load error.
func ResolveWorkflow(sessionDir, projectDir string, state *model.RunState) (ResolvedWorkflow, bool) {
	origin := readMetaCwd(projectDir)
	processCwd, _ := os.Getwd()

	var bases []string
	for _, b := range []string{origin, processCwd} {
		if b == "" {
			continue
		}
		clean := filepath.Clean(b)
		if !sliceContains(bases, clean) {
			bases = append(bases, clean)
		}
	}

	out := ResolvedWorkflow{OriginCwd: origin}

	// (0) — builtin workflow embedded in the binary; no filesystem lookup needed.
	if builtinworkflows.IsRef(state.WorkflowFile) {
		if _, err := builtinworkflows.ReadFile(state.WorkflowFile); err == nil {
			out.AbsPath = state.WorkflowFile
			out.WorkflowsRoot, out.RepoRoot = rootsFromBases(bases)
			return out, true
		}
	}

	// (1)/(2)/(3) — direct path resolution from state.WorkflowFile.
	if p, ok := tryDirectFile(state.WorkflowFile, bases); ok {
		out.AbsPath = p
		out.WorkflowsRoot, out.RepoRoot = rootsFor(p, bases)
		return out, true
	}

	// (4) — discovery by name.
	name := state.WorkflowName
	if name == "" {
		name = parseWorkflowNameFromID(filepath.Base(sessionDir))
	}
	if name != "" {
		if p, ok := findWorkflowByName(name, bases); ok {
			out.AbsPath = p
			out.WorkflowsRoot, out.RepoRoot = rootsFor(p, bases)
			return out, true
		}
	}

	// No file found, but still populate roots so the caller can at least
	// render canonical names for audit events that carry absolute workflow
	// paths.
	out.WorkflowsRoot, out.RepoRoot = rootsFromBases(bases)
	return out, false
}

// readMetaCwd returns the original cwd recorded in projectDir/meta.json, or
// "" when the file is missing or malformed. This is intentionally stricter
// than runs.ReadProjectPath (which synthesizes a "?-prefixed" placeholder
// when meta.json is absent); resolution needs a true absolute path or
// nothing.
func readMetaCwd(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(projectDir, "meta.json")) // #nosec G304 -- project dir is from internal state tracking
	if err != nil {
		return ""
	}
	var meta struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	if meta.Path == "" {
		return ""
	}
	cleaned := filepath.Clean(meta.Path)
	if !filepath.IsAbs(cleaned) {
		return ""
	}
	return cleaned
}

// tryDirectFile attempts to resolve workflowFile as an absolute path or as a
// path relative to each base in order. Returns the absolute path and true on
// the first match, or ("", false) when nothing stats.
func tryDirectFile(workflowFile string, bases []string) (string, bool) {
	if workflowFile == "" {
		return "", false
	}
	if filepath.IsAbs(workflowFile) {
		if fileExists(workflowFile) {
			return filepath.Clean(workflowFile), true
		}
		return "", false
	}
	for _, b := range bases {
		candidate := filepath.Clean(filepath.Join(b, workflowFile))
		if fileExists(candidate) {
			return candidate, true
		}
	}
	return "", false
}

// findWorkflowByName searches for a workflow matching name under the
// workflows/ subdir of each base directory. name may be bare ("plan-change")
// or namespaced ("openspec:plan-change"). Each workflow root is classified by
// the shared catalog so invalid groups cannot be bypassed by filename probing.
func findWorkflowByName(name string, bases []string) (string, bool) {
	if name == "" {
		return "", false
	}

	for _, b := range bases {
		for _, rel := range []string{"workflows", filepath.Join(".agent-runner", "workflows")} {
			wfRoot := filepath.Join(b, rel)
			info, err := os.Stat(wfRoot)
			if err != nil || !info.IsDir() {
				continue
			}

			if p, matched := searchTree(wfRoot, name); matched {
				return p, p != ""
			}
		}
	}
	return "", false
}

// searchTree selects the latest valid versioned definition for name. matched
// distinguishes an invalid matching group from an unrelated catalog so callers
// do not fall through to a lower-priority workflow root.
func searchTree(root, name string) (workflowPath string, matched bool) {
	catalog := workflowcatalog.Build(workflowCandidatePaths(root))
	catalogName := strings.ReplaceAll(name, ":", "/")
	if group, found := catalog.Lookup(catalogName); found {
		workflowPath, ok := workflowPathForGroup(root, group)
		if !ok {
			return "", true
		}
		return workflowPath, true
	}
	if strings.Contains(name, ":") {
		return "", false
	}
	return selectBareCatalogGroup(root, name, catalog.Groups)
}

func workflowCandidatePaths(root string) []string {
	var candidatePaths []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr == nil {
			candidatePaths = append(candidatePaths, filepath.ToSlash(rel))
		}
		return nil
	})
	return candidatePaths
}

func selectBareCatalogGroup(root, name string, groups []workflowcatalog.Group) (workflowPath string, matched bool) {
	for _, group := range groups {
		if filepath.Base(group.CanonicalName) != name {
			continue
		}
		matched = true
		candidatePath, ok := workflowPathForGroup(root, group)
		if !ok {
			return "", true
		}
		if workflowPath == "" {
			workflowPath = candidatePath
		}
	}
	return workflowPath, matched
}

func workflowPathForGroup(root string, group workflowcatalog.Group) (string, bool) {
	if group.Err != nil {
		legacyPath, ok := uniqueLegacyUnversionedPath(group)
		if !ok {
			return "", false
		}
		return filepath.Join(root, filepath.FromSlash(legacyPath)), true
	}
	if group.Selected == nil {
		return "", false
	}
	return filepath.Join(root, filepath.FromSlash(group.Selected.Path)), true
}

func uniqueLegacyUnversionedPath(group workflowcatalog.Group) (string, bool) {
	if group.Err == nil ||
		len(group.Definitions) != 0 ||
		len(group.Err.InvalidFilenames) != 1 ||
		len(group.Err.DuplicateVersions) != 0 {
		return "", false
	}

	candidatePath := filepath.ToSlash(group.Err.InvalidFilenames[0].Path)
	ext := filepath.Ext(candidatePath)
	if ext != ".yaml" && ext != ".yml" {
		return "", false
	}
	if strings.TrimSuffix(candidatePath, ext) != group.CanonicalName {
		return "", false
	}
	return candidatePath, true
}

// rootsFor returns the (workflowsRoot, repoRoot) pair appropriate for an
// already-resolved workflow path. It walks up from the workflow file to the
// nearest "workflows" ancestor, falling back to the per-base discovery when
// the workflow path is not under any such dir.
func rootsFor(workflowPath string, bases []string) (workflowsRoot, repoRoot string) {
	if workflowPath != "" {
		if wfRoot, ok := DiscoverWorkflowsRoot(workflowPath); ok {
			return wfRoot, filepath.Dir(wfRoot)
		}
	}
	return rootsFromBases(bases)
}

// rootsFromBases tries DiscoverWorkflowsRoot on each base and returns the
// first hit; used when we have no resolved workflow path.
func rootsFromBases(bases []string) (workflowsRoot, repoRoot string) {
	for _, b := range bases {
		if wfRoot, ok := DiscoverWorkflowsRoot(b); ok {
			return wfRoot, filepath.Dir(wfRoot)
		}
	}
	return "", ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false
		}
		return false
	}
	return !info.IsDir()
}

func sliceContains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

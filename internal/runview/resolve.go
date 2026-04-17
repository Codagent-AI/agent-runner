package runview

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
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
// or namespaced ("openspec:plan-change"). The search prefers an exact layout
// match first, then falls back to a recursive walk matching the trailing
// segment. The first hit wins.
func findWorkflowByName(name string, bases []string) (string, bool) {
	if name == "" {
		return "", false
	}
	// bare is the rightmost segment; used for recursive filename matching.
	bare := name
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		bare = name[i+1:]
	}

	for _, b := range bases {
		wfRoot := filepath.Join(b, "workflows")
		info, err := os.Stat(wfRoot)
		if err != nil || !info.IsDir() {
			continue
		}

		// Prefer an exact layout match:
		//   bare  → workflows/<name>.yaml
		//   ns:n  → workflows/<ns>/<n>.yaml
		layout := strings.ReplaceAll(name, ":", string(os.PathSeparator))
		for _, ext := range []string{".yaml", ".yml"} {
			direct := filepath.Join(wfRoot, layout+ext)
			if fileExists(direct) {
				return direct, true
			}
		}

		// Fall back to a recursive search for the bare filename.
		if p := searchTree(wfRoot, bare); p != "" {
			return p, true
		}
	}
	return "", false
}

// searchTree walks root looking for "name.yaml" or "name.yml" and returns
// the first match's absolute path.
func searchTree(root, name string) string {
	var found string
	want := map[string]bool{name + ".yaml": true, name + ".yml": true}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if want[d.Name()] {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found
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

package runview

import (
	"os"
	"path/filepath"
	"strings"
)

// ResolverConfig configures how workflow paths are converted to human-friendly
// canonical names. Both fields are optional; when they are absent, the
// resolver falls back to the cleaned absolute path.
type ResolverConfig struct {
	// WorkflowsRoot is the absolute path to the project's workflows directory
	// (typically <repo-root>/workflows). Paths under this root produce a
	// canonical runnable name.
	WorkflowsRoot string
	// RepoRoot is the absolute path to the project's repo root. When a
	// workflow file is outside WorkflowsRoot, the resolver returns a path
	// relative to RepoRoot.
	RepoRoot string
}

// CanonicalName converts a resolved absolute workflow YAML path into the
// string used in breadcrumbs and sub-workflow headers.
//
// Rules (in order):
//  1. Path inside WorkflowsRoot, exactly one subdirectory deep → "<ns>:<name>"
//     where <ns> is the subdir name and <name> is the file's base without .yaml/.yml.
//  2. Path directly under WorkflowsRoot (no subdir) → "<name>".
//  3. Path more than one subdir under WorkflowsRoot OR outside WorkflowsRoot →
//     RepoRoot-relative path (e.g. "../external/other.yaml").
//  4. If no relative path can be computed (no RepoRoot, or unrelated tree) →
//     the cleaned absolute path.
func CanonicalName(resolvedPath string, cfg ResolverConfig) string {
	if resolvedPath == "" {
		return ""
	}
	clean := filepath.Clean(resolvedPath)

	if cfg.WorkflowsRoot != "" {
		if name, ok := canonicalFromWorkflowsRoot(clean, filepath.Clean(cfg.WorkflowsRoot)); ok {
			return name
		}
	}

	if cfg.RepoRoot != "" {
		if rel, err := filepath.Rel(filepath.Clean(cfg.RepoRoot), clean); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return clean
}

func canonicalFromWorkflowsRoot(clean, root string) (string, bool) {
	rel, err := filepath.Rel(root, clean)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "../") || rel == ".." {
		return "", false
	}
	base := rel
	ext := strings.ToLower(filepath.Ext(base))
	if ext == ".yaml" || ext == ".yml" {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	parts := strings.Split(base, "/")
	switch len(parts) {
	case 1:
		return parts[0], true
	case 2:
		return parts[0] + ":" + parts[1], true
	default:
		// Deeper than one subdir: no canonical form; caller will fall back
		// to the repo-relative path.
		return "", false
	}
}

// DiscoverWorkflowsRoot walks up from start looking for a "workflows"
// directory. Returns the absolute path to that directory (including the
// "workflows" segment) and true when found. start may be a file or dir.
func DiscoverWorkflowsRoot(start string) (string, bool) {
	dir := start
	if info, err := os.Stat(start); err == nil && !info.IsDir() {
		dir = filepath.Dir(start)
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, "workflows")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

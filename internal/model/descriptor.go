package model

import (
	"os"
	"path/filepath"
	"strings"
)

// WorkflowDescriptor identifies a workflow and carries its human-friendly display name.
type WorkflowDescriptor struct {
	Path        string // file path for loading and comparison
	DisplayName string // human-friendly name, e.g. "openspec:change"
}

// ResolverConfig configures how workflow paths are converted to canonical display names.
// Both fields are optional; when absent, the resolver falls back to the cleaned absolute path.
type ResolverConfig struct {
	WorkflowsRoot string // absolute path to the project's workflows/ directory
	RepoRoot      string // absolute path to the project's repo root
}

// NewWorkflowDescriptor creates a WorkflowDescriptor with a computed DisplayName.
func NewWorkflowDescriptor(path string, cfg ResolverConfig) WorkflowDescriptor {
	return WorkflowDescriptor{
		Path:        path,
		DisplayName: CanonicalName(path, cfg),
	}
}

// CanonicalName converts a resolved workflow YAML path into a human-friendly name.
//
// Rules (in order):
//  1. Path inside WorkflowsRoot, exactly one subdirectory deep → "<ns>:<name>"
//  2. Path directly under WorkflowsRoot (no subdir) → "<name>"
//  3. Deeper or outside WorkflowsRoot → RepoRoot-relative path
//  4. Fallback → cleaned absolute path
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
		return "", false
	}
}

// DiscoverWorkflowsRoot walks up from start looking for a "workflows" directory.
// Returns the absolute path to that directory (including the "workflows" segment) and true when found.
// start may be a file or directory path.
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

package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codagent/agent-runner/internal/model"
	"github.com/codagent/agent-runner/internal/textfmt"
)

// evaluateWorkspaceDirs runs at spawn/child boundaries, after earlier captures exist.
func evaluateWorkspaceDirs(ctx *model.ExecutionContext, stepID string) error {
	base := ctx.WorkspaceDirs
	if ctx.WorkspaceDirTemplates != nil {
		base = ctx.InheritedWorkspaceDirs
	}
	dirs := append([]string(nil), base...)
	for _, template := range ctx.WorkspaceDirTemplates {
		dir, err := textfmt.InterpolateTyped(template, ctx.Params, ctx.CapturedVariables, ctx.BuiltinVarsForStep(stepID))
		if err != nil {
			return fmt.Errorf("workspace_dirs %q: %w", template, err)
		}
		dirs = append(dirs, dir)
	}
	normalized := make([]string, 0, len(dirs))
	seen := make(map[string]bool)
	project := ctx.ProjectRoot
	if project != "" {
		if root, err := filepath.EvalSymlinks(project); err == nil {
			project = root
		}
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			return fmt.Errorf("workspace directory must be absolute: %s", dir)
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return fmt.Errorf("workspace directory %s: %w", dir, err)
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("workspace directory is not an existing directory: %s", dir)
		}
		if project != "" {
			rel, err := filepath.Rel(project, resolved)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
		}
		if !seen[resolved] {
			seen[resolved] = true
			normalized = append(normalized, resolved)
		}
	}
	ctx.WorkspaceDirs = normalized
	return nil
}

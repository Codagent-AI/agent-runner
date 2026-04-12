package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/codagent/agent-runner/internal/audit"
	"github.com/codagent/agent-runner/internal/runs"
)

// WorktreeEntry represents a git worktree with its associated runs.
type WorktreeEntry struct {
	Name    string
	Path    string
	Encoded string
	Runs    []runs.RunInfo
}

// ListWorktrees discovers git worktrees and loads their runs.
// Returns nil if not in a git repo or git is not available.
func ListWorktrees(projectsRoot string) []WorktreeEntry {
	out, err := exec.Command("git", "worktree", "list", "--porcelain").Output() // #nosec G204 -- fixed git command
	if err != nil {
		return nil
	}

	paths := parseWorktreePaths(string(out))
	if len(paths) <= 1 {
		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}

	var entries []WorktreeEntry
	for _, p := range paths {
		encoded := audit.EncodePath(p)
		projectDir := filepath.Join(projectsRoot, encoded)
		runList, _ := runs.ListForDir(projectDir)
		entries = append(entries, WorktreeEntry{
			Name:    filepath.Base(p),
			Path:    p,
			Encoded: encoded,
			Runs:    runList,
		})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		isCwdI := entries[i].Path == cwd
		isCwdJ := entries[j].Path == cwd
		if isCwdI != isCwdJ {
			return isCwdI
		}
		return entries[i].Name < entries[j].Name
	})

	return entries
}

func parseWorktreePaths(output string) []string {
	var paths []string
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, strings.TrimPrefix(line, "worktree "))
		}
	}
	return paths
}

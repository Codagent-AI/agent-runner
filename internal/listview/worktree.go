package listview

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
// Returns ("", nil) if not in a git repo or git is not available.
// The returned repo name is the basename of the main worktree (git lists it first).
func ListWorktrees(projectsRoot string) (string, []WorktreeEntry) {
	out, err := exec.Command("git", "worktree", "list", "--porcelain").Output() // #nosec G204 -- fixed git command
	if err != nil {
		return "", nil
	}

	paths := parseWorktreePaths(string(out))
	if len(paths) <= 1 {
		return "", nil
	}

	repoName := filepath.Base(paths[0])

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
		ti, tj := mostRecentRun(entries[i].Runs), mostRecentRun(entries[j].Runs)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return entries[i].Name < entries[j].Name
	})

	return repoName, entries
}

// mostRecentRun returns the LastUpdate of the first run (runs are sorted
// most-recent first by runs.ListForDir), or the zero time if there are none.
func mostRecentRun(rs []runs.RunInfo) time.Time {
	if len(rs) == 0 {
		return time.Time{}
	}
	return rs[0].LastUpdate
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

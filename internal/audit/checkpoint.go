package audit

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// GitFileStat describes the observable dirty state for one path at a step
// boundary. It intentionally keeps file names in the local audit artifact.
type GitFileStat struct {
	Path    string `json:"path"`
	Added   int64  `json:"added"`
	Deleted int64  `json:"deleted"`
}

// GitCheckpoint is a conservative local Git observation. An unavailable
// checkpoint is evidence of a limitation, not evidence of zero change.
type GitCheckpoint struct {
	Available bool          `json:"available"`
	Reason    string        `json:"reason,omitempty"`
	HEAD      string        `json:"head,omitempty"`
	Index     []GitFileStat `json:"index,omitempty"`
	Worktree  []GitFileStat `json:"worktree,omitempty"`
	Untracked []GitFileStat `json:"untracked,omitempty"`
}

// GitChangeCounts is the aggregate projection used by metrics consumers.
type GitChangeCounts struct {
	Available    bool   `json:"available"`
	Reason       string `json:"reason,omitempty"`
	FilesChanged int64  `json:"files_changed,omitempty"`
	LinesAdded   int64  `json:"lines_added,omitempty"`
	LinesDeleted int64  `json:"lines_deleted,omitempty"`
}

// CheckpointLogger augments executable leaf step boundary events. It never
// returns errors or changes event outcomes: Git is optional evidence.
type CheckpointLogger struct {
	sink               EventLogger
	projectRoot        string
	executionSessionID string
	mu                 sync.Mutex
	starts             map[string]GitCheckpoint
}

func NewCheckpointLogger(sink EventLogger, projectRoot, executionSessionID string) *CheckpointLogger {
	return &CheckpointLogger{sink: sink, projectRoot: projectRoot, executionSessionID: executionSessionID, starts: make(map[string]GitCheckpoint)}
}

func (l *CheckpointLogger) Emit(event Event) {
	event = l.Decorate(event)
	if l.sink != nil {
		l.sink.Emit(event)
	}
}

// Decorate adds session and Git evidence without emitting. Pipelines use it
// before their metrics projection so both artifacts see identical evidence.
func (l *CheckpointLogger) Decorate(event Event) Event {
	if event.Data == nil {
		event.Data = map[string]any{}
	}
	if l.executionSessionID != "" {
		event.Data["execution_session_id"] = l.executionSessionID
	}
	switch event.Type {
	case EventStepStart:
		if executableStart(event.Data) {
			checkpoint := observeGit(l.projectRoot)
			event.Data["git_checkpoint"] = checkpoint
			// If the process dies before a matching step_end this persisted start
			// event remains explicit that no ending observation was captured.
			event.Data["git_end_checkpoint"] = GitCheckpoint{Reason: "ending checkpoint not yet observed"}
			l.mu.Lock()
			l.starts[event.Prefix] = checkpoint
			l.mu.Unlock()
		}
	case EventStepEnd:
		l.mu.Lock()
		start, found := l.starts[event.Prefix]
		if found {
			delete(l.starts, event.Prefix)
		}
		l.mu.Unlock()
		if found {
			end := observeGit(l.projectRoot)
			event.Data["git_start"] = start
			event.Data["git_checkpoint"] = end
			event.Data["git_start_checkpoint"] = start
			event.Data["git_end_checkpoint"] = end
			event.Data["git_changes"] = deriveGitChanges(&start, &end)
		}
	}
	return event
}

func executableStart(data map[string]any) bool {
	for _, key := range []string{"command", "script", "title", "mode"} {
		if _, ok := data[key]; ok {
			return true
		}
	}
	return false
}

func observeGit(root string) GitCheckpoint {
	if root == "" {
		return GitCheckpoint{Reason: "project root unavailable"}
	}
	inside, err := gitOutput(root, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return GitCheckpoint{Reason: "git worktree unavailable"}
	}
	head, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return GitCheckpoint{Reason: "git revision unavailable"}
	}
	index, err := gitNumstat(root, "diff", "--cached", "--numstat", "-z")
	if err != nil {
		return GitCheckpoint{Reason: "git index state unavailable"}
	}
	worktree, err := gitNumstat(root, "diff", "--numstat", "-z")
	if err != nil {
		return GitCheckpoint{Reason: "git worktree state unavailable"}
	}
	untracked, err := gitUntrackedStats(root)
	if err != nil {
		return GitCheckpoint{Reason: "git untracked state unavailable"}
	}
	return GitCheckpoint{Available: true, HEAD: strings.TrimSpace(head), Index: index, Worktree: worktree, Untracked: untracked}
}

func gitOutput(root string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", root}, args...)...) // #nosec G204 -- root is the runner's resolved project root.
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func gitNumstat(root string, args ...string) ([]GitFileStat, error) {
	output, err := gitOutput(root, args...)
	if err != nil {
		return nil, err
	}
	return parseNumstat([]byte(output))
}

func gitUntrackedStats(root string) ([]GitFileStat, error) {
	output, err := gitOutput(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	paths := bytes.Split(bytes.TrimSuffix([]byte(output), []byte{0}), []byte{0})
	stats := make([]GitFileStat, 0, len(paths))
	for _, path := range paths {
		if len(path) == 0 {
			continue
		}
		// diff --no-index exits one when it finds a difference, so CombinedOutput
		// is required to retain the valid numstat payload.
		command := exec.Command("git", "-C", root, "diff", "--no-index", "--numstat", "--", "/dev/null", filepath.FromSlash(string(path))) // #nosec G204 -- fixed Git args and local path.
		data, commandErr := command.CombinedOutput()
		if commandErr != nil {
			if exitErr, ok := commandErr.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
				return nil, commandErr
			}
		}
		parsed, parseErr := parseNumstat(data)
		if parseErr != nil || len(parsed) != 1 {
			return nil, fmt.Errorf("parse untracked %q: %w", path, parseErr)
		}
		parsed[0].Path = string(path)
		stats = append(stats, parsed[0])
	}
	return stats, nil
}

func parseNumstat(output []byte) ([]GitFileStat, error) {
	if len(output) == 0 {
		return []GitFileStat{}, nil
	}
	parts := bytes.Split(bytes.TrimSuffix(output, []byte{0}), []byte{0})
	stats := make([]GitFileStat, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		fields := strings.SplitN(string(part), "\t", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid numstat %q", part)
		}
		added, addErr := strconv.ParseInt(fields[0], 10, 64)
		deleted, deleteErr := strconv.ParseInt(fields[1], 10, 64)
		if addErr != nil || deleteErr != nil {
			return nil, fmt.Errorf("binary or invalid numstat %q", part)
		}
		stats = append(stats, GitFileStat{Path: fields[2], Added: added, Deleted: deleted})
	}
	return stats, nil
}

func deriveGitChanges(start, end *GitCheckpoint) GitChangeCounts {
	if !start.Available || !end.Available {
		reason := start.Reason
		if reason == "" {
			reason = end.Reason
		}
		return GitChangeCounts{Reason: reason}
	}
	before := flattenCheckpoint(start)
	after := flattenCheckpoint(end)
	paths := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		paths[path] = struct{}{}
	}
	for path := range after {
		paths[path] = struct{}{}
	}
	var result GitChangeCounts
	for path := range paths {
		left, right := before[path], after[path]
		if right.added < left.added || right.deleted < left.deleted {
			return GitChangeCounts{Reason: "repository change cannot be derived conservatively"}
		}
		if right != left {
			result.FilesChanged++
			result.LinesAdded += right.added - left.added
			result.LinesDeleted += right.deleted - left.deleted
		}
	}
	result.Available = true
	return result
}

type gitCounts struct{ added, deleted int64 }

func flattenCheckpoint(checkpoint *GitCheckpoint) map[string]gitCounts {
	result := make(map[string]gitCounts)
	for _, group := range [][]GitFileStat{checkpoint.Index, checkpoint.Worktree, checkpoint.Untracked} {
		for _, stat := range group {
			current := result[stat.Path]
			current.added += stat.Added
			current.deleted += stat.Deleted
			result[stat.Path] = current
		}
	}
	return result
}

// SortGitFileStats is useful to deterministic consumers of persisted evidence.
func SortGitFileStats(stats []GitFileStat) {
	sort.Slice(stats, func(i, j int) bool { return stats[i].Path < stats[j].Path })
}

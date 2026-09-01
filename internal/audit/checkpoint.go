package audit

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
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
	Available         bool          `json:"available"`
	Reason            string        `json:"reason,omitempty"`
	HEAD              string        `json:"head,omitempty"`
	Index             []GitFileStat `json:"index,omitempty"`
	Worktree          []GitFileStat `json:"worktree,omitempty"`
	Untracked         []GitFileStat `json:"untracked,omitempty"`
	Committed         []GitFileStat `json:"committed,omitempty"`
	CommittedObserved bool          `json:"committed_observed,omitempty"`
	Commits           []string      `json:"commits,omitempty"`
}

const (
	maxUntrackedFiles     = 256
	maxUntrackedFileBytes = 4 << 20
	maxUntrackedPathBytes = 32 << 10
)

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
			completeHeadTransition(l.projectRoot, &start, &end)
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
	paths, err := gitUntrackedPaths(root)
	if err != nil {
		return nil, err
	}
	stats := make([]GitFileStat, 0, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(filepath.FromSlash(path))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("unsafe untracked path %q", path)
		}
		filePath := filepath.Join(root, clean)
		info, statErr := os.Lstat(filePath) // #nosec G703 -- path is constrained to a Git-reported relative file.
		if statErr != nil {
			return nil, statErr
		}
		if !info.Mode().IsRegular() || info.Size() > maxUntrackedFileBytes {
			return nil, fmt.Errorf("untracked file %q cannot be measured within limits", path)
		}
		data, readErr := os.ReadFile(filePath) // #nosec G304 -- path was constrained above.
		if readErr != nil {
			return nil, readErr
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil, fmt.Errorf("untracked binary file %q", path)
		}
		stats = append(stats, GitFileStat{Path: path, Added: countLines(data)})
	}
	return stats, nil
}

func gitUntrackedPaths(root string) ([]string, error) {
	command := exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard", "-z") // #nosec G204 -- root is the runner's resolved project root.
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	paths, readErr := readNULPaths(bufio.NewReader(stdout), maxUntrackedFiles, maxUntrackedPathBytes)
	if readErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, readErr
	}
	if err := command.Wait(); err != nil {
		return nil, err
	}
	return paths, nil
}

func readNULPaths(reader *bufio.Reader, maxPaths, maxBytes int) ([]string, error) {
	paths := make([]string, 0, maxPaths)
	current := make([]byte, 0, 256)
	observedBytes := 0
	for {
		fragment, err := reader.ReadSlice(0)
		observedBytes += len(fragment)
		if observedBytes > maxBytes {
			return nil, fmt.Errorf("untracked path output exceeds %d bytes", maxBytes)
		}
		current = append(current, fragment...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			if len(current) == 0 {
				return paths, nil
			}
			return nil, fmt.Errorf("unterminated untracked path output")
		}
		if err != nil {
			return nil, err
		}
		if len(current) == 0 || current[len(current)-1] != 0 {
			return nil, fmt.Errorf("invalid untracked path output")
		}
		if len(paths) == maxPaths {
			return nil, fmt.Errorf("untracked file count exceeds %d", maxPaths)
		}
		paths = append(paths, string(current[:len(current)-1]))
		current = current[:0]
	}
}

func countLines(data []byte) int64 {
	if len(data) == 0 {
		return 0
	}
	lines := int64(bytes.Count(data, []byte{'\n'}))
	if data[len(data)-1] != '\n' {
		lines++
	}
	return lines
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

func completeHeadTransition(root string, start, end *GitCheckpoint) {
	if !end.Available || start.HEAD == end.HEAD {
		return
	}
	committed, err := gitNumstat(root, "diff", "--numstat", "-z", start.HEAD, end.HEAD)
	if err != nil {
		end.Available = false
		end.Reason = "committed Git delta unavailable"
		return
	}
	commits, err := gitOutput(root, "log", "--format=%H", start.HEAD+".."+end.HEAD)
	if err != nil {
		end.Available = false
		end.Reason = "Git commit evidence unavailable"
		return
	}
	end.Committed = committed
	end.CommittedObserved = true
	end.Commits = strings.Fields(commits)
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
	if start.HEAD != end.HEAD {
		if !end.CommittedObserved {
			return GitChangeCounts{Reason: "committed Git delta unavailable"}
		}
		if len(before) != 0 {
			return GitChangeCounts{Reason: "preexisting dirty state prevents conservative commit attribution"}
		}
		return countGitStats(mergeGitStats(statsMap(end.Committed), after))
	}
	return countDirtyDelta(before, after)
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

func statsMap(stats []GitFileStat) map[string]gitCounts {
	result := make(map[string]gitCounts, len(stats))
	for _, stat := range stats {
		result[stat.Path] = gitCounts{added: stat.Added, deleted: stat.Deleted}
	}
	return result
}

func mergeGitStats(left, right map[string]gitCounts) map[string]gitCounts {
	result := make(map[string]gitCounts, len(left)+len(right))
	for path, counts := range left {
		result[path] = counts
	}
	for path, counts := range right {
		current := result[path]
		current.added += counts.added
		current.deleted += counts.deleted
		result[path] = current
	}
	return result
}

func countDirtyDelta(before, after map[string]gitCounts) GitChangeCounts {
	paths := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		paths[path] = struct{}{}
	}
	for path := range after {
		paths[path] = struct{}{}
	}
	result := GitChangeCounts{Available: true}
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
	return result
}

func countGitStats(stats map[string]gitCounts) GitChangeCounts {
	result := GitChangeCounts{Available: true}
	for _, counts := range stats {
		result.FilesChanged++
		result.LinesAdded += counts.added
		result.LinesDeleted += counts.deleted
	}
	return result
}

// SortGitFileStats is useful to deterministic consumers of persisted evidence.
func SortGitFileStats(stats []GitFileStat) {
	sort.Slice(stats, func(i, j int) bool { return stats[i].Path < stats[j].Path })
}

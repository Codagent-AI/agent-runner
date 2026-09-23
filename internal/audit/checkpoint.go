package audit

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"hash"
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
	Available         bool              `json:"available"`
	Reason            string            `json:"reason,omitempty"`
	HEAD              string            `json:"head,omitempty"`
	Index             []GitFileStat     `json:"index,omitempty"`
	Worktree          []GitFileStat     `json:"worktree,omitempty"`
	Untracked         []GitFileStat     `json:"untracked,omitempty"`
	Committed         []GitFileStat     `json:"committed,omitempty"`
	CommittedObserved bool              `json:"committed_observed,omitempty"`
	Commits           []string          `json:"commits,omitempty"`
	DirtySignatures   map[string]string `json:"dirty_signatures,omitempty"`
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
	index, err := gitNumstat(root, "diff", "--cached", "--no-renames", "--numstat", "-z")
	if err != nil {
		return GitCheckpoint{Reason: "git index state unavailable"}
	}
	worktree, err := gitNumstat(root, "diff", "--no-renames", "--numstat", "-z")
	if err != nil {
		return GitCheckpoint{Reason: "git worktree state unavailable"}
	}
	untracked, err := gitUntrackedStats(root)
	if err != nil {
		return GitCheckpoint{Reason: "git untracked state unavailable"}
	}
	checkpoint := GitCheckpoint{Available: true, HEAD: strings.TrimSpace(head), Index: index, Worktree: worktree, Untracked: untracked}
	checkpoint.DirtySignatures, err = gitDirtySignatures(root, &checkpoint)
	if err != nil {
		return GitCheckpoint{Reason: "git dirty state unavailable"}
	}
	return checkpoint
}

func gitDirtySignatures(root string, checkpoint *GitCheckpoint) (map[string]string, error) {
	paths := flattenCheckpoint(checkpoint)
	signatures := make(map[string]string)
	if len(paths) == 0 {
		return signatures, nil
	}
	indexEntries, err := gitIndexEntries(root, paths)
	if err != nil {
		return nil, err
	}
	untracked := make(map[string]struct{}, len(checkpoint.Untracked))
	for _, stat := range checkpoint.Untracked {
		untracked[stat.Path] = struct{}{}
	}
	worktree := make(map[string]struct{}, len(checkpoint.Worktree))
	for _, stat := range checkpoint.Worktree {
		worktree[stat.Path] = struct{}{}
	}
	for path := range paths {
		signature := sha256.New()
		gitlink := false
		for _, entry := range indexEntries[path] {
			_, _ = io.WriteString(signature, entry)
			_, _ = signature.Write([]byte{0})
			gitlink = gitlink || strings.HasPrefix(entry, "160000 ")
		}
		_, isUntracked := untracked[path]
		_, hasWorktreeDelta := worktree[path]
		if isUntracked || hasWorktreeDelta {
			if err := hashWorktreePath(signature, filepath.Join(root, filepath.FromSlash(path)), isUntracked, gitlink); err != nil {
				return nil, err
			}
		}
		signatures[path] = fmt.Sprintf("%x", signature.Sum(nil))
	}
	return signatures, nil
}

func gitIndexEntries(root string, dirtyPaths map[string]gitCounts) (map[string][]string, error) {
	command := exec.Command("git", "-C", root, "ls-files", "--stage", "-z") // #nosec G204 -- root is the runner's resolved project root.
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	entries := make(map[string][]string)
	reader := bufio.NewReader(stdout)
	for {
		record, readErr := reader.ReadBytes(0)
		if readErr == io.EOF && len(record) == 0 {
			break
		}
		if readErr != nil || len(record) == 0 || record[len(record)-1] != 0 {
			_ = command.Process.Kill()
			_ = command.Wait()
			if readErr != nil {
				return nil, readErr
			}
			return nil, fmt.Errorf("unterminated git index entry")
		}
		metadata, path, found := bytes.Cut(record[:len(record)-1], []byte{'\t'})
		if !found {
			_ = command.Process.Kill()
			_ = command.Wait()
			return nil, fmt.Errorf("invalid git index entry")
		}
		if _, dirty := dirtyPaths[string(path)]; dirty {
			entries[string(path)] = append(entries[string(path)], string(metadata))
		}
	}
	if err := command.Wait(); err != nil {
		return nil, err
	}
	return entries, nil
}

func hashWorktreePath(signature hash.Hash, path string, bounded, gitlink bool) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		_, _ = io.WriteString(signature, "missing")
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode().IsRegular() {
		file, err := openUntrackedFileNoFollow(path)
		if err != nil {
			return err
		}
		openedInfo, err := file.Stat()
		if err != nil || !openedInfo.Mode().IsRegular() {
			_ = file.Close()
			return fmt.Errorf("dirty path is not a regular file: %q", path)
		}
		_, _ = fmt.Fprintf(signature, "regular:%d\x00", openedInfo.Mode().Perm()&0o111)
		var reader io.Reader = file
		if bounded {
			reader = io.LimitReader(file, maxUntrackedFileBytes+1)
		}
		copied, copyErr := io.Copy(signature, reader)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if bounded && copied > maxUntrackedFileBytes {
			return fmt.Errorf("untracked file exceeds %d bytes", maxUntrackedFileBytes)
		}
		return closeErr
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(signature, "symlink:\x00"+target)
		return nil
	}
	if info.IsDir() && gitlink {
		clean, err := submoduleClean(path)
		if err != nil {
			return err
		}
		if !clean {
			return fmt.Errorf("submodule has uncommitted changes: %q", path)
		}
		head, err := gitOutput(path, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		_, _ = io.WriteString(signature, "submodule:\x00"+strings.TrimSpace(head))
		return nil
	}
	return fmt.Errorf("unsupported dirty path type %q", path)
}

func submoduleClean(path string) (bool, error) {
	command := exec.Command("git", "-C", path, "status", "--porcelain", "-z") // #nosec G204 -- path is a Git-reported submodule under the resolved project root.
	stdout, err := command.StdoutPipe()
	if err != nil {
		return false, err
	}
	if err := command.Start(); err != nil {
		return false, err
	}
	var first [1]byte
	count, readErr := io.ReadFull(stdout, first[:])
	if count != 0 {
		_ = command.Process.Kill()
		_ = command.Wait()
		return false, nil
	}
	if readErr != io.EOF {
		_ = command.Process.Kill()
		_ = command.Wait()
		return false, readErr
	}
	if err := command.Wait(); err != nil {
		return false, err
	}
	return true, nil
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
		data, readErr := readBoundedUntrackedFile(filePath)
		if readErr != nil {
			return nil, fmt.Errorf("untracked file %q cannot be measured within limits: %w", path, readErr)
		}
		if bytes.IndexByte(data, 0) >= 0 {
			return nil, fmt.Errorf("untracked binary file %q", path)
		}
		stats = append(stats, GitFileStat{Path: path, Added: countLines(data)})
	}
	return stats, nil
}

var openUntrackedFile = openUntrackedFileNoFollow

func readBoundedUntrackedFile(path string) ([]byte, error) {
	file, err := openUntrackedFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxUntrackedFileBytes {
		return nil, fmt.Errorf("file is not a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxUntrackedFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxUntrackedFileBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxUntrackedFileBytes)
	}
	return data, nil
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
		if fields[0] == "-" && fields[1] == "-" {
			stats = append(stats, GitFileStat{Path: fields[2]})
			continue
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
	committed, err := gitNumstat(root, "diff", "--no-renames", "--numstat", "-z", start.HEAD, end.HEAD)
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
		committed := statsMap(end.Committed)
		for path, counts := range before {
			current, remains := after[path]
			if _, overlaps := committed[path]; overlaps || !remains || current != counts || start.DirtySignatures[path] == "" || start.DirtySignatures[path] != end.DirtySignatures[path] {
				return GitChangeCounts{Reason: "preexisting dirty state prevents conservative commit attribution"}
			}
		}
		dirtyDelta := make(map[string]gitCounts, len(after))
		for path, counts := range after {
			previous, existed := before[path]
			if !existed || counts != previous {
				dirtyDelta[path] = gitCounts{added: counts.added - previous.added, deleted: counts.deleted - previous.deleted}
			}
		}
		return countGitStats(mergeGitStats(committed, dirtyDelta))
	}
	return countDirtyDelta(start, end, before, after)
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

func countDirtyDelta(start, end *GitCheckpoint, before, after map[string]gitCounts) GitChangeCounts {
	paths := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		paths[path] = struct{}{}
	}
	for path := range after {
		paths[path] = struct{}{}
	}
	result := GitChangeCounts{Available: true}
	for path := range paths {
		left, existedBefore := before[path]
		right, existsAfter := after[path]
		if existedBefore && !existsAfter {
			return GitChangeCounts{Reason: "repository change cannot be derived conservatively"}
		}
		if right.added < left.added || right.deleted < left.deleted {
			return GitChangeCounts{Reason: "repository change cannot be derived conservatively"}
		}
		if existedBefore && existsAfter && (start.DirtySignatures[path] == "" || end.DirtySignatures[path] == "") {
			return GitChangeCounts{Reason: "repository change cannot be derived conservatively"}
		}
		if !existedBefore || right != left || start.DirtySignatures[path] != end.DirtySignatures[path] {
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

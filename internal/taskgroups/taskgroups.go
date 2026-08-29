// Package taskgroups parses planned implementation task ownership.
package taskgroups

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type PlanKind string

const (
	Full   PlanKind = "full"
	Simple PlanKind = "simple"
)

type Options struct {
	WorkspaceDir string
	ChangeDir    string
	PlanKind     PlanKind
	Repositories []string
}

type Group struct {
	Repository string   `json:"repository"`
	Tasks      []string `json:"tasks"`
}

type Plan struct {
	Repositories []string `json:"repositories"`
	Groups       []Group  `json:"groups"`
	Snapshot     Snapshot `json:"snapshot"`
	Fingerprint  string   `json:"fingerprint"`
}

// Snapshot is the persistence-safe representation of planned task ownership.
// Task paths are normalized relative to the change directory so it captures
// plan identity without tying the value to a transient absolute path spelling.
type Snapshot struct {
	Groups []SnapshotGroup `json:"groups"`
}

type SnapshotGroup struct {
	Repository string   `json:"repository"`
	Tasks      []string `json:"tasks"`
}

func (p *Plan) TaskPattern(repository string) string {
	for _, group := range p.Groups {
		if group.Repository == repository && len(group.Tasks) > 0 {
			if filepath.Base(group.Tasks[0]) == "tasks.md" && len(group.Tasks) == 1 {
				return literalGlobPath(group.Tasks[0])
			}
			return filepath.Join(literalGlobPath(filepath.Dir(group.Tasks[0])), "*.md")
		}
	}
	return ""
}

func literalGlobPath(path string) string {
	return strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`).Replace(path)
}

var (
	repositoryHeading = regexp.MustCompile(`^## Repository: (\S+)\s*$`)
	taskLink          = regexp.MustCompile(`(?m)^\s*-\s+\[[ xX]\]\s+\[[^]]+\]\(([^)]+)\)\s*$`)
)

func Parse(options Options) (Plan, error) {
	if options.PlanKind != Full && options.PlanKind != Simple {
		return Plan{}, fmt.Errorf("plan kind must be full or simple")
	}
	workspaceDir, err := canonicalDirectory(options.WorkspaceDir)
	if err != nil {
		return Plan{}, fmt.Errorf("workspace directory: %w", err)
	}
	changeDir, err := canonicalDirectory(options.ChangeDir)
	if err != nil {
		return Plan{}, fmt.Errorf("change directory: %w", err)
	}
	if !isWithin(workspaceDir, changeDir) {
		return Plan{}, fmt.Errorf("change directory %s is outside workspace %s", changeDir, workspaceDir)
	}
	if options.PlanKind == Simple {
		if len(options.Repositories) == 0 || !declaresRepositoryGroups(changeDir) {
			return parseImplicit(changeDir, Simple)
		}
		return parseConfigured(changeDir, options.Repositories)
	}
	if len(options.Repositories) == 0 {
		return parseImplicit(changeDir, Full)
	}
	return parseConfigured(changeDir, options.Repositories)
}

func declaresRepositoryGroups(changeDir string) bool {
	index, err := readTaskIndex(changeDir)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(index), "\n") {
		if repositoryHeading.MatchString(line) {
			return true
		}
	}
	return false
}

func readTaskIndex(changeDir string) ([]byte, error) {
	root, err := os.OpenRoot(changeDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	index, err := root.Open("tasks.md")
	if err != nil {
		return nil, err
	}
	defer func() { _ = index.Close() }()
	return io.ReadAll(index)
}

func parseImplicit(changeDir string, kind PlanKind) (Plan, error) {
	if kind == Simple {
		task, err := canonicalFile(filepath.Join(changeDir, "tasks.md"))
		if err != nil {
			return Plan{}, fmt.Errorf("simple task file: %w", err)
		}
		if err := requireNonEmpty(task); err != nil {
			return Plan{}, err
		}
		return newPlan(changeDir, []Group{{Repository: "default", Tasks: []string{task}}})
	}
	entries, err := os.ReadDir(filepath.Join(changeDir, "tasks"))
	if err != nil {
		return Plan{}, fmt.Errorf("read task directory: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return numericTaskNameLess(entries[i].Name(), entries[j].Name()) })
	group := Group{Repository: "default"}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		task, err := canonicalFile(filepath.Join(changeDir, "tasks", entry.Name()))
		if err != nil {
			return Plan{}, err
		}
		if err := requireNonEmpty(task); err != nil {
			return Plan{}, err
		}
		group.Tasks = append(group.Tasks, task)
	}
	if len(group.Tasks) == 0 {
		return Plan{}, fmt.Errorf("no non-empty task file under %s", filepath.Join(changeDir, "tasks"))
	}
	return newPlan(changeDir, []Group{group})
}

func numericTaskNameLess(left, right string) bool {
	leftNumber, leftRest := numericPrefix(left)
	rightNumber, rightRest := numericPrefix(right)
	if leftNumber != rightNumber {
		if len(leftNumber) != len(rightNumber) {
			return len(leftNumber) < len(rightNumber)
		}
		return leftNumber < rightNumber
	}
	if leftRest != rightRest {
		return leftRest < rightRest
	}
	return left < right
}

func numericPrefix(name string) (number, remainder string) {
	for i, r := range name {
		if r < '0' || r > '9' {
			return normalizedNumber(name[:i]), name[i:]
		}
	}
	return normalizedNumber(name), ""
}

func normalizedNumber(value string) string {
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return "0"
	}
	return value
}

func requireNonEmpty(path string) error {
	info, err := fileInfo(path)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("task file %s is missing or empty", path)
	}
	return nil
}

func parseConfigured(changeDir string, repositories []string) (Plan, error) {
	allowed := make(map[string]bool, len(repositories))
	for _, repository := range repositories {
		allowed[repository] = true
	}
	index, err := readTaskIndex(changeDir)
	if err != nil {
		return Plan{}, fmt.Errorf("read task index: %w", err)
	}
	var groups []Group
	seen := map[string]bool{}
	var current *Group
	for _, line := range strings.Split(string(index), "\n") {
		if strings.HasPrefix(line, "## Repository") && !repositoryHeading.MatchString(line) {
			return Plan{}, fmt.Errorf("malformed repository heading %q", line)
		}
		if matches := repositoryHeading.FindStringSubmatch(line); matches != nil {
			name := matches[1]
			if !allowed[name] {
				return Plan{}, fmt.Errorf("task group names unknown repository %q", name)
			}
			if seen[name] {
				return Plan{}, fmt.Errorf("task group repeats repository %q", name)
			}
			seen[name] = true
			groups = append(groups, Group{Repository: name})
			current = &groups[len(groups)-1]
			continue
		}
		matches := taskLink.FindStringSubmatch(line)
		if matches == nil {
			continue
		}
		if current == nil {
			return Plan{}, fmt.Errorf("task link has no repository group")
		}
		task, err := resolveConfiguredTask(changeDir, current.Repository, matches[1])
		if err != nil {
			return Plan{}, err
		}
		current.Tasks = append(current.Tasks, task)
	}
	if len(groups) == 0 {
		return Plan{}, fmt.Errorf("task index contains no repository groups")
	}
	for _, group := range groups {
		if len(group.Tasks) == 0 {
			return Plan{}, fmt.Errorf("task group %q is empty", group.Repository)
		}
	}
	if err := validateConfiguredTaskFiles(changeDir, groups); err != nil {
		return Plan{}, err
	}
	if err := validateGroupTaskDirectories(groups); err != nil {
		return Plan{}, err
	}
	return newPlan(changeDir, groups)
}

func validateGroupTaskDirectories(groups []Group) error {
	for _, group := range groups {
		directory := filepath.Dir(group.Tasks[0])
		for _, task := range group.Tasks[1:] {
			if filepath.Dir(task) != directory {
				return fmt.Errorf("task group %q must keep all tasks in the same directory for its loop pattern", group.Repository)
			}
		}
	}
	return nil
}

func resolveConfiguredTask(changeDir, repository, destination string) (string, error) {
	destination, err := localDestination(destination)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(destination) || filepath.VolumeName(destination) != "" || hasTraversal(destination) {
		return "", fmt.Errorf("task link %q is not a safe relative path", destination)
	}
	path := filepath.Join(changeDir, filepath.FromSlash(destination))
	canonical, err := canonicalFile(path)
	if err != nil {
		return "", fmt.Errorf("task link %q: %w", destination, err)
	}
	root, err := canonicalDirectory(filepath.Join(changeDir, "tasks", repository))
	if err != nil {
		return "", fmt.Errorf("task group %q directory: %w", repository, err)
	}
	if !isWithin(root, canonical) {
		return "", fmt.Errorf("task link %q is outside tasks/%s", destination, repository)
	}
	info, err := fileInfo(canonical)
	if err != nil || info.Size() == 0 {
		return "", fmt.Errorf("task link %q is missing or empty", destination)
	}
	return canonical, nil
}

func validateConfiguredTaskFiles(changeDir string, groups []Group) error {
	linked := make(map[string]string)
	for _, group := range groups {
		for _, task := range group.Tasks {
			if owner, exists := linked[task]; exists {
				return fmt.Errorf("task file %s is linked by both %q and %q", task, owner, group.Repository)
			}
			linked[task] = group.Repository
		}
	}
	tasksDir, err := canonicalDirectory(filepath.Join(changeDir, "tasks"))
	if err != nil {
		return fmt.Errorf("task directory: %w", err)
	}
	root, err := os.OpenRoot(tasksDir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			return nil
		}
		canonical, err := canonicalFile(filepath.Join(tasksDir, path))
		if err != nil {
			return err
		}
		if !isWithin(tasksDir, canonical) {
			return fmt.Errorf("task file %s resolves outside the task directory", path)
		}
		info, err := fileInfo(canonical)
		if err != nil {
			return err
		}
		if info.Size() == 0 {
			return fmt.Errorf("task file %s is empty", path)
		}
		if _, exists := linked[canonical]; !exists {
			return fmt.Errorf("unlinked task file %s", path)
		}
		return nil
	})
}

func localDestination(destination string) (string, error) {
	destination = strings.TrimSpace(destination)
	destination = strings.TrimPrefix(strings.TrimSuffix(destination, ">"), "<")
	destination = strings.SplitN(destination, "#", 2)[0]
	destination = strings.SplitN(destination, "?", 2)[0]
	decoded, err := url.PathUnescape(destination)
	if err != nil {
		return "", fmt.Errorf("task link %q has invalid escaping", destination)
	}
	if decoded == "" {
		return "", fmt.Errorf("task link is empty")
	}
	return decoded, nil
}

func hasTraversal(path string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

func newPlan(changeDir string, groups []Group) (Plan, error) {
	plan := Plan{Groups: groups}
	for _, group := range groups {
		plan.Repositories = append(plan.Repositories, group.Repository)
		snapshotGroup := SnapshotGroup{Repository: group.Repository}
		for _, task := range group.Tasks {
			relative, err := filepath.Rel(changeDir, task)
			if err != nil {
				return Plan{}, fmt.Errorf("normalize task path %s: %w", task, err)
			}
			snapshotGroup.Tasks = append(snapshotGroup.Tasks, filepath.ToSlash(relative))
		}
		plan.Snapshot.Groups = append(plan.Snapshot.Groups, snapshotGroup)
	}
	snapshot, err := json.Marshal(plan.Snapshot)
	if err != nil {
		return Plan{}, err
	}
	sum := sha256.Sum256(snapshot)
	plan.Fingerprint = hex.EncodeToString(sum[:])
	return plan, nil
}

func canonicalDirectory(path string) (string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := fileInfo(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	return canonical, nil
}

func canonicalFile(path string) (string, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := fileInfo(canonical)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("is a directory")
	}
	return canonical, nil
}

func fileInfo(path string) (os.FileInfo, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.Stat(filepath.Base(path))
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

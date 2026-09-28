package runretention

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/codagent/agent-runner/internal/runlock"
	"github.com/codagent/agent-runner/internal/stateio"
	"github.com/codagent/agent-runner/internal/usersettings"
)

type retentionState struct {
	ActivatedAt     time.Time `json:"activated_at,omitempty"`
	LastStartedAt   time.Time `json:"last_started_at,omitempty"`
	LastCompletedAt time.Time `json:"last_completed_at,omitempty"`
}

type Report struct {
	Notice    string
	Warnings  []string
	Removed   []string
	statePath string
	progress  func(Report)
}

func (r *Report) warn(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
	r.publish()
}

func (r *Report) publish() {
	if r.progress == nil {
		return
	}
	snapshot := *r
	snapshot.progress = nil
	snapshot.Warnings = append([]string(nil), r.Warnings...)
	snapshot.Removed = append([]string(nil), r.Removed...)
	r.progress(snapshot)
}

func due(s retentionState, now time.Time) bool {
	if !s.LastCompletedAt.IsZero() && now.Sub(s.LastCompletedAt) < 24*time.Hour {
		return false
	}
	return s.LastStartedAt.IsZero() || !s.LastStartedAt.After(s.LastCompletedAt) || now.Sub(s.LastStartedAt) >= time.Hour
}

// Sweep scans the user's projects and performs one due sweep. It is also used by integration tests.
func Sweep(home string, now time.Time) Report {
	return sweep(home, now, nil)
}

//nolint:gocognit,funlen // A sweep is a sequential fail-closed transaction across projects.
func sweep(home string, now time.Time, progress func(Report)) Report {
	wallStarted := time.Now()
	r := Report{progress: progress}
	settings, known, err := usersettings.LoadRunRetentionAt(filepath.Join(home, ".agent-runner", "settings.yaml"))
	if !known {
		r.warn("retention policy could not be read: %v", err)
		return r
	}
	p := PolicyFromSettings(settings)
	for _, invalid := range settings.Invalid {
		r.warn("invalid run_retention.%s; using default", invalid)
	}
	if !p.Enabled {
		return r
	}
	retentionDir := filepath.Join(home, ".agent-runner", "retention")
	if err := os.MkdirAll(retentionDir, 0o700); err != nil {
		r.warn("create retention directory: %v", err)
		return r
	}
	pid, err := runlock.Acquire(retentionDir)
	if pid > 0 {
		return r
	}
	if err != nil {
		r.warn("acquire sweep lock: %v", err)
		return r
	}
	defer runlock.Delete(retentionDir)
	r.statePath = filepath.Join(retentionDir, "state.json")
	s, err := readRetentionState(r.statePath)
	if err != nil {
		r.warn("read sweep state: %v", err)
		return r
	}
	if !due(s, now) {
		return r
	}
	s.LastStartedAt = now
	if err := stateio.WriteJSONAtomic(r.statePath, s); err != nil {
		r.warn("write sweep state: %v", err)
		return r
	}
	root := filepath.Join(home, ".agent-runner", "projects")
	root, err = filepath.EvalSymlinks(root)
	if errors.Is(err, fs.ErrNotExist) {
		finishState(&r, &s, now.Add(time.Since(wallStarted)))
		return r
	}
	if err != nil {
		r.warn("resolve projects: %v", err)
		return r
	}
	projects, err := os.ReadDir(root)
	if err != nil {
		r.warn("read projects: %v", err)
		return r
	}
	var candidates []projectRuns
	for _, project := range projects {
		projectDir := filepath.Join(root, project.Name())
		if !realDirectory(projectDir) {
			r.warn("skipped non-directory project %s", projectDir)
			continue
		}
		runsDir := filepath.Join(projectDir, "runs")
		if _, err := os.Lstat(runsDir); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if !realDirectory(runsDir) {
			r.warn("skipped non-directory runs path %s", runsDir)
			continue
		}
		entries, err := os.ReadDir(runsDir)
		if err != nil {
			r.warn("read %s: %v", runsDir, err)
			continue
		}
		var all []Run
		for _, entry := range entries {
			dir := filepath.Join(runsDir, entry.Name())
			if strings.HasPrefix(entry.Name(), ".pruning-") {
				if err := deleteTree(runsDir, dir); err != nil {
					r.warn("remove trash %s: %v", dir, err)
				}
				continue
			}
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			if !realDirectory(dir) {
				r.warn("skipped non-directory run %s", dir)
				continue
			}
			item, err := Classify(dir, false)
			if err != nil {
				r.warn("protected %s: %v", dir, err)
			}
			all = append(all, item)
		}
		candidates = append(candidates, projectRuns{dir: runsDir, runs: all})
	}
	if s.ActivatedAt.IsZero() {
		count := 0
		for _, project := range candidates {
			for _, unit := range Plan(project.runs, p, now) {
				count += len(unit.Members)
			}
		}
		r.Notice = fmt.Sprintf("agent-runner: run retention enabled: %s, %s, %s. %d existing runs will become eligible after 7 days. Configure or disable with run_retention.enabled: false in ~/.agent-runner/settings.yaml.\n", describeAge("finished runs", p.MaxAge), describeAge("resumable runs", p.ResumableMaxAge), describeCount(p.MaxRunsPerProject), count)
		r.publish()
	} else if !now.Before(s.ActivatedAt.Add(7 * 24 * time.Hour)) {
		for _, project := range candidates {
			units := Plan(project.runs, p, now)
			for i := range units {
				removeUnit(project.dir, &units[i], p, now, &r)
			}
		}
	}
	finishState(&r, &s, now.Add(time.Since(wallStarted)))
	return r
}

type projectRuns struct {
	dir  string
	runs []Run
}

func describeAge(kind string, age time.Duration) string {
	if age == 0 {
		return kind + " age limit disabled"
	}
	return fmt.Sprintf("%s older than %d days", kind, int(age.Hours()/24))
}

func describeCount(limit int) string {
	if limit == 0 {
		return "finished run count limit disabled"
	}
	return fmt.Sprintf("at most %d finished runs per project", limit)
}

func readRetentionState(path string) (retentionState, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- fixed retention state path.
	if errors.Is(err, fs.ErrNotExist) {
		return retentionState{}, nil
	}
	if err != nil {
		return retentionState{}, err
	}
	var s retentionState
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	return s, nil
}

func finishState(r *Report, s *retentionState, now time.Time) {
	s.LastCompletedAt = now
	if err := stateio.WriteJSONAtomic(r.statePath, s); err != nil {
		r.warn("write sweep completion: %v", err)
	}
}

func realDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

//nolint:gocognit,cyclop,funlen // Removal rechecks every safety condition while holding all group locks.
func removeUnit(runsDir string, unit *Unit, p Policy, now time.Time, r *Report) {
	ids := make([]string, 0, len(unit.Members))
	for i := range unit.Members {
		ids = append(ids, unit.Members[i].ID)
	}
	sort.Strings(ids)
	var held []string
	defer func() {
		for _, dir := range held {
			runlock.Delete(dir)
		}
	}()
	for _, id := range ids {
		dir := filepath.Join(runsDir, id)
		pid, err := runlock.Acquire(dir)
		if pid > 0 {
			return
		}
		if err != nil {
			r.warn("lock %s: %v", dir, err)
			return
		}
		held = append(held, dir)
	}
	if unit.SourceID != "" {
		release, err := runlock.ClaimLinkage(filepath.Join(runsDir, unit.SourceID))
		if err != nil {
			r.warn("claim linkage for %s: %v", unit.SourceID, err)
			return
		}
		defer release()
	}
	// Membership and eligibility may have changed after the first scan.
	latest := scanRuns(runsDir, ids, ids)
	if len(latest) != len(ids) {
		return
	}
	observed := make(map[string]time.Time, len(unit.Members))
	for i := range unit.Members {
		observed[unit.Members[i].ID] = unit.Members[i].LastActivity
	}
	rechecked := make(map[string]Run, len(latest))
	for i := range latest {
		m := &latest[i]
		if m.Class.protected() {
			return
		}
		if old := observed[m.ID]; m.LastActivity.After(old) {
			// Creating a missing lock changes the run directory mtime. Preserve
			// the earlier observation unless state or audit was updated.
			m.LastActivity = newestMetadata(m.Dir, old)
		}
		rechecked[m.ID] = *m
	}
	whole := scanRuns(runsDir, nil, ids)
	for i := range whole {
		if m, ok := rechecked[whole[i].ID]; ok {
			whole[i] = m
		}
	}
	if !slices.ContainsFunc(Plan(whole, p, now), func(u Unit) bool { return sameMembers(u.Members, ids) }) {
		return
	}
	// Detect a newly linked audit sibling before committing any rename.
	for i := range whole {
		other := &whole[i]
		if unit.SourceID != "" && other.SourceRunID == unit.SourceID && !slices.Contains(ids, other.ID) {
			return
		}
		if other.ID == unit.SourceID {
			for _, link := range other.AuditRunIDs {
				if realDirectory(filepath.Join(runsDir, link)) && !slices.Contains(ids, link) {
					return
				}
			}
		}
	}
	order := append([]string(nil), ids...)
	sort.Slice(order, func(i, j int) bool { return order[i] != unit.SourceID && order[j] == unit.SourceID })
	for _, id := range order {
		original := filepath.Join(runsDir, id)
		trash := filepath.Join(runsDir, ".pruning-"+id)
		if err := os.Rename(original, trash); err != nil {
			r.warn("remove %s: %v", original, err)
			return
		}
		if err := deleteTree(runsDir, trash); err != nil {
			r.warn("remove %s: %v", trash, err)
		}
		r.Removed = append(r.Removed, original)
	}
}

func sameMembers(members []Run, ids []string) bool {
	if len(members) != len(ids) {
		return false
	}
	for i := range ids {
		if members[i].ID != ids[i] {
			return false
		}
	}
	return true
}

func scanRuns(runsDir string, only, ownIDs []string) []Run {
	if only == nil {
		entries, err := os.ReadDir(runsDir)
		if err != nil {
			return nil
		}
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".") && realDirectory(filepath.Join(runsDir, e.Name())) {
				only = append(only, e.Name())
			}
		}
	}
	var result []Run
	for _, id := range only {
		dir := filepath.Join(runsDir, id)
		if !realDirectory(dir) {
			continue
		}
		v, _ := Classify(dir, slices.Contains(ownIDs, id))
		result = append(result, v)
	}
	return result
}

// newestMetadata returns the newest of since and the run's state and audit log mtimes.
func newestMetadata(dir string, since time.Time) time.Time {
	newest := since
	for _, name := range []string{"state.json", "audit.log"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
}

func deleteTree(runsDir, target string) error {
	if filepath.Dir(target) != runsDir || !strings.HasPrefix(filepath.Base(target), ".pruning-") {
		return errors.New("unsafe trash path")
	}
	root, err := os.OpenRoot(runsDir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	trash, err := root.OpenRoot(filepath.Base(target))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	err = makeTreeWritable(trash)
	_ = trash.Close()
	if err != nil {
		return err
	}
	return root.RemoveAll(filepath.Base(target))
}

func makeTreeWritable(root *os.Root) error {
	// Directories need owner write and execute permission for recursive removal.
	if err := root.Chmod(".", 0o700); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	_ = dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child, err := root.OpenRoot(entry.Name())
		if err != nil {
			return err
		}
		err = makeTreeWritable(child)
		_ = child.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

var background struct {
	sync.Mutex
	started bool
	done    chan struct{}
	report  Report
}

// StartBackground starts at most one non-blocking sweep per process.
func StartBackground() {
	background.Lock()
	if background.started {
		background.Unlock()
		return
	}
	background.started = true
	background.done = make(chan struct{})
	background.Unlock()
	go func() {
		defer close(background.done)
		defer func() {
			if v := recover(); v != nil {
				background.Lock()
				background.report.warn("sweep panic: %v", v)
				background.Unlock()
			}
		}()
		home, err := os.UserHomeDir()
		var report Report
		if err != nil {
			report.warn("home directory: %v", err)
		} else {
			report = sweep(home, time.Now(), func(partial Report) { background.Lock(); background.report = partial; background.Unlock() })
		}
		background.Lock()
		background.report = report
		background.Unlock()
	}()
}

// Finish waits briefly for the sweep and prints its notice and warnings at exit.
func Finish(w io.Writer, bound time.Duration) {
	background.Lock()
	done := background.done
	background.Unlock()
	if done == nil {
		return
	}
	completed := false
	select {
	case <-done:
		completed = true
	case <-time.After(bound):
	}
	background.Lock()
	report := background.report
	background.Unlock()
	if completed && report.Notice != "" {
		_, writeErr := io.WriteString(w, report.Notice)
		if writeErr == nil && report.statePath != "" {
			s, err := readRetentionState(report.statePath)
			if err == nil && s.ActivatedAt.IsZero() {
				s.ActivatedAt = time.Now()
				if err := stateio.WriteJSONAtomic(report.statePath, s); err != nil {
					report.warn("record activation: %v", err)
				}
			}
		}
	}
	for _, warning := range report.Warnings {
		_, _ = fmt.Fprintf(w, "agent-runner: warning: run retention: %s\n", warning)
	}
}

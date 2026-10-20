package audit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestStepEvidenceWindows(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "output")
	_ = os.MkdirAll(output, 0o700)
	l := NewCheckpointLogger(nil, "", "session", dir)
	start := func(prefix string) {
		l.Decorate(Event{Type: EventStepStart, Prefix: prefix, Data: map[string]any{"script": "test"}})
	}
	end := func(prefix string) WriteRecord {
		e := l.Decorate(Event{Type: EventStepEnd, Prefix: prefix})
		return e.Data["output_writes"].(WriteRecord)
	}
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(output, "ledger.md"), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	start("[check]")
	write("A\n")
	start("[check,repair]")
	write("A\nB\n")
	child := end("[check,repair]")
	start("[check,run]")
	write("A\nB\nC\n")
	run := end("[check,run]")
	parent := end("[check]")
	for i, r := range []WriteRecord{parent, child, run} {
		if r.Status != "observed" || len(r.Files) != 1 {
			t.Fatalf("%d: %+v", i, r)
		}
		c := r.Files[0].Contribution
		data, err := os.ReadFile(filepath.Join(dir, c.Blob))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != []string{"A\n", "B\n", "C\n"}[i] {
			t.Fatalf("%d: %q", i, data)
		}
	}
	start("[empty]")
	_ = os.WriteFile(filepath.Join(output, "_x_.out"), []byte("stream"), 0o600)
	_ = os.Symlink("/etc/passwd", filepath.Join(output, "link"))
	if r := end("[empty]"); r.Status != "observed" || r.Files == nil || len(r.Files) != 0 {
		t.Fatalf("%+v", r)
	}
	start("[delete]")
	_ = os.Remove(filepath.Join(output, "ledger.md"))
	if r := end("[delete]"); len(r.Files) != 1 || r.Files[0].Change != "deleted" || r.Files[0].Contribution != nil {
		t.Fatalf("%+v", r)
	}
}

func TestStepEvidenceContributions(t *testing.T) {
	for _, tc := range []struct{ name, before, after, kind string }{
		{"create", "", "new\r\n", "created"},
		{"append", "old\r\n", "old\r\nnew\r\n", "append"},
		{"large append", strings.Repeat("x", (1<<20)+1), strings.Repeat("x", (1<<20)+1) + "new", "append"},
		{"diff", "heading\nfinding\nfooter\n", "new heading\nfinding\nnew footer\n", "diff"},
		{"large edit", strings.Repeat("a", (1<<20)+1), strings.Repeat("b", (1<<20)+1), "unresolved"},
		{"binary", "old", "new\x00", "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_ = os.Mkdir(filepath.Join(dir, "output"), 0o700)
			p := filepath.Join(dir, "output", "file")
			if tc.before != "" {
				_ = os.WriteFile(p, []byte(tc.before), 0o600)
			}
			l := NewCheckpointLogger(nil, "", "s", dir)
			l.Decorate(Event{Type: EventStepStart, Prefix: "[x]", Data: map[string]any{"script": "x"}})
			_ = os.WriteFile(p, []byte(tc.after), 0o600)
			e := l.Decorate(Event{Type: EventStepEnd, Prefix: "[x]"})
			r := e.Data["output_writes"].(WriteRecord)
			if len(r.Files) != 1 {
				t.Fatalf("%+v", r)
			}
			c := r.Files[0].Contribution
			if c == nil || c.Kind != tc.kind {
				t.Fatalf("%+v", c)
			}
			if tc.kind == "append" {
				data, _ := os.ReadFile(filepath.Join(dir, c.Blob))
				if string(data) != strings.TrimPrefix(tc.after, tc.before) {
					t.Fatalf("append=%q", data)
				}
			}
			if tc.kind == "diff" {
				data, _ := os.ReadFile(filepath.Join(dir, c.Blob))
				if strings.Contains(string(data), "+finding") {
					t.Fatalf("inherited content: %s", data)
				}
			}
		})
	}
}

func TestRunnerOwnedOutputName(t *testing.T) {
	for _, name := range []string{"_x_.out", "_x_.err", "_x_.out.bak-123", "_x_.err.bak-1", "persistence-warnings.log"} {
		if !RunnerOwnedOutputName(name) {
			t.Error(name)
		}
	}
	for _, name := range []string{"x.out", "nested/_x_.out", "_findings.md", "ledger.md"} {
		if RunnerOwnedOutputName(name) {
			t.Error(name)
		}
	}
}

func TestObservationBudgetsAndIncompleteIdentity(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "output")
	_ = os.MkdirAll(output, 0o700)
	for _, name := range []string{"a", "b", "c"} {
		_ = os.WriteFile(filepath.Join(output, name), []byte("123456789"), 0o600)
	}
	for _, tc := range []struct {
		name  string
		setup func(*observeBudget)
	}{
		{"files", func(b *observeBudget) { b.fileLimit = 2 }},
		{"entries", func(b *observeBudget) { b.entryLimit = 2 }},
		{"read", func(b *observeBudget) { b.readLimit = 4 }},
		{"time", func(b *observeBudget) {
			now := b.now()
			b.now = func() time.Time { now = now.Add(time.Second); return now }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := newOutputObserver(dir, "")
			b := newObserveBudget()
			b.boundary()
			tc.setup(b)
			r := o.observe(b)
			if r.status != "truncated" || r.reason == "" {
				t.Fatalf("%+v", r)
			}
		})
	}
	o := newOutputObserver(dir, "")
	b := newObserveBudget()
	b.boundary()
	b.hashLimit = 4
	before := o.observe(b)
	_ = os.WriteFile(filepath.Join(output, "a"), []byte("987654321"), 0o600)
	after := o.observe(b)
	r := o.delta(before, after, output, b, 0)
	if len(r.Files) != 1 || r.Files[0].After.IdentityComplete || r.Files[0].After.SHA256 != "" || r.Files[0].Contribution.Kind != "unresolved" {
		t.Fatalf("%+v", r)
	}
}

func TestRepositoryPatchVersions(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	path := filepath.Join(repo, "file")
	_ = os.WriteFile(path, []byte("heading\nfinding\nfooter\n"), 0o600)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	dir := t.TempDir()
	l := NewCheckpointLogger(nil, repo, "s", dir)
	for i, content := range []string{"new heading\nfinding\nfooter\n", "new heading\nfinding\nnew footer\n"} {
		l.Decorate(Event{Type: EventStepStart, Prefix: "[edit]", Data: map[string]any{"command": "test"}})
		_ = os.WriteFile(path, []byte(content), 0o600)
		event := l.Decorate(Event{Type: EventStepEnd, Prefix: "[edit]"})
		r := event.Data["repository_patch"].(WriteRecord)
		if r.Status != "observed" || len(r.Files) != 1 {
			t.Fatalf("%+v", r)
		}
		c := r.Files[0].Contribution
		data, err := os.ReadFile(filepath.Join(dir, c.Blob))
		if err != nil {
			t.Fatal(err)
		}
		if c.Kind != "diff" || strings.Contains(string(data), "+finding") || i == 1 && strings.Contains(string(data), "+new heading") {
			t.Fatalf("%s: %s", c.Kind, data)
		}
	}
}

func TestEvidenceStoreDeduplicatesAndBounds(t *testing.T) {
	dir := t.TempDir()
	store := evidenceStore{dir: dir}
	b := newObserveBudget()
	b.boundary()
	path, err := store.put([]byte("retained"), b)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(dir, path))
	if info.Mode().Perm() != 0o600 {
		t.Fatal(info.Mode())
	}
	stamp := time.Unix(100, 0)
	_ = os.Chtimes(filepath.Join(dir, path), stamp, stamp)
	again, err := store.put([]byte("retained"), b)
	if err != nil || again != path {
		t.Fatal(again, err)
	}
	info, _ = os.Stat(filepath.Join(dir, path))
	if !info.ModTime().Equal(stamp) {
		t.Fatal("blob rewritten")
	}
	b.stored = maxStepStoredBytes
	_, err = store.put([]byte("different"), b)
	if err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatal(err)
	}
	store.size = maxRunStoredBytes
	b.stored = 0
	_, err = store.put([]byte("different"), b)
	if err == nil || err.Error() != "evidence store full" {
		t.Fatal(err)
	}
}

func TestRepositoryEvidenceFromSubdirectory(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	_ = os.WriteFile(filepath.Join(repo, "file"), []byte("base\n"), 0o600)
	_ = os.Mkdir(filepath.Join(repo, "sub"), 0o700)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	dir := t.TempDir()
	l := NewCheckpointLogger(nil, filepath.Join(repo, "sub"), "s", dir)
	l.Decorate(Event{Type: EventStepStart, Prefix: "[x]", Data: map[string]any{"command": "x"}})
	_ = os.WriteFile(filepath.Join(repo, "file"), []byte("changed\n"), 0o600)
	event := l.Decorate(Event{Type: EventStepEnd, Prefix: "[x]"})
	r := event.Data["repository_patch"].(WriteRecord)
	if len(r.Files) != 1 || r.Files[0].Contribution == nil || r.Files[0].Contribution.Kind != "diff" {
		t.Fatalf("%+v", r)
	}
}

func TestContributionTruncatesAtUTF8Boundary(t *testing.T) {
	data := []byte(strings.Repeat("世", maxContributionBytes/3+1))
	after := FileVersion{Size: int64(len(data)), IdentityComplete: true}
	r, content := Contribution(nil, &after, nil, bytes.NewReader(data))
	if !r.Truncated || !utf8.Valid(content) {
		t.Fatalf("truncated=%v valid=%v", r.Truncated, utf8.Valid(content))
	}
}

func TestRecentMtimeRehashesSameMetadata(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "output")
	_ = os.Mkdir(output, 0o700)
	path := filepath.Join(output, "file")
	_ = os.WriteFile(path, []byte("old"), 0o600)
	stamp := time.Now()
	_ = os.Chtimes(path, stamp, stamp)
	o := newOutputObserver(dir, "")
	b := newObserveBudget()
	b.boundary()
	a := o.observe(b)
	_ = os.WriteFile(path, []byte("new"), 0o600)
	_ = os.Chtimes(path, stamp, stamp)
	z := o.observe(b)
	if sameVersion(a.files["file"].version, z.files["file"].version) {
		t.Fatal("recent same-metadata rewrite used stale hash")
	}
}

func TestEvidenceCacheDetectsOldMtimeRewriteAndRetainsNoContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	stamp := time.Unix(100, 0)
	_ = os.WriteFile(path, []byte("old"), 0o600)
	_ = os.Chtimes(path, stamp, stamp)
	observer := newOutputObserver(t.TempDir(), "")
	budget := newObserveBudget()
	budget.boundary()
	before, err := observer.readFile(dir, "file", budget)
	if err != nil {
		t.Fatal(err)
	}
	// Advance past the recent-change guard to exercise an actual cache hit.
	budget.now = func() time.Time { return time.Now().Add(3 * time.Second) }
	budget.boundary()
	if info, e := os.Stat(path); e == nil {
		if _, ok := fileChangeTime(info); ok {
			budget.hashLimit = 0
		}
	}
	cached, err := observer.readFile(dir, "file", budget)
	if err != nil || string(cached.data) != "old" {
		t.Fatalf("cache hit: %q %v", cached.data, err)
	}
	if !cached.version.IdentityComplete {
		t.Fatal("stable metadata did not restore the complete cached identity")
	}
	budget.hashLimit = maxIdentityBytes
	budget.now = time.Now
	budget.boundary()
	for _, entry := range observer.cache {
		if len(entry.data) != 0 {
			t.Fatal("cache retains file content")
		}
	}
	_ = os.WriteFile(path, []byte("new"), 0o600)
	_ = os.Chtimes(path, stamp, stamp)
	after, err := observer.readFile(dir, "file", budget)
	if err != nil {
		t.Fatal(err)
	}
	if sameVersion(before.version, after.version) {
		t.Fatal("same-size old-mtime rewrite used stale identity")
	}
}

func TestEvidenceCacheHasEntryBound(t *testing.T) {
	dir := t.TempDir()
	observer := newOutputObserver(t.TempDir(), "")
	budget := newObserveBudget()
	budget.boundary()
	for i := 0; i < maxCachedFiles+1; i++ {
		path := fmt.Sprintf("file-%d", i)
		_ = os.WriteFile(filepath.Join(dir, path), []byte("x"), 0o600)
		if _, err := observer.readFile(dir, path, budget); err != nil {
			t.Fatal(err)
		}
	}
	if len(observer.cache) > maxCachedFiles {
		t.Fatalf("cache entries %d", len(observer.cache))
	}
}

// The output root and the repository share one cache, so a step at both
// supported maxima must keep every live entry instead of clearing at every
// boundary and rehashing everything.
func TestEvidenceCacheHoldsOutputAndRepositoryMaxima(t *testing.T) {
	output, repo := t.TempDir(), t.TempDir()
	observer := newOutputObserver(t.TempDir(), "")
	budget := newObserveBudget()
	budget.boundary()
	read := func(root, path string) {
		_ = os.WriteFile(filepath.Join(root, path), []byte("x"), 0o600)
		if _, err := observer.readFile(root, path, budget); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < maxObservedFiles; i++ {
		read(output, fmt.Sprintf("out-%d", i))
	}
	for i := 0; i < maxTrackedFingerprintPaths+maxUntrackedFiles; i++ {
		read(repo, fmt.Sprintf("repo-%d", i))
	}
	if _, ok := observer.cache[filepath.Join(output, "out-0")]; !ok {
		t.Fatalf("cache was cleared within supported maxima: %d entries", len(observer.cache))
	}
}

func TestGitVersionFailuresAreNotMissingFiles(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	_ = os.WriteFile(filepath.Join(repo, "file"), []byte("base"), 0o600)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	observer := newOutputObserver(t.TempDir(), repo)
	budget := newObserveBudget()
	budget.boundary()
	head, err := gitOutput(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	head = strings.TrimSpace(head)
	if _, err = observer.gitVersion(head, "missing", budget); !os.IsNotExist(err) {
		t.Fatalf("missing path: %v", err)
	}
	if _, err = observer.gitVersion(strings.Repeat("0", 40), "file", budget); err == nil || os.IsNotExist(err) {
		t.Fatalf("missing revision should be unavailable: %v", err)
	}
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho 'fatal: temporary object read failure' >&2\nexit 128\n"), 0o700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err = observer.gitVersion(head, "file", budget); err == nil || os.IsNotExist(err) {
		t.Fatalf("git failure misclassified: %v", err)
	}
}

func TestRepositoryBaselineFailureHasNoFabricatedVersion(t *testing.T) {
	for _, reason := range []string{"permission denied", "read budget exhausted"} {
		t.Run(reason, func(t *testing.T) {
			repo := t.TempDir()
			_ = os.WriteFile(filepath.Join(repo, "file"), []byte("new content"), 0o600)
			observer := newOutputObserver(t.TempDir(), repo)
			budget := newObserveBudget()
			budget.boundary()
			window := &outputWindow{budget: budget, repo: map[string]observedFile{"file": {reason: reason}}}
			start := GitCheckpoint{Available: true, Worktree: []GitFileStat{{Path: "file", Fingerprint: "before"}}}
			end := GitCheckpoint{Available: true, Worktree: []GitFileStat{{Path: "file", Fingerprint: "after"}}}
			record := observer.repositoryPatch(window, &start, &end)
			if record.Status != observationStatus(reason) || !strings.Contains(record.Reason, reason) {
				t.Fatalf("%+v", record)
			}
			if len(record.Files) != 1 || record.Files[0].Before != nil || record.Files[0].Contribution.Kind != "unresolved" {
				t.Fatalf("fabricated baseline: %+v", record)
			}
		})
	}
}

func TestShortGitBlobHasNoPartialIdentity(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
case "$*" in *' -s '*) printf '10\n';; *) printf abc;; esac
`
	_ = os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	observer := newOutputObserver(t.TempDir(), t.TempDir())
	budget := newObserveBudget()
	budget.boundary()
	file, err := observer.gitVersion(strings.Repeat("a", 40), "file", budget)
	if err != nil {
		t.Fatal(err)
	}
	if file.version.IdentityComplete || file.version.SHA256 != "" || file.version.BlobComplete {
		t.Fatalf("partial content given full identity: %+v", file.version)
	}
}

func TestSkippedDirtyBaselineCannotClaimCreatedContent(t *testing.T) {
	repo, session := t.TempDir(), t.TempDir()
	path := filepath.Join(repo, "file")
	if err := os.WriteFile(path, []byte("inherited finding\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(session, "output")
	if err := os.Mkdir(output, 0o700); err != nil {
		t.Fatal(err)
	}
	// Runner streams consume the entry budget without consuming read budget.
	for i := range maxObserveEntries {
		if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("_%05d.out", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	observer := newOutputObserver(session, repo)
	start := GitCheckpoint{Available: true, Worktree: []GitFileStat{{Path: "file", Fingerprint: "before"}}}
	end := GitCheckpoint{Available: true, Worktree: []GitFileStat{{Path: "file", Fingerprint: "after"}}}
	observer.start("step", &start)
	if err := os.WriteFile(path, []byte("inherited finding\nstep addition\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, record := observer.end("step", &start, &end)
	if record.Status != "truncated" || len(record.Files) != 1 {
		t.Fatalf("missing baseline coverage: %+v", record)
	}
	file := record.Files[0]
	if file.Before != nil || file.Contribution.Kind != "unresolved" || file.Contribution.Blob != "" || file.Contribution.Context == "" {
		t.Fatalf("inherited content credited as a contribution: %+v", file)
	}
}

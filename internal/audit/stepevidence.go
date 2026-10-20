package audit

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rogpeppe/go-internal/diff"
)

const (
	maxObservedFiles     = 2000
	maxObserveEntries    = 10000
	maxObserveDepth      = 16
	maxObserveRead       = 64 << 20
	maxObserveTime       = 2 * time.Second
	maxIdentityBytes     = 16 << 20
	maxVersionBytes      = 1 << 20
	maxContributionBytes = 64 << 10
	maxStepStoredBytes   = 8 << 20
	maxRunStoredBytes    = 512 << 20
)

type FileVersion struct {
	SHA256           string `json:"sha256,omitempty"`
	Size             int64  `json:"size"`
	MtimeNS          int64  `json:"mtime_ns"`
	IdentityComplete bool   `json:"identity_complete"`
	Binary           bool   `json:"binary,omitempty"`
	Blob             string `json:"blob,omitempty"`
	BlobComplete     bool   `json:"blob_complete,omitempty"`
	inode            uint64
}
type Retained struct {
	Kind      string `json:"kind"`
	Blob      string `json:"blob,omitempty"`
	Context   string `json:"context,omitempty"`
	Bytes     int64  `json:"bytes"`
	Truncated bool   `json:"truncated,omitempty"`
	Reason    string `json:"reason,omitempty"`
}
type FileWrite struct {
	Path         string       `json:"path"`
	Change       string       `json:"change"`
	Segment      int          `json:"segment,omitempty"`
	Before       *FileVersion `json:"before,omitempty"`
	After        *FileVersion `json:"after,omitempty"`
	Contribution *Retained    `json:"contribution,omitempty"`
}
type WriteRecord struct {
	Status string      `json:"status"`
	Reason string      `json:"reason,omitempty"`
	Files  []FileWrite `json:"files"`
}

// RunnerOwnedOutputName excludes only top-level Runner stream files and archives.
func RunnerOwnedOutputName(name string) bool {
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	return name == "persistence-warnings.log" || (strings.HasPrefix(name, "_") && (strings.HasSuffix(name, ".out") || strings.HasSuffix(name, ".err") || strings.Contains(name, ".out.bak-") || strings.Contains(name, ".err.bak-")))
}

type observeBudget struct {
	entries               int
	read, stored          int64
	deadline              time.Time
	now                   func() time.Time
	readLimit             int64
	entryLimit, fileLimit int
	hashLimit             int64
}

func newObserveBudget() *observeBudget {
	return &observeBudget{now: time.Now, readLimit: maxObserveRead, entryLimit: maxObserveEntries, fileLimit: maxObservedFiles, hashLimit: maxIdentityBytes}
}
func (b *observeBudget) boundary() { b.deadline = b.now().Add(maxObserveTime) }
func (b *observeBudget) check() error {
	if !b.now().Before(b.deadline) {
		return errors.New("time budget exhausted")
	}
	return nil
}
func (b *observeBudget) readBytes(r io.Reader, n int64) ([]byte, error) {
	var out bytes.Buffer
	for n > 0 {
		if err := b.check(); err != nil {
			return nil, err
		}
		if b.read >= b.readLimit {
			return nil, errors.New("read budget exhausted")
		}
		count := min(n, 1<<20, b.readLimit-b.read)
		data := make([]byte, count)
		got, err := io.ReadFull(r, data)
		b.read += int64(got)
		out.Write(data[:got])
		n -= int64(got)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return out.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

type evidenceStore struct {
	dir         string
	size        int64
	initialized bool
}

func (s *evidenceStore) put(data []byte, b *observeBudget) (string, error) {
	if s.dir == "" {
		return "", errors.New("session directory unavailable")
	}
	sum := sha256.Sum256(data)
	rel := fmt.Sprintf("step-evidence/blobs/sha256/%x", sum)
	path := filepath.Join(s.dir, rel)
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", errors.New("invalid evidence blob")
		}
		return rel, nil
	}
	if !s.initialized {
		s.initialized = true
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		for _, entry := range entries {
			if info, e := entry.Info(); e == nil {
				s.size += info.Size()
			}
		}
	}
	if s.size+int64(len(data)) > maxRunStoredBytes {
		return "", errors.New("evidence store full")
	}
	if b.stored+int64(len(data)) > maxStepStoredBytes {
		return "", errors.New("stored byte budget exhausted")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".blob-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	s.size += int64(len(data))
	b.stored += int64(len(data))
	return rel, nil
}

type observedFile struct {
	cacheKey string
	version  FileVersion
	data     []byte
	reason   string
}
type observation struct {
	files          map[string]observedFile
	status, reason string
}
type outputWindow struct {
	prefix   string
	baseline observation
	repo     map[string]observedFile
	budget   *observeBudget
	record   WriteRecord
	segment  int
}
type outputObserver struct {
	repoResolved         bool
	sessionDir, repoRoot string
	store                evidenceStore
	cache                map[string]observedFile
	windows              []*outputWindow
}

func newOutputObserver(sessionDir, repoRoot string) *outputObserver {
	return &outputObserver{sessionDir: sessionDir, repoRoot: repoRoot, store: evidenceStore{dir: sessionDir}, cache: map[string]observedFile{}}
}

func regularOpen(root, path string) (*os.File, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := range parts {
		info, e := r.Lstat(filepath.Join(parts[:i+1]...))
		if e != nil {
			return nil, e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symbolic link skipped")
		}
		if i == len(parts)-1 && !info.Mode().IsRegular() {
			return nil, errors.New("not a regular file")
		}
	}
	f, err := rootOpenNoFollow(r, path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.New("not a regular file")
	}
	return f, nil
}
func fileInode(info os.FileInfo) uint64 {
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		f := v.FieldByName("Ino")
		if f.IsValid() && f.CanUint() {
			return f.Uint()
		}
	}
	return 0
}
func (o *outputObserver) readFile(root, path string, b *observeBudget) (observedFile, error) {
	f, err := regularOpen(root, path)
	if err != nil {
		return observedFile{}, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return observedFile{}, err
	}
	key := fmt.Sprintf("%s/%s:%d:%d:%d", root, path, info.Size(), info.ModTime().UnixNano(), fileInode(info))
	if cached, ok := o.cache[filepath.Join(root, path)]; ok && cached.cacheKey == key && b.now().Sub(info.ModTime()) > maxObserveTime && cached.reason == "" {
		return cached, nil
	}
	limit := info.Size()
	if limit > b.hashLimit {
		limit = min(limit, int64(maxVersionBytes))
	}
	data, err := b.readBytes(f, limit)
	if err != nil {
		return observedFile{}, err
	}
	v := FileVersion{Size: info.Size(), MtimeNS: info.ModTime().UnixNano(), inode: fileInode(info), IdentityComplete: info.Size() <= b.hashLimit && int64(len(data)) == info.Size(), Binary: bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)}
	if v.IdentityComplete {
		hash := sha256.Sum256(data)
		v.SHA256 = fmt.Sprintf("%x", hash)
	}
	data = data[:min(len(data), maxVersionBytes)]
	v.Blob, err = o.store.put(data, b)
	v.BlobComplete = err == nil && int64(len(data)) == v.Size
	result := observedFile{version: v, data: data, cacheKey: key}
	if err != nil {
		result.reason = err.Error()
	}
	o.cache[filepath.Join(root, path)] = result
	return result, nil
}
func (o *outputObserver) observe(b *observeBudget) observation {
	result := observation{files: map[string]observedFile{}, status: "observed"}
	if o.sessionDir == "" {
		result.status = "unavailable"
		result.reason = "session directory unavailable"
		return result
	}
	root := filepath.Join(o.sessionDir, "output")
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return result
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		result.status = "unavailable"
		result.reason = "output root unavailable"
		return result
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		result.status = "unavailable"
		result.reason = err.Error()
		return result
	}
	defer func() { _ = r.Close() }()
	if err := o.walkObservation(r, root, ".", 0, b, &result); err != nil {
		result.reason = err.Error()
		result.status = observationStatus(err.Error())
	}
	return result
}
func (o *outputObserver) walkObservation(r *os.Root, root, dir string, depth int, b *observeBudget, result *observation) error {
	if err := b.check(); err != nil {
		return err
	}
	f, err := rootOpenNoFollow(r, dir)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	// Read at most the remaining entry budget plus one, even for huge directories.
	entries, err := f.ReadDir(max(1, b.entryLimit-b.entries+1))
	if err != nil && err != io.EOF {
		return err
	}
	if len(entries) > b.entryLimit-b.entries {
		b.entries = b.entryLimit
		return errors.New("entry budget exhausted")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := b.check(); err != nil {
			return err
		}
		b.entries++
		if b.entries > b.entryLimit {
			return errors.New("entry budget exhausted")
		}
		path := entry.Name()
		if dir != "." {
			path = filepath.Join(dir, path)
		}
		if RunnerOwnedOutputName(filepath.ToSlash(path)) {
			continue
		}
		if err := o.visitOutputPath(r, root, path, depth, b, result); err != nil {
			return err
		}
	}
	return nil
}
func (o *outputObserver) visitOutputPath(r *os.Root, root, path string, depth int, b *observeBudget, result *observation) error {
	info, err := r.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if info.IsDir() {
		if depth >= maxObserveDepth {
			return errors.New("directory depth budget exhausted")
		}
		if err := o.walkObservation(r, root, path, depth+1, b, result); err != nil {
			return err
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if len(result.files) >= b.fileLimit {
		return errors.New("file enumeration budget exhausted")
	}
	version, err := o.readFile(root, path, b)
	if err != nil {
		return err
	}
	result.files[filepath.ToSlash(path)] = version
	if version.reason != "" {
		return errors.New(version.reason)
	}
	return nil
}
func observationStatus(reason string) string {
	if strings.Contains(reason, "budget exhausted") {
		return "truncated"
	}
	return "unavailable"
}
func sameVersion(a, b FileVersion) bool {
	if a.IdentityComplete && b.IdentityComplete {
		return a.SHA256 == b.SHA256
	}
	return a.Size == b.Size && a.MtimeNS == b.MtimeNS && a.inode == b.inode
}

// Contribution determines authorship before retention. The reader is charged to
// the same step budget by the caller; unchanged diff lines remain context.
func Contribution(before, after *FileVersion, beforeBytes []byte, afterReader io.Reader) (retained Retained, content []byte) {
	if after == nil {
		return Retained{}, nil
	}
	if after.Binary || (before != nil && before.Binary) {
		return Retained{Kind: "unavailable", Reason: "binary content"}, nil
	}
	unresolved := func(reason string, data []byte) (Retained, []byte) {
		return Retained{Kind: "unresolved", Reason: reason, Truncated: int64(len(data)) < after.Size || len(data) > maxContributionBytes}, textPrefix(data, maxContributionBytes)
	}
	// The reader is bounded to the full hash limit plus an excerpt by callers.
	data, err := io.ReadAll(afterReader)
	if err != nil {
		return unresolved(err.Error(), nil)
	}
	if before == nil {
		return Retained{Kind: "created", Truncated: int64(len(data)) < after.Size || len(data) > maxContributionBytes}, textPrefix(data, maxContributionBytes)
	}
	if !before.IdentityComplete {
		return unresolved("before identity incomplete", data)
	}
	if after.Size >= before.Size && int64(len(data)) >= before.Size {
		hash := sha256.Sum256(data[:before.Size])
		if fmt.Sprintf("%x", hash) == before.SHA256 {
			delta := data[before.Size:]
			return Retained{Kind: "append", Truncated: int64(len(data)) < after.Size || len(delta) > maxContributionBytes}, textPrefix(delta, maxContributionBytes)
		}
	}
	if before.BlobComplete && after.BlobComplete && before.Size <= maxVersionBytes && after.Size <= maxVersionBytes && int64(len(beforeBytes)) == before.Size && int64(len(data)) == after.Size {
		patch := diff.Diff("before", beforeBytes, "after", data)
		return Retained{Kind: "diff", Truncated: len(patch) > maxContributionBytes}, textPrefix(patch, maxContributionBytes)
	}
	return unresolved("versions not completely retained within diff bound", data)
}
func (o *outputObserver) contribution(before, after *observedFile, root, path string, b *observeBudget) *Retained {
	if after.reason != "" {
		return &Retained{Kind: "unavailable", Reason: after.reason}
	}
	var bv *FileVersion
	var data []byte
	if before != nil {
		bv = &before.version
		data = before.data
	}
	f, err := regularOpen(root, path)
	if err != nil {
		return &Retained{Kind: "unavailable", Reason: err.Error()}
	}
	defer func() { _ = f.Close() }()
	// Enough for an append prefix and its bounded delta, or a complete diff.
	limit := int64(maxVersionBytes)
	if bv != nil && bv.IdentityComplete {
		limit = max(limit, bv.Size+maxContributionBytes)
	}
	limit = min(limit, after.version.Size)
	current, err := b.readBytes(f, limit)
	if err != nil {
		return &Retained{Kind: "unavailable", Reason: err.Error()}
	}
	retained, content := Contribution(bv, &after.version, data, bytes.NewReader(current))
	if before != nil && before.reason != "" && retained.Kind == "diff" {
		retained = Retained{Kind: "unresolved", Reason: before.reason}
		content = textPrefix(current, maxContributionBytes)
	}
	if retained.Kind == "unavailable" {
		return &retained
	}
	blob, err := o.store.put(content, b)
	if err != nil {
		return &Retained{Kind: "unavailable", Reason: err.Error()}
	}
	retained.Bytes = int64(len(content))
	if retained.Kind == "unresolved" {
		retained.Context = blob
	} else {
		retained.Blob = blob
	}
	return &retained
}
func (o *outputObserver) delta(before, after observation, root string, b *observeBudget, segment int) WriteRecord {
	result := WriteRecord{Status: "observed", Files: []FileWrite{}}
	for _, obs := range []observation{before, after} {
		if obs.status != "observed" {
			mergeRecordStatus(&result, obs.status, obs.reason)
		}
	}
	paths := map[string]bool{}
	for path := range before.files {
		paths[path] = true
	}
	for path := range after.files {
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	for _, path := range ordered {
		a, aok := before.files[path]
		z, zok := after.files[path]
		if aok && zok && sameVersion(a.version, z.version) {
			continue
		}
		if !zok && after.status != "observed" {
			continue
		}
		w := FileWrite{Path: path, Segment: segment}
		if aok {
			v := a.version
			w.Before = &v
		}
		if zok {
			v := z.version
			w.After = &v
		}
		switch {
		case !zok:
			w.Change = "deleted"
		case !aok:
			w.Change = "created"
		default:
			w.Change = "modified"
		}
		if zok {
			var old *observedFile
			if aok {
				old = &a
			}
			w.Contribution = o.contribution(old, &z, root, path, b)
			o.classifyContribution(&w, aok, before.status == "observed", &result)
		}
		result.Files = append(result.Files, w)
	}
	return result
}
func (o *outputObserver) classifyContribution(w *FileWrite, baselineKnown, baselineComplete bool, result *WriteRecord) {
	if !baselineKnown && !baselineComplete && w.Contribution.Kind != "unavailable" {
		w.Contribution.Kind = "unresolved"
		w.Contribution.Reason = "baseline observation incomplete"
		w.Contribution.Context = w.Contribution.Blob
		w.Contribution.Blob = ""
	}
	if w.Contribution.Kind == "unavailable" {
		mergeRecordStatus(result, observationStatus(w.Contribution.Reason), w.Contribution.Reason)
	}
}
func mergeRecordStatus(r *WriteRecord, status, reason string) {
	if r.Status != "unavailable" || status == "unavailable" {
		r.Status = status
	}
	if reason != "" && !strings.Contains(r.Reason, reason) {
		if r.Reason != "" {
			r.Reason += "; "
		}
		r.Reason += reason
	}
}
func (o *outputObserver) start(prefix string, cp *GitCheckpoint) {
	if cp.Available && !o.repoResolved {
		if top, err := gitOutput(o.repoRoot, "rev-parse", "--show-toplevel"); err == nil {
			o.repoRoot = strings.TrimSuffix(top, "\n")
			o.repoResolved = true
		}
	}
	b := newObserveBudget()
	b.boundary()
	obs := o.observe(b)
	if n := len(o.windows); n > 0 {
		p := o.windows[n-1]
		p.budget.boundary()
		segment := o.delta(p.baseline, obs, filepath.Join(o.sessionDir, "output"), p.budget, p.segment)
		p.record.Files = append(p.record.Files, segment.Files...)
		if segment.Status != "observed" {
			mergeRecordStatus(&p.record, segment.Status, segment.Reason)
		}
	}
	w := &outputWindow{prefix: prefix, baseline: obs, budget: b, record: WriteRecord{Status: "observed", Files: []FileWrite{}}, repo: map[string]observedFile{}}
	if cp.Available {
		for _, path := range ChangedDirtyPaths(nil, cp) {
			b.entries++
			if b.entries > b.entryLimit {
				mergeRecordStatus(&w.record, "truncated", "entry budget exhausted")
				break
			}
			f, err := o.readFile(o.repoRoot, path, b)
			if err == nil {
				w.repo[path] = f
			} else if !os.IsNotExist(err) {
				w.repo[path] = observedFile{reason: err.Error()}
			}
		}
	}
	o.windows = append(o.windows, w)
}
func (o *outputObserver) end(prefix string, start, end *GitCheckpoint) (output WriteRecord, repository *WriteRecord) {
	n := len(o.windows)
	if n == 0 || o.windows[n-1].prefix != prefix {
		return WriteRecord{Status: "unavailable", Reason: "observation window unavailable", Files: []FileWrite{}}, nil
	}
	w := o.windows[n-1]
	w.budget.boundary()
	obs := o.observe(w.budget)
	segment := o.delta(w.baseline, obs, filepath.Join(o.sessionDir, "output"), w.budget, w.segment)
	w.record.Files = append(w.record.Files, segment.Files...)
	if segment.Status != "observed" {
		mergeRecordStatus(&w.record, segment.Status, segment.Reason)
	}
	repo := o.repositoryPatch(w, start, end)
	o.windows = o.windows[:n-1]
	if n > 1 {
		p := o.windows[n-2]
		p.baseline = obs
		p.segment++
	}
	return w.record, repo
}
func (o *outputObserver) repositoryPatch(w *outputWindow, start, end *GitCheckpoint) *WriteRecord {
	if !start.Available || !end.Available {
		reason := start.Reason
		if reason == "" {
			reason = end.Reason
		}
		return &WriteRecord{Status: "unavailable", Reason: reason, Files: []FileWrite{}}
	}
	paths := ChangedDirtyPaths(start, end)
	if len(paths) == 0 {
		return nil
	}
	before := observation{files: map[string]observedFile{}, status: "observed"}
	after := observation{files: map[string]observedFile{}, status: "observed"}
	dirty := flattenCheckpoint(start)
	for _, path := range paths {
		if f, ok := w.repo[path]; ok {
			before.files[path] = f
		} else if _, wasDirty := dirty[path]; !wasDirty {
			f, err := o.gitVersion(start.HEAD, path, w.budget)
			if err == nil {
				before.files[path] = f
			} else if !errors.Is(err, os.ErrNotExist) {
				before.files[path] = observedFile{reason: err.Error()}
				before.status = observationStatus(err.Error())
				before.reason = err.Error()
			}
		}
		f, err := o.readFile(o.repoRoot, path, w.budget)
		if err == nil {
			after.files[path] = f
		} else if !os.IsNotExist(err) {
			after.status = observationStatus(err.Error())
			after.reason = err.Error()
		}
	}
	r := o.delta(before, after, o.repoRoot, w.budget, 0)
	return &r
}
func (o *outputObserver) gitVersion(head, path string, b *observeBudget) (observedFile, error) {
	object := head + ":" + path
	cmd := exec.Command("git", "-C", o.repoRoot, "cat-file", "-s", object)
	sizeText, err := cmd.Output()
	if err != nil {
		return observedFile{}, os.ErrNotExist
	}
	var size int64
	if _, err = fmt.Sscan(string(sizeText), &size); err != nil {
		return observedFile{}, err
	}
	cmd = exec.Command("git", "-C", o.repoRoot, "cat-file", "blob", object)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return observedFile{}, err
	}
	if err := cmd.Start(); err != nil {
		return observedFile{}, err
	}
	limit := size
	if size > b.hashLimit {
		limit = min(size, int64(maxVersionBytes))
	}
	data, readErr := b.readBytes(pipe, limit)
	if limit < size || readErr != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return observedFile{}, readErr
	}
	if limit == size && waitErr != nil {
		return observedFile{}, waitErr
	}
	v := FileVersion{Size: size, IdentityComplete: limit == size, Binary: bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data)}
	if v.IdentityComplete {
		hash := sha256.Sum256(data)
		v.SHA256 = fmt.Sprintf("%x", hash)
	}
	data = data[:min(len(data), maxVersionBytes)]
	v.Blob, err = o.store.put(data, b)
	v.BlobComplete = err == nil && int64(len(data)) == size
	f := observedFile{version: v, data: data}
	if err != nil {
		f.reason = err.Error()
	}
	return f, nil
}

// textPrefix never splits a UTF-8 encoding at the retention boundary.
func textPrefix(data []byte, limit int) []byte {
	n := min(len(data), limit)
	for n > 0 && n < len(data) && !utf8.RuneStart(data[n]) {
		n--
	}
	return data[:n]
}

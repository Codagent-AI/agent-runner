package cli

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

const crossThreadBaselineFreshness = 10 * time.Minute
const maxCodexLogLineBytes = 1 << 20

const codexLogRecordHeaderBytes = 512

var safeCodexThreadID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
var codexTokenCountHeader = regexp.MustCompile(`"payload"\s*:\s*\{\s*"type"\s*:\s*"token_count"`)

type CodexRateLimitReader struct{ Home string }

func NewCodexRateLimitReader() CodexRateLimitReader {
	return CodexRateLimitReader{Home: codexSourceHome()}
}

func codexSourceHome() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

func (r CodexRateLimitReader) home() string {
	if r.Home != "" {
		return r.Home
	}
	return codexSourceHome()
}

// timedSnapshot keeps the parsed observation time beside the persisted snapshot
// so scans compare times without reparsing ObservedAt.
type timedSnapshot struct {
	at       time.Time
	snapshot *model.RateLimitSnapshot
}

func (t timedSnapshot) before(other timedSnapshot) bool {
	return t.snapshot == nil || t.at.Before(other.at)
}

type codexLogScan struct {
	account     string
	before      timedSnapshot
	end         timedSnapshot
	unparseable bool
}

type cachedCodexLog struct {
	mu          sync.Mutex
	file        os.FileInfo
	offset      int64
	account     string
	snapshots   []timedSnapshot
	unparseable bool
}

var codexLogCache = struct {
	sync.Mutex
	entries map[string]*cachedCodexLog
	order   []string
}{entries: make(map[string]*cachedCodexLog)}

func cachedCodexLogFor(path string) *cachedCodexLog {
	codexLogCache.Lock()
	defer codexLogCache.Unlock()
	if entry := codexLogCache.entries[path]; entry != nil {
		return entry
	}
	if len(codexLogCache.order) >= 128 {
		delete(codexLogCache.entries, codexLogCache.order[0])
		codexLogCache.order = codexLogCache.order[1:]
	}
	entry := &cachedCodexLog{}
	codexLogCache.entries[path] = entry
	codexLogCache.order = append(codexLogCache.order, path)
	return entry
}

//nolint:gocritic // Keep the reader's public request value API consistent with invocation callers.
func (r CodexRateLimitReader) Read(req model.CodexRateLimitRequest) model.CodexRateLimitEvidence {
	e := model.CodexRateLimitEvidence{
		Status: model.RateLimitStatusUnavailable, Source: model.RateLimitSourceCodexSessionLog, AccountScope: model.RateLimitAccountUnverified,
		AttemptStartedAt: req.StartedAt.Format(time.RFC3339Nano), AttemptEndedAt: req.EndedAt.Format(time.RFC3339Nano),
		Deltas: model.UnavailableRateLimitDeltas(model.RateLimitReasonNoBaseline),
	}
	if req.ThreadID == "" || !safeCodexThreadID.MatchString(req.ThreadID) {
		e.Reason = model.RateLimitReasonSessionUnidentified
		return e
	}
	home := r.home()
	if home == "" {
		// An unresolved home must not glob sessions relative to the working directory.
		e.Reason = model.RateLimitReasonLogUnavailable
		return e
	}
	paths, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+req.ThreadID+".jsonl"))
	var scan codexLogScan
	readable := false
	for _, path := range paths {
		if err := scanCodexLog(path, req.StartedAt, req.EndedAt.Add(req.EndTolerance), nil, &scan); err == nil {
			readable = true
		}
	}
	if !readable {
		e.Reason = model.RateLimitReasonLogUnavailable
		return e
	}
	if scan.account != "" {
		digest := sha256.Sum256([]byte(req.RunID + "\x00" + scan.account))
		e.AccountScope = "acct-" + hex.EncodeToString(digest[:8])
	}
	if scan.end.snapshot == nil {
		e.Reason = model.RateLimitReasonNoSnapshots
		if scan.unparseable {
			e.Reason = model.RateLimitReasonUnparseable
		}
		return e
	}
	e.Status = model.RateLimitStatusCaptured
	e.End = scan.end.snapshot
	baseline := scan.before
	if baseline.snapshot != nil {
		e.StartProvenance = model.RateLimitProvenanceSameThread
	} else if scan.account != "" {
		baseline = crossThreadBaseline(home, req, scan.account, scan.end.snapshot)
		if baseline.snapshot != nil {
			e.StartProvenance = model.RateLimitProvenanceCrossThread
		}
	}
	e.Start = baseline.snapshot
	gap := req.StartedAt.Sub(baseline.at)
	// A same-thread baseline only carries an unobserved gap once it is stale.
	gapped := e.StartProvenance == model.RateLimitProvenanceCrossThread ||
		e.StartProvenance == model.RateLimitProvenanceSameThread && gap > crossThreadBaselineFreshness
	if gapped {
		milliseconds := gap.Milliseconds()
		e.BaselineGapMS = &milliseconds
	}
	e.Deltas = computeCodexRateLimitDeltas(e.Start, e.End, e.StartProvenance)
	if gapped {
		for i := range e.Deltas {
			if e.Deltas[i].Availability == model.RateLimitAvailable {
				e.Deltas[i].Limitations = append(e.Deltas[i].Limitations, model.RateLimitLimitationUnobservedGap)
			}
		}
	}
	return e
}

// crossThreadBaseline returns the latest fresh pre-launch snapshot recorded by
// another session of the same account, limit, and plan.
//
//nolint:gocritic // Request is immutable across candidate scans.
func crossThreadBaseline(home string, req model.CodexRateLimitRequest, account string, end *model.RateLimitSnapshot) timedSnapshot {
	var latest timedSnapshot
	first := req.StartedAt.Add(-crossThreadBaselineFreshness)
	localFirst := first.In(time.Local)
	lastDay := req.StartedAt.In(time.Local)
	for day := time.Date(localFirst.Year(), localFirst.Month(), localFirst.Day(), 0, 0, 0, 0, time.Local); !day.After(lastDay); day = day.AddDate(0, 0, 1) {
		paths, _ := filepath.Glob(filepath.Join(home, "sessions", day.Format("2006/01/02"), "rollout-*.jsonl"))
		for _, path := range paths {
			if strings.HasSuffix(path, "-"+req.ThreadID+".jsonl") {
				continue
			}
			if info, err := os.Stat(path); err != nil || info.ModTime().Before(first) {
				continue
			}
			var scan codexLogScan
			if err := scanCodexLog(path, first, req.StartedAt.Add(-time.Nanosecond), end.SameLimit, &scan); err != nil {
				continue
			}
			candidate := scan.end
			if scan.account != account || candidate.snapshot == nil {
				continue
			}
			if latest.before(candidate) {
				latest = candidate
			}
		}
	}
	return latest
}

// scanCodexLog folds the file's snapshots into out. A non-nil accept restricts
// which snapshots may be chosen as out.before or out.end.
func scanCodexLog(path string, start, end time.Time, accept func(*model.RateLimitSnapshot) bool, out *codexLogScan) error {
	entry := cachedCodexLogFor(path)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := entry.refresh(path); err != nil {
		return err
	}
	if entry.account != "" {
		out.account = entry.account
	}
	out.unparseable = out.unparseable || entry.unparseable
	for _, snapshot := range entry.snapshots {
		if accept != nil && !accept(snapshot.snapshot) {
			continue
		}
		switch {
		case snapshot.at.Before(start):
			if out.before.before(snapshot) {
				out.before = snapshot
			}
		case !snapshot.at.After(end):
			if out.end.before(snapshot) {
				out.end = snapshot
			}
		}
	}
	return nil
}

// refresh reads only appended JSONL records. Parsed snapshots are kept in a
// bounded file cache so resumed attempts do not rescan large rollout logs.
func (entry *cachedCodexLog) refresh(path string) error {
	f, err := os.Open(path) // #nosec G304 -- path is located under the configured Codex sessions directory
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if entry.file == nil || !os.SameFile(entry.file, info) || info.Size() < entry.offset {
		entry.offset, entry.account, entry.snapshots, entry.unparseable = 0, "", nil, false
	}
	entry.file = info
	if info.Size() == entry.offset {
		return nil
	}
	if _, err := f.Seek(entry.offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(f)
	var buffer []byte
	for {
		line, consumed, oversized, readErr := readCodexLogLine(reader, buffer[:0])
		buffer = line
		if readErr != nil {
			if readErr == io.EOF {
				return nil // Retry an unterminated record from its start on the next refresh.
			}
			return readErr
		}
		entry.offset += consumed
		account, snapshot, unparseable := parseCodexLogLine(line, oversized)
		if account != "" {
			entry.account = account
		}
		if snapshot.snapshot != nil {
			entry.snapshots = append(entry.snapshots, snapshot)
		}
		entry.unparseable = entry.unparseable || unparseable
	}
}

func parseCodexLogLine(line []byte, oversized bool) (account string, snapshot timedSnapshot, unparseable bool) {
	if oversized {
		// Only a record whose envelope declares a token_count payload is lost
		// metric evidence; oversized tool output merely mentioning it is not.
		head := line[:min(len(line), codexLogRecordHeaderBytes)]
		return "", timedSnapshot{}, codexTokenCountHeader.Match(head)
	}
	metric := bytes.Contains(line, []byte("token_count"))
	if !metric && !bytes.Contains(line, []byte("session_meta")) {
		return "", timedSnapshot{}, false
	}
	var event struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   struct {
			Type       string          `json:"type"`
			Account    string          `json:"creator_account_id"`
			RateLimits json.RawMessage `json:"rate_limits"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &event) != nil {
		return "", timedSnapshot{}, true
	}
	if event.Type == "session_meta" {
		return event.Payload.Account, timedSnapshot{}, false
	}
	if event.Type != "event_msg" || event.Payload.Type != "token_count" || len(event.Payload.RateLimits) == 0 || string(event.Payload.RateLimits) == "null" {
		return "", timedSnapshot{}, false
	}
	var raw struct {
		LimitID   *string                `json:"limit_id"`
		PlanType  *string                `json:"plan_type"`
		Primary   *model.RateLimitWindow `json:"primary"`
		Secondary *model.RateLimitWindow `json:"secondary"`
	}
	at, timeErr := time.Parse(time.RFC3339Nano, event.Timestamp)
	if json.Unmarshal(event.Payload.RateLimits, &raw) != nil || timeErr != nil {
		return "", timedSnapshot{}, true
	}
	if raw.Primary == nil && raw.Secondary == nil {
		return "", timedSnapshot{}, false // No window carries rate-limit state.
	}
	parsed := &model.RateLimitSnapshot{
		ObservedAt: at.Format(time.RFC3339Nano), LimitID: raw.LimitID, PlanType: raw.PlanType,
		Primary: reportedCodexWindow(raw.Primary), Secondary: reportedCodexWindow(raw.Secondary),
	}
	return "", timedSnapshot{at: at, snapshot: parsed}, false
}

// reportedCodexWindow evaluates one window independently: an absent window is
// not reported, and a present one keeps exactly the values Codex supplied.
func reportedCodexWindow(w *model.RateLimitWindow) model.RateLimitWindow {
	if w == nil {
		return model.RateLimitWindow{}
	}
	out := *w
	out.Reported = true
	return out
}

// readCodexLogLine consumes exactly one line while retaining at most one MiB in
// line, which reuses the caller's buffer. consumed counts every byte read so
// callers can track the file offset without seeking.
func readCodexLogLine(reader *bufio.Reader, line []byte) (kept []byte, consumed int64, oversized bool, readErr error) {
	for {
		fragment, err := reader.ReadSlice('\n')
		consumed += int64(len(fragment))
		if remaining := maxCodexLogLineBytes - len(line); len(fragment) > remaining {
			line = append(line, fragment[:max(remaining, 0)]...)
			oversized = true
		} else {
			line = append(line, fragment...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return line, consumed, oversized, err
	}
}

func computeCodexRateLimitDeltas(start, end *model.RateLimitSnapshot, provenance string) []model.RateLimitDelta {
	result := make([]model.RateLimitDelta, 0, len(model.RateLimitWindowRoles))
	for _, role := range model.RateLimitWindowRoles {
		result = append(result, codexRateLimitDelta(start, end, provenance, role))
	}
	return result
}

func codexRateLimitDelta(start, end *model.RateLimitSnapshot, provenance, role string) model.RateLimitDelta {
	if end == nil {
		return model.UnavailableRateLimitDelta(role, model.RateLimitReasonNoBaseline)
	}
	endWindow := end.Window(role)
	if !endWindow.Reported {
		return model.UnavailableRateLimitDelta(role, model.RateLimitReasonWindowNotReported)
	}
	if start == nil {
		return model.UnavailableRateLimitDelta(role, model.RateLimitReasonNoBaseline)
	}
	startWindow := start.Window(role)
	switch {
	case !startWindow.Reported || !comparableWindow(&startWindow) || !comparableWindow(&endWindow):
		return model.UnavailableRateLimitDelta(role, model.RateLimitReasonWindowNotReported)
	case !start.SameWindow(end, role) && provenance == model.RateLimitProvenanceCrossThread:
		// A cross-thread snapshot from another window is no baseline at all.
		return model.UnavailableRateLimitDelta(role, model.RateLimitReasonNoBaseline)
	case !start.SameWindow(end, role):
		return model.UnavailableRateLimitDelta(role, model.RateLimitReasonWindowReset)
	case *endWindow.UsedPercent < *startWindow.UsedPercent:
		return model.UnavailableRateLimitDelta(role, model.RateLimitReasonInconsistent)
	}
	// Read adds unobserved-gap for every gapped baseline, cross-thread or stale.
	return model.AvailableRateLimitDelta(role, *endWindow.UsedPercent-*startWindow.UsedPercent)
}

// comparableWindow reports whether a window carries every value needed to show
// it is the same quota window and to subtract usage.
func comparableWindow(w *model.RateLimitWindow) bool {
	return w.UsedPercent != nil && w.WindowMinutes != nil && w.ResetsAt != nil
}

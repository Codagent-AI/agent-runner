package cli

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

const crossThreadBaselineFreshness = 10 * time.Minute

var safeCodexThreadID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type CodexRateLimitRequest struct {
	ThreadID, RunID    string
	StartedAt, EndedAt time.Time
	EndTolerance       time.Duration
}

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

type codexLogScan struct {
	account string
	before  *model.RateLimitSnapshot
	end     *model.RateLimitSnapshot
	bad     bool
}

//nolint:gocritic // Keep the reader's public request value API consistent with invocation callers.
func (r CodexRateLimitReader) Read(req CodexRateLimitRequest) model.CodexRateLimitEvidence {
	e := model.CodexRateLimitEvidence{
		Status: "unavailable", Source: "codex:session-log", AccountScope: "unverified",
		AttemptStartedAt: req.StartedAt.Format(time.RFC3339Nano), AttemptEndedAt: req.EndedAt.Format(time.RFC3339Nano),
		Deltas: []model.RateLimitDelta{{Window: "primary", Availability: "unavailable", Reason: "no-baseline"}, {Window: "secondary", Availability: "unavailable", Reason: "no-baseline"}},
	}
	if req.ThreadID == "" || !safeCodexThreadID.MatchString(req.ThreadID) {
		e.Reason = "session-unidentified"
		return e
	}
	home := r.Home
	if home == "" {
		home = codexSourceHome()
	}
	paths, _ := filepath.Glob(filepath.Join(home, "sessions", "*", "*", "*", "rollout-*-"+req.ThreadID+".jsonl"))
	if len(paths) == 0 {
		e.Reason = "session-log-unavailable"
		return e
	}
	var scan codexLogScan
	readable := false
	for _, path := range paths {
		if err := scanCodexLog(path, req.StartedAt, req.EndedAt.Add(req.EndTolerance), &scan); err == nil {
			readable = true
		}
	}
	if !readable {
		e.Reason = "session-log-unavailable"
		return e
	}
	if scan.account != "" {
		digest := sha256.Sum256([]byte(req.RunID + "\x00" + scan.account))
		e.AccountScope = "acct-" + hex.EncodeToString(digest[:8])
	}
	if scan.end == nil {
		e.Reason = "no-snapshots"
		if scan.bad {
			e.Reason = "unparseable"
		}
		return e
	}
	e.Status = "captured"
	e.End = scan.end
	if scan.before != nil {
		e.Start, e.StartProvenance = scan.before, "same-thread"
	} else if scan.account != "" {
		if candidate := r.crossThreadBaseline(req, scan.account, scan.end); candidate != nil {
			e.Start, e.StartProvenance = candidate, "cross-thread"
			at, _ := time.Parse(time.RFC3339Nano, candidate.ObservedAt)
			gap := req.StartedAt.Sub(at).Milliseconds()
			e.BaselineGapMS = &gap
		}
	}
	e.Deltas = ComputeCodexRateLimitDeltas(e.Start, e.End, e.StartProvenance)
	return e
}

//nolint:gocritic // Request is immutable across candidate scans.
func (r CodexRateLimitReader) crossThreadBaseline(req CodexRateLimitRequest, account string, end *model.RateLimitSnapshot) *model.RateLimitSnapshot {
	home := r.Home
	if home == "" {
		home = codexSourceHome()
	}
	var latest *model.RateLimitSnapshot
	first := req.StartedAt.Add(-crossThreadBaselineFreshness)
	for day := time.Date(first.In(time.Local).Year(), first.In(time.Local).Month(), first.In(time.Local).Day(), 0, 0, 0, 0, time.Local); !day.After(req.StartedAt.In(time.Local)); day = day.AddDate(0, 0, 1) {
		paths, _ := filepath.Glob(filepath.Join(home, "sessions", day.Format("2006/01/02"), "rollout-*.jsonl"))
		for _, path := range paths {
			if strings.HasSuffix(path, "-"+req.ThreadID+".jsonl") {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || info.ModTime().Before(first) {
				continue
			}
			var scan codexLogScan
			if scanCodexLog(path, first, req.StartedAt.Add(-time.Nanosecond), &scan) != nil || scan.account != account || scan.end == nil || !sameString(scan.end.LimitID, end.LimitID) || !sameString(scan.end.PlanType, end.PlanType) {
				continue
			}
			if latest == nil || snapshotBefore(latest, scan.end) {
				latest = scan.end
			}
		}
	}
	return latest
}

//nolint:gocognit // Streaming parsing and error isolation share one scan state.
func scanCodexLog(path string, start, end time.Time, out *codexLogScan) error {
	f, err := os.Open(path) // #nosec G304 -- path is located under the configured Codex sessions directory
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 && (strings.Contains(string(line), "token_count") || strings.Contains(string(line), "session_meta")) {
			var event struct {
				Timestamp string `json:"timestamp"`
				Type      string `json:"type"`
				Payload   struct {
					Type       string          `json:"type"`
					Account    string          `json:"creator_account_id"`
					RateLimits json.RawMessage `json:"rate_limits"`
				} `json:"payload"`
			}
			switch {
			case json.Unmarshal(line, &event) != nil:
				out.bad = true
			case event.Type == "session_meta":
				out.account = event.Payload.Account
			case event.Type == "event_msg" && event.Payload.Type == "token_count" && len(event.Payload.RateLimits) > 0 && string(event.Payload.RateLimits) != "null":
				var raw struct {
					LimitID   *string                `json:"limit_id"`
					PlanType  *string                `json:"plan_type"`
					Primary   *model.RateLimitWindow `json:"primary"`
					Secondary *model.RateLimitWindow `json:"secondary"`
				}
				at, timeErr := time.Parse(time.RFC3339Nano, event.Timestamp)
				if json.Unmarshal(event.Payload.RateLimits, &raw) != nil || timeErr != nil || raw.Primary == nil || raw.Primary.UsedPercent == nil || raw.Primary.WindowMinutes == nil || raw.Primary.ResetsAt == nil {
					out.bad = true
				} else {
					snapshot := &model.RateLimitSnapshot{ObservedAt: at.Format(time.RFC3339Nano), LimitID: raw.LimitID, PlanType: raw.PlanType}
					snapshot.Primary = *raw.Primary
					snapshot.Primary.Reported = true
					if raw.Secondary != nil && raw.Secondary.UsedPercent != nil && raw.Secondary.WindowMinutes != nil && raw.Secondary.ResetsAt != nil {
						snapshot.Secondary = *raw.Secondary
						snapshot.Secondary.Reported = true
					}
					if at.Before(start) {
						if out.before == nil || snapshotBefore(out.before, snapshot) {
							out.before = snapshot
						}
					} else if !at.After(end) && (out.end == nil || snapshotBefore(out.end, snapshot)) {
						out.end = snapshot
					}
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}

func ComputeCodexRateLimitDeltas(start, end *model.RateLimitSnapshot, provenance string) []model.RateLimitDelta {
	result := make([]model.RateLimitDelta, 0, 2)
	for _, role := range []string{"primary", "secondary"} {
		d := model.RateLimitDelta{Window: role, Availability: "unavailable"}
		if end == nil {
			d.Reason = "no-baseline"
			result = append(result, d)
			continue
		}
		ew := end.Primary
		if role == "secondary" {
			ew = end.Secondary
		}
		var sw model.RateLimitWindow
		if start != nil {
			sw = start.Primary
			if role == "secondary" {
				sw = start.Secondary
			}
		}
		switch {
		case !ew.Reported:
			d.Reason = "window-not-reported"
		case start == nil:
			d.Reason = "no-baseline"
		case !sw.Reported:
			d.Reason = "window-not-reported"
		case !sameString(start.LimitID, end.LimitID) || !sameInt(sw.WindowMinutes, ew.WindowMinutes) || !sameInt(sw.ResetsAt, ew.ResetsAt):
			d.Reason = "window-reset"
			if provenance == "cross-thread" {
				d.Reason = "no-baseline"
			}
		case ew.UsedPercent == nil || sw.UsedPercent == nil:
			d.Reason = "window-not-reported"
		case *ew.UsedPercent < *sw.UsedPercent:
			d.Reason = "inconsistent"
		default:
			value := *ew.UsedPercent - *sw.UsedPercent
			d.Availability, d.PercentagePoints, d.Precision = "available", &value, "approximate"
			d.Limitations = []string{"account-wide", "coarse-precision"}
			if provenance == "cross-thread" {
				d.Limitations = append(d.Limitations, "unobserved-gap")
			}
		}
		result = append(result, d)
	}
	return result
}

func sameString(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func sameInt(a, b *int64) bool     { return a == nil && b == nil || a != nil && b != nil && *a == *b }

func snapshotBefore(a, b *model.RateLimitSnapshot) bool {
	aTime, aErr := time.Parse(time.RFC3339Nano, a.ObservedAt)
	bTime, bErr := time.Parse(time.RFC3339Nano, b.ObservedAt)
	return aErr == nil && bErr == nil && aTime.Before(bTime)
}

package cli

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codagent/agent-runner/internal/model"
)

func TestCodexRateLimitReaderSameThread(t *testing.T) {
	home := t.TempDir()
	start := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	writeRateLimitTestLog(t, home, "thread-1", "account-1", []string{
		rateLimitTestEvent(start.Add(-time.Second), 40, 12345),
		"not json token_count",
		rateLimitTestEvent(start.Add(time.Second), 42, 12345),
		rateLimitTestEvent(start.Add(2*time.Second), 43, 12345),
	})
	got := (CodexRateLimitReader{Home: home}).Read(model.CodexRateLimitRequest{ThreadID: "thread-1", RunID: "run", StartedAt: start, EndedAt: start.Add(3 * time.Second)})
	if got.Status != "captured" || got.StartProvenance != "same-thread" || got.Start == nil || got.End == nil || *got.Start.Primary.UsedPercent != 40 || *got.End.Primary.UsedPercent != 43 || got.Deltas[0].PercentagePoints == nil || *got.Deltas[0].PercentagePoints != 3 {
		t.Fatalf("unexpected evidence: %+v", got)
	}
	if got.AccountScope == "account-1" || got.AccountScope == "" {
		t.Fatalf("account scope leaked or missing: %q", got.AccountScope)
	}
	other := (CodexRateLimitReader{Home: home}).Read(model.CodexRateLimitRequest{ThreadID: "thread-1", RunID: "another-run", StartedAt: start, EndedAt: start.Add(3 * time.Second)})
	if other.AccountScope == got.AccountScope {
		t.Fatalf("account scope shared across runs: %q", got.AccountScope)
	}
}

func TestCodexRateLimitReaderOldSameThreadBaselineExposesGap(t *testing.T) {
	home := t.TempDir()
	start := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	writeRateLimitTestLog(t, home, "old-thread", "account-1", []string{
		rateLimitTestEvent(start.Add(-15*time.Minute), 40, 12345),
		rateLimitTestEvent(start.Add(time.Second), 43, 12345),
	})
	got := (CodexRateLimitReader{Home: home}).Read(model.CodexRateLimitRequest{ThreadID: "old-thread", RunID: "run", StartedAt: start, EndedAt: start.Add(2 * time.Second)})
	if got.StartProvenance != "same-thread" || got.BaselineGapMS == nil || *got.BaselineGapMS != 15*60*1000 || got.Deltas[0].Availability != "available" || !slices.Contains(got.Deltas[0].Limitations, "unobserved-gap") {
		t.Fatalf("stale same-thread baseline lacks gap limitation: %+v", got)
	}
}

func TestCodexRateLimitReaderOversizedToolOutputMentioningTokenCountIsNotMalformed(t *testing.T) {
	home := t.TempDir()
	start := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	writeRateLimitTestLog(t, home, "tool-output", "account-1", []string{
		`{"timestamp":"` + start.Add(time.Second).Format(time.RFC3339Nano) + `","type":"response_item","payload":{"type":"function_call_output","output":"grep token_count ` + strings.Repeat("x", maxCodexLogLineBytes) + `"}}`,
		`{"timestamp":"` + start.Add(2*time.Second).Format(time.RFC3339Nano) + `","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":500}},"rate_limits":null}}`,
	})
	got := (CodexRateLimitReader{Home: home}).Read(model.CodexRateLimitRequest{ThreadID: "tool-output", RunID: "run", StartedAt: start, EndedAt: start.Add(3 * time.Second)})
	if got.Reason != "no-snapshots" {
		t.Fatalf("oversized tool output treated as a malformed metric record: %+v", got)
	}
}

func TestCodexRateLimitReaderOversizedTokenCountRecordIsMalformed(t *testing.T) {
	home := t.TempDir()
	start := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	writeRateLimitTestLog(t, home, "huge-metric", "account-1", []string{
		`{"timestamp":"` + start.Add(time.Second).Format(time.RFC3339Nano) + `","type":"event_msg","payload":{"type":"token_count","info":"` + strings.Repeat("x", maxCodexLogLineBytes) + `"}}`,
	})
	got := (CodexRateLimitReader{Home: home}).Read(model.CodexRateLimitRequest{ThreadID: "huge-metric", RunID: "run", StartedAt: start, EndedAt: start.Add(3 * time.Second)})
	if got.Reason != "unparseable" {
		t.Fatalf("oversized token_count record silently dropped: %+v", got)
	}
}

func TestCodexRateLimitReaderSkipsOversizedNonMetricLine(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(strings.Repeat("x", maxCodexLogLineBytes+1) + "\n" + "next\n"))
	line, consumed, oversized, err := readCodexLogLine(reader, nil)
	if err != nil || !oversized || len(line) > maxCodexLogLineBytes || consumed != int64(maxCodexLogLineBytes+2) {
		t.Fatalf("unbounded line: len=%d consumed=%d oversized=%v err=%v", len(line), consumed, oversized, err)
	}
	line, consumed, oversized, err = readCodexLogLine(reader, line[:0])
	if err != nil || oversized || string(line) != "next\n" || consumed != 5 {
		t.Fatalf("reader did not advance past large line: %q %d %v %v", line, consumed, oversized, err)
	}
}

func TestCodexRateLimitReaderCachesAppendedSnapshots(t *testing.T) {
	home := t.TempDir()
	start := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	writeRateLimitTestLog(t, home, "append-thread", "account-1", []string{
		`{"type":"tool_response","payload":"` + strings.Repeat("x", maxCodexLogLineBytes+1) + `"}`,
		rateLimitTestEvent(start.Add(time.Second), 40, 12345),
	})
	path := filepath.Join(home, "sessions", "2026", "09", "29", "rollout-2026-09-29T02-00-00-append-thread.jsonl")
	reader := CodexRateLimitReader{Home: home}
	first := reader.Read(model.CodexRateLimitRequest{ThreadID: "append-thread", RunID: "run", StartedAt: start, EndedAt: start.Add(2 * time.Second)})
	if first.Status != "captured" {
		t.Fatalf("large nonmetric line hid evidence: %+v", first)
	}
	entry := cachedCodexLogFor(path)
	entry.mu.Lock()
	firstOffset, firstCount := entry.offset, len(entry.snapshots)
	entry.mu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(rateLimitTestEvent(start.Add(time.Minute+time.Second), 43, 12345) + "\n")
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	second := reader.Read(model.CodexRateLimitRequest{ThreadID: "append-thread", RunID: "run", StartedAt: start.Add(time.Minute), EndedAt: start.Add(time.Minute + 2*time.Second)})
	entry.mu.Lock()
	secondOffset, secondCount := entry.offset, len(entry.snapshots)
	entry.mu.Unlock()
	if second.StartProvenance != "same-thread" || *second.Deltas[0].PercentagePoints != 3 || firstCount != 1 || secondCount != 2 || secondOffset <= firstOffset {
		t.Fatalf("append cache failed: first=%d/%d second=%d/%d evidence=%+v", firstOffset, firstCount, secondOffset, secondCount, second)
	}
}

func TestCodexRateLimitReaderRetriesPartialRecord(t *testing.T) {
	home := t.TempDir()
	start := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	writeRateLimitTestLog(t, home, "partial-thread", "account-1", []string{
		rateLimitTestEvent(start.Add(-time.Second), 40, 12345),
	})
	path := filepath.Join(home, "sessions", "2026", "09", "29", "rollout-2026-09-29T02-00-00-partial-thread.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	completeOffset := info.Size()
	event := rateLimitTestEvent(start.Add(time.Second), 43, 12345)
	split := len(event) - 10
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(event[:split])
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	reader := CodexRateLimitReader{Home: home}
	req := model.CodexRateLimitRequest{ThreadID: "partial-thread", RunID: "run", StartedAt: start, EndedAt: start.Add(2 * time.Second)}
	first := reader.Read(req)
	entry := cachedCodexLogFor(path)
	entry.mu.Lock()
	offset, bad := entry.offset, entry.unparseable
	entry.mu.Unlock()
	if first.Reason != "no-snapshots" || offset != completeOffset || bad {
		t.Fatalf("partial record consumed: evidence=%+v offset=%d want=%d bad=%v", first, offset, completeOffset, bad)
	}

	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(event[split:] + "\n")
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	second := reader.Read(req)
	if second.Status != "captured" || second.Deltas[0].PercentagePoints == nil || *second.Deltas[0].PercentagePoints != 3 {
		t.Fatalf("completed record missed: %+v", second)
	}
}

func TestCodexRateLimitFixtureIsReadOnly(t *testing.T) {
	original, err := os.ReadFile("testdata/ratelimits/resumed.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	dir := filepath.Join(home, "sessions", "2026", "09", "29")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-09-29T14-00-00-thread-1.jsonl")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	got := (CodexRateLimitReader{Home: home}).Read(model.CodexRateLimitRequest{ThreadID: "thread-1", RunID: "run", StartedAt: start, EndedAt: start.Add(3 * time.Second)})
	if got.Status != "captured" || got.Start == nil || got.End == nil || *got.Start.Primary.UsedPercent != 40 || *got.End.Primary.UsedPercent != 43 || *got.Deltas[0].PercentagePoints != 3 {
		t.Fatalf("fixture evidence: %+v", got)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("fixture modified: %v", err)
	}
}

func TestCodexRateLimitPrivateHomeUsesSharedSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", home)
	writeRateLimitTestLog(t, home, "thread-1", "account-1", []string{
		rateLimitTestEvent(time.Date(2026, 9, 29, 14, 0, 1, 0, time.UTC), 43, 12345),
	})
	private, err := prepareCodexCompletionHome(CompletionCommand{Executable: "/bin/true", Args: []string{"step", "complete"}}, "run")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(private, "sessions"))
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("private sessions not linked: %v", err)
	}
	start := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	for _, dir := range []string{home, private} {
		got := (CodexRateLimitReader{Home: dir}).Read(model.CodexRateLimitRequest{ThreadID: "thread-1", RunID: "run", StartedAt: start, EndedAt: start.Add(2 * time.Second)})
		if got.Status != "captured" || got.End == nil || *got.End.Primary.UsedPercent != 43 {
			t.Fatalf("read from %s: %+v", dir, got)
		}
	}
}

func TestCodexRateLimitCrossThreadFindsPreviousLocalDay(t *testing.T) {
	home := t.TempDir()
	start := time.Date(2026, 9, 29, 0, 2, 0, 0, time.Local)
	candidateAt := start.Add(-3 * time.Minute)
	write := func(id string, at time.Time, used float64) string {
		t.Helper()
		dir := filepath.Join(home, "sessions", at.In(time.Local).Format("2006/01/02"))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "rollout-test-"+id+".jsonl")
		body := `{"type":"session_meta","payload":{"creator_account_id":"account-1"}}` + "\n" + rateLimitTestEvent(at, used, 12345) + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	candidatePath := write("older", candidateAt, 40)
	write("fresh", start.Add(time.Second), 43)
	if err := os.Chtimes(candidatePath, candidateAt, candidateAt); err != nil {
		t.Fatal(err)
	}
	got := (CodexRateLimitReader{Home: home}).Read(model.CodexRateLimitRequest{ThreadID: "fresh", RunID: "run", StartedAt: start, EndedAt: start.Add(2 * time.Second)})
	if got.StartProvenance != "cross-thread" || *got.Deltas[0].PercentagePoints != 3 || *got.BaselineGapMS != 180000 {
		t.Fatalf("midnight baseline: %+v", got)
	}
}

func TestCodexRateLimitReaderNestedLifecycleTolerance(t *testing.T) {
	home := t.TempDir()
	start := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	writeRateLimitTestLog(t, home, "thread-1", "account-1", []string{
		rateLimitTestEvent(start.Add(-time.Second), 40, 12345),
		rateLimitTestEvent(start, 41, 12345),
		rateLimitTestEvent(end.Add(time.Second), 42, 12345),
		rateLimitTestEvent(end.Add(3*time.Second), 43, 12345),
	})
	got := (CodexRateLimitReader{Home: home}).Read(model.CodexRateLimitRequest{ThreadID: "thread-1", RunID: "run", StartedAt: start, EndedAt: end, EndTolerance: 2 * time.Second})
	if got.End == nil || *got.End.Primary.UsedPercent != 42 || got.Start == nil || *got.Start.Primary.UsedPercent != 40 {
		t.Fatalf("lifecycle bounds: %+v", got)
	}
}

func TestComputeCodexRateLimitDeltas(t *testing.T) {
	limit := "codex"
	minutes, reset, resetNext := int64(10080), int64(12345), int64(23456)
	before, after, lower := 52.5, 53.5, 51.5
	start := &model.RateLimitSnapshot{LimitID: &limit, Primary: model.RateLimitWindow{Reported: true, UsedPercent: &before, WindowMinutes: &minutes, ResetsAt: &reset}}
	end := &model.RateLimitSnapshot{LimitID: &limit, Primary: model.RateLimitWindow{Reported: true, UsedPercent: &after, WindowMinutes: &minutes, ResetsAt: &reset}}
	check := func(wantAvailability, wantReason string, wantValue *float64) {
		t.Helper()
		d := computeCodexRateLimitDeltas(start, end, "same-thread")[0]
		if d.Availability != wantAvailability || d.Reason != wantReason || (wantValue == nil) != (d.PercentagePoints == nil) || wantValue != nil && *d.PercentagePoints != *wantValue {
			t.Fatalf("delta = %+v, want %s/%s/%v", d, wantAvailability, wantReason, wantValue)
		}
	}
	one := 1.0
	check("available", "", &one)
	end.Primary.UsedPercent = &before
	zero := 0.0
	check("available", "", &zero)
	end.Primary.UsedPercent = &lower
	check("unavailable", "inconsistent", nil)
	end.Primary.UsedPercent = &after
	end.Primary.ResetsAt = &resetNext
	check("unavailable", "window-reset", nil)
	end.Primary.ResetsAt = &reset
	end.Primary.Reported = false
	check("unavailable", "window-not-reported", nil)
}

func TestCodexRateLimitReaderCrossThreadAndFailures(t *testing.T) {
	start := time.Date(2026, 9, 29, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name, candidateAccount     string
		candidateAge               time.Duration
		candidateReset             int64
		wantProvenance, wantReason string
	}{
		{"matching", "account-1", time.Minute, 12345, "cross-thread", ""},
		{"other-account", "account-2", time.Minute, 12345, "", "no-baseline"},
		{"stale", "account-1", 11 * time.Minute, 12345, "", "no-baseline"},
		{"other-window", "account-1", time.Minute, 23456, "cross-thread", "no-baseline"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			writeRateLimitTestLog(t, home, "fresh", "account-1", []string{rateLimitTestEvent(start.Add(time.Second), 43, 12345)})
			writeRateLimitTestLog(t, home, "candidate", tc.candidateAccount, []string{rateLimitTestEvent(start.Add(-tc.candidateAge), 40, tc.candidateReset)})
			candidatePath := filepath.Join(home, "sessions", "2026", "09", "29", "rollout-2026-09-29T02-00-00-candidate.jsonl")
			mtime := start.Add(-tc.candidateAge)
			if err := os.Chtimes(candidatePath, mtime, mtime); err != nil {
				t.Fatal(err)
			}
			got := (CodexRateLimitReader{Home: home}).Read(model.CodexRateLimitRequest{ThreadID: "fresh", RunID: "run", StartedAt: start, EndedAt: start.Add(2 * time.Second)})
			if got.StartProvenance != tc.wantProvenance || got.Deltas[0].Reason != tc.wantReason {
				t.Fatalf("unexpected evidence: %+v", got)
			}
			if tc.name == "matching" && (got.BaselineGapMS == nil || *got.BaselineGapMS != int64(time.Minute/time.Millisecond) || *got.Deltas[0].PercentagePoints != 3) {
				t.Fatalf("cross-thread delta: %+v", got)
			}
			if tc.name == "matching" && !slices.Equal(got.Deltas[0].Limitations, []string{"account-wide", "coarse-precision", "unobserved-gap"}) {
				t.Fatalf("cross-thread limitations = %v", got.Deltas[0].Limitations)
			}
		})
	}
	home := t.TempDir()
	reader := CodexRateLimitReader{Home: home}
	if got := reader.Read(model.CodexRateLimitRequest{StartedAt: start, EndedAt: start.Add(time.Second)}); got.Reason != "session-unidentified" {
		t.Fatalf("unidentified session: %+v", got)
	}
	if got := reader.Read(model.CodexRateLimitRequest{ThreadID: "missing", StartedAt: start, EndedAt: start.Add(time.Second)}); got.Reason != "session-log-unavailable" {
		t.Fatalf("missing log: %+v", got)
	}
	writeRateLimitTestLog(t, home, "broken", "account-1", []string{`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":"broken"}}}`})
	if got := reader.Read(model.CodexRateLimitRequest{ThreadID: "broken", StartedAt: start, EndedAt: start.Add(time.Second)}); got.Reason != "unparseable" {
		t.Fatalf("malformed log: %+v", got)
	}
	writeRateLimitTestLog(t, home, "tokens-only", "account-1", []string{`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":500}}}}`})
	if got := reader.Read(model.CodexRateLimitRequest{ThreadID: "tokens-only", StartedAt: start, EndedAt: start.Add(time.Second)}); got.Reason != "no-snapshots" || got.End != nil {
		t.Fatalf("tokens converted into rate-limit state: %+v", got)
	}
}

func writeRateLimitTestLog(t *testing.T, home, id, account string, events []string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "2026", "09", "29")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"session_meta","payload":{"id":"` + id + `","creator_account_id":"` + account + `"}}` + "\n"
	for _, event := range events {
		data += event + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "rollout-2026-09-29T02-00-00-"+id+".jsonl"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func rateLimitTestEvent(at time.Time, used float64, reset int64) string {
	return `{"timestamp":"` + at.Format(time.RFC3339Nano) + `","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","plan_type":"pro","primary":{"used_percent":` + strconv.FormatFloat(used, 'f', -1, 64) + `,"window_minutes":10080,"resets_at":` + strconv.FormatInt(reset, 10) + `},"secondary":null}}}`
}

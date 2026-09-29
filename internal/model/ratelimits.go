package model

import "time"

// CodexRateLimitEvidence contains only Codex-reported quota state. AccountScope
// is a run-local digest; the underlying account identifier is never persisted.
type CodexRateLimitEvidence struct {
	Status           string             `json:"status"`
	Reason           string             `json:"reason,omitempty"`
	Source           string             `json:"source"`
	AttemptStartedAt string             `json:"attempt_started_at"`
	AttemptEndedAt   string             `json:"attempt_ended_at"`
	Start            *RateLimitSnapshot `json:"start"`
	StartProvenance  string             `json:"start_provenance,omitempty"`
	BaselineGapMS    *int64             `json:"baseline_gap_ms,omitempty"`
	AccountScope     string             `json:"account_scope,omitempty"`
	End              *RateLimitSnapshot `json:"end"`
	Deltas           []RateLimitDelta   `json:"deltas"`
}

type RateLimitSnapshot struct {
	ObservedAt string          `json:"observed_at"`
	LimitID    *string         `json:"limit_id"`
	PlanType   *string         `json:"plan_type"`
	Primary    RateLimitWindow `json:"primary"`
	Secondary  RateLimitWindow `json:"secondary"`
}

type RateLimitWindow struct {
	Reported      bool     `json:"reported"`
	UsedPercent   *float64 `json:"used_percent"`
	WindowMinutes *int64   `json:"window_minutes"`
	ResetsAt      *int64   `json:"resets_at"`
}

type RateLimitDelta struct {
	Window           string   `json:"window"`
	Availability     string   `json:"availability"`
	PercentagePoints *float64 `json:"percentage_points"`
	Reason           string   `json:"reason,omitempty"`
	Precision        string   `json:"precision,omitempty"`
	Limitations      []string `json:"limitations,omitempty"`
}

// Rate-limit evidence vocabulary shared by the Codex reader and run metrics.
const (
	RateLimitSourceCodexSessionLog = "codex:session-log"
	RateLimitAccountUnverified     = "unverified"

	RateLimitStatusCaptured    = "captured"
	RateLimitStatusUnavailable = "unavailable"

	RateLimitAvailable            = "available"
	RateLimitUnavailable          = "unavailable"
	RateLimitPrecisionApproximate = "approximate"

	RateLimitWindowPrimary   = "primary"
	RateLimitWindowSecondary = "secondary"

	RateLimitProvenanceSameThread  = "same-thread"
	RateLimitProvenanceCrossThread = "cross-thread"

	RateLimitReasonSessionUnidentified = "session-unidentified"
	RateLimitReasonLogUnavailable      = "session-log-unavailable"
	RateLimitReasonNoSnapshots         = "no-snapshots"
	RateLimitReasonUnparseable         = "unparseable"
	RateLimitReasonNoBaseline          = "no-baseline"
	RateLimitReasonWindowReset         = "window-reset"
	RateLimitReasonWindowNotReported   = "window-not-reported"
	RateLimitReasonInconsistent        = "inconsistent"
	RateLimitReasonStaleEnrichment     = "stale-enrichment"
	RateLimitReasonExcludedMeasurement = "excluded-measurement"
	RateLimitReasonAccountUnverified   = "account-unverified"
	RateLimitReasonOverlappingAttempts = "overlapping-attempts"
	RateLimitLimitationAccountWide     = "account-wide"
	RateLimitLimitationCoarsePrecision = "coarse-precision"
	RateLimitLimitationUnobservedGap   = "unobserved-gap"
	RateLimitLimitationOverlapping     = "overlapping-attempts"
)

// RateLimitWindowRoles lists every window role in artifact order.
var RateLimitWindowRoles = [...]string{RateLimitWindowPrimary, RateLimitWindowSecondary}

// CodexRateLimitRequest identifies one attempt's Codex thread and time bounds.
type CodexRateLimitRequest struct {
	ThreadID, RunID    string
	StartedAt, EndedAt time.Time
	EndTolerance       time.Duration
}

// CodexRateLimitReadFunc reads rate-limit evidence for one attempt.
type CodexRateLimitReadFunc func(CodexRateLimitRequest) CodexRateLimitEvidence

// Window returns the snapshot's window for role.
func (s *RateLimitSnapshot) Window(role string) RateLimitWindow {
	if role == RateLimitWindowSecondary {
		return s.Secondary
	}
	return s.Primary
}

// SameWindow reports whether both snapshots describe the same quota window for role.
func (s *RateLimitSnapshot) SameWindow(other *RateLimitSnapshot, role string) bool {
	a, b := s.Window(role), other.Window(role)
	return equalPtr(s.LimitID, other.LimitID) && equalPtr(a.WindowMinutes, b.WindowMinutes) && equalPtr(a.ResetsAt, b.ResetsAt)
}

// SameLimit reports whether both snapshots belong to the same limit and plan.
func (s *RateLimitSnapshot) SameLimit(other *RateLimitSnapshot) bool {
	return equalPtr(s.LimitID, other.LimitID) && equalPtr(s.PlanType, other.PlanType)
}

// ObservedTime parses the snapshot's observation timestamp.
func (s *RateLimitSnapshot) ObservedTime() (time.Time, bool) {
	at, err := time.Parse(time.RFC3339Nano, s.ObservedAt)
	return at, err == nil
}

// Complete reports whether Codex reported every value needed to compare the window.
func (w *RateLimitWindow) Complete() bool {
	return w.UsedPercent != nil && w.WindowMinutes != nil && w.ResetsAt != nil
}

// AvailableRateLimitDelta is an approximate change carrying the limitations every delta has.
func AvailableRateLimitDelta(role string, points float64) RateLimitDelta {
	return RateLimitDelta{
		Window: role, Availability: RateLimitAvailable, PercentagePoints: &points, Precision: RateLimitPrecisionApproximate,
		Limitations: []string{RateLimitLimitationAccountWide, RateLimitLimitationCoarsePrecision},
	}
}

// UnavailableRateLimitDelta records why no change is reported for role.
func UnavailableRateLimitDelta(role, reason string) RateLimitDelta {
	return RateLimitDelta{Window: role, Availability: RateLimitUnavailable, Reason: reason}
}

// UnavailableRateLimitDeltas returns one unavailable delta per window role.
func UnavailableRateLimitDeltas(reason string) []RateLimitDelta {
	deltas := make([]RateLimitDelta, 0, len(RateLimitWindowRoles))
	for _, role := range RateLimitWindowRoles {
		deltas = append(deltas, UnavailableRateLimitDelta(role, reason))
	}
	return deltas
}

func equalPtr[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
